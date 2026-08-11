package embedded

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

// os.File's finalizer closes its descriptor, and closing it releases the flock.
// A lock we deliberately keep must therefore be retained explicitly, or the
// garbage collector quietly undoes the safety invariant.
func TestRetainedOwnerLockSurvivesCollection(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")

	func() {
		l, err := acquireOwnerLock(dataDir, time.Second)
		if err != nil {
			t.Fatalf("acquire: %v", err)
		}
		retainOwnerLock(l) // the failed-shutdown / failed-start path
	}() // l is now unreachable

	runtime.GC()
	runtime.GC()

	if _, err := acquireOwnerLock(dataDir, 200*time.Millisecond); err == nil {
		t.Fatal("lock was released by the garbage collector while it was meant to be held")
	}
}

// The password file lives in the locked directory, so it must not be created
// before the lock is held: two cold starts racing there would each generate a
// password and overwrite the other's, leaving a cluster initialized with one and
// a file containing the other.
func TestPasswordFileIsNotCreatedWithoutTheLock(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")

	restore := ownerLockTimeout
	ownerLockTimeout = 200 * time.Millisecond
	t.Cleanup(func() { ownerLockTimeout = restore })

	held, err := acquireOwnerLock(dataDir, time.Second)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	t.Cleanup(held.release)

	// A second starter must block on the lock and therefore never reach the
	// password file.
	if _, err := StartPostgres(context.Background(), dataDir, 5999); err == nil {
		t.Fatal("started while another instance held the lock")
	}
	if _, statErr := os.Stat(passwordFilePath(dataDir)); statErr == nil {
		t.Fatal("wrote the shared password file without holding the lock")
	}
}

// Losing ownership does not mean our postmaster is gone: postmaster.pid may be
// missing or unreadable while the process we started is still running. Stop must
// keep the lock in that case, or another instance starts alongside a live
// database -- the hazard this whole change exists to prevent.
func TestStopKeepsLockWhenOwnershipIsLostButOurProcessLives(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	ours := exec.Command("sleep", "30")
	if err := ours.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	go func() { _ = ours.Wait() }() // reap: a zombie still answers signal 0
	t.Cleanup(func() { _ = ours.Process.Kill() })

	lock, err := acquireOwnerLock(dataDir, time.Second)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}

	// No postmaster.pid at all -> ownership cannot be established, while the
	// process we started is very much alive.
	p := &PG{
		dataDir: dataDir,
		pid:     ours.Process.Pid,
		ep:      newEmbeddedPostgres(dataDir, 5999, "pw"),
		lock:    lock,
	}

	if err := p.Stop(context.Background()); !errors.Is(err, ErrPostmasterNotOwned) {
		t.Fatalf("Stop err = %v, want ErrPostmasterNotOwned", err)
	}
	if _, err := acquireOwnerLock(dataDir, 200*time.Millisecond); err == nil {
		t.Fatal("released the data directory while our postmaster was still alive")
	}
}
