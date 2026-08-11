package embedded

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// ownerLockSuffix names a lock that sits beside its data directory rather than
// inside it: Postgres owns the contents of the data directory and a stray file
// there invites trouble. The data directory's own name is part of the lock name,
// so sibling data directories do not end up sharing one lock.
const ownerLockSuffix = ".owner.lock"

// ownerLock is an inter-process lock over one embedded Postgres data directory.
//
// It exists because two instances legitimately manipulate the same directory
// around a crash (#585): the relaunched app reaps whatever postmaster it finds
// and starts its own, while the previous instance is still running its shutdown.
// Binding shutdown to a captured pid stops the old instance killing the new
// postmaster, but it does not stop the two from interleaving their reap/start and
// stop sequences -- which still produced an intermittently dead server.
//
// flock is the right primitive here because the kernel releases it when the
// holder dies, however it dies. That matters: the case being defended against is
// precisely a process that was SIGKILLed and ran no cleanup.
type ownerLock struct {
	f *os.File
}

func ownerLockPath(dataDir string) string {
	return filepath.Join(filepath.Dir(dataDir), filepath.Base(dataDir)+ownerLockSuffix)
}

// acquireOwnerLock blocks until it holds the lock for dataDir or timeout elapses.
//
// A timeout is a real failure, not something to proceed past: it means another
// live process still owns this directory, and starting anyway is what the lock
// exists to prevent.
func acquireOwnerLock(dataDir string, timeout time.Duration) (*ownerLock, error) {
	path := ownerLockPath(dataDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create lock dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open owner lock: %w", err)
	}

	deadline := time.Now().Add(timeout)
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return &ownerLock{f: f}, nil
		}
		if !isLockBusy(err) {
			_ = f.Close()
			return nil, fmt.Errorf("lock %q: %w", path, err)
		}
		if time.Now().After(deadline) {
			_ = f.Close()
			return nil, fmt.Errorf("embedded postgres: data directory %q is still owned by another process after %s", dataDir, timeout)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func isLockBusy(err error) bool {
	return err == syscall.EWOULDBLOCK || err == syscall.EAGAIN
}

// release drops the lock. Closing the file releases it too, so this is safe to
// call more than once and safe to skip if the process dies.
func (l *ownerLock) release() {
	if l == nil || l.f == nil {
		return
	}
	_ = syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	_ = l.f.Close()
	l.f = nil
}

// retainedLocks keeps deliberately-unreleased locks reachable for the life of the
// process.
//
// os.File carries a finalizer that closes its descriptor, and closing the
// descriptor releases the flock. So a lock we intend to hold -- because shutdown
// could not confirm the postmaster is gone -- would be silently released by the
// garbage collector once the last reference dropped, which is precisely the
// invariant it exists to uphold. Holding a reference here is what makes "keep it
// until this process dies" true rather than aspirational.
var retainedLocks struct {
	mu    sync.Mutex
	locks []*ownerLock
}

func retainOwnerLock(l *ownerLock) {
	if l == nil || l.f == nil {
		return
	}
	retainedLocks.mu.Lock()
	retainedLocks.locks = append(retainedLocks.locks, l)
	retainedLocks.mu.Unlock()
}
