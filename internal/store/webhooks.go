package store

import (
	"context"
	"errors"
	"time"

	"github.com/abedegno/muesli/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ListPendingDeliveries returns up to limit deliveries that are due for
// processing: status='pending' AND (next_attempt_at IS NULL OR
// next_attempt_at <= NOW()). Uses FOR UPDATE SKIP LOCKED to allow multiple
// workers to race without double-claiming.
func (s *Store) ListPendingDeliveries(ctx context.Context, limit int) ([]model.WebhookDelivery, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, webhook_id, payload, status, attempts, max_attempts,
		        next_attempt_at, COALESCE(last_error,''), created_at, updated_at
		 FROM webhook_deliveries
		 WHERE status = 'pending'
		   AND (next_attempt_at IS NULL OR next_attempt_at <= NOW())
		 ORDER BY created_at
		 FOR UPDATE SKIP LOCKED
		 LIMIT $1`,
		limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.WebhookDelivery
	for rows.Next() {
		var d model.WebhookDelivery
		if err := rows.Scan(
			&d.ID, &d.WebhookID, &d.Payload, &d.Status, &d.Attempts,
			&d.MaxAttempts, &d.NextAttemptAt, &d.LastError, &d.CreatedAt, &d.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ClaimDelivery marks a delivery as in_flight so only one worker processes it.
func (s *Store) ClaimDelivery(ctx context.Context, id uuid.UUID) error {
	ct, err := s.pool.Exec(ctx,
		`UPDATE webhook_deliveries
		 SET status = 'in_flight', updated_at = NOW()
		 WHERE id = $1 AND status = 'pending'`,
		id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// MarkDelivered marks a delivery as successfully delivered.
func (s *Store) MarkDelivered(ctx context.Context, id uuid.UUID) error {
	ct, err := s.pool.Exec(ctx,
		`UPDATE webhook_deliveries
		 SET status = 'delivered', updated_at = NOW()
		 WHERE id = $1`,
		id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// MarkFailed increments attempts, records lastErr, and either:
//   - requeues the delivery (status='pending') with next_attempt_at=*nextAttempt, or
//   - permanently fails it (status='failed') when nextAttempt is nil.
func (s *Store) MarkFailed(ctx context.Context, id uuid.UUID, lastErr string, nextAttempt *time.Time) error {
	var ct interface{ RowsAffected() int64 }
	var err error
	if nextAttempt == nil {
		// Give up: set status=failed, no next_attempt_at.
		ct, err = s.pool.Exec(ctx,
			`UPDATE webhook_deliveries
			 SET status       = 'failed',
			     attempts     = attempts + 1,
			     last_error   = $2,
			     next_attempt_at = NULL,
			     updated_at   = NOW()
			 WHERE id = $1`,
			id, lastErr)
	} else {
		// Requeue for later retry.
		ct, err = s.pool.Exec(ctx,
			`UPDATE webhook_deliveries
			 SET status          = 'pending',
			     attempts        = attempts + 1,
			     last_error      = $2,
			     next_attempt_at = $3,
			     updated_at      = NOW()
			 WHERE id = $1`,
			id, lastErr, *nextAttempt)
	}
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ResetInflightDeliveries resets any in_flight rows back to pending. Called on
// worker startup to recover deliveries that were in-flight when the process died.
func (s *Store) ResetInflightDeliveries(ctx context.Context) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE webhook_deliveries
		 SET status = 'pending', updated_at = NOW()
		 WHERE status = 'in_flight'`)
	return err
}

// ListEnabledWebhooksForOwner returns all enabled webhook subscriptions owned
// by ownerID. Worker-side (called from the finalize pipeline with an owner id
// already resolved from the note, so no request-level owner scoping applies
// here — this IS the owner scope). Zero enabled webhooks returns an empty,
// non-nil slice and no error.
func (s *Store) ListEnabledWebhooksForOwner(ctx context.Context, ownerID string) ([]model.Webhook, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, owner_id, url, secret, enabled, created_at, updated_at
		 FROM webhooks WHERE owner_id=$1 AND enabled=true`,
		ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.Webhook
	for rows.Next() {
		var w model.Webhook
		if err := rows.Scan(&w.ID, &w.OwnerID, &w.URL, &w.Secret, &w.Enabled, &w.CreatedAt, &w.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if out == nil {
		out = []model.Webhook{}
	}
	return out, nil
}

// CreateDelivery inserts a new pending delivery row for webhookID carrying
// payload. status/attempts/max_attempts/created_at/updated_at all use their
// DB defaults (see migration 0016_webhooks).
func (s *Store) CreateDelivery(ctx context.Context, webhookID string, payload []byte) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO webhook_deliveries (webhook_id, payload) VALUES ($1, $2)`,
		webhookID, payload)
	return err
}

