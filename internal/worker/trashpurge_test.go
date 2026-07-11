package worker

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakePurgeDeleter implements both ExpiredPurger and BlobDeleter. It returns
// the canned keys on the first PurgeExpired call only, and records all
// PurgeExpired and Delete invocations.
type fakePurgeDeleter struct {
	keys              []string
	purgeErr          error
	purgeCalls        int
	deletedKeys       []string
	lastOlderArg      time.Duration
	folderPurgeCalls  int
	folderOlderArg    time.Duration
	folderPurgedCount int
	smartPurgeCalls   int
	smartOlderArg     time.Duration
	smartPurgedCount  int
}

func (f *fakePurgeDeleter) PurgeExpired(_ context.Context, olderThan time.Duration) ([]string, error) {
	f.purgeCalls++
	f.lastOlderArg = olderThan
	if f.purgeErr != nil {
		return nil, f.purgeErr
	}
	if f.purgeCalls == 1 {
		return f.keys, nil
	}
	return nil, nil
}

func (f *fakePurgeDeleter) PurgeExpiredFolders(_ context.Context, olderThan time.Duration) (int, error) {
	f.folderPurgeCalls++
	f.folderOlderArg = olderThan
	return f.folderPurgedCount, nil
}

func (f *fakePurgeDeleter) PurgeExpiredSmartLists(_ context.Context, olderThan time.Duration) (int, error) {
	f.smartPurgeCalls++
	f.smartOlderArg = olderThan
	return f.smartPurgedCount, nil
}

func (f *fakePurgeDeleter) Delete(key string) error {
	f.deletedKeys = append(f.deletedKeys, key)
	return nil
}

func TestRunTrashPurgeOnceDeletesBlobs(t *testing.T) {
	fake := &fakePurgeDeleter{keys: []string{"notes/a/audio/x.webm"}}

	runTrashPurgeOnce(context.Background(), fake, fake, 30*24*time.Hour)

	if fake.purgeCalls != 1 {
		t.Fatalf("PurgeExpired calls = %d, want 1", fake.purgeCalls)
	}
	if fake.lastOlderArg != 30*24*time.Hour {
		t.Errorf("PurgeExpired olderThan = %v, want %v", fake.lastOlderArg, 30*24*time.Hour)
	}
	if len(fake.deletedKeys) != 1 {
		t.Fatalf("Delete calls = %d, want 1", len(fake.deletedKeys))
	}
	if fake.deletedKeys[0] != "notes/a/audio/x.webm" {
		t.Errorf("Delete key = %q, want %q", fake.deletedKeys[0], "notes/a/audio/x.webm")
	}
}

func TestRunTrashPurgeOncePurgesFolders(t *testing.T) {
	fake := &fakePurgeDeleter{folderPurgedCount: 2}

	runTrashPurgeOnce(context.Background(), fake, fake, 30*24*time.Hour)

	if fake.folderPurgeCalls != 1 {
		t.Fatalf("PurgeExpiredFolders calls = %d, want 1", fake.folderPurgeCalls)
	}
	if fake.folderOlderArg != 30*24*time.Hour {
		t.Errorf("PurgeExpiredFolders olderThan = %v, want %v", fake.folderOlderArg, 30*24*time.Hour)
	}
}

func TestRunTrashPurgeOncePurgesSmartLists(t *testing.T) {
	fake := &fakePurgeDeleter{smartPurgedCount: 3}

	runTrashPurgeOnce(context.Background(), fake, fake, 30*24*time.Hour)

	if fake.smartPurgeCalls != 1 {
		t.Fatalf("PurgeExpiredSmartLists calls = %d, want 1", fake.smartPurgeCalls)
	}
	if fake.smartOlderArg != 30*24*time.Hour {
		t.Errorf("PurgeExpiredSmartLists olderThan = %v, want %v", fake.smartOlderArg, 30*24*time.Hour)
	}
}

// smartListPurger is a self-contained fake that implements both ExpiredPurger
// and BlobDeleter. It stores smart-list rows as id→deletedAt and removes them
// from the map when PurgeExpiredSmartLists is called.
type smartListPurger struct {
	rows       map[string]time.Time
	smartCalls int
}

func (s *smartListPurger) PurgeExpired(_ context.Context, _ time.Duration) ([]string, error) {
	return nil, nil
}

func (s *smartListPurger) PurgeExpiredFolders(_ context.Context, _ time.Duration) (int, error) {
	return 0, nil
}

func (s *smartListPurger) PurgeExpiredSmartLists(_ context.Context, olderThan time.Duration) (int, error) {
	s.smartCalls++
	count := 0
	for id, deletedAt := range s.rows {
		if time.Since(deletedAt) > olderThan {
			delete(s.rows, id)
			count++
		}
	}
	return count, nil
}

func (s *smartListPurger) Delete(_ string) error { return nil }

// TestTrashPurge_SmartList verifies that runTrashPurgeOnce invokes
// PurgeExpiredSmartLists and that expired rows are removed from the store.
func TestTrashPurge_SmartList(t *testing.T) {
	const id = "sl-expired"
	fake := &smartListPurger{
		rows: map[string]time.Time{
			id: time.Now().Add(-8 * 24 * time.Hour),
		},
	}

	runTrashPurgeOnce(context.Background(), fake, fake, 7*24*time.Hour)

	if fake.smartCalls != 1 {
		t.Fatalf("PurgeExpiredSmartLists calls = %d, want 1", fake.smartCalls)
	}
	if _, ok := fake.rows[id]; ok {
		t.Errorf("smart-list %q still present in map after purge, want absent", id)
	}
}

// A note-purge failure must not skip the (independent) folder purge.
func TestRunTrashPurgeOnceFolderPurgeRunsDespiteNoteError(t *testing.T) {
	fake := &fakePurgeDeleter{purgeErr: errors.New("notes db down")}

	runTrashPurgeOnce(context.Background(), fake, fake, 30*24*time.Hour)

	if fake.folderPurgeCalls != 1 {
		t.Fatalf("PurgeExpiredFolders calls = %d, want 1 even when note purge errors", fake.folderPurgeCalls)
	}
}

func TestRunTrashPurgeOnceUsesConfiguredRetention(t *testing.T) {
	fake := &fakePurgeDeleter{}

	runTrashPurgeOnce(context.Background(), fake, fake, 7*24*time.Hour)

	if fake.lastOlderArg != 7*24*time.Hour {
		t.Errorf("PurgeExpired olderThan = %v, want %v", fake.lastOlderArg, 7*24*time.Hour)
	}
	if fake.folderOlderArg != 7*24*time.Hour {
		t.Errorf("PurgeExpiredFolders olderThan = %v, want %v", fake.folderOlderArg, 7*24*time.Hour)
	}
	if fake.smartOlderArg != 7*24*time.Hour {
		t.Errorf("PurgeExpiredSmartLists olderThan = %v, want %v", fake.smartOlderArg, 7*24*time.Hour)
	}
}
