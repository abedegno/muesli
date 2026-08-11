package embedded

import (
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
