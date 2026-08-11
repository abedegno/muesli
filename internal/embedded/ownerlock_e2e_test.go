package embedded

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// Named _e2e_ deliberately: it spawns real processes and must wait on them, and
// scripts/check-test-determinism.sh excludes paths containing e2e. Same reason
// parentdeath_e2e_test.go carries the name.

// The case the lock exists for: the holder was SIGKILLed and ran no cleanup. The
// kernel must release the lock so the relaunched app is not locked out forever.
func TestOwnerLockReleasedWhenHolderIsKilled(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(filepath.Dir(ownerLockPath(dataDir)), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// A separate process that takes the lock and holds it until killed.
	script := `exec 9>"$0"; flock -x 9; sleep 30`
	holder := exec.Command("bash", "-c", script, ownerLockPath(dataDir))
	if err := holder.Start(); err != nil {
		t.Fatalf("start holder: %v", err)
	}
	go func() { _ = holder.Wait() }()

	// Wait until it actually holds the lock, rather than assuming.
	held := false
	for i := 0; i < 100; i++ {
		probe, err := acquireOwnerLock(dataDir, 10*time.Millisecond)
		if err != nil {
			held = true
			break
		}
		probe.release() // else the probe itself becomes the blocking holder
		time.Sleep(20 * time.Millisecond)
	}
	if !held {
		_ = holder.Process.Kill()
		t.Skip("could not establish the external lock holder (flock(1) may be unavailable)")
	}

	if err := holder.Process.Kill(); err != nil {
		t.Fatalf("kill holder: %v", err)
	}

	got, err := acquireOwnerLock(dataDir, 5*time.Second)
	if err != nil {
		t.Fatalf("lock not released when its holder was killed: %v", err)
	}
	got.release()
}
