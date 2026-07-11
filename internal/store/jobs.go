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

// MaxJobAttempts caps retries before a job is marked terminally failed.
const MaxJobAttempts = 3

// EnqueueJob inserts a pending job and returns its id.
func (s *Store) EnqueueJob(ctx context.Context, noteID, jobType string, payload json.RawMessage) (string, error) {
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	id := uuid.NewString()
	_, err := s.pool.Exec(ctx,
		`INSERT INTO jobs (id, note_id, type, status, payload)
		 VALUES ($1,$2,$3,$4,$5::jsonb)`,
		id, noteID, jobType, model.JobPending, string(payload))
	return id, err
}

// ClaimJob atomically selects one claimable job (pending, or running with an
// expired lease) using SELECT ... FOR UPDATE SKIP LOCKED, marks it running,
// increments attempts, and sets a fresh lease. Returns ok=false if none.
func (s *Store) ClaimJob(ctx context.Context, lease time.Duration) (model.Job, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.Job{}, false, err
	}
	defer tx.Rollback(ctx)

	var j model.Job
	var payload []byte
	err = tx.QueryRow(ctx,
		`SELECT id, note_id, type, status, attempts, COALESCE(last_error,''), priority, payload
		 FROM jobs
		 WHERE status='pending'
		    OR (status='running' AND lease_expires_at < now())
		 ORDER BY priority DESC, created_at ASC
		 FOR UPDATE SKIP LOCKED
		 LIMIT 1`).
		Scan(&j.ID, &j.NoteID, &j.Type, &j.Status, &j.Attempts, &j.LastError, &j.Priority, &payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.Job{}, false, tx.Commit(ctx)
	}
	if err != nil {
		return model.Job{}, false, err
	}
	j.Payload = json.RawMessage(payload)

	expires := time.Now().Add(lease)
	err = tx.QueryRow(ctx,
		`UPDATE jobs
		 SET status=$1, attempts=attempts+1, lease_expires_at=$2,
		     started_at=now(), finished_at=NULL, updated_at=now()
		 WHERE id=$3
		 RETURNING attempts, started_at, finished_at`,
		model.JobRunning, expires, j.ID).Scan(&j.Attempts, &j.StartedAt, &j.FinishedAt)
	if err != nil {
		return model.Job{}, false, err
	}
	j.Status = model.JobRunning
	if err := tx.Commit(ctx); err != nil {
		return model.Job{}, false, err
	}
	return j, true, nil
}

