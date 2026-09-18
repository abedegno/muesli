package store

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/abedegno/muesli/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// MaxLiveTemplates caps how many during-phase, auto-run templates may
// participate in live prompts for one owner (issue #764).
const MaxLiveTemplates = 8

// LiveScheduleCadence bounds how often a new live-generation run may start
// for the same (note,template,stream), measured between starts and enforced
// via the job's durable lease_expires_at ("not-before") column.
const LiveScheduleCadence = 15 * time.Second

// LiveStaleRecoveryAge is how long a live_template_outputs row (active or
// idle) may go without its last_demand_at changing before the recovery
// sweep removes it (see RecoverLiveTemplateOutputs).
const LiveStaleRecoveryAge = time.Hour

// visibleLiveEligibleTemplatesTx returns every owner-visible template (a
// built-in, owner_id IS NULL, or one owned by ownerID) whose phase is
// "during" and auto_run is true, in the repository's stable visible-template
// order: built-ins first, then case-insensitive name, then id as the final
// deterministic tie-breaker (see the accepted plan's ruling).
func visibleLiveEligibleTemplatesTx(ctx context.Context, q txQuerier, ownerID string) ([]model.Template, error) {
	rows, err := q.Query(ctx,
		`SELECT id, name, phase, sections, (owner_id IS NULL) AS built_in, auto_run,
		        system_prompt, model, temperature
		   FROM templates
		  WHERE (owner_id IS NULL OR owner_id=$1) AND phase=$2 AND auto_run=TRUE
		  ORDER BY (owner_id IS NULL) DESC, lower(name), id`,
		ownerID, templatePhaseDuring)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Template{}
	for rows.Next() {
		var tm model.Template
		var sectionsJSON []byte
		var systemPrompt, modelName *string
		var temperature *float64
		if err := rows.Scan(&tm.ID, &tm.Name, &tm.Phase, &sectionsJSON, &tm.BuiltIn, &tm.AutoRun,
			&systemPrompt, &modelName, &temperature); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(sectionsJSON, &tm.Sections); err != nil {
			return nil, err
		}
		applyTemplateOverrides(&tm, systemPrompt, modelName, temperature)
		out = append(out, tm)
	}
	return out, rows.Err()
}

// EligibleLiveTemplatesTx returns the owner's stable-order eligible during/
// auto-run templates, split at MaxLiveTemplates: eligible is the first eight
// (the only ones that ever run), omitted is every remaining one beyond the
// cap (logged by callers, never scheduled). Both are empty when the owner has
// none.
func EligibleLiveTemplatesTx(ctx context.Context, q txQuerier, ownerID string) (eligible, omitted []model.Template, err error) {
	all, err := visibleLiveEligibleTemplatesTx(ctx, q, ownerID)
	if err != nil {
		return nil, nil, err
	}
	if len(all) > MaxLiveTemplates {
		return all[:MaxLiveTemplates], all[MaxLiveTemplates:], nil
	}
	return all, nil, nil
}

// countOtherEligibleLiveTemplatesTx counts the owner's visible during/
// auto-run templates excluding excludeID (pass "" to count all of them),
// backing CreateTemplate/UpdateTemplate's cap validation.
func countOtherEligibleLiveTemplatesTx(ctx context.Context, q queryRower, ownerID, excludeID string) (int, error) {
	var n int
	var err error
	if excludeID == "" {
		err = q.QueryRow(ctx,
			`SELECT count(*) FROM templates WHERE (owner_id IS NULL OR owner_id=$1) AND phase=$2 AND auto_run=TRUE`,
			ownerID, templatePhaseDuring).Scan(&n)
	} else {
		err = q.QueryRow(ctx,
			`SELECT count(*) FROM templates WHERE (owner_id IS NULL OR owner_id=$1) AND phase=$2 AND auto_run=TRUE AND id<>$3::uuid`,
			ownerID, templatePhaseDuring, excludeID).Scan(&n)
	}
	return n, err
}

