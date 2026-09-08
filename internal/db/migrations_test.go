package db

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/abedegno/muesli/internal/testsupport"
	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestValidateMigrationNames(t *testing.T) {
	good := []string{
		"0001_init.up.sql", "0001_init.down.sql",
		"20260701143000_webhook.up.sql", "20260701143000_webhook.down.sql",
	}
	if err := validateMigrationNames(good); err != nil {
		t.Fatalf("valid set rejected: %v", err)
	}

	dupVersion := []string{
		"0016_a.up.sql", "0016_a.down.sql",
		"0016_b.up.sql", "0016_b.down.sql", // same version 0016 as _a -> two ups
	}
	if err := validateMigrationNames(dupVersion); err == nil {
		t.Fatal("expected duplicate version 0016 to be rejected")
	}

	missingDown := []string{"0002_x.up.sql"}
	if err := validateMigrationNames(missingDown); err == nil {
		t.Fatal("expected missing .down.sql to be rejected")
	}

	nonDigit := []string{"abc_x.up.sql", "abc_x.down.sql"}
	if err := validateMigrationNames(nonDigit); err == nil {
		t.Fatal("expected non-numeric version prefix to be rejected")
	}
}

// TestEmbeddedMigrationsValid runs the invariant against what actually ships.
func TestEmbeddedMigrationsValid(t *testing.T) {
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		t.Fatalf("read embedded migrations: %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if err := validateMigrationNames(names); err != nil {
		t.Fatalf("embedded migrations invalid: %v", err)
	}
}

// --- Task 1: team-sharing schema (roles, invites, folder_members) ---
//
// These tests exercise the actual migration SQL against a real, isolated
// Postgres schema (they cannot live in internal/testutil, which imports this
// package). Each test builds its own throwaway schema so it can freely roll
// the team_sharing migration back and forward without disturbing any other
// test's schema.

func newTeamSharingSchema(t *testing.T) (*pgxpool.Pool, func()) {
	t.Helper()
	baseURL := os.Getenv("TEST_DATABASE_URL")
	testsupport.RequireDependency(t, "TEST_DATABASE_URL", baseURL != "", "TEST_DATABASE_URL not set; run `make test-db`")
	ctx := context.Background()

	if err := Migrate(baseURL); err != nil {
		t.Fatalf("base migrate: %v", err)
	}

	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand: %v", err)
	}
	schema := "test_ts_" + hex.EncodeToString(b)

	u, err := url.Parse(baseURL)
	if err != nil {
		t.Fatalf("parse base url: %v", err)
	}
	q := u.Query()
	q.Set("search_path", schema+",public")
	u.RawQuery = q.Encode()
	schemaURL := u.String()

	boot, err := Connect(ctx, baseURL)
	if err != nil {
		t.Fatalf("bootstrap connect: %v", err)
	}
	if _, err := boot.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		boot.Close()
		t.Fatalf("create schema %s: %v", schema, err)
	}
	boot.Close()

	cleanup := func() {
		p, err := Connect(context.Background(), baseURL)
		if err != nil {
			return
		}
		defer p.Close()
		_, _ = p.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+schema+" CASCADE")
	}

	if err := Migrate(schemaURL); err != nil {
		cleanup()
		t.Fatalf("migrate: %v", err)
	}

	pool, err := Connect(ctx, schemaURL)
	if err != nil {
		cleanup()
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() {
		pool.Close()
		cleanup()
	})
	return pool, func() { rollbackTeamSharing(t, schemaURL) }
}

// rollbackTeamSharing undoes exactly the team_sharing migration (it is the
// newest embedded migration at the time this test was written) and then
// reapplies it, so the promotion logic runs against rows the test seeded
// while it was rolled back.
func rollbackTeamSharing(t *testing.T, schemaURL string) {
	t.Helper()
	src, err := iofs.New(migrationFS, "migrations")
	if err != nil {
		t.Fatalf("migration source: %v", err)
	}
	m, err := migrate.NewWithSourceInstance("iofs", src, "pgx5://"+stripScheme(schemaURL))
	if err != nil {
		t.Fatalf("migrate init: %v", err)
	}
	defer m.Close()
	if err := m.Steps(-1); err != nil {
		t.Fatalf("migrate down one step: %v", err)
	}
}

func reapplyTeamSharing(t *testing.T, schemaURL string) {
	t.Helper()
	if err := Migrate(schemaURL); err != nil {
		t.Fatalf("reapply migrate: %v", err)
	}
}

func indexNames(t *testing.T, pool *pgxpool.Pool, table string) map[string]bool {
	t.Helper()
	rows, err := pool.Query(context.Background(),
		`SELECT indexname FROM pg_indexes WHERE tablename=$1 AND schemaname = current_schema()`, table)
	if err != nil {
		t.Fatalf("list indexes on %s: %v", table, err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan index name: %v", err)
		}
		out[name] = true
	}
	return out
}

