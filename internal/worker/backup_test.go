package worker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakeBackupRunner is a backup.Runner stub — never shells out to pg_dump.
type fakeBackupRunner struct {
	calls int
	err   error
}

func (f *fakeBackupRunner) Run(_ context.Context, _, outputPath string) error {
	f.calls++
	if f.err != nil {
		return f.err
	}
	return os.WriteFile(outputPath, []byte("dump"), 0o644)
}

// TestRunBackupScheduler_RunsImmediately verifies that RunBackupScheduler
// creates a backup on entry, before entering the ticker loop, even when ctx
// is cancelled right away.
func TestRunBackupScheduler_RunsImmediately(t *testing.T) {
	dir := t.TempDir()
	r := &fakeBackupRunner{}
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		RunBackupScheduler(ctx, r, "postgres://x", dir, 7, time.Hour)
		close(done)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("RunBackupScheduler did not exit within 3 seconds after context cancellation")
	}

	if r.calls < 1 {
		t.Errorf("Runner.Run calls = %d, want >= 1 (must run immediately on entry)", r.calls)
	}
}

// TestRunBackupScheduler_ExitsOnCancel verifies RunBackupScheduler exits
// promptly on an already-cancelled context, without blocking for the (long)
// configured interval.
func TestRunBackupScheduler_ExitsOnCancel(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	r := &fakeBackupRunner{}
	done := make(chan struct{})
	go func() {
		RunBackupScheduler(ctx, r, "postgres://x", dir, 7, 24*time.Hour)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("RunBackupScheduler did not exit within 3 seconds (should exit on pre-cancelled context)")
	}
}

// TestRunBackupScheduler_TicksAndPrunes verifies that a short interval causes
// a second backup run, and that pruning keeps the dir from growing past the
// configured retention count.
func TestRunBackupScheduler_TicksAndPrunes(t *testing.T) {
	dir := t.TempDir()
	r := &fakeBackupRunner{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		RunBackupScheduler(ctx, r, "postgres://x", dir, 1, 20*time.Millisecond)
		close(done)
	}()

	deadline := time.After(3 * time.Second)
	for {
		if r.calls >= 3 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("Runner.Run calls = %d after 3s, want >= 3", r.calls)
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("RunBackupScheduler did not exit within 3 seconds after cancel")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) > 1 {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("dir has %d files after pruning to retention 1: %v", len(entries), names)
	}
}

// TestRunBackupScheduler_RunnerErrorDoesNotCrashLoop verifies a Runner error
// is swallowed (logged) rather than propagated, so the ticker loop keeps
// going on the next tick.
func TestRunBackupScheduler_RunnerErrorDoesNotCrashLoop(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sub")
	r := &fakeBackupRunner{err: errors.New("pg_dump exploded")}
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		RunBackupScheduler(ctx, r, "postgres://x", dir, 7, time.Hour)
		close(done)
	}()
	cancel()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("RunBackupScheduler did not exit within 3 seconds")
	}
	if r.calls < 1 {
		t.Errorf("Runner.Run calls = %d, want >= 1 even though it errors", r.calls)
	}
}
