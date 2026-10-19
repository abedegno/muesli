package store

import (
	"context"
	"errors"

	"github.com/abedegno/muesli/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ErrNotFound is returned when a row does not exist.
var ErrNotFound = errors.New("not found")

// setupAdvisoryLockKey serializes first-run setup across replicas.
const setupAdvisoryLockKey int64 = 7293161372302018804

func (s *Store) CreateUser(ctx context.Context, email, passwordHash string) (model.User, error) {
	u := model.User{ID: uuid.NewString(), Email: email, PasswordHash: passwordHash}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO users (id, email, password_hash) VALUES ($1,$2,$3) RETURNING created_at, role`,
		u.ID, u.Email, u.PasswordHash).Scan(&u.CreatedAt, &u.Role)
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

	u := model.User{ID: uuid.NewString(), Email: email, PasswordHash: passwordHash}
	if err := tx.QueryRow(ctx,
		`INSERT INTO users (id, email, password_hash, role) VALUES ($1,$2,$3,'admin') RETURNING created_at, role`,
		u.ID, u.Email, u.PasswordHash).Scan(&u.CreatedAt, &u.Role); err != nil {
		return model.User{}, err
	}
	return u, tx.Commit(ctx)
}

func (s *Store) GetUserByEmail(ctx context.Context, email string) (model.User, error) {
	var u model.User
	err := s.pool.QueryRow(ctx,
		`SELECT id, email, password_hash, created_at, role FROM users WHERE email=$1`, email).
		Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt, &u.Role)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.User{}, ErrNotFound
	}
	return u, err
}

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&n)
	return n, err
}

func (s *Store) IsAdmin(ctx context.Context, id string) (bool, error) {
	var admin bool
	err := s.pool.QueryRow(ctx, `SELECT role='admin' FROM users WHERE id=$1`, id).Scan(&admin)
	return admin, err
}

var ErrLastAdmin = errors.New("the last administrator cannot be demoted")

func (s *Store) SetUserRole(ctx context.Context, email, role string) (model.User, error) {
	if role != "admin" && role != "member" {
		return model.User{}, errors.New("invalid role")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.User{}, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `LOCK TABLE users IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return model.User{}, err
	}
	var u model.User
	err = tx.QueryRow(ctx, `SELECT id,email,password_hash,created_at,role FROM users WHERE email=$1`, email).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt, &u.Role)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.User{}, ErrNotFound
	}
	if err != nil {
		return model.User{}, err
	}
	if u.Role == "admin" && role == "member" {
		var count int
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM users WHERE role='admin'`).Scan(&count); err != nil {
			return model.User{}, err
		}
		if count <= 1 {
			return model.User{}, ErrLastAdmin
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE users SET role=$1 WHERE id=$2`, role, u.ID); err != nil {
		return model.User{}, err
	}
	u.Role = role
	return u, tx.Commit(ctx)
}
