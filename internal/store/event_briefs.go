package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/abedegno/muesli/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// preGenerateJobsQueuedForBriefStatuses are the job statuses "queued" work for
// a brief covers when cleaning it up: pending jobs are removed outright,
// running jobs are left alone (they finish, but guarded publication makes
// them harmless -- see EventBrief's doc comment and the accepted spec).
const preGenerateJobsQueuedStatus = model.JobPending

func decodeBriefSections(raw []byte) ([]model.SummarySection, error) {
	var out []model.SummarySection
	if len(raw) == 0 {
		return []model.SummarySection{}, nil
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = []model.SummarySection{}
	}
	return out, nil
}

func scanEventBrief(row pgx.Row) (model.EventBrief, error) {
	var b model.EventBrief
	var hash *string
	var sections []byte
	err := row.Scan(&b.ID, &b.EventID, &b.TemplateID, &b.TemplateName, &hash, &b.Generation,
		&b.Status, &b.AgentPlugin, &b.Model, &sections, &b.CreatedAt, &b.UpdatedAt)
	if err != nil {
		return model.EventBrief{}, err
	}
	b.InputHash = hash
	b.Sections, err = decodeBriefSections(sections)
	if err != nil {
		return model.EventBrief{}, err
	}
	return b, nil
}

const eventBriefColumns = `id, event_id, template_id, template_name, input_hash, generation,
	        status, agent_plugin, model, sections::text, created_at, updated_at`

// EventBriefsForEvents returns every current brief row for the given event
// ids, owner-scoped through the calendar_events join (the same owner
// predicate every calendar read uses), grouped by event id. Events with no
// briefs are simply absent from the map -- callers must default to an empty
// slice for the API's "always an array" guarantee (see internal/api/calendar.go).
func (s *Store) EventBriefsForEvents(ctx context.Context, ownerID string, eventIDs []string) (map[string][]model.EventBrief, error) {
	out := map[string][]model.EventBrief{}
	if len(eventIDs) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx,
		`SELECT b.`+eventBriefColumns+`
		 FROM event_briefs b
		 JOIN calendar_events e ON e.id = b.event_id
		 WHERE e.owner_id = $1 AND b.event_id = ANY($2::uuid[])
		 ORDER BY b.template_name`,
		ownerID, eventIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		b, err := scanEventBrief(rows)
		if err != nil {
			return nil, err
		}
		out[b.EventID] = append(out[b.EventID], b)
	}
	return out, rows.Err()
}

