package embedded

import (
	"errors"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestOwnerLockExcludesASecondHolder(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")

	first, err := acquireOwnerLock(dataDir, time.Second)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	t.Cleanup(first.release)

	// Same process, same file: flock is per open-file-description, so a second
	// open must still be excluded.
	if _, err := acquireOwnerLock(dataDir, 200*time.Millisecond); err == nil {
		t.Fatal("acquired a lock already held; instances could interleave on the data dir")
	}
}

func TestOwnerLockReleasedIsReacquirable(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")

	first, err := acquireOwnerLock(dataDir, time.Second)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	first.release()

	second, err := acquireOwnerLock(dataDir, time.Second)
	if err != nil {
		t.Fatalf("re-acquire after release: %v", err)
	}
	second.release()
}

// A failed shutdown must NOT hand the directory on: releasing while our postmaster
// is still alive lets the next instance start alongside it, which is the hazard
// the lock exists to prevent.
func TestShouldReleaseOwnerLockOnlyWhenExitIsConfirmed(t *testing.T) {
	alive := exec.Command("sleep", "30")
	if err := alive.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	go func() { _ = alive.Wait() }() // reap: a zombie still answers signal 0
	t.Cleanup(func() { _ = alive.Process.Kill() })

	if shouldReleaseOwnerLock(errors.New("shutdown failed"), alive.Process.Pid) {
		t.Fatal("released the lock while the postmaster was still alive")
	}
	if !shouldReleaseOwnerLock(nil, alive.Process.Pid) {
		t.Fatal("withheld the lock after a successful shutdown")
	}
	// Errored, but the process is demonstrably gone: safe to hand on.
	if !shouldReleaseOwnerLock(errors.New("shutdown failed"), 4194303) {
		t.Fatal("withheld the lock for a process that no longer exists")
	}
	if !shouldReleaseOwnerLock(errors.New("shutdown failed"), 0) {
		t.Fatal("withheld the lock with no captured pid")
	}
}

// The case the reviewer identified: the embedded library launched a postmaster,
// failed later, failed its own cleanup, and returned an error -- so no pid was
// ever captured while a postmaster is alive. Releasing the lock there hands the
// directory to a second instance alongside a live database.
func TestPostmasterStillRunningWithoutACapturedPID(t *testing.T) {
	dataDir := t.TempDir()

	live := exec.Command("bash", "-c", `exec -a "$0" sleep 30`, "postgres -D "+dataDir)
	if err := live.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	go func() { _ = live.Wait() }() // reap: a zombie still answers signal 0
	t.Cleanup(func() { _ = live.Process.Kill() })
	writePostmasterPID(t, dataDir, live.Process.Pid)

	if !postmasterStillRunning(dataDir, 0) {
		t.Fatal("missed a live postmaster because no pid was captured; the lock would be released")
	}

	// A pid file naming something that is not our postgres must not hold the lock.
	other := t.TempDir()
	writePostmasterPID(t, other, live.Process.Pid) // live, but serving a different dir
	if postmasterStillRunning(other, 0) {
		t.Fatal("treated an unrelated process as this directory's postmaster")
	}

	// No pid file at all.
	if postmasterStillRunning(t.TempDir(), 0) {
		t.Fatal("claimed a postmaster with no pid file present")
	}
}
