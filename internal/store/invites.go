package store

import (
	"context"
	"errors"
	"time"

	"github.com/abedegno/muesli/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// CreateInvite persists a new single-use invite. Only tokenHash is ever
// stored; producing and showing the raw token to the issuing admin is the
// API layer's job (internal/api/invites.go), not this store's.
func (s *Store) CreateInvite(ctx context.Context, tokenHash, role, createdBy string, expiresAt time.Time) (model.UserInvite, error) {
	if role != model.RoleAdmin && role != model.RoleMember {
		return model.UserInvite{}, ValidationError("invalid role")
	}
	inv := model.UserInvite{ID: uuid.NewString(), TokenHash: tokenHash, Role: role, CreatedBy: createdBy, ExpiresAt: expiresAt}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO user_invites (id, token_hash, role, created_by, expires_at)
		 VALUES ($1,$2,$3,$4,$5) RETURNING created_at`,
		inv.ID, inv.TokenHash, inv.Role, inv.CreatedBy, inv.ExpiresAt).Scan(&inv.CreatedAt)
	if err != nil {
		return model.UserInvite{}, err
	}
	return inv, nil
}

// GetLiveInviteByHash returns the invite for tokenHash if it exists, is
// unconsumed, and has not expired -- ErrNotFound otherwise. An unknown,
// expired, or already-consumed token is deliberately indistinguishable to a
// caller of GET /api/invites/{token}.
func (s *Store) GetLiveInviteByHash(ctx context.Context, tokenHash string) (model.UserInvite, error) {
	var inv model.UserInvite
	err := s.pool.QueryRow(ctx,
		`SELECT id, token_hash, role, created_by, expires_at, consumed_at, created_at
		 FROM user_invites
		 WHERE token_hash=$1 AND consumed_at IS NULL AND expires_at > now()`, tokenHash).
		Scan(&inv.ID, &inv.TokenHash, &inv.Role, &inv.CreatedBy, &inv.ExpiresAt, &inv.ConsumedAt, &inv.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.UserInvite{}, ErrNotFound
	}
	return inv, err
}

// AcceptInvite atomically consumes a live invite and creates its user in one
// transaction: lock the invite row FOR UPDATE, re-verify it is still
// unconsumed and unexpired under that lock, insert the user with the
// invite's own role, mark the invite consumed, and commit.
//
// Because the lookup is re-evaluated against the WHERE clause when the FOR
// UPDATE lock is granted (standard Postgres read-committed behaviour), two
// concurrent accepts of the SAME token serialize on this row: the first to
// commit consumes it, and the second's re-check then finds zero matching
// rows and gets ErrNotFound -- exactly one caller can ever create a user
// from a given invite. A duplicate email rolls back the whole transaction,
// including consumption, so a failed accept never burns the invite.
func (s *Store) AcceptInvite(ctx context.Context, tokenHash, email, passwordHash string) (model.User, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.User{}, err
	}
	defer tx.Rollback(ctx)

	var inviteID, role string
	err = tx.QueryRow(ctx,
		`SELECT id, role FROM user_invites
		 WHERE token_hash=$1 AND consumed_at IS NULL AND expires_at > now()
		 FOR UPDATE`, tokenHash).Scan(&inviteID, &role)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.User{}, ErrNotFound
	}
	if err != nil {
		return model.User{}, err
	}

	u := model.User{ID: uuid.NewString(), Email: email, PasswordHash: passwordHash, Role: role}
	if err := tx.QueryRow(ctx,
		`INSERT INTO users (id, email, password_hash, role) VALUES ($1,$2,$3,$4) RETURNING created_at`,
		u.ID, u.Email, u.PasswordHash, u.Role).Scan(&u.CreatedAt); err != nil {
		if isUniqueViolation(err) {
			return model.User{}, ErrDuplicate
		}
		return model.User{}, err
	}

	if _, err := tx.Exec(ctx, `UPDATE user_invites SET consumed_at=now() WHERE id=$1`, inviteID); err != nil {
		return model.User{}, err
	}

	return u, tx.Commit(ctx)
}
