package embedded

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
)

// writePostmasterPID writes a minimal postmaster.pid naming pid as the owner.
// Line 2 is the data directory, which readPostmasterInfo requires.
func writePostmasterPID(t *testing.T, dataDir string, pid int) {
	t.Helper()
	body := strconv.Itoa(pid) + "\n" + dataDir + "\n1700000000\n5432\n/tmp\nlocalhost\n  123 456\nready\n"
	if err := os.WriteFile(filepath.Join(dataDir, postmasterPIDFile), []byte(body), 0o600); err != nil {
		t.Fatalf("write postmaster.pid: %v", err)
	}
}

// The case that produced #585: this instance started a postmaster, a replacement
// instance took the data directory over, and then our deferred Stop ran. Stopping
// here would shut down the replacement's database.
func TestStillOwnsPostmasterFalseAfterTakeover(t *testing.T) {
	dataDir := t.TempDir()
	p := &PG{dataDir: dataDir, pid: 4242}

	writePostmasterPID(t, dataDir, 9999) // someone else owns it now

	if p.stillOwnsPostmaster() {
		t.Fatal("claimed ownership of a postmaster started by another instance")
	}
}

func TestStillOwnsPostmasterTrueForOwnProcess(t *testing.T) {
	dataDir := t.TempDir()
	p := &PG{dataDir: dataDir, pid: 4242}

	writePostmasterPID(t, dataDir, 4242)

	if !p.stillOwnsPostmaster() {
		t.Fatal("disowned the postmaster this instance started; a clean quit would leak it")
	}
}

// Fail closed: no captured pid, or no readable pid file, means we cannot claim
// ownership. Leaking a postmaster someone else is responsible for beats stopping
// a database out from under a live instance.
func TestStillOwnsPostmasterFailsClosed(t *testing.T) {
	dataDir := t.TempDir()

	if (&PG{dataDir: dataDir, pid: 0}).stillOwnsPostmaster() {
		t.Fatal("claimed ownership with no captured pid")
	}

	if (&PG{dataDir: dataDir, pid: 4242}).stillOwnsPostmaster() {
		t.Fatal("claimed ownership with no postmaster.pid present")
	}

	if err := os.WriteFile(filepath.Join(dataDir, postmasterPIDFile), []byte("garbage\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if (&PG{dataDir: dataDir, pid: 4242}).stillOwnsPostmaster() {
		t.Fatal("claimed ownership from an unparseable postmaster.pid")
	}
}

// The behavioural case, driven through the real PG.Stop: a replacement instance
// owns the directory, and Stop must decline AND leave that process alive. The
// helper-only tests above cannot show this -- they never reach Stop.
func TestStopDeclinesAndLeavesReplacementAlive(t *testing.T) {
	dataDir := t.TempDir()

	// Stand-in for the replacement postmaster: a real, live process we can assert on.
	replacement := exec.Command("sleep", "30")
	if err := replacement.Start(); err != nil {
		t.Fatalf("start replacement: %v", err)
	}
	t.Cleanup(func() { _ = replacement.Process.Kill() })
	writePostmasterPID(t, dataDir, replacement.Process.Pid)

	p := &PG{
		dataDir: dataDir,
		pid:     replacement.Process.Pid + 100000, // we started something else
		ep:      newEmbeddedPostgres(dataDir, 5999, "pw"),
	}

	err := p.Stop(context.Background())
	if !errors.Is(err, ErrPostmasterNotOwned) {
		t.Fatalf("Stop err = %v, want ErrPostmasterNotOwned", err)
	}
	if !processAlive(replacement.Process.Pid) {
		t.Fatal("Stop killed a postmaster belonging to another instance (this is #585)")
	}
}

// The other direction: when we DO own it, Stop must not short-circuit. Stopping a
// never-started embedded instance surfaces an error from the library, which is
// proof the guard let the call through rather than declining.
func TestStopProceedsWhenOwned(t *testing.T) {
	dataDir := t.TempDir()
	writePostmasterPID(t, dataDir, 4242)

	p := &PG{dataDir: dataDir, pid: 4242, ep: newEmbeddedPostgres(dataDir, 5999, "pw")}

	err := p.Stop(context.Background())
	if errors.Is(err, ErrPostmasterNotOwned) {
		t.Fatal("declined to stop a postmaster this instance owns; a clean quit would leak it")
	}
}

// forceKill must never fall back to the shared pid file.
func TestForceKillRefusesWithoutCapturedPID(t *testing.T) {
	dataDir := t.TempDir()

	bystander := exec.Command("sleep", "30")
	if err := bystander.Start(); err != nil {
		t.Fatalf("start bystander: %v", err)
	}
	t.Cleanup(func() { _ = bystander.Process.Kill() })
	writePostmasterPID(t, dataDir, bystander.Process.Pid)

	p := &PG{dataDir: dataDir, pid: 0}
	if err := p.forceKill(); !errors.Is(err, ErrPostmasterNotOwned) {
		t.Fatalf("forceKill err = %v, want ErrPostmasterNotOwned", err)
	}
	if !processAlive(bystander.Process.Pid) {
		t.Fatal("forceKill killed a process named only by the shared pid file")
	}
}
