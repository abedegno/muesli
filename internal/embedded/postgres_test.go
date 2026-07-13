package embedded

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestFormatURL(t *testing.T) {
	got := formatURL("test-password", 54321)
	want := "postgres://postgres:test-password@127.0.0.1:54321/postgres?sslmode=disable"
	if got != want {
		t.Fatalf("formatURL() = %q, want %q", got, want)
	}
}

func TestGeneratePassword(t *testing.T) {
	const samples = 16

	seen := make(map[string]struct{}, samples)
	for i := 0; i < samples; i++ {
		got, err := generatePassword()
		if err != nil {
			t.Fatalf("generatePassword() error: %v", err)
		}
		if got == "" {
			t.Fatal("generatePassword() returned an empty password")
		}
		if strings.ContainsAny(got, " \t\n\r") {
			t.Fatalf("generatePassword() returned whitespace: %q", got)
		}
		if _, ok := seen[got]; ok {
			t.Fatalf("generatePassword() returned a duplicate password: %q", got)
		}
		seen[got] = struct{}{}
	}
}

func TestRemoveStalePostmasterPID(t *testing.T) {
	dataDir := t.TempDir()
	pidPath := filepath.Join(dataDir, postmasterPIDFile)

	if err := os.WriteFile(pidPath, []byte("999999\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error: %v", pidPath, err)
	}

	removed, err := removeStalePostmasterPID(dataDir)
	if err != nil {
		t.Fatalf("removeStalePostmasterPID() error: %v", err)
	}
	if !removed {
		t.Fatal("removeStalePostmasterPID() = false, want true")
	}
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Fatalf("postmaster.pid still exists after removal, stat err=%v", err)
	}
}

func TestRemoveStalePostmasterPIDKeepsLivePID(t *testing.T) {
	dataDir := t.TempDir()
	pidPath := filepath.Join(dataDir, postmasterPIDFile)

	if err := os.WriteFile(pidPath, []byte(fmt.Sprintf("%d\n", os.Getpid())), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error: %v", pidPath, err)
	}

	removed, err := removeStalePostmasterPID(dataDir)
	if err != nil {
		t.Fatalf("removeStalePostmasterPID() error: %v", err)
	}
	if removed {
		t.Fatal("removeStalePostmasterPID() = true, want false for live PID")
	}
	if _, err := os.Stat(pidPath); err != nil {
		t.Fatalf("postmaster.pid unexpectedly removed: %v", err)
	}
}

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
