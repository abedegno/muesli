package store

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"time"

	"github.com/abedegno/muesli/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func scanShare(row interface{ Scan(...any) error }) (model.Share, error) {
	var share model.Share
	err := row.Scan(&share.ID, &share.Token, &share.NoteID, &share.OwnerID, &share.CreatedAt, &share.ExpiresAt, &share.RevokedAt)
	return share, err
}

func randomShareToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func (s *Store) insertShare(ctx context.Context, ownerID, noteID, token string, expiresAt *time.Time) (model.Share, error) {
	var share model.Share
	err := s.pool.QueryRow(ctx,
		`INSERT INTO note_shares (id, token, note_id, owner_id, expires_at)
		 VALUES ($1,$2,$3,$4,$5)
		 RETURNING id, token, note_id, owner_id, created_at, expires_at, revoked_at`,
		uuid.NewString(), token, noteID, ownerID, expiresAt).
		Scan(&share.ID, &share.Token, &share.NoteID, &share.OwnerID, &share.CreatedAt, &share.ExpiresAt, &share.RevokedAt)
	return share, err
}

// CreateShare creates a revocable read-only share token for a live note owned
// by ownerID.
func (s *Store) CreateShare(ctx context.Context, ownerID, noteID string, expiresAt *time.Time) (*model.Share, error) {
	ok, err := s.noteOwnedLive(ctx, ownerID, noteID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNotFound
	}

	for range 5 {
		token, err := randomShareToken()
		if err != nil {
			return nil, err
		}
		share, err := s.insertShare(ctx, ownerID, noteID, token, expiresAt)
		if err == nil {
			return &share, nil
		}
		if isUniqueViolation(err) {
			continue
		}
		return nil, err
	}
	return nil, ErrDuplicate
}

// RevokeShare marks one owner-scoped share as revoked.
// Returns ErrNotFound when the share is absent, not owned, or already revoked.
func (s *Store) RevokeShare(ctx context.Context, ownerID, shareID string) error {
	ct, err := s.pool.Exec(ctx,
		`UPDATE note_shares AS ns
		 SET revoked_at = now()
		 FROM notes AS n
		 WHERE ns.id = $2
		   AND ns.note_id = n.id
		   AND n.owner_id = $1
		   AND n.deleted_at IS NULL
		   AND ns.revoked_at IS NULL`,
		ownerID, shareID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// RevokeShareByToken marks one owner-scoped share as revoked using its public
// token. Returns ErrNotFound when the share is absent, not owned, or already
// revoked.
func (s *Store) RevokeShareByToken(ctx context.Context, ownerID, token string) error {
	ct, err := s.pool.Exec(ctx,
		`UPDATE note_shares AS ns
		 SET revoked_at = now()
		 FROM notes AS n
		 WHERE ns.token = $2
		   AND ns.note_id = n.id
		   AND n.owner_id = $1
		   AND n.deleted_at IS NULL
		   AND ns.revoked_at IS NULL`,
		ownerID, token)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListSharesForNote returns all shares for the owner's live note, including
// revoked and expired tokens.
func (s *Store) ListSharesForNote(ctx context.Context, ownerID, noteID string) ([]model.Share, error) {
	ok, err := s.noteOwnedLive(ctx, ownerID, noteID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNotFound
	}

	rows, err := s.pool.Query(ctx,
		`SELECT id, token, note_id, owner_id, created_at, expires_at, revoked_at
		 FROM note_shares
		 WHERE owner_id = $1 AND note_id = $2
		 ORDER BY created_at, id`,
		ownerID, noteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []model.Share{}
	for rows.Next() {
		share, err := scanShare(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, share)
	}
	return out, rows.Err()
}

// GetActiveShare looks up a public share token and returns it only if it is
// unrevoked and unexpired.
func (s *Store) GetActiveShare(ctx context.Context, token string) (*model.Share, error) {
	var share model.Share
	err := s.pool.QueryRow(ctx,
		`SELECT id, token, note_id, owner_id, created_at, expires_at, revoked_at
		 FROM note_shares
		 WHERE token = $1
		   AND revoked_at IS NULL
		   AND (expires_at IS NULL OR expires_at >= now())`,
		token).
		Scan(&share.ID, &share.Token, &share.NoteID, &share.OwnerID, &share.CreatedAt, &share.ExpiresAt, &share.RevokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &share, nil
}
