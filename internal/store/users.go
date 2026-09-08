package store

import (
	"context"
	"errors"

	"time"

	"github.com/abedegno/muesli/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ErrNotFound is returned when a row does not exist.
var ErrNotFound = errors.New("not found")

// setupAdvisoryLockKey serializes first-run setup across replicas.
const setupAdvisoryLockKey int64 = 7293161372302018804

// CreateUser creates an ordinary deployment member. Used for /api/setup's
// account is NOT this -- see CreateFirstUser -- and for tests seeding
// additional users directly. Invite acceptance uses AcceptInvite instead, so
// it can write the invite's own role.
func (s *Store) CreateUser(ctx context.Context, email, passwordHash string) (model.User, error) {
	u := model.User{ID: uuid.NewString(), Email: email, PasswordHash: passwordHash, Role: model.RoleMember}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO users (id, email, password_hash, role) VALUES ($1,$2,$3,$4) RETURNING created_at`,
		u.ID, u.Email, u.PasswordHash, u.Role).Scan(&u.CreatedAt)
	if err != nil {
		return model.User{}, err
	}
	return u, nil
}

func (s *Store) CreateFirstUser(ctx context.Context, email, passwordHash string) (model.User, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.User{}, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, setupAdvisoryLockKey); err != nil {
		return model.User{}, err
	}

	var n int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&n); err != nil {
		return model.User{}, err
	}
	if n > 0 {
		return model.User{}, ErrAlreadySetUp
	}

	u := model.User{ID: uuid.NewString(), Email: email, PasswordHash: passwordHash, Role: model.RoleAdmin}
	if err := tx.QueryRow(ctx,
		`INSERT INTO users (id, email, password_hash, role) VALUES ($1,$2,$3,$4) RETURNING created_at`,
		u.ID, u.Email, u.PasswordHash, u.Role).Scan(&u.CreatedAt); err != nil {
		return model.User{}, err
	}
	return u, tx.Commit(ctx)
}

func (s *Store) GetUserByEmail(ctx context.Context, email string) (model.User, error) {
	var u model.User
	err := s.pool.QueryRow(ctx,
		`SELECT id, email, password_hash, role, created_at FROM users WHERE email=$1`, email).
		Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Role, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.User{}, ErrNotFound
	}
	return u, err
}

// GetUser returns one user by id, regardless of role.
func (s *Store) GetUser(ctx context.Context, id string) (model.User, error) {
	var u model.User
	err := s.pool.QueryRow(ctx,
		`SELECT id, email, password_hash, role, created_at FROM users WHERE id=$1`, id).
		Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Role, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.User{}, ErrNotFound
	}
	return u, err
}

// GetUserRole returns id's current role, read fresh from the database every
// call -- auth.RequireRole relies on this having no cache so a role change
// takes effect on the very next request.
func (s *Store) GetUserRole(ctx context.Context, id string) (string, error) {
	var role string
	err := s.pool.QueryRow(ctx, `SELECT role FROM users WHERE id=$1`, id).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return role, err
}

// ListUsers returns up to limit users ordered by (created_at,id), the only
// ordering every user list in this API uses. When afterID is non-empty the
// results start strictly after (afterCreatedAt,afterID) (keyset pagination);
// callers pass limit+1 to detect whether another page follows.
func (s *Store) ListUsers(ctx context.Context, limit int, afterCreatedAt time.Time, afterID string) ([]model.User, error) {
	var rows pgx.Rows
	var err error
	if afterID == "" {
		rows, err = s.pool.Query(ctx,
			`SELECT id, email, role, created_at FROM users ORDER BY created_at, id LIMIT $1`, limit)
	} else {
		rows, err = s.pool.Query(ctx,
			`SELECT id, email, role, created_at FROM users
			 WHERE (created_at, id) > ($1, $2)
			 ORDER BY created_at, id LIMIT $3`, afterCreatedAt, afterID, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.User{}
	for rows.Next() {
		var u model.User
		if err := rows.Scan(&u.ID, &u.Email, &u.Role, &u.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// roleAdvisoryLockKey serializes every role change so two concurrent
// PATCHes (e.g. two admins each demoting a different admin at the same
// instant) count the admin set under one consistent lock rather than racing
// each other's read of "how many admins are left".
const roleAdvisoryLockKey int64 = 830111213424627261

// ErrLastAdmin is returned by SetUserRole when the change would leave the
// deployment with zero admins.
var ErrLastAdmin = errors.New("cannot demote the last admin")

// SetUserRole changes id's role, validating it, locking the deployment-wide
// admin set (roleAdvisoryLockKey) and then the target row, so concurrent
// role changes serialize. Reapplying the current role is a no-op success.
// Demoting the last remaining admin returns ErrLastAdmin.
func (s *Store) SetUserRole(ctx context.Context, id, role string) error {
	if role != model.RoleAdmin && role != model.RoleMember {
		return ValidationError("invalid role")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, roleAdvisoryLockKey); err != nil {
		return err
	}

	var current string
	err = tx.QueryRow(ctx, `SELECT role FROM users WHERE id=$1 FOR UPDATE`, id).Scan(&current)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if current == role {
		return tx.Commit(ctx)
	}
	if current == model.RoleAdmin && role == model.RoleMember {
		var admins int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM users WHERE role=$1`, model.RoleAdmin).Scan(&admins); err != nil {
			return err
		}
		if admins <= 1 {
			return ErrLastAdmin
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE users SET role=$1 WHERE id=$2`, role, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&n)
	return n, err
}
