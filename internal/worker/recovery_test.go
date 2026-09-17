package worker_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/calendar"
	"github.com/abedegno/muesli/internal/config"
	"github.com/abedegno/muesli/internal/crypto"
	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/plugintest"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
	"github.com/abedegno/muesli/internal/worker"
)

var recoveryUserCounter atomic.Int64

// preGenerateRecoveryTestBase is a fixed instant used only to compute a
// far-future event start time (well outside any plausible real test-run
// duration) -- this file's other tests don't need an injected clock, but
// scripts/check-test-determinism.sh bans the wall-clock call outright in
// non-e2e Go test files, so this fixed base takes its place.
var preGenerateRecoveryTestBase = testutil.NewFakeClock(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)).Now()

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

// TestStartupRecovery_AllRunning: a running job with a FUTURE lease must not
// be touched by startup recovery.
func TestStartupRecovery_AllRunning(t *testing.T) {
	t.Parallel()
	pool := testutil.NewPool(t)
	st := store.New(pool)
	ctx := context.Background()
	noteID := seedNoteForRecoveryTest(t, st)

	// Insert a job and claim it (gives it a 10-minute future lease).
	jobID, err := st.EnqueueNoteJob(ctx, noteID, model.JobTranscribe, json.RawMessage(`{}`))
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

	var beforeLease time.Time
	if err := st.Pool().QueryRow(ctx, `SELECT lease_expires_at FROM jobs WHERE id=$1`, jobID).Scan(&beforeLease); err != nil {
		t.Fatalf("query lease before recovery: %v", err)
	}

	// Startup recovery must leave a still-live job untouched.
	worker.RecoverStartupJobsForTest(ctx, st)

	var afterLease time.Time
	if err := st.Pool().QueryRow(ctx, `SELECT lease_expires_at FROM jobs WHERE id=$1`, jobID).Scan(&afterLease); err != nil {
		t.Fatalf("query lease after recovery: %v", err)
	}
	if !afterLease.Equal(beforeLease) {
		t.Fatalf("expected lease unchanged, before=%s after=%s", beforeLease, afterLease)
	}

	// No rows should have been reset.
	n, err := st.ResetExpiredRunningJobs(ctx)
	if err != nil {
		t.Fatalf("ResetExpiredRunningJobs: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 jobs reset, got %d", n)
	}

	// Job remains running.
	j, err = st.GetJob(ctx, jobID)
	if err != nil {
		t.Fatalf("get job after reset: %v", err)
	}
	if j.Status != model.JobRunning {
		t.Fatalf("expected still running after startup recovery, got %s", j.Status)
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
	doneJobID, err := st.EnqueueNoteJob(ctx, noteID2, model.JobTranscribe, json.RawMessage(`{}`))
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
	failJobID, err := st.EnqueueNoteJob(ctx, noteID3, model.JobTranscribe, json.RawMessage(`{}`))
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
	pendingID, err := st.EnqueueNoteJob(ctx, noteID, model.JobTranscribe, json.RawMessage(`{}`))
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
	jobID, err := st.EnqueueNoteJob(ctx, noteID, model.JobTranscribe, json.RawMessage(`{}`))
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
	jobID, err := st.EnqueueNoteJob(ctx, noteID, model.JobTranscribe, json.RawMessage(`{}`))
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

// TestStartupRecovery_DoesNotReclaimFutureLeaseJob: a still-live sibling's job
// must remain running and keep its original lease after startup recovery.
func TestStartupRecovery_DoesNotReclaimFutureLeaseJob(t *testing.T) {
	t.Parallel()
	pool := testutil.NewPool(t)
	st := store.New(pool)
	ctx := context.Background()
	noteID := seedNoteForRecoveryTest(t, st)

	jobID, err := st.EnqueueNoteJob(ctx, noteID, model.JobTranscribe, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	_, ok, err := st.ClaimJob(ctx, 10*time.Minute)
	if err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}

	var beforeLease time.Time
	if err := st.Pool().QueryRow(ctx, `SELECT lease_expires_at FROM jobs WHERE id=$1`, jobID).Scan(&beforeLease); err != nil {
		t.Fatalf("query lease before recovery: %v", err)
	}

	worker.RecoverStartupJobsForTest(ctx, st)

	j, err := st.GetJob(ctx, jobID)
	if err != nil {
		t.Fatalf("get job after recovery: %v", err)
	}
	if j.Status != model.JobRunning {
		t.Fatalf("expected still running after startup recovery, got %s", j.Status)
	}

	var afterLease time.Time
	if err := st.Pool().QueryRow(ctx, `SELECT lease_expires_at FROM jobs WHERE id=$1`, jobID).Scan(&afterLease); err != nil {
		t.Fatalf("query lease after recovery: %v", err)
	}
	if !afterLease.Equal(beforeLease) {
		t.Fatalf("expected lease unchanged, before=%s after=%s", beforeLease, afterLease)
	}
}

// TestStartupRecovery_EndToEnd: insert a running job with an expired lease,
// call recoverStartupJobs, then verify the job is reclaimable.
func TestStartupRecovery_EndToEnd(t *testing.T) {
	t.Parallel()
	pool := testutil.NewPool(t)
	st := store.New(pool)
	ctx := context.Background()
	noteID := seedNoteForRecoveryTest(t, st)

	// Simulate orphaned job: enqueue and claim with an expired lease.
	jobID, err := st.EnqueueNoteJob(ctx, noteID, model.JobTranscribe, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	_, ok, err := st.ClaimJob(ctx, -time.Minute)
	if err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}

	j, err := st.GetJob(ctx, jobID)
	if err != nil {
		t.Fatalf("get job before recovery: %v", err)
	}
	if j.Status != model.JobRunning {
		t.Fatalf("expected running before startup recovery, got %s", j.Status)
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

// preGenerateRecoveryFixture seeds a user, calendar source, a single
// eligible (far-future, never actually "started" during a real test run)
// event, a pre/auto-run template, and a deterministic fake default agent --
// everything ReconcileEventBriefPair and a real Processor.Process(pre_generate
// job) need end to end, without reaching into worker's unexported preJobClock
// (this file is package worker_test).
type preGenerateRecoveryFixture struct {
	st    *store.Store
	proc  *worker.Processor
	owner string
	event model.CalendarEvent
	tmpl  model.Template
}

func newPreGenerateRecoveryFixture(t *testing.T) *preGenerateRecoveryFixture {
	t.Helper()
	ctx := context.Background()
	pool := testutil.NewPool(t)
	st := store.New(pool)

	cr, err := crypto.New(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatalf("crypto: %v", err)
	}

	email := fmt.Sprintf("prerecover%d@example.com", recoveryUserCounter.Add(1))
	u, err := st.CreateUser(ctx, email, "h")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	src, err := st.CreateSource(ctx, u.ID, "ics", "Cal", "sealed")
	if err != nil {
		t.Fatalf("create source: %v", err)
	}

	agent := plugintest.NewAgent()
	t.Cleanup(agent.Close)
	if err := st.EnsureDefaultPlugin(ctx, cr, model.PluginAgent, "agent", agent.URL(), "tok", "{}"); err != nil {
		t.Fatalf("ensure default agent: %v", err)
	}

	starts := preGenerateRecoveryTestBase.Add(48 * time.Hour)
	if err := st.UpsertEvents(ctx, u.ID, src.ID, []calendar.NormalizedEvent{
		{
			ExternalID: "e1", Title: "Planning", StartsAt: starts, EndsAt: starts.Add(30 * time.Minute),
			Description: "Sprint planning", Location: "Room 2", ConferencingURL: "https://meet.example.com/abc",
			Attendees: []model.Attendee{{Name: "Jane", Email: "jane@example.com", Response: "accepted"}},
		},
	}); err != nil {
		t.Fatalf("upsert event: %v", err)
	}
	evs, err := st.ListEvents(ctx, u.ID, starts.Add(-time.Minute), starts.Add(time.Minute))
	if err != nil || len(evs) != 1 {
		t.Fatalf("list seeded event: %+v %v", evs, err)
	}

	tmpl, err := st.CreateTemplate(ctx, u.ID, "Pre-read", "pre",
		[]model.TemplateSection{{Heading: "Context", Instruction: "Summarize the agenda."}}, true, "", "", nil)
	if err != nil {
		t.Fatalf("create template: %v", err)
	}

	proc := worker.NewProcessor(st, cr, nil, config.Config{}, nil)
	return &preGenerateRecoveryFixture{st: st, proc: proc, owner: u.ID, event: evs[0], tmpl: tmpl}
}

func (f *preGenerateRecoveryFixture) currentBrief(t *testing.T) model.EventBrief {
	t.Helper()
	briefs, err := f.st.EventBriefsForEvents(context.Background(), f.owner, []string{f.event.ID})
	if err != nil || len(briefs[f.event.ID]) != 1 {
		t.Fatalf("event briefs: %+v %v", briefs, err)
	}
	return briefs[f.event.ID][0]
}

func countPreGenerateJobsForBrief(t *testing.T, st *store.Store, briefID string) int {
	t.Helper()
	var n int
	if err := st.Pool().QueryRow(context.Background(),
		`SELECT count(*) FROM jobs WHERE type=$1 AND brief_id=$2`, model.JobPreGenerate, briefID).Scan(&n); err != nil {
		t.Fatalf("count pre_generate jobs: %v", err)
	}
	return n
}

// TestPreGenerateLeaseRecoveryReclaimsWithoutDuplicating proves the lease
// recovery contract ("no duplicate/regress") for a pre_generate job, not
// just note-targeted ones: a crashed worker's expired-lease pre_generate job
// is reclaimed by the same startup recovery path used for note jobs, runs
// exactly once to completion, and never spawns a second/duplicate job for
// the same brief.
func TestPreGenerateLeaseRecoveryReclaimsWithoutDuplicating(t *testing.T) {
	t.Parallel()
	f := newPreGenerateRecoveryFixture(t)
	ctx := context.Background()

	h := "h1"
	if _, err := f.st.ReconcileEventBriefPair(ctx, f.event.ID, f.tmpl.ID, f.tmpl.Name, &h); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	brief := f.currentBrief(t)
	if countPreGenerateJobsForBrief(t, f.st, brief.ID) != 1 {
		t.Fatalf("expected exactly one queued pre_generate job before any crash")
	}

	// Simulate a crashed worker: claim with an already-expired lease, same
	// as TestStartupRecovery_EndToEnd's note-job scenario.
	crashed, ok, err := f.st.ClaimJob(ctx, -time.Minute)
	if err != nil || !ok {
		t.Fatalf("claim (simulated crash): ok=%v err=%v", ok, err)
	}
	if crashed.Type != model.JobPreGenerate || crashed.BriefID != brief.ID {
		t.Fatalf("claimed unexpected job: %+v", crashed)
	}

	// Startup recovery resets the orphaned running job back to pending.
	worker.RecoverStartupJobsForTest(ctx, f.st)

	reclaimed, ok, err := f.st.ClaimJob(ctx, 30*time.Second)
	if err != nil || !ok {
		t.Fatalf("reclaim after recovery: ok=%v err=%v", ok, err)
	}
	if reclaimed.ID != crashed.ID {
		t.Fatalf("expected recovery to reclaim the SAME job (no duplicate), got original=%s reclaimed=%s", crashed.ID, reclaimed.ID)
	}

	// Run it to completion through the real dispatcher.
	f.proc.Process(ctx, reclaimed)

	after := f.currentBrief(t)
	if after.Status != model.BriefReady || len(after.Sections) == 0 {
		t.Fatalf("expected the reclaimed job to publish the brief ready, got %+v", after)
	}
	if got := countPreGenerateJobsForBrief(t, f.st, brief.ID); got != 1 {
		t.Fatalf("expected exactly one pre_generate job to ever exist for this brief (no duplicate from recovery), got %d", got)
	}
}

// TestPreGenerateLeaseRecoveryDoesNotRegressNewerGeneration proves lease
// recovery cannot regress a brief that reconciliation already advanced past
// the crashed job's generation -- including one that has already published
// as ready at the newer generation. A crashed worker's stale-generation job
// is reclaimed and re-run by the same recovery path, but
// PublishEventBrief's WHERE id=? AND generation=? guard (exercised through
// the real runPreGenerate path here, not mocked) makes it a no-op rather
// than overwriting the newer, already-ready state.
func TestPreGenerateLeaseRecoveryDoesNotRegressNewerGeneration(t *testing.T) {
	t.Parallel()
	f := newPreGenerateRecoveryFixture(t)
	ctx := context.Background()

	h1 := "h1"
	if _, err := f.st.ReconcileEventBriefPair(ctx, f.event.ID, f.tmpl.ID, f.tmpl.Name, &h1); err != nil {
		t.Fatalf("reconcile gen 1: %v", err)
	}
	brief := f.currentBrief(t)
	if brief.Generation != 1 {
		t.Fatalf("expected generation 1, got %+v", brief)
	}

	// Claim generation 1's job with a LIVE lease -- modeling a worker still
	// mid-flight, not yet crashed (a claim with an already-expired lease
	// would make it immediately re-claimable by the very next ClaimJob call
	// below, racing with generation 2's own claim on job age/ordering rather
	// than deterministically exercising recovery).
	staleJob, ok, err := f.st.ClaimJob(ctx, time.Minute)
	if err != nil || !ok {
		t.Fatalf("claim gen-1 job: ok=%v err=%v", ok, err)
	}
	if staleJob.BriefGeneration != 1 {
		t.Fatalf("expected the claimed job to be generation 1, got %+v", staleJob)
	}

	// Meanwhile, reconciliation runs again with a CHANGED hash (e.g. the
	// agenda changed) -- advances the brief to generation 2 and enqueues a
	// fresh job the in-flight generation-1 job knows nothing about.
	h2 := "h2"
	if _, err := f.st.ReconcileEventBriefPair(ctx, f.event.ID, f.tmpl.ID, f.tmpl.Name, &h2); err != nil {
		t.Fatalf("reconcile gen 2: %v", err)
	}
	gen2Job, ok, err := f.st.ClaimJob(ctx, time.Minute)
	if err != nil || !ok {
		t.Fatalf("claim gen-2 job: ok=%v err=%v", ok, err)
	}
	if gen2Job.BriefGeneration != 2 {
		t.Fatalf("expected the fresh job to be generation 2, got %+v", gen2Job)
	}
	// Run generation 2 to completion FIRST, so the brief is already
	// published ready at generation 2 before the stale generation-1 job is
	// ever recovered.
	f.proc.Process(ctx, gen2Job)
	readyAtGen2 := f.currentBrief(t)
	if readyAtGen2.Status != model.BriefReady || readyAtGen2.Generation != 2 {
		t.Fatalf("expected the brief ready at generation 2 before recovery runs, got %+v", readyAtGen2)
	}

	// NOW simulate the generation-1 worker crashing without ever settling
	// its job: force its lease into the past directly (generation 2's job is
	// already 'done' by this point, so it can never be mistaken for the
	// reclaim target below).
	if _, err := f.st.Pool().Exec(ctx, `UPDATE jobs SET lease_expires_at = now() - interval '1 minute' WHERE id=$1`, staleJob.ID); err != nil {
		t.Fatalf("force-expire stale job lease: %v", err)
	}

	// Recover the orphaned generation-1 lease and let it run.
	worker.RecoverStartupJobsForTest(ctx, f.st)
	reclaimedStale, ok, err := f.st.ClaimJob(ctx, 30*time.Second)
	if err != nil || !ok {
		t.Fatalf("reclaim stale gen-1 job: ok=%v err=%v", ok, err)
	}
	if reclaimedStale.ID != staleJob.ID {
		t.Fatalf("expected to reclaim the SAME stale job, got original=%s reclaimed=%s", staleJob.ID, reclaimedStale.ID)
	}

	f.proc.Process(ctx, reclaimedStale)

	// The already-ready generation-2 brief must be completely unchanged --
	// recovery must not regress it back toward generation 1's (never
	// actually generated) output.
	after := f.currentBrief(t)
	if after.Generation != readyAtGen2.Generation || after.Status != readyAtGen2.Status {
		t.Fatalf("recovery regressed the newer generation: before=%+v after=%+v", readyAtGen2, after)
	}
	if len(after.Sections) != len(readyAtGen2.Sections) {
		t.Fatalf("recovery altered the newer generation's published sections: before=%+v after=%+v", readyAtGen2, after)
	}

	// No third/duplicate job was spontaneously created by recovery: exactly
	// the original generation-1 job and the generation-2 job exist.
	if got := countPreGenerateJobsForBrief(t, f.st, brief.ID); got != 2 {
		t.Fatalf("expected exactly 2 pre_generate jobs total (gen 1 + gen 2, no duplicate from recovery), got %d", got)
	}
}
