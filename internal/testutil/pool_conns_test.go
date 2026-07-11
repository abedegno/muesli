package testutil

import "testing"

// TestNewPoolCapsConnections verifies each per-test pool is capped so the suite
// cannot exhaust Postgres max_connections. Skips if TEST_DATABASE_URL is unset
// (NewPool itself skips), so it only asserts when a DB is present (e.g. CI).
func TestNewPoolCapsConnections(t *testing.T) {
	pool := NewPool(t)
	if got := pool.Config().MaxConns; got != 3 {
		t.Fatalf("per-test pool MaxConns = %d, want 3", got)
	}
}
