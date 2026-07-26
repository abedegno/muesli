package testutil

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"sync"
	"testing"

	"github.com/abedegno/muesli/internal/db"
	"github.com/abedegno/muesli/internal/testsupport"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	// baseOnce ensures the base migration (which installs DB-level extensions
	// like pgvector) runs exactly once per test process. golang-migrate's own
	// advisory lock serialises concurrent runs across different OS processes.
	baseOnce sync.Once
	baseErr  error
)

// NewPool connects to TEST_DATABASE_URL, creates a unique Postgres schema for
// this test, migrates it, and returns a ready pool. The schema (and all its
// tables) is dropped via t.Cleanup, giving each call fully isolated state.
// Locally this skips if TEST_DATABASE_URL is not set; in CI it fails loudly so
// a missing server-job dependency does not silently pass.
func NewPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	baseURL := os.Getenv("TEST_DATABASE_URL")
	testsupport.RequireDependency(t, "TEST_DATABASE_URL", baseURL != "", "TEST_DATABASE_URL not set; run `make test-db`")

	ctx := context.Background()

	// Phase 1: ensure database-level objects (extensions, etc.) exist once.
	// db.Migrate is idempotent and uses an advisory lock, so multiple processes
	// calling this concurrently are safe.
	baseOnce.Do(func() {
		baseErr = db.Migrate(baseURL)
	})
	if baseErr != nil {
		t.Fatalf("base migrate: %v", baseErr)
	}

	// Phase 2: create a unique schema for this test.
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand: %v", err)
	}
	schema := "test_" + hex.EncodeToString(b)

	// Build the schema-qualified URL.
	// search_path=<schema>,public: tables land in <schema> (isolation);
	// types from public extensions (e.g. vector) remain accessible.
	schemaURL, err := withSearchPath(baseURL, schema+",public")
	if err != nil {
		t.Fatalf("build schema URL: %v", err)
	}

	// Create the schema.
	bootPool, err := db.Connect(ctx, baseURL)
	if err != nil {
		t.Fatalf("bootstrap connect: %v", err)
	}
	if _, err := bootPool.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		bootPool.Close()
		t.Fatalf("create schema %s: %v", schema, err)
	}
	bootPool.Close()

	// Register cleanup: drop schema and all its objects.
	t.Cleanup(func() {
		p, err := db.Connect(context.Background(), baseURL)
		if err != nil {
			return
		}
		defer p.Close()
		_, _ = p.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+schema+" CASCADE")
	})

	// Phase 3: migrate into the schema.
	// CREATE EXTENSION IF NOT EXISTS vector is now a no-op (already in public).
	// golang-migrate's advisory lock serialises concurrent calls here too.
	if err := db.Migrate(schemaURL); err != nil {
		t.Fatalf("schema migrate: %v", err)
	}

	// Cap the per-test pool so the many concurrent per-test schemas don't
	// exhaust Postgres max_connections (stock 100). Budget = (parallel tests) x
	// (pool_max_conns); with -parallel 4 and cap 3 that is ~12 plus transient
	// bootstrap pools, far under 100, and stays bounded as the suite grows.
	// This pgx-only param must NOT be on the migrate URL above: golang-migrate
	// forwards unknown params to the server, which rejects pool_max_conns.
	poolURL := schemaURL
	if u, perr := url.Parse(poolURL); perr == nil {
		q := u.Query()
		q.Set("pool_max_conns", "3")
		u.RawQuery = q.Encode()
		poolURL = u.String()
	}
	pool, err := db.Connect(ctx, poolURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// withSearchPath appends (or replaces) the search_path query parameter in a
// Postgres URL. The comma in "schema,public" is URL-encoded so pgx and
// golang-migrate parse it correctly.
func withSearchPath(rawURL, searchPath string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("search_path", searchPath)
	u.RawQuery = q.Encode()
	return u.String(), nil
}