// UpcomingEventsForSourceBatch returns up to limit events for (ownerID,
// sourceID) that start strictly after now and no later than now+7d (the same
// eligibility window as Coming Up), in a fixed UUID-keyset page ordered by
// id, strictly after afterID (pass "" for the first page). Backs bounded
// reconciliation batches (see internal/worker/prebriefs.go) -- callers page
// until a short page signals the end.
func (s *Store) UpcomingEventsForSourceBatch(ctx context.Context, ownerID, sourceID string, now time.Time, afterID string, limit int) ([]model.CalendarEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	horizon := now.Add(7 * 24 * time.Hour)
	rows, err := s.pool.Query(ctx,
		`SELECT id, owner_id, source_id, external_id, title, starts_at, ends_at,
		        description, location, conferencing_url, attendees::text, updated_at
		 FROM calendar_events
		 WHERE owner_id = $1 AND source_id = $2
		   AND starts_at > $3 AND starts_at <= $4
		   AND id > $5
		 ORDER BY id
		 LIMIT $6`,
		ownerID, sourceID, now, horizon, afterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.CalendarEvent{}
	for rows.Next() {
		var ev model.CalendarEvent
		var attendeesRaw string
		if err := rows.Scan(&ev.ID, &ev.OwnerID, &ev.SourceID, &ev.ExternalID, &ev.Title, &ev.StartsAt, &ev.EndsAt,
			&ev.Description, &ev.Location, &ev.ConferencingURL, &attendeesRaw, &ev.UpdatedAt); err != nil {
			return nil, err
		}
		ev.Attendees, err = decodeCalendarAttendees(attendeesRaw)
		if err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

// DeleteIneligibleEventBriefs deletes, in one transaction, every existing
// brief row among eventIDs whose template is NOT in eligibleTemplateIDs
// (missing, no longer pre/auto-run, or no longer visible to the owner --
// eligibleTemplateIDs is expected to already reflect that), together with
// their still-queued (pending) pre_generate jobs. Running jobs are left
// intact; guarded publication makes them harmless once their brief is gone.
// An empty eligibleTemplateIDs deletes every existing brief for these events
// (nothing is eligible). Returns the number of briefs deleted.
func (s *Store) DeleteIneligibleEventBriefs(ctx context.Context, eventIDs []string, eligibleTemplateIDs []string) (int, error) {
	if len(eventIDs) == 0 {
		return 0, nil
	}
	if eligibleTemplateIDs == nil {
		eligibleTemplateIDs = []string{}
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	ids, err := deleteBriefsReturningIDs(ctx, tx,
		`DELETE FROM event_briefs
		 WHERE event_id = ANY($1::uuid[]) AND NOT (template_id = ANY($2::uuid[]))
		 RETURNING id`,
		eventIDs, eligibleTemplateIDs)
	if err != nil {
		return 0, err
	}
	if err := deleteQueuedPreJobsForBriefs(ctx, tx, ids); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return len(ids), nil
}

// DeleteEventBriefsForTemplate deletes, in one transaction, every current
// brief row for templateID owned (via its event) by one of ownerIDs, together
// with their still-queued pre_generate jobs. Used by template-mutation
// cleanup (internal/store/templates.go) the moment a template stops being
// pre+auto-run+visible for those owners -- see the accepted spec's
// "ineligibility cleanup" rules. Running jobs are left intact.
func (s *Store) DeleteEventBriefsForTemplate(ctx context.Context, tx pgx.Tx, templateID string, ownerIDs []string) (int, error) {
	if len(ownerIDs) == 0 {
		return 0, nil
	}
	ids, err := deleteBriefsReturningIDs(ctx, tx,
		`DELETE FROM event_briefs b
		 USING calendar_events e
		 WHERE e.id = b.event_id AND b.template_id = $1 AND e.owner_id = ANY($2::uuid[])
		 RETURNING b.id`,
		templateID, ownerIDs)
	if err != nil {
		return 0, err
	}
	if err := deleteQueuedPreJobsForBriefs(ctx, tx, ids); err != nil {
		return 0, err
	}
	return len(ids), nil
}

func deleteBriefsReturningIDs(ctx context.Context, tx pgx.Tx, sql string, args ...any) ([]string, error) {
	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func deleteQueuedPreJobsForBriefs(ctx context.Context, tx pgx.Tx, briefIDs []string) error {
	if len(briefIDs) == 0 {
		return nil
	}
	_, err := tx.Exec(ctx,
		`DELETE FROM jobs WHERE type=$1 AND status=$2 AND brief_id = ANY($3::uuid[])`,
		model.JobPreGenerate, preGenerateJobsQueuedStatus, briefIDs)
	return err
}

// ReconcileEventBriefPair transactionally compares hash against the current
// event_briefs row for (eventID, templateID). hash nil means "no default
// agent is configured" (see the accepted spec's missing-default-agent
// ruling): the row (if it changes) becomes status=failed with a null hash and
// NO job is enqueued. A non-nil hash that differs from the stored one (or no
// row yet) advances the generation, sets status=pending, refreshes the
// snapshotted template name, clears prior sections/agent_plugin/model, and
// enqueues exactly one pre_generate job in the same transaction. An unchanged
// hash (including nil==nil) is a no-op. A unique-constraint race against a
// concurrent reconciler inserting the same brand-new pair is retried, so the
// loser reads and compares against the winner's row instead of erroring.
func (s *Store) ReconcileEventBriefPair(ctx context.Context, eventID, templateID, templateName string, hash *string) (enqueued bool, err error) {
	for attempt := 0; attempt < 3; attempt++ {
		enqueued, err = s.reconcileEventBriefPairOnce(ctx, eventID, templateID, templateName, hash)
		if err == nil || !isUniqueViolation(err) {
			return enqueued, err
		}
	}
	return false, err
}

func (s *Store) reconcileEventBriefPairOnce(ctx context.Context, eventID, templateID, templateName string, hash *string) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)

	var briefID, storedHash *string
	var generation int
	err = tx.QueryRow(ctx,
		`SELECT id, input_hash, generation FROM event_briefs
		 WHERE event_id=$1 AND template_id=$2 FOR UPDATE`,
		eventID, templateID).Scan(&briefID, &storedHash, &generation)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		if hash == nil {
			// No row yet and no agent: create the failed placeholder directly at
			// generation 1 rather than a pending row nothing will ever complete.
			if err := insertEventBrief(ctx, tx, eventID, templateID, templateName, nil, model.BriefFailed, 1); err != nil {
				return false, err
			}
			return false, tx.Commit(ctx)
		}
		newID := uuid.NewString()
		if err := insertEventBriefWithID(ctx, tx, newID, eventID, templateID, templateName, hash, model.BriefPending, 1); err != nil {
			return false, err
		}
		if _, err := enqueuePreGenerateJobTx(ctx, tx, eventID, newID, templateID, 1); err != nil {
			return false, err
		}
		return true, tx.Commit(ctx)
	case err != nil:
		return false, err
	}

	if stringPtrEqual(storedHash, hash) {
		return false, tx.Commit(ctx)
	}

	newGeneration := generation + 1
	status := model.BriefPending
	if hash == nil {
		status = model.BriefFailed
	}
	if _, err := tx.Exec(ctx,
		`UPDATE event_briefs
		 SET input_hash=$1, generation=$2, status=$3, template_name=$4,
		     agent_plugin='', model='', sections='[]'::jsonb, updated_at=now()
		 WHERE id=$5`,
		hash, newGeneration, status, templateName, *briefID); err != nil {
		return false, err
	}
	if hash == nil {
		return false, tx.Commit(ctx)
	}
	if _, err := enqueuePreGenerateJobTx(ctx, tx, eventID, *briefID, templateID, newGeneration); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

func stringPtrEqual(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func insertEventBrief(ctx context.Context, tx pgx.Tx, eventID, templateID, templateName string, hash *string, status string, generation int) error {
	return insertEventBriefWithID(ctx, tx, uuid.NewString(), eventID, templateID, templateName, hash, status, generation)
}

func insertEventBriefWithID(ctx context.Context, tx pgx.Tx, id, eventID, templateID, templateName string, hash *string, status string, generation int) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO event_briefs (id, event_id, template_id, template_name, input_hash, generation, status)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		id, eventID, templateID, templateName, hash, generation, status)
	return err
}

func enqueuePreGenerateJobTx(ctx context.Context, tx pgx.Tx, eventID, briefID, templateID string, generation int) (string, error) {
	payload, err := json.Marshal(map[string]any{
		"brief_id":    briefID,
		"template_id": templateID,
		"generation":  generation,
	})
	if err != nil {
		return "", err
	}
	id := uuid.NewString()
	_, err = tx.Exec(ctx,
		`INSERT INTO jobs (id, calendar_event_id, brief_id, brief_generation, type, status, payload)
		 VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb)`,
		id, eventID, briefID, generation, model.JobPreGenerate, model.JobPending, string(payload))
	return id, err
}

// GetEventBriefByID returns one brief row by id, unscoped by owner (ownership
// is derived through its event_id -- callers that need an owner check load
// the event separately and compare). found is false (with a zero-value
// EventBrief and no error) when no row matches, distinguishing "already
// removed" from a real error for the worker's ineligibility-cleanup paths.
func (s *Store) GetEventBriefByID(ctx context.Context, briefID string) (model.EventBrief, bool, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+eventBriefColumns+` FROM event_briefs WHERE id=$1`, briefID)
	b, err := scanEventBrief(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.EventBrief{}, false, nil
	}
	if err != nil {
		return model.EventBrief{}, false, err
	}
	return b, true, nil
}

// PublishEventBrief guardedly marks a brief ready with its generated
// sections: WHERE id=briefID AND generation=generation, so a stale worker
// attempt (superseded by a newer reconciliation, or whose brief no longer
// exists) can never overwrite newer state. Returns published=false (no
// error) when zero rows matched -- the caller's output is discarded, not an
// error condition.
func (s *Store) PublishEventBrief(ctx context.Context, briefID string, generation int, agentPlugin, modelName string, sections []model.SummarySection) (bool, error) {
	secJSON, err := json.Marshal(sections)
	if err != nil {
		return false, err
	}
	ct, err := s.pool.Exec(ctx,
		`UPDATE event_briefs
		 SET status=$1, agent_plugin=$2, model=$3, sections=$4::jsonb, updated_at=now()
		 WHERE id=$5 AND generation=$6`,
		model.BriefReady, agentPlugin, modelName, string(secJSON), briefID, generation)
	if err != nil {
		return false, err
	}
	return ct.RowsAffected() > 0, nil
}

// FailEventBriefIfCurrent guardedly marks the matching current generation
// failed, e.g. after plugin retries are exhausted. Idempotent: calling it
// again for the same (already failed) generation is a harmless no-op update,
// which is what keeps a settled job from ever leaving a brief permanently
// pending. A generation mismatch (superseded or removed) affects zero rows
// and is not an error.
func (s *Store) FailEventBriefIfCurrent(ctx context.Context, briefID string, generation int) (bool, error) {
	ct, err := s.pool.Exec(ctx,
		`UPDATE event_briefs SET status=$1, updated_at=now() WHERE id=$2 AND generation=$3`,
		model.BriefFailed, briefID, generation)
	if err != nil {
		return false, err
	}
	return ct.RowsAffected() > 0, nil
}

// CleanupIneligibleEventBriefIfCurrent transactionally deletes the brief row
// (and its queued pre jobs) if, and only if, its generation still matches --
// used when the worker discovers mid-flight that the pair it was about to
// generate for has become ineligible (event started, template no longer
// pre/auto-run/visible). A generation mismatch means a newer reconciliation
// already moved past this attempt, so this is a no-op: the newer writer owns
// the row now. Returns deleted=true only when this call performed the
// deletion.
func (s *Store) CleanupIneligibleEventBriefIfCurrent(ctx context.Context, briefID string, generation int) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)

	ct, err := tx.Exec(ctx, `DELETE FROM event_briefs WHERE id=$1 AND generation=$2`, briefID, generation)
	if err != nil {
		return false, err
	}
	if ct.RowsAffected() == 0 {
		return false, tx.Commit(ctx)
	}
	if err := deleteQueuedPreJobsForBriefs(ctx, tx, []string{briefID}); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

