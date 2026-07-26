package db_test

import (
	"context"
	"os"
	"testing"

	"github.com/abedegno/muesli/internal/db"
	"github.com/abedegno/muesli/internal/testsupport"
)

func TestConnectAndMigrate(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	testsupport.RequireDependency(t, "TEST_DATABASE_URL", url != "", "TEST_DATABASE_URL not set; run `make test-db`")
	ctx := context.Background()

	if err := db.Migrate(url); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	pool, err := db.Connect(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	// Start from a clean slate: this suite shares one test database with other
	// packages (run serially via `make test` / -p 1), so don't assume emptiness.
	if _, err := pool.Exec(ctx, `TRUNCATE notes CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM notes`).Scan(&n); err != nil {
		t.Fatalf("query notes: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected empty notes table after truncate, got %d", n)
	}
}