// GetDeliveryWebhook returns the webhook configuration for a given delivery.
func (s *Store) GetDeliveryWebhook(ctx context.Context, deliveryID uuid.UUID) (model.Webhook, error) {
	var w model.Webhook
	err := s.pool.QueryRow(ctx,
		`SELECT w.id, w.owner_id, w.url, w.secret, w.enabled, w.created_at, w.updated_at
		 FROM webhooks w
		 JOIN webhook_deliveries d ON d.webhook_id = w.id
		 WHERE d.id = $1`,
		deliveryID).
		Scan(&w.ID, &w.OwnerID, &w.URL, &w.Secret, &w.Enabled, &w.CreatedAt, &w.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.Webhook{}, ErrNotFound
	}
	return w, err
}

// ErrInvalidState is returned when an operation is attempted against a
// resource whose current state doesn't allow it (e.g. manually retrying a
// delivery that is still pending or in_flight).
var ErrInvalidState = errors.New("invalid state")

// ListDeliveriesForOwner returns up to limit deliveries whose webhook belongs
// to ownerID, most recent first. Owner-scoped: joins to webhooks on
// owner_id, so callers only ever see their own deliveries. Zero deliveries
// returns an empty, non-nil slice and no error.
func (s *Store) ListDeliveriesForOwner(ctx context.Context, ownerID string, limit int) ([]model.WebhookDelivery, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT d.id, d.webhook_id, d.payload, d.status, d.attempts, d.max_attempts,
		        d.next_attempt_at, COALESCE(d.last_error,''), d.created_at, d.updated_at
		 FROM webhook_deliveries d
		 JOIN webhooks w ON w.id = d.webhook_id
		 WHERE w.owner_id = $1
		 ORDER BY d.created_at DESC
		 LIMIT $2`,
		ownerID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.WebhookDelivery
	for rows.Next() {
		var d model.WebhookDelivery
		if err := rows.Scan(
			&d.ID, &d.WebhookID, &d.Payload, &d.Status, &d.Attempts,
			&d.MaxAttempts, &d.NextAttemptAt, &d.LastError, &d.CreatedAt, &d.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if out == nil {
		out = []model.WebhookDelivery{}
	}
	return out, nil
}

// RetryDelivery manually retries a delivery, owner-scoped (the delivery's
// webhook must belong to ownerID, verified in the same query that reads its
// current status, and the row is locked FOR UPDATE for the duration of the
// transaction so this races safely against the background worker and
// concurrent retry calls). Semantics by current status:
//   - not found / not owned by ownerID -> ErrNotFound.
//   - delivered -> idempotent no-op: no row changes, returns
//     (model.DeliveryDelivered, nil).
//   - failed -> re-enqueued: status='pending', attempts=0, last_error=NULL,
//     next_attempt_at=NULL (a fresh full retry cycle, not an instant
//     re-give-up), returns (model.DeliveryPending, nil).
//   - pending / in_flight -> already queued or in progress, returns
//     ("", ErrInvalidState).
//
// The returned status lets the caller (the admin API handler) distinguish
// the two success paths without a second query.
func (s *Store) RetryDelivery(ctx context.Context, ownerID string, deliveryID uuid.UUID) (string, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	var status string
	err = tx.QueryRow(ctx,
		`SELECT d.status
		 FROM webhook_deliveries d
		 JOIN webhooks w ON w.id = d.webhook_id
		 WHERE d.id = $1 AND w.owner_id = $2
		 FOR UPDATE OF d`,
		deliveryID, ownerID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}

	switch status {
	case model.DeliveryDelivered:
		// Already delivered: retrying is a safe no-op, no row changes.
		return model.DeliveryDelivered, tx.Commit(ctx)
	case model.DeliveryFailed:
		if _, err := tx.Exec(ctx,
			`UPDATE webhook_deliveries
			 SET status          = 'pending',
			     attempts        = 0,
			     last_error      = NULL,
			     next_attempt_at = NULL,
			     updated_at      = NOW()
			 WHERE id = $1`,
			deliveryID); err != nil {
			return "", err
		}
		return model.DeliveryPending, tx.Commit(ctx)
	default: // pending or in_flight: already queued/in progress.
		return "", ErrInvalidState
	}
}