// RetryPreBriefJob re-enqueues a fresh pre_generate job for jobID's own
// (brief, generation), starting from the job id alone: one transaction loads
// the job, joins its calendar event to derive the authoritative owner_id,
// and uses that owner for every subsequent event/brief/template-visibility
// check, so a global admin action can never leak a cross-owner join. Returns
// the new job's id on success. Returns ErrNotFound when the job (or its
// event/brief/template) no longer exists or the job does not target a
// calendar event, and ErrIneligible when the pair still exists but the
// event has started, the template is no longer pre/auto-run/visible, or the
// job's generation is no longer the brief's current one.
func (s *Store) RetryPreBriefJob(ctx context.Context, jobID string) (string, error) {
	job, err := s.GetJob(ctx, jobID)
	if err != nil {
		return "", err
	}
	kind, err := job.TargetKind()
	if err != nil || kind != model.JobTargetCalendarEvent {
		return "", ErrNotFound
	}
	var pl struct {
		BriefID    string `json:"brief_id"`
		TemplateID string `json:"template_id"`
		Generation int    `json:"generation"`
	}
	if err := json.Unmarshal(job.Payload, &pl); err != nil || pl.BriefID == "" || pl.TemplateID == "" || pl.Generation <= 0 {
		return "", ErrNotFound
	}

	event, err := s.GetCalendarEventByID(ctx, job.CalendarEventID)
	if errors.Is(err, ErrNotFound) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	ownerID := event.OwnerID

	if !event.StartsAt.After(time.Now()) {
		return "", ErrIneligible
	}

	tmpl, err := s.GetTemplate(ctx, ownerID, pl.TemplateID)
	if errors.Is(err, ErrNotFound) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if tmpl.Phase != "pre" || !tmpl.AutoRun {
		return "", ErrIneligible
	}

	brief, found, err := s.GetEventBriefByID(ctx, pl.BriefID)
	if err != nil {
		return "", err
	}
	if !found {
		return "", ErrNotFound
	}
	if brief.Generation != pl.Generation {
		return "", ErrIneligible
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx,
		`UPDATE event_briefs SET status=$1, updated_at=now() WHERE id=$2 AND generation=$3`,
		model.BriefPending, pl.BriefID, pl.Generation); err != nil {
		return "", err
	}
	newJobID, err := enqueuePreGenerateJobTx(ctx, tx, job.CalendarEventID, pl.BriefID, pl.TemplateID, pl.Generation)
	if err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return newJobID, nil
}
