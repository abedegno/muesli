package embedded

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestPgvectorIntegration(t *testing.T) {
	if os.Getenv("MUESLI_EMBEDDED_IT") == "" || os.Getenv("MUESLI_EMBEDDED_PGVECTOR_DIR") == "" {
		t.Skip("set MUESLI_EMBEDDED_IT and MUESLI_EMBEDDED_PGVECTOR_DIR to run embedded pgvector integration tests")
	}

	ctx := context.Background()
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")

	port, err := FreeLoopbackPort()
	if err != nil {
		t.Fatalf("FreeLoopbackPort() error: %v", err)
	}

	pg, err := StartPostgres(ctx, dataDir, port)
	if err != nil {
		t.Fatalf("StartPostgres() error: %v", err)
	}
	defer func() {
		if err := pg.Stop(ctx); err != nil {
			t.Errorf("pg.Stop() error: %v", err)
		}
	}()

	targetRoot := pg.installRoot()
	libDir := filepath.Join(targetRoot, "lib")
	shareExtDir := filepath.Join(targetRoot, "share", "extension")

	installed, err := pgvectorControlInstalled(shareExtDir)
	if err != nil {
		t.Fatalf("pgvectorControlInstalled() error: %v", err)
	}
	if !installed {
		if err := InstallPgvector(libDir, shareExtDir, os.Getenv("MUESLI_EMBEDDED_PGVECTOR_DIR")); err != nil {
			t.Fatalf("InstallPgvector() error: %v", err)
		}
	}

	if err := EnsureExtension(ctx, pg.URL()); err != nil {
		t.Fatalf("EnsureExtension() error: %v", err)
	}

	conn, err := pgx.Connect(ctx, pg.URL())
	if err != nil {
		t.Fatalf("pgx.Connect() error: %v", err)
	}
	defer func() {
		if err := conn.Close(ctx); err != nil {
			t.Errorf("close connection error: %v", err)
		}
	}()

	rows, err := conn.Query(ctx, "SELECT '[1,2,3]'::vector")
	if err != nil {
		t.Fatalf("Query() error: %v", err)
	}
	defer rows.Close()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			t.Fatalf("rows.Next() error: %v", err)
		}
		t.Fatal("SELECT '[1,2,3]'::vector returned no rows")
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err() error: %v", err)
	}
}
