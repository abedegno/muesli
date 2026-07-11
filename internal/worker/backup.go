package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/abedegno/muesli/internal/backup"
)

// runBackupOnce runs a single backup via r and prunes the backup dir to
// retentionCount. Failures are logged and swallowed so the scheduler
// self-heals on the next tick rather than crashing the process.
func runBackupOnce(ctx context.Context, r backup.Runner, databaseURL, dir string, retentionCount int) {
	info, err := backup.Create(ctx, r, databaseURL, dir)
	if err != nil {
		slog.ErrorContext(ctx, "scheduled backup: create failed", "error", err)
		return
	}
	slog.InfoContext(ctx, "scheduled backup: created", "filename", info.Filename, "size_bytes", info.SizeBytes)
	if err := backup.Prune(dir, retentionCount); err != nil {
		slog.ErrorContext(ctx, "scheduled backup: prune failed", "error", err)
	}
}

// RunBackupScheduler runs a Postgres backup (then prunes to retentionCount)
// immediately on entry, then again every interval, until ctx is cancelled.
// Callers should only start this when both a backup dir and a schedule
// interval are configured (see cmd/muesli/main.go).
func RunBackupScheduler(ctx context.Context, r backup.Runner, databaseURL, dir string, retentionCount int, interval time.Duration) {
	runBackupOnce(ctx, r, databaseURL, dir, retentionCount)
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			runBackupOnce(ctx, r, databaseURL, dir, retentionCount)
		}
	}
}
