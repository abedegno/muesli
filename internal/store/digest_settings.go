package store

import (
	"context"
	"errors"
	"time"

	"github.com/abedegno/muesli/internal/model"
	"github.com/jackc/pgx/v5"
)

func validateDigestCadence(cadence string) bool {
	switch cadence {
	case model.DigestCadenceOff, model.DigestCadenceDaily, model.DigestCadenceWeekly:
		return true
	default:
		return false
	}
}

// GetDigestConfig returns the owner's digest configuration. Missing rows are
// treated as the default off state rather than ErrNotFound.
func (s *Store) GetDigestConfig(ctx context.Context, ownerID string) (model.DigestConfig, error) {
	var cfg model.DigestConfig
	err := s.pool.QueryRow(ctx,
		`SELECT owner_id, cadence, last_sent_at, updated_at
		 FROM digest_settings
		 WHERE owner_id = $1`,
		ownerID).Scan(&cfg.OwnerID, &cfg.Cadence, &cfg.LastSentAt, &cfg.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.DigestConfig{OwnerID: ownerID, Cadence: model.DigestCadenceOff}, nil
	}
	if err != nil {
		return model.DigestConfig{}, err
	}
	return cfg, nil
}

// SetDigestConfig creates or updates the owner's cadence while preserving the
// last sent timestamp across updates.
func (s *Store) SetDigestConfig(ctx context.Context, ownerID, cadence string) (model.DigestConfig, error) {
	if !validateDigestCadence(cadence) {
		return model.DigestConfig{}, ErrInvalidState
	}
	var cfg model.DigestConfig
	err := s.pool.QueryRow(ctx,
		`INSERT INTO digest_settings (owner_id, cadence)
		 VALUES ($1, $2)
		 ON CONFLICT (owner_id) DO UPDATE
		 SET cadence = EXCLUDED.cadence,
		     updated_at = NOW()
		 RETURNING owner_id, cadence, last_sent_at, updated_at`,
		ownerID, cadence).Scan(&cfg.OwnerID, &cfg.Cadence, &cfg.LastSentAt, &cfg.UpdatedAt)
	if err != nil {
		return model.DigestConfig{}, err
	}
	return cfg, nil
}

// ListEnabledDigestConfigs returns every owner with a non-off cadence. It is
// used by the background scheduler, so it is intentionally not owner-scoped.
func (s *Store) ListEnabledDigestConfigs(ctx context.Context) ([]model.DigestConfig, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT owner_id, cadence, last_sent_at, updated_at
		 FROM digest_settings
		 WHERE cadence <> $1
		 ORDER BY owner_id`,
		model.DigestCadenceOff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []model.DigestConfig{}
	for rows.Next() {
		var cfg model.DigestConfig
		if err := rows.Scan(&cfg.OwnerID, &cfg.Cadence, &cfg.LastSentAt, &cfg.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, cfg)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if out == nil {
		out = []model.DigestConfig{}
	}
	return out, nil
}

// MarkDigestSent records the last successful send time for an owner.
func (s *Store) MarkDigestSent(ctx context.Context, ownerID string, sentAt time.Time) error {
	ct, err := s.pool.Exec(ctx,
		`UPDATE digest_settings
		 SET last_sent_at = $2,
		     updated_at = NOW()
		 WHERE owner_id = $1`,
		ownerID, sentAt)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