// validateLiveTemplateCap returns a ValidationError, matching the existing
// template validation-error shape, when enabling phase=during+auto_run=true
// would put the owner over MaxLiveTemplates. excludeID is the template being
// updated (empty for a create).
func validateLiveTemplateCap(ctx context.Context, q queryRower, ownerID, excludeID, phase string, autoRun bool) error {
	if phase != templatePhaseDuring || !autoRun {
		return nil
	}
	n, err := countOtherEligibleLiveTemplatesTx(ctx, q, ownerID, excludeID)
	if err != nil {
		return err
	}
	if n >= MaxLiveTemplates {
		return ValidationError("at most 8 live (during-phase, auto-run) templates may be enabled")
	}
	return nil
}

// activeLiveStream identifies one owner-owned note's current (unsealed,
// non-superseded, non-trashed) live transcript stream.
type activeLiveStream struct {
	NoteID       string
	TranscriptID string
	StreamID     string
}

// activeLiveStreamsForOwnerTx discovers affected owners' current active
// streams directly from the authoritative note/transcript relation -- never
// through live_template_outputs -- so an owner whose stream has finalized
// speech but no live output row yet (a newly eligible template, or one that
// has simply never run) is still found (see the accepted plan's ruling).
func activeLiveStreamsForOwnerTx(ctx context.Context, q txQuerier, ownerID string) ([]activeLiveStream, error) {
	rows, err := q.Query(ctx,
		`SELECT n.id, t.id, t.stream_id
		   FROM notes n JOIN transcripts t ON t.note_id = n.id
		  WHERE n.owner_id=$1 AND n.deleted_at IS NULL AND t.stream_id IS NOT NULL AND t.sealed=FALSE`,
		ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []activeLiveStream
	for rows.Next() {
		var s activeLiveStream
		if err := rows.Scan(&s.NoteID, &s.TranscriptID, &s.StreamID); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// currentStreamForNoteTx returns the note's current (unsealed) live stream,
// or ok=false if the note has no transcript or its transcript is
// batch-authored / sealed.
func currentStreamForNoteTx(ctx context.Context, q queryRower, noteID string) (transcriptID, streamID string, ok bool, err error) {
	var sid *string
	var sealed bool
	err = q.QueryRow(ctx, `SELECT id, stream_id, sealed FROM transcripts WHERE note_id=$1`, noteID).
		Scan(&transcriptID, &sid, &sealed)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, err
	}
	if sid == nil || sealed {
		return "", "", false, nil
	}
	return transcriptID, *sid, true, nil
}

// finalizedRevisionTx returns the current finalized-segment count for a
// transcript -- the store's transcript order, per the accepted plan's
// revision ruling. Only finalized (server-persisted) live segments are ever
// written to transcript_segments for a stream-owned transcript (interim
// hypotheses never reach the store), so this is simply the row count.
func finalizedRevisionTx(ctx context.Context, q queryRower, transcriptID string) (int, error) {
	var n int
	err := q.QueryRow(ctx, `SELECT count(*) FROM transcript_segments WHERE transcript_id=$1`, transcriptID).Scan(&n)
	return n, err
}

const liveOutputColumns = `id, note_id, template_id, stream_id, owner_id, desired_revision, rendered_revision,
	        event_version, status, client_visible, cancellation_requested_at, sections::text, agent_plugin, model,
	        error_code, last_started_at, last_demand_at, updated_at, ended_at, COALESCE(active_job_id::text,'')`

func scanLiveOutput(row pgx.Row) (model.LiveTemplateOutput, error) {
	var o model.LiveTemplateOutput
	var sectionsJSON []byte
	var agentPlugin, modelName, errorCode *string
	err := row.Scan(&o.ID, &o.NoteID, &o.TemplateID, &o.StreamID, &o.OwnerID, &o.DesiredRevision, &o.RenderedRevision,
		&o.EventVersion, &o.Status, &o.ClientVisible, &o.CancellationRequestedAt, &sectionsJSON, &agentPlugin, &modelName,
		&errorCode, &o.LastStartedAt, &o.LastDemandAt, &o.UpdatedAt, &o.EndedAt, &o.ActiveJobID)
	if err != nil {
		return model.LiveTemplateOutput{}, err
	}
	if agentPlugin != nil {
		o.AgentPlugin = *agentPlugin
	}
	if modelName != nil {
		o.Model = *modelName
	}
	if errorCode != nil {
		o.ErrorCode = *errorCode
	}
	if len(sectionsJSON) == 0 {
		o.Sections = []model.SummarySection{}
	} else if err := json.Unmarshal(sectionsJSON, &o.Sections); err != nil {
		return model.LiveTemplateOutput{}, err
	}
	if o.Sections == nil {
		o.Sections = []model.SummarySection{}
	}
	return o, nil
}

// lockLiveOutputsForStreamTx locks (FOR UPDATE, in a deterministic order)
// every current live_template_outputs row for one note/stream, for
// applyEligibilityTx's reconciliation.
func lockLiveOutputsForStreamTx(ctx context.Context, tx pgx.Tx, noteID, streamID string) ([]model.LiveTemplateOutput, error) {
	rows, err := tx.Query(ctx,
		`SELECT `+liveOutputColumns+` FROM live_template_outputs
		  WHERE note_id=$1 AND stream_id=$2
		  ORDER BY template_id
		  FOR UPDATE`,
		noteID, streamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.LiveTemplateOutput
	for rows.Next() {
		o, err := scanLiveOutput(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// scheduleLiveJobTx enqueues a new live_generate job for o at
// max(now, o.LastStartedAt+LiveScheduleCadence), setting the job's
// lease_expires_at to that not-before time so ClaimJob's existing lease
// mechanism enforces cadence without a separate scheduler. It records the
// new job as o's active job and mutates o in place.
func scheduleLiveJobTx(ctx context.Context, tx pgx.Tx, o *model.LiveTemplateOutput, now time.Time) error {
	notBefore := now
	if o.LastStartedAt != nil {
		if cand := o.LastStartedAt.Add(LiveScheduleCadence); cand.After(notBefore) {
			notBefore = cand
		}
	}
	jobID := uuid.NewString()
	if _, err := tx.Exec(ctx,
		`INSERT INTO jobs (id, note_id, template_id, stream_id, live_output_id, type, status, payload, lease_expires_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,'{}'::jsonb,$8)`,
		jobID, o.NoteID, o.TemplateID, o.StreamID, o.ID, model.JobLiveGenerate, model.JobPending, notBefore); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE live_template_outputs SET active_job_id=$2, status=$3, updated_at=$4 WHERE id=$1`,
		o.ID, jobID, model.LiveOutputPending, now); err != nil {
		return err
	}
	o.ActiveJobID = jobID
	o.Status = model.LiveOutputPending
	return nil
}

// applyEligibilityTx is the shared reconciliation core behind
// reconcileStreamDemandTx and reconcileOwnerEligibilityTx (issue #764). It
// locks (noteID,streamID)'s current live_template_outputs rows, recomputes
// the owner's stable first-eight eligible during/auto-run templates and the
// stream's current finalized revision, and applies the accepted spec's
// eligibility rules: an ineligible idle row is deleted; an ineligible queued
// row has its job cancelled and is deleted; an ineligible running row is
// hidden and cancellation-requested; an eligible template with at least one
// finalized segment gets its demand advanced (creating the row if new) and,
// if it has no active job, a freshly scheduled one. It returns whether any
// client-visible row changed, so callers can notify after commit.
func applyEligibilityTx(ctx context.Context, tx pgx.Tx, ownerID, noteID, transcriptID, streamID string, now time.Time) (changed bool, err error) {
	revision, err := finalizedRevisionTx(ctx, tx, transcriptID)
	if err != nil {
		return false, err
	}

	eligible, omitted, err := EligibleLiveTemplatesTx(ctx, tx, ownerID)
	if err != nil {
		return false, err
	}
	if len(omitted) > 0 {
		ids := make([]string, len(omitted))
		for i, t := range omitted {
			ids[i] = t.ID
		}
		slog.InfoContext(ctx, "live templates: owner over cap, omitting", "owner_id", ownerID, "omitted_template_ids", ids)
	}
	eligibleByID := make(map[string]model.Template, len(eligible))
	for _, t := range eligible {
		eligibleByID[t.ID] = t
	}

	existing, err := lockLiveOutputsForStreamTx(ctx, tx, noteID, streamID)
	if err != nil {
		return false, err
	}
	existingByID := make(map[string]model.LiveTemplateOutput, len(existing))
	for _, o := range existing {
		existingByID[o.TemplateID] = o
	}

	for _, o := range existing {
		if _, ok := eligibleByID[o.TemplateID]; ok {
			continue
		}
		switch {
		case o.ActiveJobID != "" && o.Status == model.LiveOutputRunning:
			if _, err := tx.Exec(ctx,
				`UPDATE live_template_outputs
				    SET client_visible=FALSE, cancellation_requested_at=$2, event_version=event_version+1, updated_at=$2
				  WHERE id=$1`,
				o.ID, now); err != nil {
				return false, err
			}
			changed = true
		case o.ActiveJobID != "":
			// Queued (pending) job: cancel it (a no-op if it already raced to
			// running/done) and remove the now-ineligible row.
			if _, err := tx.Exec(ctx,
				`UPDATE jobs SET status=$1, updated_at=now() WHERE id=$2 AND status='pending'`,
				model.JobCancelled, o.ActiveJobID); err != nil {
				return false, err
			}
			if _, err := tx.Exec(ctx, `DELETE FROM live_template_outputs WHERE id=$1`, o.ID); err != nil {
				return false, err
			}
			changed = true
		default:
			// Idle (no active job): delete outright.
			if _, err := tx.Exec(ctx, `DELETE FROM live_template_outputs WHERE id=$1`, o.ID); err != nil {
				return false, err
			}
			changed = true
		}
	}

	for _, t := range eligible {
		o, has := existingByID[t.ID]
		if !has {
			// A newly eligible template is not run until there is at least
			// one finalized segment.
			if revision <= 0 {
				continue
			}
			id := uuid.NewString()
			if _, err := tx.Exec(ctx,
				`INSERT INTO live_template_outputs
				   (id, note_id, template_id, stream_id, owner_id, desired_revision, last_demand_at, updated_at, event_version)
				 VALUES ($1,$2,$3,$4,$5,$6,$7,$7,1)`,
				id, noteID, t.ID, streamID, ownerID, revision, now); err != nil {
				return false, err
			}
			o = model.LiveTemplateOutput{
				ID: id, NoteID: noteID, TemplateID: t.ID, StreamID: streamID, OwnerID: ownerID,
				DesiredRevision: revision, ClientVisible: true, Status: model.LiveOutputPending,
			}
			changed = true
		} else if revision > o.DesiredRevision {
			if _, err := tx.Exec(ctx,
				`UPDATE live_template_outputs SET desired_revision=$2, last_demand_at=$3, updated_at=$3, event_version=event_version+1
				  WHERE id=$1`,
				o.ID, revision, now); err != nil {
				return false, err
			}
			o.DesiredRevision = revision
			changed = true
		}
		if o.ActiveJobID == "" && revision > 0 {
			if err := scheduleLiveJobTx(ctx, tx, &o, now); err != nil {
				return false, err
			}
			changed = true
		}
	}
	return changed, nil
}

// ReconcileStreamDemandTx is called from within the same transaction that
// just persisted a finalized live-transcript segment (see
// AppendStreamSegment), isolated behind a savepoint so a reconciliation
// failure never fails recording (see the accepted plan's savepoint
// ruling). It re-verifies the transcript is still the current, unsealed one
// this streamID owns before reconciling, since the caller may have released
// its own lock between the segment insert and this call.
func ReconcileStreamDemandTx(ctx context.Context, tx pgx.Tx, ownerID, noteID, transcriptID, streamID string, now time.Time) (changed bool, err error) {
	var sid *string
	var sealed bool
	err = tx.QueryRow(ctx, `SELECT stream_id, sealed FROM transcripts WHERE id=$1`, transcriptID).Scan(&sid, &sealed)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if sid == nil || *sid != streamID || sealed {
		return false, nil
	}
	return applyEligibilityTx(ctx, tx, ownerID, noteID, transcriptID, streamID, now)
}

// ReconcileOwnerEligibilityTx reconciles every one of ownerID's current
// active live streams against a just-changed eligibility input (template
// deletion, auto_run/phase change, or an ordering change that can alter the
// stable first eight) -- all in the mutation's own transaction, so a
// reconciliation failure rolls back the mutation itself (see the accepted
// spec's error-handling ruling). Owners are discovered from the
// authoritative active-stream relation, not from live_template_outputs, so
// an owner with finalized speech and no output rows yet is still found.
func ReconcileOwnerEligibilityTx(ctx context.Context, tx pgx.Tx, ownerID string, now time.Time) error {
	streams, err := activeLiveStreamsForOwnerTx(ctx, tx, ownerID)
	if err != nil {
		return err
	}
	for _, st := range streams {
		if _, err := applyEligibilityTx(ctx, tx, ownerID, st.NoteID, st.TranscriptID, st.StreamID, now); err != nil {
			return err
		}
	}
	return nil
}

// EndLiveTemplateStream idempotently removes every live_template_outputs row
// for one note/stream when it seals, closes, or is superseded (including
// batch transcription replacing live transcription). Pending jobs are
// cancelled; running rows are hidden and cancellation-requested so their
// eventual completion fence discards their result rather than publishing it
// against a stream that is no longer current. Safe to call multiple times.
func EndLiveTemplateStream(ctx context.Context, tx pgx.Tx, noteID, streamID string, now time.Time) (changed bool, err error) {
	rows, err := lockLiveOutputsForStreamTx(ctx, tx, noteID, streamID)
	if err != nil {
		return false, err
	}
	for _, o := range rows {
		switch {
		case o.ActiveJobID != "" && o.Status == model.LiveOutputRunning:
			if _, err := tx.Exec(ctx,
				`UPDATE live_template_outputs
				    SET client_visible=FALSE, cancellation_requested_at=$2, event_version=event_version+1, updated_at=$2
				  WHERE id=$1`,
				o.ID, now); err != nil {
				return false, err
			}
			changed = true
		case o.ActiveJobID != "":
			if _, err := tx.Exec(ctx,
				`UPDATE jobs SET status=$1, updated_at=now() WHERE id=$2 AND status='pending'`,
				model.JobCancelled, o.ActiveJobID); err != nil {
				return false, err
			}
			if _, err := tx.Exec(ctx, `DELETE FROM live_template_outputs WHERE id=$1`, o.ID); err != nil {
				return false, err
			}
			changed = true
		default:
			if _, err := tx.Exec(ctx, `DELETE FROM live_template_outputs WHERE id=$1`, o.ID); err != nil {
				return false, err
			}
			changed = true
		}
	}
	return changed, nil
}

// ListVisibleLiveTemplateOutputs returns ownerID's current client-visible
// live outputs for one note, joined with each template's name, in stable
// visible-template order -- the authoritative reread behind both the SSE
// snapshot and every subsequent coalesced wake-up / heartbeat (issue #764).
func (s *Store) ListVisibleLiveTemplateOutputs(ctx context.Context, ownerID, noteID string) ([]model.LiveTemplateOutput, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+qualifiedLiveOutputColumns()+`, t.name
		   FROM live_template_outputs o
		   JOIN templates t ON t.id = o.template_id
		  WHERE o.owner_id=$1 AND o.note_id=$2 AND o.client_visible=TRUE
		  ORDER BY (t.owner_id IS NULL) DESC, lower(t.name), t.id`,
		ownerID, noteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.LiveTemplateOutput{}
	for rows.Next() {
		o, err := scanLiveOutputWithName(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// qualifiedLiveOutputColumns is liveOutputColumns qualified with the "o"
// alias for ListVisibleLiveTemplateOutputs's join against templates (which
// also has an id/model column name that would otherwise be ambiguous).
func qualifiedLiveOutputColumns() string {
	return `o.id, o.note_id, o.template_id, o.stream_id, o.owner_id, o.desired_revision, o.rendered_revision,
	        o.event_version, o.status, o.client_visible, o.cancellation_requested_at, o.sections::text, o.agent_plugin, o.model,
	        o.error_code, o.last_started_at, o.last_demand_at, o.updated_at, o.ended_at, COALESCE(o.active_job_id::text,'')`
}

func scanLiveOutputWithName(rows pgx.Rows) (model.LiveTemplateOutput, error) {
	o, err := scanLiveOutputRowScanner(rows)
	if err != nil {
		return model.LiveTemplateOutput{}, err
	}
	return o, nil
}

// scanLiveOutputRowScanner scans the qualifiedLiveOutputColumns() shape plus
// a trailing template name column.
func scanLiveOutputRowScanner(rows pgx.Rows) (model.LiveTemplateOutput, error) {
	var o model.LiveTemplateOutput
	var sectionsJSON []byte
	var agentPlugin, modelName, errorCode *string
	if err := rows.Scan(&o.ID, &o.NoteID, &o.TemplateID, &o.StreamID, &o.OwnerID, &o.DesiredRevision, &o.RenderedRevision,
		&o.EventVersion, &o.Status, &o.ClientVisible, &o.CancellationRequestedAt, &sectionsJSON, &agentPlugin, &modelName,
		&errorCode, &o.LastStartedAt, &o.LastDemandAt, &o.UpdatedAt, &o.EndedAt, &o.ActiveJobID, &o.TemplateName); err != nil {
		return model.LiveTemplateOutput{}, err
	}
	if agentPlugin != nil {
		o.AgentPlugin = *agentPlugin
	}
	if modelName != nil {
		o.Model = *modelName
	}
	if errorCode != nil {
		o.ErrorCode = *errorCode
	}
	if len(sectionsJSON) == 0 {
		o.Sections = []model.SummarySection{}
	} else if err := json.Unmarshal(sectionsJSON, &o.Sections); err != nil {
		return model.LiveTemplateOutput{}, err
	}
	if o.Sections == nil {
		o.Sections = []model.SummarySection{}
	}
	return o, nil
}

// RecoverLiveTemplateOutputs removes live_template_outputs rows (active or
// idle) that are either no longer for a current stream, or whose
// last_demand_at has not changed for more than LiveStaleRecoveryAge, in
// bounded FOR UPDATE SKIP LOCKED pages so a large backlog cannot monopolize
// a single sweep. It returns the note ids whose rows changed, so the caller
// can send a note-ID-only notification for each, plus the number of rows
// removed. Idempotent and safe to call concurrently/repeatedly.
func (s *Store) RecoverLiveTemplateOutputs(ctx context.Context, now time.Time, limit int) (noteIDs []string, removed int, err error) {
	if limit <= 0 {
		limit = 200
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback(ctx)

	cutoff := now.Add(-LiveStaleRecoveryAge)
	rows, err := tx.Query(ctx,
		`SELECT o.id, o.note_id, o.stream_id
		   FROM live_template_outputs o
		   LEFT JOIN transcripts t ON t.note_id = o.note_id AND t.stream_id = o.stream_id AND t.sealed = FALSE
		  WHERE t.id IS NULL OR o.last_demand_at < $1
		  ORDER BY o.id
		  LIMIT $2
		  FOR UPDATE OF o SKIP LOCKED`,
		cutoff, limit)
	if err != nil {
		return nil, 0, err
	}
	type staleRow struct{ id, noteID, streamID string }
	var stale []staleRow
	for rows.Next() {
		var r staleRow
		if err := rows.Scan(&r.id, &r.noteID, &r.streamID); err != nil {
			rows.Close()
			return nil, 0, err
		}
		stale = append(stale, r)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	rows.Close()

	seen := map[string]bool{}
	for _, r := range stale {
		if _, err := tx.Exec(ctx,
			`UPDATE jobs SET status=$1, updated_at=now()
			  WHERE live_output_id=$2 AND status='pending'`, model.JobCancelled, r.id); err != nil {
			return nil, 0, err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM live_template_outputs WHERE id=$1`, r.id); err != nil {
			return nil, 0, err
		}
		if !seen[r.noteID] {
			seen[r.noteID] = true
			noteIDs = append(noteIDs, r.noteID)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, 0, err
	}
	return noteIDs, len(stale), nil
}
