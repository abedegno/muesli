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

// EnqueueSummarizeJobsIfCurrent is EnqueueSummarizeJobs's generation-guarded
// counterpart, for the transcribe pipeline: it must not fan out summarize
// jobs for a transcript a newer job has since replaced.
//
// Unlike the other seven guarded writes this cannot be a single conditional
// UPDATE/DELETE: it spans multiple statements (SetNoteStatus, one
// CreatePendingSummary + EnqueueJob pair per template, or the
// zero-templates SetNoteStatus(ready) fallback). So instead it takes its own
// generation-guarded transaction: lock the notes row FIRST — the SAME lock
// SaveTranscript takes before publishing a replacement (see the invariant
// documented on SaveTranscript in transcripts.go) — then re-verify the
// generation once under that lock before any sub-write, mirroring how
// SaveTranscript takes the notes row lock via
// `UPDATE notes SET transcribing_job_id=NULL WHERE id=$1` before its own
// generation logic. Holding the lock for the duration blocks a concurrent
// SaveTranscript/CreateStreamTranscript (which takes the same lock first)
// until this transaction commits or rolls back, so a replacement cannot land
// midway through the fan-out.
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
	if _, err := tx.Exec(ctx,
		`UPDATE notes SET status=$1, updated_at=now() WHERE id=$2`, newStatus, noteID); err != nil {
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

// DeleteNoteSummaries removes all summaries for an owner's note (used before a re-run).
func (s *Store) DeleteNoteSummaries(ctx context.Context, ownerID, noteID string) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM summaries WHERE note_id=$1 AND note_id IN (SELECT id FROM notes WHERE owner_id=$2)`,
		noteID, ownerID)
	return err
}

// DeleteNoteSummariesIfCurrent is DeleteNoteSummaries's generation-guarded
// counterpart, for the transcribe pipeline: it must not remove summaries
// belonging to a transcript a newer job has since replaced.
//
// A DELETE's RowsAffected alone cannot distinguish "guard failed, nothing
// deleted" from "guard passed, there simply were no summaries yet" (the
// normal case on a note's first transcription) — both report 0. So the
// generation check is evaluated once, in a CTE the DELETE's WHERE clause
// reads from, and its result is what the trailing SELECT reports back,
// independently of how many rows the DELETE itself touched. Both run in one
// statement (one snapshot), so this is still a single atomic check-and-write,
// not a read followed by a write a replacement could land between.
func (s *Store) DeleteNoteSummariesIfCurrent(ctx context.Context, ownerID, noteID string, expectedGeneration int) error {
	var genOK bool
	err := s.pool.QueryRow(ctx,
		`WITH gen_ok AS (
		   SELECT EXISTS (SELECT 1 FROM transcripts WHERE note_id=$1 AND generation=$3) AS ok
		 ), deleted AS (
		   DELETE FROM summaries
		   WHERE note_id=$1
		     AND note_id IN (SELECT id FROM notes WHERE owner_id=$2)
		     AND (SELECT ok FROM gen_ok)
		   RETURNING 1
		 )
		 SELECT ok FROM gen_ok`,
		noteID, ownerID, expectedGeneration).Scan(&genOK)
	if err != nil {
		return err
	}
	if !genOK {
		return ErrGenerationMismatch
	}
	return nil
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
