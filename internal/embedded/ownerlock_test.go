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
