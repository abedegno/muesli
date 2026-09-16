package worker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"time"

	"github.com/abedegno/muesli/internal/crypto"
	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
)

// preBriefBatchSize bounds how many upcoming events ReconcilePreBriefs loads
// and processes per page, per the accepted spec's "owner/source-scoped
// fixed-size batches" ruling (UUID-keyset batches of 100).
const preBriefBatchSize = 100

// ReconcilePreBriefs reconciles one owner/source's eligible upcoming calendar
// events (starts_at in (now, now+7d]) against that owner's visible,
// pre-phase, auto-run templates: a new or changed event/template pair
// receives one durable generation job; an unchanged pair receives none; a
// stale (no-longer-eligible) existing brief is removed together with its
// queued pre jobs. now is injected so tests are deterministic. Called after a
// successful calendar sync (see calendarsync.go) with the same clock as the
// rest of that sync pass.
func ReconcilePreBriefs(ctx context.Context, st *store.Store, cr *crypto.Crypto, ownerID, sourceID string, now time.Time) error {
	eligibleTemplates, err := st.TemplatesForPhase(ctx, ownerID, "pre", true)
	if err != nil {
		return err
	}
	eligibleIDs := make([]string, len(eligibleTemplates))
	for i, tmpl := range eligibleTemplates {
		eligibleIDs[i] = tmpl.ID
	}

	// Resolve the owner's default agent ONCE per source, not per event: its
	// identity is part of every pair's hash, so a later default-agent change
	// (including "none -> configured") naturally advances every pair's hash on
	// the next reconciliation pass.
	agentID, hasAgent := resolveDefaultAgentIdentity(ctx, st, cr)

	afterID := ""
	for {
		events, err := st.UpcomingEventsForSourceBatch(ctx, ownerID, sourceID, now, afterID, preBriefBatchSize)
		if err != nil {
			return err
		}
		if len(events) == 0 {
			return nil
		}

		eventIDs := make([]string, len(events))
		for i, ev := range events {
			eventIDs[i] = ev.ID
		}
		// Cleanup considers EXISTING brief rows for this batch, independent of
		// the eligible-template query above -- a stale row whose template was
		// deleted entirely is still caught here even though it can never appear
		// in eligibleTemplates.
		if _, err := st.DeleteIneligibleEventBriefs(ctx, eventIDs, eligibleIDs); err != nil {
			return err
		}

		if len(eligibleTemplates) > 0 {
			for _, ev := range events {
				for _, tmpl := range eligibleTemplates {
					var hash *string
					if hasAgent {
						h := computePreBriefHash(ev, tmpl, agentID)
						hash = &h
					}
					if _, err := st.ReconcileEventBriefPair(ctx, ev.ID, tmpl.ID, tmpl.Name, hash); err != nil {
						return err
					}
				}
			}
		}

		if len(events) < preBriefBatchSize {
			return nil
		}
		afterID = events[len(events)-1].ID
	}
}

// resolveDefaultAgentIdentity resolves the owner's default agent plugin and
// returns a stable, non-secret identity string for it plus whether one
// exists. A lookup error (including "not configured") is treated the same as
// "no agent" -- see the accepted spec's missing-default-agent ruling: briefs
// go to failed with a null hash rather than blocking reconciliation.
func resolveDefaultAgentIdentity(ctx context.Context, st *store.Store, cr *crypto.Crypto) (string, bool) {
	plug, err := st.DefaultPlugin(ctx, cr, model.PluginAgent)
	if err != nil {
		return "", false
	}
	return plug.ID + "|" + plug.Name, true
}

// preBriefHashInput is exactly what ComputePreBriefHash hashes: normalized
// event fields (attendees stably ordered), the effective template
// definition, and the resolved default-agent identity. Deliberately excludes
// anything secret (plugin Config/Token are never included -- only the
// resolved agent's stable identity is).
type preBriefHashInput struct {
	Event    hashedCalendarEvent `json:"event"`
	Template hashedTemplate      `json:"template"`
	AgentID  string              `json:"agent_id"`
}

type hashedCalendarEvent struct {
	Title           string           `json:"title"`
	StartsAt        string           `json:"starts_at"`
	EndsAt          string           `json:"ends_at"`
	Description     string           `json:"description"`
	Location        string           `json:"location"`
	ConferencingURL string           `json:"conferencing_url"`
	Attendees       []hashedAttendee `json:"attendees"`
}

type hashedAttendee struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Response string `json:"response"`
}

type hashedTemplate struct {
	Name         string                  `json:"name"`
	Sections     []model.TemplateSection `json:"sections"`
	SystemPrompt string                  `json:"system_prompt"`
	Model        string                  `json:"model"`
	Temperature  *float64                `json:"temperature"`
}

// computePreBriefHash is the reconciliation freshness hash: a changed hash
// advances the brief's generation and enqueues a job; an equal hash is a
// no-op (see ReconcileEventBriefPair). Attendees are sorted by (email, name)
// so reordering the same attendee set on an upstream sync never changes the
// hash.
func computePreBriefHash(ev model.CalendarEvent, tmpl model.Template, agentID string) string {
	attendees := make([]hashedAttendee, len(ev.Attendees))
	for i, a := range ev.Attendees {
		attendees[i] = hashedAttendee{Name: a.Name, Email: a.Email, Response: a.Response}
	}
	sort.Slice(attendees, func(i, j int) bool {
		if attendees[i].Email != attendees[j].Email {
			return attendees[i].Email < attendees[j].Email
		}
		return attendees[i].Name < attendees[j].Name
	})

	input := preBriefHashInput{
		Event: hashedCalendarEvent{
			Title:           ev.Title,
			StartsAt:        ev.StartsAt.UTC().Format(time.RFC3339),
			EndsAt:          ev.EndsAt.UTC().Format(time.RFC3339),
			Description:     ev.Description,
			Location:        ev.Location,
			ConferencingURL: ev.ConferencingURL,
			Attendees:       attendees,
		},
		Template: hashedTemplate{
			Name:         tmpl.Name,
			Sections:     tmpl.Sections,
			SystemPrompt: tmpl.SystemPrompt,
			Model:        tmpl.Model,
			Temperature:  tmpl.Temperature,
		},
		AgentID: agentID,
	}
	// Marshal errors are impossible here (every field is a plain string/slice/
	// pointer-to-float64) -- and even if one occurred, an empty sum would still
	// be a stable, deterministic (if degenerate) hash rather than a panic.
	b, _ := json.Marshal(input)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
