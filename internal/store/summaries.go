package store

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/abedegno/muesli/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// EnqueueSummarizeJobs fans out one pending summary + summarize job per template the
// owner sees and has opted into auto-run for, and sets the note to 'summarizing'. If
// the owner has no auto-run templates the note is set 'ready' (nothing to summarize).
// Used by the post-transcription pipeline AND the resummarize endpoint.
func (s *Store) EnqueueSummarizeJobs(ctx context.Context, ownerID, noteID string) error {
	templates, err := s.TemplatesForSummary(ctx, ownerID)
	if err != nil {
		return err
	}
	if err := s.SetNoteStatus(ctx, noteID, model.NoteSummarizing); err != nil {
		return err
	}
	for _, tmpl := range templates {
		sumID, err := s.CreatePendingSummary(ctx, noteID, tmpl.ID)
		if err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]string{"template_id": tmpl.ID, "summary_id": sumID})
		if _, err := s.EnqueueJob(ctx, noteID, model.JobSummarize, payload); err != nil {
			return err
		}
	}
	if len(templates) == 0 {
		return s.SetNoteStatus(ctx, noteID, model.NoteReady)
	}
	return nil
}

// DeleteNoteSummaries removes all summaries for an owner's note (used before a re-run).
func (s *Store) DeleteNoteSummaries(ctx context.Context, ownerID, noteID string) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM summaries WHERE note_id=$1 AND note_id IN (SELECT id FROM notes WHERE owner_id=$2)`,
		noteID, ownerID)
	return err
}

// testHookAfterDeleteNoteSummariesGenerationCheck runs inside
// DeleteNoteSummariesIfCurrent's transaction, after the notes-row lock is
// taken and the generation re-check passes, and before the DELETE. It is nil
// in production and exists so a test can hold this transaction open — still
// holding the notes-row lock — while proving a concurrent SaveTranscript
// blocks on that same lock rather than racing past it.
var testHookAfterDeleteNoteSummariesGenerationCheck func()

// EnqueueSummarizeJobsIfCurrent is EnqueueSummarizeJobs guarded by the
// transcript generation. It can't be a single conditional UPDATE — it spans
// SetNoteStatus + N x (CreatePendingSummary+EnqueueJob), or the
// zero-templates ready fallback — so it takes its own tx: lock the notes row
// FIRST, re-verify generation once under that lock, then do the sub-writes,
// then commit. Locking first is what excludes a concurrent SaveTranscript:
// SaveTranscript's very first statement takes the same notes-row lock.
func (s *Store) EnqueueSummarizeJobsIfCurrent(ctx context.Context, ownerID, noteID string, expectedGeneration int) error {
	templates, err := s.TemplatesForSummary(ctx, ownerID)
	if err != nil {
		return err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `SELECT id FROM notes WHERE id=$1 FOR UPDATE`, noteID); err != nil {
		return err
	}

	var generation int
	err = tx.QueryRow(ctx, `SELECT generation FROM transcripts WHERE note_id=$1`, noteID).Scan(&generation)
	if errors.Is(err, pgx.ErrNoRows) {
		generation = 0
	} else if err != nil {
		return err
	}
	if generation != expectedGeneration {
		return ErrGenerationMismatch
	}

	newStatus := model.NoteSummarizing
	if len(templates) == 0 {
		newStatus = model.NoteReady
	}
	if _, err := tx.Exec(ctx, `UPDATE notes SET status=$1, updated_at=now() WHERE id=$2`, newStatus, noteID); err != nil {
		return err
	}
	for _, tmpl := range templates {
		sumID := uuid.NewString()
		empty, merr := json.Marshal([]model.SummarySection{})
		if merr != nil {
			return merr
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO summaries (id, note_id, template_id, agent_plugin, model, content)
			 VALUES ($1,$2,$3,'','', jsonb_build_object('status',$4::text,'sections',$5::jsonb,'truncated',false))`,
			sumID, noteID, tmpl.ID, model.SummaryPending, string(empty)); err != nil {
			return err
		}
		payload, merr := json.Marshal(map[string]string{"template_id": tmpl.ID, "summary_id": sumID})
		if merr != nil {
			return merr
		}
		jobID := uuid.NewString()
		if _, err := tx.Exec(ctx,
			`INSERT INTO jobs (id, note_id, type, status, payload) VALUES ($1,$2,$3,$4,$5::jsonb)`,
			jobID, noteID, model.JobSummarize, model.JobPending, string(payload)); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// DeleteNoteSummariesIfCurrent is DeleteNoteSummaries guarded by the
