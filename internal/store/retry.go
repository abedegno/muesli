package store

import (
	"context"
	"encoding/json"

	"github.com/abedegno/muesli/internal/model"
	"github.com/google/uuid"
)

// RetryNote atomically enqueues a new copy of the supplied job and resets the
// note's status to "uploaded" inside a single transaction. This prevents the
// split-brain where the INSERT succeeds but the status UPDATE fails (or vice
// versa), which would leave an orphan pending job or a permanently-stuck note.
func (s *Store) RetryNote(ctx context.Context, noteID, jobType string, payload json.RawMessage) error {
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	jobID := uuid.NewString()
	if _, err := tx.Exec(ctx,
		`INSERT INTO jobs (id, note_id, type, status, payload)
		 VALUES ($1, $2, $3, $4, $5::jsonb)`,
		jobID, noteID, jobType, model.JobPending, string(payload)); err != nil {
		return err
	}

	ct, err := tx.Exec(ctx,
		`UPDATE notes SET status=$1, partial_transcript=FALSE, updated_at=now() WHERE id=$2`,
		model.NoteUploaded, noteID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}

	return tx.Commit(ctx)
}
