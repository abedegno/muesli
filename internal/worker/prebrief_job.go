package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/plugin"
	"github.com/abedegno/muesli/internal/store"
)

// preJobClock is injected so worker tests can control "has this event
// started yet" deterministically. Production leaves it as time.Now.
var preJobClock = time.Now

// prebriefPayload is the entire pre_generate job payload: brief_id,
// template_id, and generation -- never calendar credentials or copied event
// content (those are re-loaded from the authoritative event row at execution
// time; see the accepted spec's "Input and freshness" section).
type prebriefPayload struct {
	BriefID    string `json:"brief_id"`
	TemplateID string `json:"template_id"`
	Generation int    `json:"generation"`
}

// resolveDefaultAgent resolves the owner-independent default agent plugin
// shared by both after-summary and pre-meeting-brief generation, mapping a
// missing plugin to the same ErrPluginNotConfigured sentinel runSummarize
// uses (see pipeline.go's Process, which logs it at Warn rather than Error).
func (p *Processor) resolveDefaultAgent(ctx context.Context) (model.Plugin, error) {
	plug, err := p.store.DefaultPlugin(ctx, p.crypto, model.PluginAgent)
	if errors.Is(err, store.ErrNotFound) {
		return model.Plugin{}, fmt.Errorf("no default agent plugin configured: %w", ErrPluginNotConfigured)
	}
	return plug, err
}

// applyTemplateOverrides copies a resolved template's optional per-template
// agent overrides onto a GenerateRequest, shared by after-summary and
// pre-meeting-brief execution. An empty/nil override leaves the request field
// at its zero value (the agent falls back to its own default).
func applyTemplateOverrides(req *plugin.GenerateRequest, tmpl model.Template) {
	if tmpl.SystemPrompt != "" {
		req.SystemPrompt = tmpl.SystemPrompt
	}
	if tmpl.Model != "" {
		req.Model = tmpl.Model
	}
	if tmpl.Temperature != nil {
		req.Temperature = tmpl.Temperature
	}
}

// buildCalendarEventSource normalizes a calendar event into the generation
// source's typed calendar_event payload (see plugin.GenerateSource). Only
// stored preparation fields -- never credentials, provider identifiers, or
// source ids.
func buildCalendarEventSource(ev model.CalendarEvent) *plugin.GenerateSource {
	attendees := make([]plugin.CalendarEventAttendee, len(ev.Attendees))
	for i, a := range ev.Attendees {
		attendees[i] = plugin.CalendarEventAttendee{Name: a.Name, Email: a.Email, Response: a.Response}
	}
	return &plugin.GenerateSource{
		Kind: plugin.GenerateSourceCalendarEvent,
		CalendarEvent: &plugin.CalendarEventSource{
			Title:           ev.Title,
			StartsAt:        ev.StartsAt.UTC().Format(time.RFC3339),
			EndsAt:          ev.EndsAt.UTC().Format(time.RFC3339),
			Description:     ev.Description,
			Location:        ev.Location,
			ConferencingURL: ev.ConferencingURL,
			Attendees:       attendees,
		},
	}
}

