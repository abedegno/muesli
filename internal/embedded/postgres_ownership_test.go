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
	go func() { _ = replacement.Wait() }() // reap: a zombie still answers signal 0
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

	// A live stand-in whose argv satisfies the identity check, so Stop treats it as
	// our postmaster and must actually signal it. Without a real process this test
	// passes trivially via the "already gone" path and proves nothing.
	// bash, and argv via $0: `exec -a` is a bashism and fails under dash, which is
	// /bin/sh on the Ubuntu CI runners. Matches postgres_test.go's fixture.
	owned := exec.Command("bash", "-c", `exec -a "$0" sleep 30`, "postgres -D "+dataDir)
	if err := owned.Start(); err != nil {
		t.Fatalf("start owned: %v", err)
	}
	go func() { _ = owned.Wait() }() // reap: a zombie still answers signal 0
	t.Cleanup(func() { _ = owned.Process.Kill() })
	writePostmasterPID(t, dataDir, owned.Process.Pid)

	p := &PG{dataDir: dataDir, pid: owned.Process.Pid, ep: newEmbeddedPostgres(dataDir, 5999, "pw")}

	if err := p.Stop(context.Background()); err != nil {
		t.Fatalf("Stop err = %v, want nil for an owned postmaster", err)
	}
	if processAlive(owned.Process.Pid) {
		t.Fatal("did not stop the postmaster this instance owns; a clean quit would leak it")
	}
}

func TestStopOwnPostmasterRevalidatesImmediatelyBeforeSignal(t *testing.T) {
	dataDir := t.TempDir()
	owned := exec.Command("bash", "-c", `exec -a "$0" sleep 30`, "postgres -D "+dataDir)
	if err := owned.Start(); err != nil {
		t.Fatalf("start owned: %v", err)
	}
	go func() { _ = owned.Wait() }()
	t.Cleanup(func() { _ = owned.Process.Kill() })

	bystander := exec.Command("sleep", "30")
	if err := bystander.Start(); err != nil {
		t.Fatalf("start bystander: %v", err)
	}
	go func() { _ = bystander.Wait() }()
	t.Cleanup(func() { _ = bystander.Process.Kill() })

	originalIdentityCheck := processIsPostgresFor
	t.Cleanup(func() { processIsPostgresFor = originalIdentityCheck })
	checks := 0
	processIsPostgresFor = func(pid int, checkedDataDir string) bool {
		checks++
		return checks == 1
	}

	p := &PG{dataDir: dataDir, pid: owned.Process.Pid}
	err := p.stopOwnPostmaster(context.Background())
	if !errors.Is(err, ErrPostmasterNotOwned) {
		t.Fatalf("stopOwnPostmaster err = %v, want ErrPostmasterNotOwned", err)
	}
	if checks != 2 {
		t.Fatalf("identity checks = %d, want initial and immediate pre-signal checks", checks)
	}
	if !processAlive(owned.Process.Pid) {
		t.Fatal("stopOwnPostmaster signalled the former identity after pre-signal validation failed")
	}
	if !processAlive(bystander.Process.Pid) {
		t.Fatal("stopOwnPostmaster signalled the replacement bystander")
	}
}

func TestForceKillRevalidatesImmediatelyBeforeSignal(t *testing.T) {
	dataDir := t.TempDir()
	owned := exec.Command("bash", "-c", `exec -a "$0" sleep 30`, "postgres -D "+dataDir)
	if err := owned.Start(); err != nil {
		t.Fatalf("start owned: %v", err)
	}
	go func() { _ = owned.Wait() }()
	t.Cleanup(func() { _ = owned.Process.Kill() })

	originalIdentityCheck := processIsPostgresFor
	t.Cleanup(func() { processIsPostgresFor = originalIdentityCheck })
	processIsPostgresFor = func(pid int, checkedDataDir string) bool { return false }

	p := &PG{dataDir: dataDir, pid: owned.Process.Pid}
	if err := p.forceKill(); !errors.Is(err, ErrPostmasterNotOwned) {
		t.Fatalf("forceKill err = %v, want ErrPostmasterNotOwned", err)
	}
	if !processAlive(owned.Process.Pid) {
		t.Fatal("forceKill killed the process after pre-signal validation failed")
	}
}

// forceKill must never fall back to the shared pid file.
func TestForceKillRefusesWithoutCapturedPID(t *testing.T) {
	dataDir := t.TempDir()

	bystander := exec.Command("sleep", "30")
	if err := bystander.Start(); err != nil {
		t.Fatalf("start bystander: %v", err)
	}
	go func() { _ = bystander.Wait() }() // reap: a zombie still answers signal 0
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
