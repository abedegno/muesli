package store

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (s *Store) CreateToken(ctx context.Context, userID, name, tokenHash, kind string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO app_tokens (id, user_id, name, token_hash, kind)
		 VALUES ($1,$2,$3,$4,$5)`,
		uuid.NewString(), userID, name, tokenHash, kind)
	return err
}

// UserIDByTokenHash returns the owning user of a non-revoked token.
func (s *Store) UserIDByTokenHash(ctx context.Context, tokenHash string) (string, error) {
	var uid string
	err := s.pool.QueryRow(ctx,
		`SELECT user_id FROM app_tokens WHERE token_hash=$1 AND revoked_at IS NULL`, tokenHash).
		Scan(&uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return uid, err
}
