package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// MaxLiveNoteSubscribers is the global (across every hosted API process)
// cap on concurrent live-prompt SSE subscribers for one note (issue #764).
const MaxLiveNoteSubscribers = 40

// AcquireLiveNoteSubscription admits one new SSE subscriber for a note,
// enforcing MaxLiveNoteSubscribers globally across hosted API processes via
// a locked per-note capacity row plus a renewable, expiring lease row. It
// deletes expired leases for the note before counting, so a crashed
// process's abandoned lease frees capacity automatically. Returns
// ok=false (no error) at capacity; the caller responds 429.
func (s *Store) AcquireLiveNoteSubscription(ctx context.Context, noteID, ownerID string, ttl time.Duration) (connectionID string, ok bool, err error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", false, err
	}
	defer tx.Rollback(ctx)

	// Lazily create and lock the per-note capacity row so concurrent
	// admissions for the same note serialize on this row across every
	// hosted API process sharing the database.
	if _, err := tx.Exec(ctx,
		`INSERT INTO live_note_capacity (note_id) VALUES ($1) ON CONFLICT (note_id) DO NOTHING`, noteID); err != nil {
		return "", false, err
	}
	if _, err := tx.Exec(ctx, `SELECT note_id FROM live_note_capacity WHERE note_id=$1 FOR UPDATE`, noteID); err != nil {
		return "", false, err
	}

	if _, err := tx.Exec(ctx, `DELETE FROM live_note_subscriptions WHERE note_id=$1 AND lease_expires_at < now()`, noteID); err != nil {
		return "", false, err
	}

	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM live_note_subscriptions WHERE note_id=$1`, noteID).Scan(&count); err != nil {
		return "", false, err
	}
	if count >= MaxLiveNoteSubscribers {
		return "", false, tx.Commit(ctx)
	}

	connectionID = uuid.NewString()
	if _, err := tx.Exec(ctx,
		`INSERT INTO live_note_subscriptions (connection_id, note_id, owner_id, lease_expires_at) VALUES ($1,$2,$3,$4)`,
		connectionID, noteID, ownerID, time.Now().Add(ttl)); err != nil {
		return "", false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", false, err
	}
	return connectionID, true, nil
}

// RenewLiveNoteSubscription extends a subscriber's lease. ok=false (no
// error) means the lease is already gone (expired and reclaimed, or the
// note was deleted) -- the caller must close the stream rather than serve
// without capacity. Renewal never generates a live-output notification.
func (s *Store) RenewLiveNoteSubscription(ctx context.Context, connectionID string, ttl time.Duration) (bool, error) {
	ct, err := s.pool.Exec(ctx,
		`UPDATE live_note_subscriptions SET lease_expires_at=$2 WHERE connection_id=$1`,
		connectionID, time.Now().Add(ttl))
	if err != nil {
		return false, err
	}
	return ct.RowsAffected() > 0, nil
}

// ReleaseLiveNoteSubscription explicitly frees a subscriber's capacity slot
// on disconnect. Safe to call on an already-expired/missing lease.
func (s *Store) ReleaseLiveNoteSubscription(ctx context.Context, connectionID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM live_note_subscriptions WHERE connection_id=$1`, connectionID)
	return err
}