// runPreGenerate executes one pre_generate job, following the same
// (retryable, error) contract as runSummarize (see pipeline.go's Process):
// the caller settles the job row and, on terminal failure, marks the
// matching current generation failed (see handlePreGenerateTerminalFailure).
//
// Before ever calling the plugin it verifies, in order: the payload is
// well-formed and matches the job's own typed columns; the event still
// exists and has not started; the template still exists, is visible to the
// event's owner, and is still pre/auto-run; and the brief's generation still
// matches. Any of those failing makes the pair ineligible: the worker
// transactionally deletes the current brief (if its generation still
// matches) and its queued jobs, then completes WITHOUT invoking the plugin --
// this is not a job failure.
func (p *Processor) runPreGenerate(ctx context.Context, job model.Job) (bool, error) {
	kind, err := job.TargetKind()
	if err != nil || kind != model.JobTargetCalendarEvent {
		return false, errors.New("invalid pre_generate job: missing calendar event target")
	}

	var pl prebriefPayload
	if err := json.Unmarshal(job.Payload, &pl); err != nil {
		return false, fmt.Errorf("invalid pre_generate payload: %w", err)
	}
	if pl.BriefID == "" || pl.TemplateID == "" || pl.Generation <= 0 {
		return false, errors.New("invalid pre_generate payload: missing brief_id/template_id/generation")
	}
	// The typed job columns are the authoritative target (set only by the
	// server's own enqueue path); the payload must agree with them, not the
	// other way around, so a malformed/tampered payload can never disagree
	// with what this job was actually leased to do.
	if job.BriefID != pl.BriefID || job.BriefGeneration != pl.Generation {
		return false, errors.New("invalid pre_generate payload: does not match job target")
	}

	event, err := p.store.GetCalendarEventByID(ctx, job.CalendarEventID)
	if errors.Is(err, store.ErrNotFound) {
		// The event is gone -- its ON DELETE CASCADE already removed the brief
		// and this job would have been pruned with it in the ordinary case; if
		// we are somehow still running, there is nothing left to clean up or
		// generate. Complete as a no-op.
		slog.InfoContext(ctx, "pre_generate: event no longer exists, skipping", "job_id", job.ID, "event_id", job.CalendarEventID)
		return false, nil
	}
	if err != nil {
		return true, err
	}
	ownerID := event.OwnerID

	if !event.StartsAt.After(preJobClock()) {
		p.cleanupIneligiblePreBrief(ctx, job, pl.BriefID, pl.Generation, "event has started")
		return false, nil
	}

	tmpl, err := p.store.GetTemplate(ctx, ownerID, pl.TemplateID)
	if errors.Is(err, store.ErrNotFound) {
		p.cleanupIneligiblePreBrief(ctx, job, pl.BriefID, pl.Generation, "template no longer visible")
		return false, nil
	}
	if err != nil {
		return true, err
	}
	if tmpl.Phase != "pre" || !tmpl.AutoRun {
		p.cleanupIneligiblePreBrief(ctx, job, pl.BriefID, pl.Generation, "template no longer pre/auto-run")
		return false, nil
	}

	brief, found, err := p.store.GetEventBriefByID(ctx, pl.BriefID)
	if err != nil {
		return true, err
	}
	if !found {
		slog.InfoContext(ctx, "pre_generate: brief already removed, skipping", "job_id", job.ID, "brief_id", pl.BriefID)
		return false, nil
	}
	if brief.Generation != pl.Generation {
		slog.InfoContext(ctx, "pre_generate: stale generation superseded, skipping", "job_id", job.ID, "brief_id", pl.BriefID,
			"job_generation", pl.Generation, "current_generation", brief.Generation)
		return false, nil
	}

	plug, err := p.resolveDefaultAgent(ctx)
	if err != nil {
		return false, err
	}

	genReq := plugin.GenerateRequest{
		Transcript:    []model.Segment{},
		NotesMarkdown: "",
		Template:      plugin.TemplatePayload{Sections: tmpl.Sections},
		Config:        plug.Config,
		Source:        buildCalendarEventSource(event),
	}
	applyTemplateOverrides(&genReq, tmpl)

	client := plugin.New(plug.EndpointURL, plug.Token)
	resp, err := client.Generate(ctx, genReq)
	if err != nil {
		return isRetryable(err), err
	}

	published, err := p.store.PublishEventBrief(ctx, pl.BriefID, pl.Generation, plug.Name, resp.Model, resp.Summary.Sections)
	if err != nil {
		return true, err
	}
	if !published {
		slog.InfoContext(ctx, "pre_generate: publish discarded, generation superseded or brief removed",
			"job_id", job.ID, "brief_id", pl.BriefID, "generation", pl.Generation)
	}
	return false, nil
}

// cleanupIneligiblePreBrief performs the worker-side half of ineligibility
// cleanup (the transactional deletion of the current brief and its queued
// jobs, generation-guarded) and logs why. It never fails the job -- the pair
// simply no longer applies, which is not a generation failure.
func (p *Processor) cleanupIneligiblePreBrief(ctx context.Context, job model.Job, briefID string, generation int, reason string) {
	deleted, err := p.store.CleanupIneligibleEventBriefIfCurrent(ctx, briefID, generation)
	if err != nil {
		slog.ErrorContext(ctx, "pre_generate: ineligibility cleanup failed", "error", err, "job_id", job.ID, "brief_id", briefID, "reason", reason)
		return
	}
	slog.InfoContext(ctx, "pre_generate: pair no longer eligible, cleaned up", "job_id", job.ID, "brief_id", briefID, "reason", reason, "deleted", deleted)
}

// handlePreGenerateTerminalFailure marks the pre_generate job's matching
// current generation failed once its attempts are exhausted (or the failure
// was non-retryable), mirroring handleTerminalFailure's summarize branch.
// Malformed payloads that never reach a real brief id are simply logged.
func (p *Processor) handlePreGenerateTerminalFailure(ctx context.Context, job model.Job) {
	var pl prebriefPayload
	if err := json.Unmarshal(job.Payload, &pl); err != nil || pl.BriefID == "" || pl.Generation <= 0 {
		slog.WarnContext(ctx, "terminal pre_generate: no brief to fail (malformed payload)", "job_id", job.ID)
		return
	}
	if _, err := p.store.FailEventBriefIfCurrent(ctx, pl.BriefID, pl.Generation); err != nil {
		slog.ErrorContext(ctx, "terminal pre_generate: mark brief failed", "error", err, "job_id", job.ID, "brief_id", pl.BriefID)
	}
}
