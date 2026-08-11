package embedded

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func requireEmbeddedIntegration(t *testing.T) {
	t.Helper()
	if os.Getenv("MUESLI_EMBEDDED_IT") == "" {
		t.Skip("set MUESLI_EMBEDDED_IT to run embedded Postgres integration tests")
	}
}

func TestStopOwnPostmasterExitsViaGracefulSignalIntegration(t *testing.T) {
	requireEmbeddedIntegration(t)
	dataDir := t.TempDir()
	owned := exec.Command("bash", "-c", `exec -a "$0" sleep 30`, "postgres -D "+dataDir)
	if err := owned.Start(); err != nil {
		t.Fatalf("start owned: %v", err)
	}
	go func() { _ = owned.Wait() }()
	t.Cleanup(func() { _ = owned.Process.Kill() })

	p := &PG{dataDir: dataDir, pid: owned.Process.Pid}
	started := time.Now()
	if err := p.stopOwnPostmaster(context.Background()); err != nil {
		t.Fatalf("stopOwnPostmaster err = %v, want nil", err)
	}
	if elapsed := time.Since(started); elapsed >= 2*time.Second {
		t.Fatalf("graceful shutdown took %v, want under 2s (SIGKILL fallback starts after %v)", elapsed, pgStopTimeout)
	}
	if processAlive(owned.Process.Pid) {
		t.Fatal("postmaster still alive after graceful shutdown")
	}
}

func TestStopOwnPostmasterForceKillsAfterGracefulTimeoutIntegration(t *testing.T) {
	requireEmbeddedIntegration(t)
	dataDir := t.TempDir()
	ready := filepath.Join(t.TempDir(), "ready")
	owned := exec.Command("bash", "-c", `trap "" INT; : > "$1"; exec -a "$0" sleep 30`, "postgres -D "+dataDir, ready)
	if err := owned.Start(); err != nil {
		t.Fatalf("start SIGINT-ignoring owned process: %v", err)
	}
	go func() { _ = owned.Wait() }()
	t.Cleanup(func() { _ = owned.Process.Kill() })
	for deadline := time.Now().Add(2 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		if _, err := os.Stat(ready); err == nil {
			break
		} else if time.Now().After(deadline) {
			t.Fatalf("wait for fixture readiness: %v", err)
		}
	}

	originalStopTimeout, originalKillWait := pgStopTimeout, killWait
	pgStopTimeout, killWait = 200*time.Millisecond, time.Second
	t.Cleanup(func() { pgStopTimeout, killWait = originalStopTimeout, originalKillWait })

	p := &PG{dataDir: dataDir, pid: owned.Process.Pid}
	started := time.Now()
	if err := p.stopOwnPostmaster(context.Background()); err != nil {
		t.Fatalf("stopOwnPostmaster err = %v, want nil after SIGKILL fallback", err)
	}
	if elapsed := time.Since(started); elapsed < pgStopTimeout {
		t.Fatalf("shutdown took %v, want at least graceful timeout %v before SIGKILL", elapsed, pgStopTimeout)
	}
	if processAlive(owned.Process.Pid) {
		t.Fatal("postmaster still alive after SIGKILL fallback")
	}
}