func TestTeamSharingMigration_RolesInvitesMembership(t *testing.T) {
	pool, _ := newTeamSharingSchema(t)
	ctx := context.Background()

	// --- Indexes exist as the migration created them. ---
	userIdx := indexNames(t, pool, "users")
	if !userIdx["users_created_at_id_idx"] {
		t.Fatalf("missing users_created_at_id_idx: %v", userIdx)
	}
	fmIdx := indexNames(t, pool, "folder_members")
	for _, want := range []string{
		"folder_members_user_folder_idx",
		"folder_members_folder_created_user_idx",
		"folder_members_user_created_folder_idx",
	} {
		if !fmIdx[want] {
			t.Fatalf("missing index %s on folder_members: %v", want, fmIdx)
		}
	}
	// The token hash has ONLY its unique-constraint backing index -- no
	// separate explicit lookup index was added.
	inviteIdx := indexNames(t, pool, "user_invites")
	tokenIdxCount := 0
	for name := range inviteIdx {
		if strings.Contains(name, "token_hash") {
			tokenIdxCount++
		}
	}
	if tokenIdxCount != 1 {
		t.Fatalf("expected exactly one token_hash-backed index, got %d: %v", tokenIdxCount, inviteIdx)
	}

	// --- Invalid role rejected by the CHECK constraint. ---
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, password_hash, role) VALUES (gen_random_uuid(),'bad@example.com','h','root')`); err == nil {
		t.Fatal("expected invalid role to be rejected")
	}

	// --- Hard folder deletion cascades membership removal. ---
	var ownerID, memberID, folderID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (id, email, password_hash) VALUES (gen_random_uuid(),'owner-cascade@example.com','h') RETURNING id`).
		Scan(&ownerID); err != nil {
		t.Fatalf("insert owner: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (id, email, password_hash) VALUES (gen_random_uuid(),'member-cascade@example.com','h') RETURNING id`).
		Scan(&memberID); err != nil {
		t.Fatalf("insert member: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO folders (id, owner_id, name) VALUES (gen_random_uuid(),$1,'Shared') RETURNING id`, ownerID).
		Scan(&folderID); err != nil {
		t.Fatalf("insert folder: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO folder_members (folder_id, user_id, role) VALUES ($1,$2,'viewer')`, folderID, memberID); err != nil {
		t.Fatalf("insert membership: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM folders WHERE id=$1`, folderID); err != nil {
		t.Fatalf("hard delete folder: %v", err)
	}
	var remaining int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM folder_members WHERE folder_id=$1`, folderID).Scan(&remaining); err != nil {
		t.Fatalf("count memberships: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("expected cascade to remove membership row, got %d remaining", remaining)
	}
}

func TestTeamSharingMigration_PromotesEarliestUser(t *testing.T) {
	baseURL := os.Getenv("TEST_DATABASE_URL")
	testsupport.RequireDependency(t, "TEST_DATABASE_URL", baseURL != "", "TEST_DATABASE_URL not set; run `make test-db`")
	ctx := context.Background()

	if err := Migrate(baseURL); err != nil {
		t.Fatalf("base migrate: %v", err)
	}
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand: %v", err)
	}
	schema := "test_ts_promote_" + hex.EncodeToString(b)
	u, err := url.Parse(baseURL)
	if err != nil {
		t.Fatalf("parse base url: %v", err)
	}
	q := u.Query()
	q.Set("search_path", schema+",public")
	u.RawQuery = q.Encode()
	schemaURL := u.String()

	boot, err := Connect(ctx, baseURL)
	if err != nil {
		t.Fatalf("bootstrap connect: %v", err)
	}
	if _, err := boot.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		boot.Close()
		t.Fatalf("create schema: %v", err)
	}
	boot.Close()
	t.Cleanup(func() {
		p, err := Connect(context.Background(), baseURL)
		if err != nil {
			return
		}
		defer p.Close()
		_, _ = p.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+schema+" CASCADE")
	})

	if err := Migrate(schemaURL); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	pool, err := Connect(ctx, schemaURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	// Roll the team_sharing migration back so `role`, user_invites, and
	// folder_members are gone -- then insert two REAL user rows with
	// distinct created_at timestamps before reapplying.
	rollbackTeamSharing(t, schemaURL)

	var cols int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM information_schema.columns WHERE table_name='users' AND column_name='role' AND table_schema=current_schema()`).
		Scan(&cols); err != nil {
		t.Fatalf("check role column: %v", err)
	}
	if cols != 0 {
		t.Fatal("expected role column to be dropped after rollback")
	}
	var tableCount int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM information_schema.tables WHERE table_schema=current_schema() AND table_name IN ('user_invites','folder_members')`).
		Scan(&tableCount); err != nil {
		t.Fatalf("check tables dropped: %v", err)
	}
	if tableCount != 0 {
		t.Fatalf("expected user_invites/folder_members dropped after rollback, found %d", tableCount)
	}

	var earlyID, laterID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (id, email, password_hash, created_at) VALUES (gen_random_uuid(),'early@example.com','h', now() - interval '1 hour') RETURNING id`).
		Scan(&earlyID); err != nil {
		t.Fatalf("insert early user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (id, email, password_hash, created_at) VALUES (gen_random_uuid(),'later@example.com','h', now()) RETURNING id`).
		Scan(&laterID); err != nil {
		t.Fatalf("insert later user: %v", err)
	}

	reapplyTeamSharing(t, schemaURL)

	var earlyRole, laterRole string
	if err := pool.QueryRow(ctx, `SELECT role FROM users WHERE id=$1`, earlyID).Scan(&earlyRole); err != nil {
		t.Fatalf("read early role: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT role FROM users WHERE id=$1`, laterID).Scan(&laterRole); err != nil {
		t.Fatalf("read later role: %v", err)
	}
	if earlyRole != "admin" {
		t.Fatalf("earliest user role = %q, want admin", earlyRole)
	}
	if laterRole != "member" {
		t.Fatalf("later user role = %q, want member", laterRole)
	}
}