// CompleteJob marks a job done and clears its lease.
func (s *Store) CompleteJob(ctx context.Context, id string) error {
	ct, err := s.pool.Exec(ctx,
		`UPDATE jobs SET status=$1, lease_expires_at=NULL, last_error=NULL, finished_at=now(), updated_at=now()
		 WHERE id=$2`, model.JobDone, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// FailJob records an error. If retryable AND under the attempt cap, the job goes
// back to pending (lease cleared) for another worker; otherwise it is terminal.
func (s *Store) FailJob(ctx context.Context, id, errMsg string, retryable bool) error {
	var attempts int
	if err := s.pool.QueryRow(ctx, `SELECT attempts FROM jobs WHERE id=$1`, id).Scan(&attempts); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	status := model.JobFailed
	if retryable && attempts < MaxJobAttempts {
		status = model.JobPending
	}
	_, err := s.pool.Exec(ctx,
		`UPDATE jobs SET status=$1, last_error=$2, lease_expires_at=NULL, finished_at=now(), updated_at=now()
		 WHERE id=$3`, status, errMsg, id)
	return err
}

// CountActiveSummarizeJobs counts summarize jobs for a note that are still
// pending or running (i.e. not done and not terminally failed).
func (s *Store) CountActiveSummarizeJobs(ctx context.Context, noteID string) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM jobs
		 WHERE note_id=$1 AND type=$2 AND status IN ('pending','running')`,
		noteID, model.JobSummarize).Scan(&n)
	return n, err
}

// BumpNoteJobPriority raises the priority of a note's still-PENDING jobs so
// they are claimed ahead of every other currently-pending job, without
// touching anything already running/done/failed (no preemption of in-flight
// work). The new priority is one greater than the current max pending
// priority across the whole queue, guaranteeing it dequeues first via
// ClaimJob's "ORDER BY priority DESC, created_at ASC". Returns the number of
// jobs bumped (0 means the note had no pending job to prioritize).
func (s *Store) BumpNoteJobPriority(ctx context.Context, noteID string) (int, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE jobs
		 SET priority = (SELECT COALESCE(MAX(priority), 0) + 1 FROM jobs WHERE status='pending'),
		     updated_at = now()
		 WHERE note_id=$1 AND status='pending'`,
		noteID)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

// CancelJob cancels a job that is still PENDING, leaving running/done/failed
// jobs untouched (never preempts in-flight or already-terminal work). Callers
// (e.g. handleCancelJob) are expected to have already fetched the job and
// checked its status is pending; CancelJob returns ErrInvalidTransition if the
// conditional update affects no rows (the job is no longer pending, e.g. a
// race between the caller's check and this call).
func (s *Store) CancelJob(ctx context.Context, id string) error {
	ct, err := s.pool.Exec(ctx,
		`UPDATE jobs SET status=$1, updated_at=now()
		 WHERE id=$2 AND status='pending'`,
		model.JobCancelled, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrInvalidTransition
	}
	return nil
}

// ListJobs returns jobs (optionally filtered by status) for the admin monitor.
func (s *Store) ListJobs(ctx context.Context, status string) ([]model.Job, error) {
	q := `SELECT id, note_id, type, status, attempts, COALESCE(last_error,''), priority, started_at, finished_at
	      FROM jobs`
	args := []any{}
	if status != "" {
		q += ` WHERE status=$1`
		args = append(args, status)
	}
	q += ` ORDER BY updated_at DESC`
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Job
	for rows.Next() {
		var j model.Job
		if err := rows.Scan(&j.ID, &j.NoteID, &j.Type, &j.Status, &j.Attempts, &j.LastError, &j.Priority, &j.StartedAt, &j.FinishedAt); err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	if out == nil {
		out = []model.Job{}
	}
	return out, rows.Err()
}

// ListJobsByNoteID returns all jobs for a note ordered by created_at ASC, so
// pipeline stages (transcribe/summarize/embed) come back in the order they
// were enqueued. Admin-scoped like ListJobs: no owner filtering, since this
// backs an admin endpoint over the global jobs table.
func (s *Store) ListJobsByNoteID(ctx context.Context, noteID string) ([]model.Job, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, note_id, type, status, attempts, COALESCE(last_error,''), priority, started_at, finished_at
		 FROM jobs
		 WHERE note_id=$1
		 ORDER BY created_at ASC`, noteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Job
	for rows.Next() {
		var j model.Job
		if err := rows.Scan(&j.ID, &j.NoteID, &j.Type, &j.Status, &j.Attempts, &j.LastError, &j.Priority, &j.StartedAt, &j.FinishedAt); err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	if out == nil {
		out = []model.Job{}
	}
	return out, rows.Err()
}

// GetJob fetches a single job by ID. Returns ErrNotFound if no row matches.
func (s *Store) GetJob(ctx context.Context, jobID string) (model.Job, error) {
	var j model.Job
	var payload []byte
	err := s.pool.QueryRow(ctx,
		`SELECT id, note_id, type, status, attempts, COALESCE(last_error,''), priority, payload, started_at, finished_at
		 FROM jobs WHERE id=$1`, jobID).
		Scan(&j.ID, &j.NoteID, &j.Type, &j.Status, &j.Attempts, &j.LastError, &j.Priority, &payload, &j.StartedAt, &j.FinishedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.Job{}, ErrNotFound
	}
	if err != nil {
		return model.Job{}, err
	}
	j.Payload = json.RawMessage(payload)
	return j, nil
}

// ResetRunningJobs sets ALL status='running' jobs to status='pending' with
// lease_expires_at=NULL. Called once at startup to recover orphaned jobs.
// Returns the number of jobs reset.
func (s *Store) ResetRunningJobs(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE jobs SET status='pending', lease_expires_at=NULL, updated_at=now()
		 WHERE status='running'`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// ResetExpiredRunningJobs sets status='running' jobs whose lease has expired to
// status='pending' with lease_expires_at=NULL. Called periodically at runtime.
// Returns the number of jobs reset.
func (s *Store) ResetExpiredRunningJobs(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE jobs SET status='pending', lease_expires_at=NULL, updated_at=now()
		 WHERE status='running' AND lease_expires_at < now()`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// JobStatusCounts returns the number of jobs currently in each known status
// (model.JobPending/Running/Done/Failed/Cancelled), always including every
// known status in the result even when its count is 0. Backs the admin health
// panel's job-queue-depth section (ADM05).
func (s *Store) JobStatusCounts(ctx context.Context) (map[string]int, error) {
	counts := map[string]int{
		model.JobPending:   0,
		model.JobRunning:   0,
		model.JobDone:      0,
		model.JobFailed:    0,
		model.JobCancelled: 0,
	}
	rows, err := s.pool.Query(ctx, `SELECT status, count(*) FROM jobs GROUP BY status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var n int
		if err := rows.Scan(&status, &n); err != nil {
			return nil, err
		}
		counts[status] = n
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return counts, nil
}

// GetLatestFailedJobByNoteID returns the most recently updated failed job for
// the given note, or ErrNotFound if no such job exists.
func (s *Store) GetLatestFailedJobByNoteID(ctx context.Context, noteID string) (*model.Job, error) {
	var j model.Job
	var payload []byte
	err := s.pool.QueryRow(ctx,
		`SELECT id, note_id, type, status, attempts, COALESCE(last_error,''), priority, payload
		 FROM jobs
		 WHERE note_id = $1 AND status = 'failed'
		 ORDER BY updated_at DESC
		 LIMIT 1`, noteID).
		Scan(&j.ID, &j.NoteID, &j.Type, &j.Status, &j.Attempts, &j.LastError, &j.Priority, &payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	j.Payload = json.RawMessage(payload)
	return &j, nil
}
