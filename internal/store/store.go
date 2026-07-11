package store

import "github.com/jackc/pgx/v5/pgxpool"

// Store provides persistence operations over a pgx pool.
type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Pool returns the underlying pgxpool.Pool. Exposed for tests that need
// low-level DB access (e.g. to drop a table to force a DB error).
func (s *Store) Pool() *pgxpool.Pool { return s.pool }
