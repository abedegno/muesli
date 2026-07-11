package worker

import (
	"context"
	"log/slog"
	"time"
)

// trashPurgeInterval is how often the auto-purge runs.
const trashPurgeInterval = 6 * time.Hour

// ExpiredPurger permanently removes notes trashed longer than olderThan ago,
// returning their audio object keys for blob cleanup.
type ExpiredPurger interface {
	PurgeExpired(ctx context.Context, olderThan time.Duration) ([]string, error)
	PurgeExpiredFolders(ctx context.Context, olderThan time.Duration) (int, error)
	PurgeExpiredSmartLists(ctx context.Context, olderThan time.Duration) (int, error)
}

// BlobDeleter removes a stored object by key.
type BlobDeleter interface {
	Delete(key string) error
}

// runTrashPurgeOnce purges expired trashed notes and deletes their audio blobs.
func runTrashPurgeOnce(ctx context.Context, p ExpiredPurger, d BlobDeleter, olderThan time.Duration) {
	// Note and folder purges are independent: a note-purge failure must not skip
	// the folder purge (each self-heals on the next tick otherwise).
	if keys, err := p.PurgeExpired(ctx, olderThan); err != nil {
		slog.ErrorContext(ctx, "trash purge: notes", "error", err)
	} else {
		for _, k := range keys {
			if err := d.Delete(k); err != nil {
				slog.ErrorContext(ctx, "trash purge: audio blob", "audio_key", k, "error", err)
			}
		}
		if len(keys) > 0 {
			slog.InfoContext(ctx, "trash purge: removed expired notes", "count", len(keys))
		}
	}
	if n, err := p.PurgeExpiredFolders(ctx, olderThan); err != nil {
		slog.ErrorContext(ctx, "trash purge: folders", "error", err)
	} else if n > 0 {
		slog.InfoContext(ctx, "trash purge: removed expired folders", "count", n)
	}
	if n, err := p.PurgeExpiredSmartLists(ctx, olderThan); err != nil {
		slog.ErrorContext(ctx, "trash purge: smart lists", "error", err)
	} else if n > 0 {
		slog.InfoContext(ctx, "trash purge: removed expired smart lists", "count", n)
	}
}

// RunTrashPurger runs the auto-purge on start and then every trashPurgeInterval
// until ctx is cancelled. retention controls how old a trashed item must be
// before it is permanently deleted.
func RunTrashPurger(ctx context.Context, p ExpiredPurger, d BlobDeleter, retention time.Duration) {
	runTrashPurgeOnce(ctx, p, d, retention)
	t := time.NewTicker(trashPurgeInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			runTrashPurgeOnce(ctx, p, d, retention)
		}
	}
}
