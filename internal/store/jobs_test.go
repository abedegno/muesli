package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
)

// seedUserCounter gives each seeded user a unique email so seedNote can be
// called more than once within a single test (e.g. to create a second note
// that has no transcript) without tripping the users.email unique constraint.
var seedUserCounter atomic.Int64

func seedNote(t *testing.T, st *store.Store) string {
	t.Helper()
	ctx := context.Background()
	email := fmt.Sprintf("o%d@example.com", seedUserCounter.Add(1))
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

func TestEnqueueAndClaim(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	noteID := seedNote(t, st)

	jobID, err := st.EnqueueJob(ctx, noteID, model.JobTranscribe, json.RawMessage(`{"audio_key":"k"}`))
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if jobID == "" {
		t.Fatal("expected job id")
	}

	// Claim leases the job for 30s and marks it running.
	job, ok, err := st.ClaimJob(ctx, 30*time.Second)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if !ok || job.ID != jobID || job.Status != model.JobRunning {
		t.Fatalf("claim returned %+v ok=%v", job, ok)
	}
	if job.Attempts != 1 {
		t.Fatalf("attempts = %d, want 1", job.Attempts)
	}

	// A second immediate claim finds nothing (the only job is leased).
	if _, ok, _ := st.ClaimJob(ctx, 30*time.Second); ok {
		t.Fatal("second claim should find no claimable job")
	}
}

func TestCompleteJob(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	noteID := seedNote(t, st)
	_, _ = st.EnqueueJob(ctx, noteID, model.JobTranscribe, json.RawMessage(`{}`))
	job, _, _ := st.ClaimJob(ctx, 30*time.Second)

	if err := st.CompleteJob(ctx, job.ID); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if _, ok, _ := st.ClaimJob(ctx, 30*time.Second); ok {
		t.Fatal("completed job must not be reclaimable")
	}
}

func TestFailJobRetryThenTerminal(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	noteID := seedNote(t, st)
	_, _ = st.EnqueueJob(ctx, noteID, model.JobTranscribe, json.RawMessage(`{}`))

	// Attempt 1: claim, fail retryable → back to pending, no lease.
	job, _, _ := st.ClaimJob(ctx, 30*time.Second)
	if err := st.FailJob(ctx, job.ID, "boom", true); err != nil {
		t.Fatalf("fail retryable: %v", err)
	}
	// Retryable jobs now wait for their backoff window before they can be
	// reclaimed again.
	if _, ok, _ := st.ClaimJob(ctx, 30*time.Second); ok {
		t.Fatal("retryable failure should not be immediately reclaimable")
	}
	if _, err := st.Pool().Exec(ctx, "UPDATE jobs SET lease_expires_at = NULL WHERE id=$1", job.ID); err != nil {
		t.Fatalf("clear retry lease: %v", err)
	}
	job, ok, _ := st.ClaimJob(ctx, 30*time.Second)
	if !ok || job.Attempts != 2 {
		t.Fatalf("reclaim after retryable failure: ok=%v attempts=%d", ok, job.Attempts)
	}

	// Attempt 2: fail terminally → status failed, not reclaimable.
	if err := st.FailJob(ctx, job.ID, "fatal", false); err != nil {
		t.Fatalf("fail terminal: %v", err)
	}
	if _, ok, _ := st.ClaimJob(ctx, 30*time.Second); ok {
		t.Fatal("terminally failed job must not be reclaimable")
	}
}

func TestFailJob_RetryBackoffBlocksImmediateReclaim(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	noteID := seedNote(t, st)
	_, _ = st.EnqueueJob(ctx, noteID, model.JobTranscribe, json.RawMessage(`{}`))

	job, ok, err := st.ClaimJob(ctx, 30*time.Second)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if !ok {
		t.Fatal("expected job to be claimable")
	}
	if err := st.FailJob(ctx, job.ID, "boom", true); err != nil {
		t.Fatalf("fail retryable: %v", err)
	}
	if _, ok, err := st.ClaimJob(ctx, 30*time.Second); err != nil {
		t.Fatalf("ClaimJob after retryable failure: %v", err)
	} else if ok {
		t.Fatal("retryable failure should not be immediately reclaimable")
	}
}

func TestReclaimExpiredLease(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	noteID := seedNote(t, st)
	_, _ = st.EnqueueJob(ctx, noteID, model.JobTranscribe, json.RawMessage(`{}`))

	// Claim with a lease that is already expired (negative ttl).
	job, ok, _ := st.ClaimJob(ctx, -time.Second)
	if !ok {
		t.Fatal("expected to claim")
	}
	// A fresh claim reclaims the expired lease (worker crash recovery).
	reclaimed, ok, _ := st.ClaimJob(ctx, 30*time.Second)
	if !ok || reclaimed.ID != job.ID {
		t.Fatalf("expected to reclaim expired lease: ok=%v id=%s", ok, reclaimed.ID)
	}
	if reclaimed.Attempts != 2 {
		t.Fatalf("attempts after reclaim = %d, want 2", reclaimed.Attempts)
	}
}

func TestResetRunningJobs_ResetsAllRegardlessOfLease(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	noteID := seedNote(t, st)

	// Insert and claim a job with a future lease (10 min from now).
	jobID, err := st.EnqueueJob(ctx, noteID, model.JobTranscribe, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	_, ok, err := st.ClaimJob(ctx, 10*time.Minute)
	if err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}

	// ResetRunningJobs must reset it unconditionally (startup path).
	n, err := st.ResetRunningJobs(ctx)
	if err != nil {
		t.Fatalf("ResetRunningJobs: %v", err)
	}
	if n != 1 {
		t.Fatalf("want 1 reset, got %d", n)
	}
	j, err := st.GetJob(ctx, jobID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if j.Status != model.JobPending {
		t.Fatalf("want pending, got %s", j.Status)
	}
}

func TestResetExpiredRunningJobs_SkipsFutureLease(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	noteID := seedNote(t, st)

	// Claim with a future lease.
	jobID, err := st.EnqueueJob(ctx, noteID, model.JobTranscribe, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	_, ok, err := st.ClaimJob(ctx, 10*time.Minute)
	if err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}

	// Periodic sweep must NOT reset it (future lease).
	n, err := st.ResetExpiredRunningJobs(ctx)
	if err != nil {
		t.Fatalf("ResetExpiredRunningJobs: %v", err)
	}
	if n != 0 {
		t.Fatalf("want 0 reset (future lease), got %d", n)
	}
	j, err := st.GetJob(ctx, jobID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if j.Status != model.JobRunning {
		t.Fatalf("want still running, got %s", j.Status)
	}
}

func TestResetExpiredRunningJobs_ResetsExpiredLease(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	noteID := seedNote(t, st)

	// Claim with a lease already in the past.
	jobID, err := st.EnqueueJob(ctx, noteID, model.JobTranscribe, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	_, ok, err := st.ClaimJob(ctx, -time.Minute)
	if err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}

	// Periodic sweep must reset it.
	n, err := st.ResetExpiredRunningJobs(ctx)
	if err != nil {
		t.Fatalf("ResetExpiredRunningJobs: %v", err)
	}
	if n != 1 {
		t.Fatalf("want 1 reset, got %d", n)
	}
	j, err := st.GetJob(ctx, jobID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if j.Status != model.JobPending {
		t.Fatalf("want pending, got %s", j.Status)
	}
}

// TestBumpNoteJobPriority_DequeuesBeforeOlderUnbumped verifies that a bumped
// PENDING job on note B dequeues before an older PENDING job on note A that
// was enqueued first and never bumped.
func TestBumpNoteJobPriority_DequeuesBeforeOlderUnbumped(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()

	noteA := seedNote(t, st)
	oldJobID, err := st.EnqueueJob(ctx, noteA, model.JobTranscribe, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("enqueue A: %v", err)
	}

	noteB := seedNote(t, st)
	bumpedJobID, err := st.EnqueueJob(ctx, noteB, model.JobTranscribe, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("enqueue B: %v", err)
	}

	n, err := st.BumpNoteJobPriority(ctx, noteB)
	if err != nil {
		t.Fatalf("bump: %v", err)
	}
	if n != 1 {
		t.Fatalf("want 1 job bumped, got %d", n)
	}

	job, ok, err := st.ClaimJob(ctx, 30*time.Second)
	if err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	if job.ID != bumpedJobID {
		t.Fatalf("expected bumped job %s to dequeue first, got %s (old job %s)", bumpedJobID, job.ID, oldJobID)
	}

	// The older, never-bumped job is still claimable next.
	next, ok, err := st.ClaimJob(ctx, 30*time.Second)
	if err != nil || !ok {
		t.Fatalf("second claim: ok=%v err=%v", ok, err)
	}
	if next.ID != oldJobID {
		t.Fatalf("expected old job %s to dequeue second, got %s", oldJobID, next.ID)
	}
}

// TestBumpNoteJobPriority_LeavesRunningJobUntouched verifies that a job
// already RUNNING on a note is unaffected by a bump call on that note: its
// status, lease, and attempts must not change (no preemption of in-flight work).
func TestBumpNoteJobPriority_LeavesRunningJobUntouched(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	noteID := seedNote(t, st)

	runningJobID, err := st.EnqueueJob(ctx, noteID, model.JobTranscribe, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	claimed, ok, err := st.ClaimJob(ctx, 10*time.Minute)
	if err != nil || !ok || claimed.ID != runningJobID {
		t.Fatalf("claim: ok=%v err=%v job=%+v", ok, err, claimed)
	}
	before, err := st.GetJob(ctx, runningJobID)
	if err != nil {
		t.Fatalf("get before: %v", err)
	}

	// A second, still-pending job on the same note is the one that should move.
	pendingJobID, err := st.EnqueueJob(ctx, noteID, model.JobSummarize, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("enqueue pending: %v", err)
	}

	n, err := st.BumpNoteJobPriority(ctx, noteID)
	if err != nil {
		t.Fatalf("bump: %v", err)
	}
	if n != 1 {
		t.Fatalf("want 1 job bumped (only the pending one), got %d", n)
	}

	after, err := st.GetJob(ctx, runningJobID)
	if err != nil {
		t.Fatalf("get after: %v", err)
	}
	if after.Status != model.JobRunning || after.Status != before.Status {
		t.Fatalf("running job status changed: before=%s after=%s", before.Status, after.Status)
	}
	if after.Attempts != before.Attempts {
		t.Fatalf("running job attempts changed: before=%d after=%d", before.Attempts, after.Attempts)
	}
	if after.Priority != before.Priority {
		t.Fatalf("running job priority changed: before=%d after=%d", before.Priority, after.Priority)
	}

	pending, err := st.GetJob(ctx, pendingJobID)
	if err != nil {
		t.Fatalf("get pending: %v", err)
	}
	if pending.Priority <= before.Priority {
		t.Fatalf("bumped pending job priority %d should be > running job's original priority %d", pending.Priority, before.Priority)
	}
}

// TestCancelJob_HappyPath cancels a pending job and verifies its status
// becomes cancelled.
func TestCancelJob_HappyPath(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	noteID := seedNote(t, st)

	jobID, err := st.EnqueueJob(ctx, noteID, model.JobTranscribe, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	if err := st.CancelJob(ctx, jobID); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	job, err := st.GetJob(ctx, jobID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if job.Status != model.JobCancelled {
		t.Fatalf("want status %s, got %s", model.JobCancelled, job.Status)
	}
}

// TestCancelJob_LeavesRunningJobUntouched verifies CancelJob never touches a
// job that isn't pending: the conditional UPDATE affects no rows, an error is
// returned, and the running job's status is unchanged.
func TestCancelJob_LeavesRunningJobUntouched(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	noteID := seedNote(t, st)

	runningJobID, err := st.EnqueueJob(ctx, noteID, model.JobTranscribe, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	claimed, ok, err := st.ClaimJob(ctx, 10*time.Minute)
	if err != nil || !ok || claimed.ID != runningJobID {
		t.Fatalf("claim: ok=%v err=%v job=%+v", ok, err, claimed)
	}
	before, err := st.GetJob(ctx, runningJobID)
	if err != nil {
		t.Fatalf("get before: %v", err)
	}

	if err := st.CancelJob(ctx, runningJobID); !errors.Is(err, store.ErrInvalidTransition) {
		t.Fatalf("want ErrInvalidTransition, got %v", err)
	}

	after, err := st.GetJob(ctx, runningJobID)
	if err != nil {
		t.Fatalf("get after: %v", err)
	}
	if after.Status != before.Status {
		t.Fatalf("running job status changed: before=%s after=%s", before.Status, after.Status)
	}
}

// TestJobStatusCounts_AllKnownStatusesAlwaysPresent verifies every known job
// status is present in the result even at 0, and that counts reflect an
// actual mix of pending/running/done/failed jobs. Backs the admin health
// panel's job-queue-depth section (ADM05).
func TestJobStatusCounts_AllKnownStatusesAlwaysPresent(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()

	// Zero jobs: every status must still be present, all zero.
	counts, err := st.JobStatusCounts(ctx)
	if err != nil {
		t.Fatalf("JobStatusCounts (empty): %v", err)
	}
	for _, status := range []string{model.JobPending, model.JobRunning, model.JobDone, model.JobFailed, model.JobCancelled} {
		if n, ok := counts[status]; !ok || n != 0 {
			t.Fatalf("status %q = %d (present=%v), want 0", status, n, ok)
		}
	}

	noteID := seedNote(t, st)

	// Enqueue 4 pending jobs, then claim exactly 3 of them. ClaimJob's
	// ORDER BY priority DESC, created_at ASC means the *earliest-enqueued*
	// job is always claimed first - so which specific job ends up in which
	// bucket is NOT arbitrary. The previous version of this test assumed the
	// first-enqueued job would be the one left pending, but it's actually
	// always claimed first, making the assertion flaky/wrong. Fixed by never
	// assuming which job lands where: drive the *claimed* jobs (whichever
	// they are) to done/failed/still-running, and leave whichever job wasn't
	// claimed as the pending one.
	const totalJobs = 4
	for i := 0; i < totalJobs; i++ {
		if _, err := st.EnqueueJob(ctx, noteID, model.JobTranscribe, nil); err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
	}

	var claimed []string
	for i := 0; i < 3; i++ {
		job, ok, err := st.ClaimJob(ctx, 30*time.Second)
		if err != nil || !ok {
			t.Fatalf("claim %d: ok=%v err=%v", i, ok, err)
		}
		claimed = append(claimed, job.ID)
	}
	if len(claimed) != 3 {
		t.Fatalf("expected to claim exactly 3 jobs, got %v", claimed)
	}

	if err := st.CompleteJob(ctx, claimed[0]); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if err := st.FailJob(ctx, claimed[1], "boom", false); err != nil {
		t.Fatalf("fail: %v", err)
	}
	// claimed[2] is left running (no further action); the 4th, never-claimed
	// job is left pending.

	counts, err = st.JobStatusCounts(ctx)
	if err != nil {
		t.Fatalf("JobStatusCounts: %v", err)
	}
	want := map[string]int{
		model.JobPending:   1, // the one job never claimed
		model.JobRunning:   1, // claimed[2], left running
		model.JobDone:      1, // claimed[0]
		model.JobFailed:    1, // claimed[1]
		model.JobCancelled: 0,
	}
	for status, n := range want {
		if counts[status] != n {
			t.Errorf("status %q = %d, want %d (full: %+v)", status, counts[status], n, counts)
		}
	}
}
