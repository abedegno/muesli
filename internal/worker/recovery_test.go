package worker_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
	"github.com/abedegno/muesli/internal/worker"
)

var recoveryUserCounter atomic.Int64

func seedNoteForRecoveryTest(t *testing.T, st *store.Store) string {
	t.Helper()
	ctx := context.Background()
	email := fmt.Sprintf("rtest%d@example.com", recoveryUserCounter.Add(1))
	u, err := st.CreateUser(ctx, email, "h")
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	n, err := st.CreateNote(ctx, u.ID, "M")
	if err != nil {
		t.Fatalf("note: %v", err)
	}
	return n.ID
}

// TestStartupRecovery_AllRunning: a running job with a FUTURE lease must be
// reset to pending by ResetRunningJobs (startup recovery is unconditional).
func TestStartupRecovery_AllRunning(t *testing.T) {
	t.Parallel()
	pool := testutil.NewPool(t)
	st := store.New(pool)
	ctx := context.Background()
	noteID := seedNoteForRecoveryTest(t, st)

	// Insert a job and claim it (gives it a 10-minute future lease).
	jobID, err := st.EnqueueJob(ctx, noteID, model.JobTranscribe, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	_, ok, err := st.ClaimJob(ctx, 10*time.Minute)
	if err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}

	// Verify it's running.
	j, err := st.GetJob(ctx, jobID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if j.Status != model.JobRunning {
		t.Fatalf("expected running, got %s", j.Status)
	}

	// Reset all running jobs (startup recovery — unconditional, no lease check).
	n, err := st.ResetRunningJobs(ctx)
	if err != nil {
		t.Fatalf("ResetRunningJobs: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 job reset, got %d", n)
	}

	// Job must now be pending.
	j, err = st.GetJob(ctx, jobID)
	if err != nil {
		t.Fatalf("get job after reset: %v", err)
	}
	if j.Status != model.JobPending {
		t.Fatalf("expected pending after reset, got %s", j.Status)
	}

	// It must be reclaimable immediately.
	reclaimed, ok2, err := st.ClaimJob(ctx, 30*time.Second)
	if err != nil || !ok2 || reclaimed.ID != jobID {
		t.Fatalf("expected reclaimable: ok=%v id=%s err=%v", ok2, reclaimed.ID, err)
	}
}

// TestStartupRecovery_IgnoresNonRunning: pending/done/failed jobs must not be
// touched by ResetRunningJobs.
func TestStartupRecovery_IgnoresNonRunning(t *testing.T) {
	t.Parallel()
	pool := testutil.NewPool(t)
	st := store.New(pool)
	ctx := context.Background()

	// Set up done job FIRST so its created_at is earliest: enqueue, claim, complete.
	// ClaimJob orders by created_at, so we must ensure the pending job is inserted
	// AFTER the done/failed jobs are already claimed.
	noteID2 := seedNoteForRecoveryTest(t, st)
	doneJobID, err := st.EnqueueJob(ctx, noteID2, model.JobTranscribe, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("enqueue done: %v", err)
	}
	doneJob, _, _ := st.ClaimJob(ctx, time.Minute)
	if doneJob.ID != doneJobID {
		t.Fatalf("claimed wrong job for done setup: got %s want %s", doneJob.ID, doneJobID)
	}
	_ = st.CompleteJob(ctx, doneJob.ID)

	// Set up failed job SECOND: enqueue, claim, fail terminally.
	noteID3 := seedNoteForRecoveryTest(t, st)
	failJobID, err := st.EnqueueJob(ctx, noteID3, model.JobTranscribe, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}
	failJob, _, _ := st.ClaimJob(ctx, time.Minute)
	if failJob.ID != failJobID {
		t.Fatalf("claimed wrong job for fail setup: got %s want %s", failJob.ID, failJobID)
	}
	_ = st.FailJob(ctx, failJob.ID, "fatal", false)

	// Insert the pending job LAST so it was not picked up by ClaimJob above.
	noteID := seedNoteForRecoveryTest(t, st)
	pendingID, err := st.EnqueueJob(ctx, noteID, model.JobTranscribe, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("enqueue pending: %v", err)
	}

	// Reset — should touch 0 rows (no running jobs).
	n, err := st.ResetRunningJobs(ctx)
	if err != nil {
		t.Fatalf("ResetRunningJobs: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 rows reset, got %d", n)
	}

	// Pending job is still pending.
	j, err := st.GetJob(ctx, pendingID)
	if err != nil {
		t.Fatalf("get pending: %v", err)
	}
	if j.Status != model.JobPending {
		t.Fatalf("pending job status changed to %s", j.Status)
	}
}

// TestPeriodicSweep_ExpiredLease: a running job whose lease has already expired
// must be reset by ResetExpiredRunningJobs.
func TestPeriodicSweep_ExpiredLease(t *testing.T) {
	t.Parallel()
	pool := testutil.NewPool(t)
	st := store.New(pool)
	ctx := context.Background()
	noteID := seedNoteForRecoveryTest(t, st)

	// Claim with a lease already in the past.
	jobID, err := st.EnqueueJob(ctx, noteID, model.JobTranscribe, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	_, ok, err := st.ClaimJob(ctx, -time.Minute) // lease in the past
	if err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}

	// Periodic sweep should reset it (lease expired).
	n, err := st.ResetExpiredRunningJobs(ctx)
	if err != nil {
		t.Fatalf("ResetExpiredRunningJobs: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 job reset, got %d", n)
	}

	j, err := st.GetJob(ctx, jobID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if j.Status != model.JobPending {
		t.Fatalf("expected pending, got %s", j.Status)
	}
}

// TestPeriodicSweep_FutureLease_NotRecovered: a running job with a FUTURE lease
// must NOT be touched by ResetExpiredRunningJobs (periodic sweep is lease-gated).
func TestPeriodicSweep_FutureLease_NotRecovered(t *testing.T) {
	t.Parallel()
	pool := testutil.NewPool(t)
	st := store.New(pool)
	ctx := context.Background()
	noteID := seedNoteForRecoveryTest(t, st)

	// Claim with a future lease.
	jobID, err := st.EnqueueJob(ctx, noteID, model.JobTranscribe, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	_, ok, err := st.ClaimJob(ctx, 10*time.Minute)
	if err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}

	// Periodic sweep must leave this job alone (lease not yet expired).
	n, err := st.ResetExpiredRunningJobs(ctx)
	if err != nil {
		t.Fatalf("ResetExpiredRunningJobs: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 jobs reset (future lease), got %d", n)
	}

	j, err := st.GetJob(ctx, jobID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if j.Status != model.JobRunning {
		t.Fatalf("expected still running (future lease), got %s", j.Status)
	}
}

// TestStartupRecovery_EndToEnd: insert a running job with a future lease,
// call recoverStartupJobs, then verify the job is reclaimable.
func TestStartupRecovery_EndToEnd(t *testing.T) {
	t.Parallel()
	pool := testutil.NewPool(t)
	st := store.New(pool)
	ctx := context.Background()
	noteID := seedNoteForRecoveryTest(t, st)

	// Simulate orphaned job: enqueue and claim with a future lease.
	jobID, err := st.EnqueueJob(ctx, noteID, model.JobTranscribe, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	_, ok, err := st.ClaimJob(ctx, 10*time.Minute)
	if err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}

	// Confirm ClaimJob won't reclaim a future-lease running job.
	_, ok2, err := st.ClaimJob(ctx, 30*time.Second)
	if err != nil {
		t.Fatalf("second claim err: %v", err)
	}
	if ok2 {
		t.Fatal("expected ClaimJob to skip future-lease running job (test setup error)")
	}

	// Run startup recovery via the exported test helper.
	worker.RecoverStartupJobsForTest(ctx, st)

	// Now the job should be pending and reclaimable.
	reclaimed, ok3, err := st.ClaimJob(ctx, 30*time.Second)
	if err != nil {
		t.Fatalf("claim after recovery: %v", err)
	}
	if !ok3 || reclaimed.ID != jobID {
		t.Fatalf("expected reclaimable after startup recovery: ok=%v id=%s err=%v", ok3, reclaimed.ID, err)
	}
}
