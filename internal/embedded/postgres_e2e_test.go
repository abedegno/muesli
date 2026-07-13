package embedded

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestEmbeddedPostgresIntegration(t *testing.T) {
	if os.Getenv("MUESLI_EMBEDDED_IT") == "" {
		t.Skip("set MUESLI_EMBEDDED_IT to run embedded Postgres integration tests")
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

	conn, err := pgx.Connect(ctx, pg.URL())
	if err != nil {
		t.Fatalf("pgx.Connect() error: %v", err)
	}
	var one int
	if err := conn.QueryRow(ctx, "SELECT 1").Scan(&one); err != nil {
		_ = conn.Close(ctx)
		t.Fatalf("SELECT 1 error: %v", err)
	}
	if one != 1 {
		_ = conn.Close(ctx)
		t.Fatalf("SELECT 1 = %d, want 1", one)
	}
	if err := conn.Close(ctx); err != nil {
		t.Fatalf("close first connection error: %v", err)
	}

	if err := pg.Stop(ctx); err != nil {
		t.Fatalf("first Stop() error: %v", err)
	}

	stalePID := filepath.Join(dataDir, postmasterPIDFile)
	if err := os.WriteFile(stalePID, []byte("999999\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error: %v", stalePID, err)
	}

	pg2, err := StartPostgres(ctx, dataDir, port)
	if err != nil {
		t.Fatalf("second StartPostgres() error: %v", err)
	}

	conn2, err := pgx.Connect(ctx, pg2.URL())
	if err != nil {
		t.Fatalf("second pgx.Connect() error: %v", err)
	}
	var two int
	if err := conn2.QueryRow(ctx, "SELECT 1").Scan(&two); err != nil {
		_ = conn2.Close(ctx)
		t.Fatalf("second SELECT 1 error: %v", err)
	}
	if two != 1 {
		_ = conn2.Close(ctx)
		t.Fatalf("second SELECT 1 = %d, want 1", two)
	}
	if err := conn2.Close(ctx); err != nil {
		t.Fatalf("close second connection error: %v", err)
	}

	if err := pg2.Stop(ctx); err != nil {
		t.Fatalf("second Stop() error: %v", err)
	}

	if processAlive(pg2.pid) {
		t.Fatalf("postgres process %d still appears alive after Stop()", pg2.pid)
	}

	deadline := time.Now().Add(5 * time.Second)
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(pg2.port))
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err != nil {
			break
		}
		_ = conn.Close()
		time.Sleep(100 * time.Millisecond)
	}
	if conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond); err == nil {
		_ = conn.Close()
		t.Fatalf("postgres still listening on %s after Stop()", addr)
	}
}
