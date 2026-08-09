package embedded

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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

func TestLoadOrCreatePasswordReusesExistingPassword(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatalf("MkdirAll(%q) error: %v", dataDir, err)
	}

	first, err := loadOrCreatePassword(dataDir)
	if err != nil {
		t.Fatalf("first loadOrCreatePassword() error: %v", err)
	}
	second, err := loadOrCreatePassword(dataDir)
	if err != nil {
		t.Fatalf("second loadOrCreatePassword() error: %v", err)
	}
	if first != second {
		t.Fatalf("password rotated between calls: first=%q second=%q", first, second)
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

func TestReapOrphanPostgresStopsLiveOwnerProcess(t *testing.T) {
	t.Setenv(embeddedPGBinaryEnv, "")
	dataDir := t.TempDir()

	proc := exec.Command("bash", "-c", `exec -a "$0" sleep 30`, "postgres -D "+dataDir)
	if err := proc.Start(); err != nil {
		t.Fatalf("start dummy process: %v", err)
	}
	pid := proc.Process.Pid
	t.Cleanup(func() { _ = proc.Process.Kill() })

	pidBody := fmt.Sprintf("%d\n%s\n1700000000\n55999\n/tmp\nlocalhost\n  123 456\nready\n", pid, dataDir)
	if err := os.WriteFile(filepath.Join(dataDir, postmasterPIDFile), []byte(pidBody), 0o600); err != nil {
		t.Fatalf("write postmaster.pid: %v", err)
	}

	p := &PG{dataDir: dataDir}
	reaped, err := p.reapOrphanPostgres(context.Background())
	if err != nil {
		t.Fatalf("reapOrphanPostgres: %v", err)
	}
	if !reaped {
		t.Fatal("expected reaped=true for a live owner process")
	}
	if processAlive(pid) {
		t.Fatal("expected the orphan process to be stopped")
	}
	if _, err := os.Stat(filepath.Join(dataDir, postmasterPIDFile)); !os.IsNotExist(err) {
		t.Fatalf("expected postmaster.pid removed, stat err=%v", err)
	}
}

func TestReapOrphanPostgresLeavesForeignProcess(t *testing.T) {
	t.Setenv(embeddedPGBinaryEnv, "")
	dataDir := t.TempDir()
	proc := exec.Command("sleep", "30")
	if err := proc.Start(); err != nil {
		t.Fatalf("start dummy process: %v", err)
	}
	pid := proc.Process.Pid
	t.Cleanup(func() { _ = proc.Process.Kill() })

	pidBody := fmt.Sprintf("%d\n%s\n1700000000\n55999\n/tmp\nlocalhost\n  123 456\nready\n", pid, filepath.Join(dataDir, "other"))
	if err := os.WriteFile(filepath.Join(dataDir, postmasterPIDFile), []byte(pidBody), 0o600); err != nil {
		t.Fatalf("write postmaster.pid: %v", err)
	}

	p := &PG{dataDir: dataDir}
	reaped, err := p.reapOrphanPostgres(context.Background())
	if err != nil {
		t.Fatalf("reapOrphanPostgres: %v", err)
	}
	if reaped {
		t.Fatal("expected reaped=false for a foreign owner dir")
	}
	if !processAlive(pid) {
		t.Fatal("expected the foreign process to be left running")
	}
}

func TestReapOrphanPostgresLeavesUnrelatedLiveOwnerProcess(t *testing.T) {
	t.Setenv(embeddedPGBinaryEnv, "")
	dataDir := t.TempDir()
	proc := exec.Command("sleep", "30")
	if err := proc.Start(); err != nil {
		t.Fatalf("start dummy process: %v", err)
	}
	pid := proc.Process.Pid
	t.Cleanup(func() { _ = proc.Process.Kill() })

	pidBody := fmt.Sprintf("%d\n%s\n1700000000\n55999\n/tmp\nlocalhost\n  123 456\nready\n", pid, dataDir)
	if err := os.WriteFile(filepath.Join(dataDir, postmasterPIDFile), []byte(pidBody), 0o600); err != nil {
		t.Fatalf("write postmaster.pid: %v", err)
	}

	p := &PG{dataDir: dataDir}
	reaped, err := p.reapOrphanPostgres(context.Background())
	if err != nil {
		t.Fatalf("reapOrphanPostgres: %v", err)
	}
	if reaped {
		t.Fatal("expected reaped=false for an unrelated live process")
	}
	if !processAlive(pid) {
		t.Fatal("expected the unrelated live process to be left running")
	}
}

func TestProcessIsPostgresForRejectsUnrelatedLiveProcess(t *testing.T) {
	proc := exec.Command("sleep", "30")
	if err := proc.Start(); err != nil {
		t.Fatalf("start dummy process: %v", err)
	}
	t.Cleanup(func() { _ = proc.Process.Kill() })

	if processIsPostgresFor(proc.Process.Pid, t.TempDir()) {
		t.Fatal("processIsPostgresFor() = true for an unrelated live process")
	}
}

func TestProcessIsPostgresForRejectsInvalidAndMissingPIDs(t *testing.T) {
	dataDir := t.TempDir()
	for _, pid := range []int{0, -1, 1_000_000_000} {
		if processIsPostgresFor(pid, dataDir) {
			t.Fatalf("processIsPostgresFor(%d, %q) = true, want false", pid, dataDir)
		}
	}
}

func TestInstallRootUsesEmbeddedBinaryOverride(t *testing.T) {
	pg := &PG{runtimeDir: "/tmp/runtime"}

	t.Setenv(embeddedPGBinaryEnv, "/tmp/binaries")
	if got := pg.installRoot(); got != "/tmp/binaries" {
		t.Fatalf("installRoot() = %q, want %q", got, "/tmp/binaries")
	}

	t.Setenv(embeddedPGBinaryEnv, "")
	if got := pg.installRoot(); got != "/tmp/runtime" {
		t.Fatalf("installRoot() = %q, want %q", got, "/tmp/runtime")
	}
}

func TestPgvectorControlInstalled(t *testing.T) {
	shareExtDir := t.TempDir()

	installed, err := pgvectorControlInstalled(shareExtDir)
	if err != nil {
		t.Fatalf("pgvectorControlInstalled() error: %v", err)
	}
	if installed {
		t.Fatal("pgvectorControlInstalled() = true, want false")
	}

	controlPath := filepath.Join(shareExtDir, "vector.control")
	if err := os.WriteFile(controlPath, []byte("control"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error: %v", controlPath, err)
	}

	installed, err = pgvectorControlInstalled(shareExtDir)
	if err != nil {
		t.Fatalf("pgvectorControlInstalled() error: %v", err)
	}
	if !installed {
		t.Fatal("pgvectorControlInstalled() = false, want true")
	}
}
