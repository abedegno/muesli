package db

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5" // registers the pgx5:// migrate driver via init()
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Connect opens a pgx connection pool.
func Connect(ctx context.Context, url string) (*pgxpool.Pool, error) {
	return pgxpool.New(ctx, url)
}

// Migrate applies all up migrations. Safe to call repeatedly.
func Migrate(url string) error {
	src, err := iofs.New(migrationFS, "migrations")
	if err != nil {
		return fmt.Errorf("migration source: %w", err)
	}
	m, err := migrate.NewWithSourceInstance("iofs", src, "pgx5://"+stripScheme(url))
	if err != nil {
		return fmt.Errorf("migrate init: %w", err)
	}
	// Close the migrate instance so its dedicated DB connection is released as
	// soon as Migrate returns. Without this, every call (notably one per test
	// schema in the suite) leaks a connection until GC, which accumulates and
	// exhausts Postgres max_connections under the test load.
	defer m.Close()
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate up: %w", err)
	}
	return nil
}

// stripScheme converts a postgres:// URL into the host/path the pgx5 migrate driver expects.
func stripScheme(url string) string {
	for _, p := range []string{"postgres://", "postgresql://", "pgx5://", "pgx://"} {
		if len(url) >= len(p) && url[:len(p)] == p {
			return url[len(p):]
		}
	}
	return url
}

var migrationVersionRE = regexp.MustCompile(`^[0-9]+$`)

// validateMigrationNames enforces the invariants golang-migrate relies on:
// every version has exactly one .up.sql and one .down.sql, no two migrations
// share a version prefix, and the prefix is all digits. Non-.sql names are
// ignored. Timestamp-based IDs make collisions unlikely; this is the backstop.
func validateMigrationNames(names []string) error {
	type counts struct{ up, down int }
	seen := map[string]*counts{}
	for _, n := range names {
		if !strings.HasSuffix(n, ".sql") {
			continue
		}
		var dir string
		switch {
		case strings.HasSuffix(n, ".up.sql"):
			dir = "up"
		case strings.HasSuffix(n, ".down.sql"):
			dir = "down"
		default:
			return fmt.Errorf("migration %q must end in .up.sql or .down.sql", n)
		}
		us := strings.IndexByte(n, '_')
		if us <= 0 {
			return fmt.Errorf("migration %q must be <version>_<name>.%s.sql", n, dir)
		}
		version := n[:us]
		if !migrationVersionRE.MatchString(version) {
			return fmt.Errorf("migration %q has non-numeric version prefix %q", n, version)
		}
		c := seen[version]
		if c == nil {
			c = &counts{}
			seen[version] = c
		}
		if dir == "up" {
			c.up++
		} else {
			c.down++
		}
	}
	for v, c := range seen {
		if c.up != 1 || c.down != 1 {
			return fmt.Errorf("migration version %s must have exactly one .up.sql and one .down.sql (got up=%d down=%d)", v, c.up, c.down)
		}
	}
	return nil
}
