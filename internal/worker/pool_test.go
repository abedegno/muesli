package worker

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/plugin"
)

// TestNewPool_ClampsWorkersToMinOne verifies that NewPool enforces a minimum of
// 1 worker regardless of the value passed. Calling Stop() on a pool that has
// never Run is also safe (nil cancel → no-op).
func TestNewPool_ClampsWorkersToMinOne(t *testing.T) {
	p0 := NewPool(nil, nil, 0)
	if p0.workers != 1 {
		t.Errorf("NewPool(nil,nil,0).workers = %d, want 1", p0.workers)
	}
	p0.Stop() // nil cancel → no-op; must not panic

	pNeg := NewPool(nil, nil, -3)
	if pNeg.workers != 1 {
		t.Errorf("NewPool(nil,nil,-3).workers = %d, want 1", pNeg.workers)
	}
	pNeg.Stop()
}

// TestPool_StopBeforeRun verifies that Stop() on a fresh (never-Run) pool is a
// no-op and does not panic.
func TestPool_StopBeforeRun(t *testing.T) {
	p := NewPool(nil, nil, 2)
	p.Stop() // must not panic: p.cancel is nil
}

// TestPool_RunAndStopIdle verifies that Run returns promptly when the context is
// already cancelled on entry. Because ctx.Done() is already closed, the select
// in loop() always takes the <-ctx.Done() case (not default:), so ClaimJob is
// never called — a nil store is therefore safe here.
func TestPool_RunAndStopIdle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before Run is called

	p := NewPool(nil, nil, 1)
	done := make(chan struct{})
	go func() {
		p.Run(ctx)
		close(done)
	}()

	select {
	case <-done:
		// Good: Run returned after the cancelled context caused the worker to exit.
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return within 3 seconds after context was pre-cancelled")
	}
}

// TestIsRetryable verifies that isRetryable classifies errors correctly:
//   - plain (non-HTTP) errors → true (transport/network failure → retry)
//   - plugin.HTTPError with 4xx → false (client error → fail fast)
//   - plugin.HTTPError with 5xx or 429 → true (upstream error → retry)
func TestIsRetryable(t *testing.T) {
	// Plain (non-HTTP) errors are always retryable.
	if !isRetryable(errors.New("connection refused")) {
		t.Error("plain error should be retryable")
	}

	// 4xx responses are NOT retryable.
	for _, code := range []int{
		http.StatusBadRequest,
		http.StatusUnauthorized,
		http.StatusForbidden,
		http.StatusNotFound,
		http.StatusUnprocessableEntity,
	} {
		if isRetryable(&plugin.HTTPError{StatusCode: code}) {
			t.Errorf("HTTP %d should NOT be retryable", code)
		}
	}

	// 5xx responses ARE retryable.
	for _, code := range []int{
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout,
	} {
		if !isRetryable(&plugin.HTTPError{StatusCode: code}) {
			t.Errorf("HTTP %d should be retryable", code)
		}
	}

	// 429 Too Many Requests is also retryable.
	if !isRetryable(&plugin.HTTPError{StatusCode: http.StatusTooManyRequests}) {
		t.Error("HTTP 429 should be retryable")
	}
}

// TestRunTrashPurger_RunsImmediately verifies that RunTrashPurger calls
// runTrashPurgeOnce immediately on entry, BEFORE entering the ticker loop.
// We reuse fakePurgeDeleter from trashpurge_test.go (same package).
func TestRunTrashPurger_RunsImmediately(t *testing.T) {
	fake := &fakePurgeDeleter{}
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		RunTrashPurger(ctx, fake, fake, 30*24*time.Hour)
		close(done)
	}()

	// Cancel immediately; RunTrashPurger still calls runTrashPurgeOnce first
	// (unconditionally, before the ticker loop), then the for-loop select sees
	// ctx.Done() and returns. The cancel cannot skip the immediate call.
	cancel()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("RunTrashPurger did not exit within 3 seconds after context cancellation")
	}

	if fake.purgeCalls < 1 {
		t.Errorf("PurgeExpired calls = %d, want >= 1 (must run immediately on entry)", fake.purgeCalls)
	}
}

// TestRunTrashPurger_ExitsOnCancel verifies that RunTrashPurger exits promptly
// when the context is already cancelled, without blocking for trashPurgeInterval
// (6 hours). Even with a pre-cancelled ctx the immediate runTrashPurgeOnce call
// still runs, then the for-loop select hits ctx.Done() and returns.
func TestRunTrashPurger_ExitsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel BEFORE the call

	fake := &fakePurgeDeleter{}
	done := make(chan struct{})
	go func() {
		RunTrashPurger(ctx, fake, fake, 30*24*time.Hour)
		close(done)
	}()

	select {
	case <-done:
		// Good: RunTrashPurger returned promptly.
	case <-time.After(3 * time.Second):
		t.Fatal("RunTrashPurger did not exit within 3 seconds (should exit on pre-cancelled context)")
	}
}