// transcript generation. This is the fix for the round-2 review finding: a
// prior attempt used a single CTE-based statement
// (`WITH gen_ok AS (...), deleted AS (DELETE ... WHERE ... AND gen_ok) ...`)
// that checked the generation and deleted without ever taking a lock on the
// notes row, so it did not compose with the locking discipline every other
// guarded write in this fix uses and did NOT actually exclude a concurrent
// SaveTranscript. This version instead uses the same lock-then-check-then-write
// transaction shape as SetRetentionStateDiscardedIfCurrent /
// EnqueueSummarizeJobsIfCurrent above: it locks the notes row FIRST
// (`SELECT id FROM notes WHERE id=$1 FOR UPDATE`), re-checks the generation
// under that lock, then deletes, then commits. SaveTranscript's own first
// statement (`UPDATE notes SET transcribing_job_id=NULL WHERE id=$1`) takes
// the SAME row lock and blocks until this transaction commits or rolls back,
// which is what actually closes the race — see the invariant documented on
// SaveTranscript in transcripts.go.
func (s *Store) DeleteNoteSummariesIfCurrent(ctx context.Context, ownerID, noteID string, expectedGeneration int) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `SELECT id FROM notes WHERE id=$1 FOR UPDATE`, noteID); err != nil {
		return err
	}

	var generation int
	err = tx.QueryRow(ctx, `SELECT generation FROM transcripts WHERE note_id=$1`, noteID).Scan(&generation)
	if errors.Is(err, pgx.ErrNoRows) {
		generation = 0
	} else if err != nil {
		return err
	}
	if generation != expectedGeneration {
		return ErrGenerationMismatch
	}

	if testHookAfterDeleteNoteSummariesGenerationCheck != nil {
		testHookAfterDeleteNoteSummariesGenerationCheck()
	}

	if _, err := tx.Exec(ctx,
		`DELETE FROM summaries WHERE note_id=$1 AND note_id IN (SELECT id FROM notes WHERE owner_id=$2)`,
		noteID, ownerID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// CreatePendingSummary inserts a summary row in the "pending" state with empty
// sections as a placeholder, overwritten by CompleteSummary on success or marked
// "failed" by FailSummary on terminal failure. The pending state lets /full
// distinguish "not started yet" from "genuinely failed".
func (s *Store) CreatePendingSummary(ctx context.Context, noteID, templateID string) (string, error) {
	id := uuid.NewString()
	empty, _ := json.Marshal([]model.SummarySection{})
	_, err := s.pool.Exec(ctx,
		`INSERT INTO summaries (id, note_id, template_id, agent_plugin, model, content)
		 VALUES ($1,$2,$3,'','', jsonb_build_object('status',$4::text,'sections',$5::jsonb,'truncated',false))`,
		id, noteID, templateID, model.SummaryPending, string(empty))
	return id, err
}

// CompleteSummary marks a summary ready with its produced sections. truncated
// flags a summary that looks complete but may have been cut short by a
// context-window overflow on a long transcript (see internal/worker's
// DetectTruncation) — a separate signal from the ready/failed status above.
func (s *Store) CompleteSummary(ctx context.Context, id, agentPlugin, modelName string, sections []model.SummarySection, truncated bool) error {
	secJSON, err := json.Marshal(sections)
	if err != nil {
		return err
	}
	ct, err := s.pool.Exec(ctx,
		`UPDATE summaries
		 SET agent_plugin=$1, model=$2,
		     content=jsonb_build_object('status',$3::text,'sections',$4::jsonb,'truncated',$5::boolean)
		 WHERE id=$6`,
		agentPlugin, modelName, model.SummaryReady, string(secJSON), truncated, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// FailSummary marks a summary failed (retryable from the admin UI).
func (s *Store) FailSummary(ctx context.Context, id string) error {
	ct, err := s.pool.Exec(ctx,
		`UPDATE summaries SET content=jsonb_set(content,'{status}', to_jsonb($1::text)) WHERE id=$2`,
		model.SummaryFailed, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// GetSummaries returns all summaries for a note, joined to template names.
func (s *Store) GetSummaries(ctx context.Context, noteID string) ([]model.Summary, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT s.id, s.note_id, COALESCE(s.template_id::text,''), COALESCE(t.name,''),
		        s.agent_plugin, s.model,
		        COALESCE(s.content->>'status', $2), COALESCE(s.content->'sections','[]'::jsonb),
		        COALESCE((s.content->>'truncated')::boolean, false)
		 FROM summaries s
		 LEFT JOIN templates t ON t.id = s.template_id
		 WHERE s.note_id=$1
		 ORDER BY t.name`, noteID, model.SummaryReady)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Summary
	for rows.Next() {
		var sm model.Summary
		var sectionsJSON []byte
		if err := rows.Scan(&sm.ID, &sm.NoteID, &sm.TemplateID, &sm.TemplateName,
			&sm.AgentPlugin, &sm.Model, &sm.Status, &sectionsJSON, &sm.Truncated); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(sectionsJSON, &sm.Sections); err != nil {
			return nil, err
		}
		out = append(out, sm)
	}
	if out == nil {
		out = []model.Summary{}
	}
	return out, rows.Err()
}

// deleteNoteTemplateSummary removes the single summary row (if any) for a specific
// (noteID, templateID) pair, leaving the note's other summaries untouched. Scoped by
// owner via the notes join so callers don't need a separate ownership check here.
func (s *Store) deleteNoteTemplateSummary(ctx context.Context, ownerID, noteID, templateID string) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM summaries WHERE note_id=$1 AND template_id=$2
		 AND note_id IN (SELECT id FROM notes WHERE owner_id=$3)`,
		noteID, templateID, ownerID)
	return err
}

// EnqueueTemplateSummarizeJob (re)generates a single template's summary for a note
// without touching its other summaries or re-transcribing: it validates the template
// is visible to the owner (built-in or their own), deletes any existing summary row
// for that (note, template) pair, creates a fresh pending summary, enqueues exactly
// one summarize job for it, and sets the note to 'summarizing' so the existing
// FinalizeNote path flips it back to 'ready' once this one job settles.
func (s *Store) EnqueueTemplateSummarizeJob(ctx context.Context, ownerID, noteID, templateID string) error {
	if _, err := s.GetTemplate(ctx, ownerID, templateID); errors.Is(err, ErrNotFound) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	if err := s.deleteNoteTemplateSummary(ctx, ownerID, noteID, templateID); err != nil {
		return err
	}
	if err := s.SetNoteStatus(ctx, noteID, model.NoteSummarizing); err != nil {
		return err
	}
	sumID, err := s.CreatePendingSummary(ctx, noteID, templateID)
	if err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]string{"template_id": templateID, "summary_id": sumID})
	_, err = s.EnqueueJob(ctx, noteID, model.JobSummarize, payload)
	return err
}
