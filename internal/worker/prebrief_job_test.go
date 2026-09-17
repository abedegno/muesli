package worker

// This file is DB-backed and CI-only: testutil.NewPool skips when
// TEST_DATABASE_URL is unset. package worker (not worker_test) because it
// exercises runPreGenerate and preJobClock directly.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/calendar"
	"github.com/abedegno/muesli/internal/config"
	"github.com/abedegno/muesli/internal/crypto"
	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/plugin"
	"github.com/abedegno/muesli/internal/plugintest"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
)

// preJobFixture is a store + owner + calendar source + processor, with a
// fixed "now" so event-started eligibility checks are deterministic.
type preJobFixture struct {
	t     *testing.T
	proc  *Processor
	st    *store.Store
	owner string
	src   string
	now   time.Time
	agent *plugintest.Stub
}

func newPreJobFixture(t *testing.T) *preJobFixture {
	t.Helper()
	ctx := context.Background()
	st := store.New(testutil.NewPool(t))
	cr, err := crypto.New(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	u, err := st.CreateUser(ctx, "prejob-owner@example.com", "h")
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	src, err := st.CreateSource(ctx, u.ID, "ics", "Cal", "sealed")
	if err != nil {
		t.Fatalf("create source: %v", err)
	}
	agent := plugintest.NewAgent()
	t.Cleanup(agent.Close)

	now := time.Date(2026, 9, 20, 8, 0, 0, 0, time.UTC)
	originalClock := preJobClock
	preJobClock = func() time.Time { return now }
	t.Cleanup(func() { preJobClock = originalClock })

	proc := NewProcessor(st, cr, nil, config.Config{}, nil)
	return &preJobFixture{t: t, proc: proc, st: st, owner: u.ID, src: src.ID, now: now, agent: agent}
}

func (f *preJobFixture) setDefaultAgent(t *testing.T) {
	t.Helper()
	if err := f.st.EnsureDefaultPlugin(context.Background(), f.proc.crypto, model.PluginAgent, "agent", f.agent.URL(), "tok", "{}"); err != nil {
		t.Fatalf("ensure default agent: %v", err)
	}
}

func (f *preJobFixture) seedEvent(t *testing.T, externalID string, startsIn time.Duration) model.CalendarEvent {
	t.Helper()
	ctx := context.Background()
	starts := f.now.Add(startsIn)
	if err := f.st.UpsertEvents(ctx, f.owner, f.src, []calendar.NormalizedEvent{
		{
			ExternalID: externalID, Title: "Planning", StartsAt: starts, EndsAt: starts.Add(30 * time.Minute),
			Description: "Sprint planning", Location: "Room 2", ConferencingURL: "https://meet.example.com/abc",
			Attendees: []model.Attendee{{Name: "Jane", Email: "jane@example.com", Response: "accepted"}},
		},
	}); err != nil {
		t.Fatalf("upsert event: %v", err)
	}
	evs, err := f.st.ListEvents(ctx, f.owner, starts.Add(-time.Minute), starts.Add(time.Minute))
	if err != nil || len(evs) != 1 {
		t.Fatalf("list seeded event: %+v %v", evs, err)
	}
	return evs[0]
}

func (f *preJobFixture) createTemplate(t *testing.T, phase string, autoRun bool) model.Template {
	t.Helper()
	tmpl, err := f.st.CreateTemplate(context.Background(), f.owner, "Pre-read", phase,
		[]model.TemplateSection{{Heading: "Context", Instruction: "Summarize the agenda."}}, autoRun, "", "", nil)
	if err != nil {
		t.Fatalf("create template: %v", err)
	}
	return tmpl
}

// seedPendingPair reconciles a brand-new eligible pair WITHOUT claiming its
// job, returning the event, template, and brief id, so callers that need to
// control their own claim/release cycle (e.g. driving a job through several
// attempts) do not have to fight an already-leased job left over from a
// claim they never settled.
func (f *preJobFixture) seedPendingPair(t *testing.T, startsIn time.Duration) (model.CalendarEvent, model.Template, string) {
	t.Helper()
	ctx := context.Background()
	ev := f.seedEvent(t, "e1", startsIn)
	tmpl := f.createTemplate(t, "pre", true)
	h := "h1"
	if _, err := f.st.ReconcileEventBriefPair(ctx, ev.ID, tmpl.ID, tmpl.Name, &h); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	briefs, err := f.st.EventBriefsForEvents(ctx, f.owner, []string{ev.ID})
	if err != nil || len(briefs[ev.ID]) != 1 {
		t.Fatalf("event briefs: %+v %v", briefs, err)
	}
	return ev, tmpl, briefs[ev.ID][0].ID
}

// seedReadyPair reconciles a brand-new eligible pair and claims its job,
// returning the event, template, brief id, and claimed job.
func (f *preJobFixture) seedReadyPair(t *testing.T, startsIn time.Duration) (model.CalendarEvent, model.Template, string, model.Job) {
	t.Helper()
	ctx := context.Background()
	ev, tmpl, briefID := f.seedPendingPair(t, startsIn)
	job, ok, err := f.st.ClaimJob(ctx, time.Minute)
	if err != nil || !ok {
		t.Fatalf("claim job: ok=%v err=%v", ok, err)
	}
	return ev, tmpl, briefID, job
}

// TestRunPreGenerateSuccessPublishesTypedSourceAndEmptyLegacyFields proves a
// real event produces empty legacy transcript/notes_markdown plus the typed
// calendar_event source, and publishes the plugin's sections.
func TestRunPreGenerateSuccessPublishesTypedSourceAndEmptyLegacyFields(t *testing.T) {
	f := newPreJobFixture(t)
	f.setDefaultAgent(t)
	ev, tmpl, briefID, job := f.seedReadyPair(t, 2*time.Hour)

	retryable, err := f.proc.runPreGenerate(context.Background(), job)
	if err != nil {
		t.Fatalf("runPreGenerate: %v (retryable=%v)", err, retryable)
	}

	var sent plugin.GenerateRequest
	if err := json.Unmarshal(f.agent.LastBody(), &sent); err != nil {
		t.Fatalf("decode captured request: %v", err)
	}
	if len(sent.Transcript) != 0 {
		t.Fatalf("expected empty transcript, got %+v", sent.Transcript)
	}
	if sent.NotesMarkdown != "" {
		t.Fatalf("expected empty notes_markdown, got %q", sent.NotesMarkdown)
	}
	if sent.Source == nil || sent.Source.Kind != plugin.GenerateSourceCalendarEvent || sent.Source.CalendarEvent == nil {
		t.Fatalf("expected a calendar_event source, got %+v", sent.Source)
	}
	if sent.Source.CalendarEvent.Title != ev.Title {
		t.Fatalf("source title = %q, want %q", sent.Source.CalendarEvent.Title, ev.Title)
	}

	briefs, err := f.st.EventBriefsForEvents(context.Background(), f.owner, []string{ev.ID})
	if err != nil {
		t.Fatalf("event briefs: %v", err)
	}
	got := briefs[ev.ID][0]
	if got.ID != briefID || got.Status != model.BriefReady || len(got.Sections) == 0 {
		t.Fatalf("expected a ready brief with sections: %+v", got)
	}
	_ = tmpl
}

// TestRunPreGenerateMalformedPayloadNeverCallsPlugin covers a malformed
// payload, and a payload that disagrees with the job's own typed columns:
// both are terminal and neither ever reaches the plugin.
func TestRunPreGenerateMalformedPayloadNeverCallsPlugin(t *testing.T) {
	f := newPreJobFixture(t)
	f.setDefaultAgent(t)
	ev := f.seedEvent(t, "e1", time.Hour)
	tmpl := f.createTemplate(t, "pre", true)

	cases := []model.Job{
		{ID: "j1", CalendarEventID: ev.ID, BriefID: "b1", BriefGeneration: 1, Type: model.JobPreGenerate, Payload: json.RawMessage(`not json`)},
		{ID: "j2", CalendarEventID: ev.ID, BriefID: "b1", BriefGeneration: 1, Type: model.JobPreGenerate, Payload: json.RawMessage(`{}`)},
		{
			ID: "j3", CalendarEventID: ev.ID, BriefID: "b1", BriefGeneration: 1, Type: model.JobPreGenerate,
			Payload: json.RawMessage(`{"brief_id":"different","template_id":"` + tmpl.ID + `","generation":1}`),
		},
	}
	for _, job := range cases {
		retryable, err := f.proc.runPreGenerate(context.Background(), job)
		if err == nil {
			t.Fatalf("job %s: expected an error", job.ID)
		}
		if retryable {
			t.Fatalf("job %s: expected a terminal (non-retryable) error", job.ID)
		}
	}
	if body := f.agent.LastBody(); len(body) != 0 {
		t.Fatalf("plugin must never be called for a malformed/mismatched payload, got body: %s", body)
	}
}

// TestRunPreGenerateStartedEventCleansUpWithoutPublishing proves an event
// that has started by execution time is ineligible: the brief and its
// queued jobs are removed, and the plugin is never called.
func TestRunPreGenerateStartedEventCleansUpWithoutPublishing(t *testing.T) {
	f := newPreJobFixture(t)
	f.setDefaultAgent(t)
	// Seed at +1h (eligible at reconcile time), then move the fixture clock
	// forward past the start time before running the job.
	ev, _, briefID, job := f.seedReadyPair(t, time.Hour)
	preJobClock = func() time.Time { return f.now.Add(2 * time.Hour) }

	retryable, err := f.proc.runPreGenerate(context.Background(), job)
	if err != nil || retryable {
		t.Fatalf("expected a clean (non-error) no-op, got err=%v retryable=%v", err, retryable)
	}

	briefs, err := f.st.EventBriefsForEvents(context.Background(), f.owner, []string{ev.ID})
	if err != nil {
		t.Fatalf("event briefs: %v", err)
	}
	if len(briefs[ev.ID]) != 0 {
		t.Fatalf("expected the brief removed once the event started: %+v", briefs[ev.ID])
	}
	if _, found, err := f.st.GetEventBriefByID(context.Background(), briefID); err != nil || found {
		t.Fatalf("expected the brief gone: found=%v err=%v", found, err)
	}
	if len(f.agent.LastBody()) != 0 {
		t.Fatal("plugin must never be called once the pair is ineligible")
	}
}

// TestRunPreGenerateWrongPhaseOrAutoRunOffCleansUp covers both other
// ineligibility causes independently: phase changed away from pre, and
// auto_run disabled.
func TestRunPreGenerateWrongPhaseOrAutoRunOffCleansUp(t *testing.T) {
	for _, tc := range []struct {
		name    string
		phase   string
		autoRun bool
	}{
		{name: "phase changed", phase: "after", autoRun: true},
		{name: "auto_run disabled", phase: "pre", autoRun: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newPreJobFixture(t)
			f.setDefaultAgent(t)
			ctx := context.Background()
			ev := f.seedEvent(t, "e1", time.Hour)
			tmpl := f.createTemplate(t, "pre", true)
			h := "h1"
			if _, err := f.st.ReconcileEventBriefPair(ctx, ev.ID, tmpl.ID, tmpl.Name, &h); err != nil {
				t.Fatalf("reconcile: %v", err)
			}
			job, ok, err := f.st.ClaimJob(ctx, time.Minute)
			if err != nil || !ok {
				t.Fatalf("claim: ok=%v err=%v", ok, err)
			}

			// Mutate the template directly at the store level to become
			// ineligible WITHOUT going through UpdateTemplate's own cleanup
			// (that is Task 6's job) -- this isolates runPreGenerate's own
			// pre-execution eligibility check.
			if err := f.st.UpdateTemplate(ctx, f.owner, tmpl.ID, tmpl.Name, tc.phase, tmpl.Sections, tc.autoRun, "", "", nil); err != nil {
				t.Fatalf("update template: %v", err)
			}

			retryable, err := f.proc.runPreGenerate(ctx, job)
			if err != nil || retryable {
				t.Fatalf("expected a clean no-op, got err=%v retryable=%v", err, retryable)
			}
			briefs, err := f.st.EventBriefsForEvents(ctx, f.owner, []string{ev.ID})
			if err != nil {
				t.Fatalf("event briefs: %v", err)
			}
			if len(briefs[ev.ID]) != 0 {
				t.Fatalf("expected the brief removed: %+v", briefs[ev.ID])
			}
			if len(f.agent.LastBody()) != 0 {
				t.Fatal("plugin must never be called once ineligible")
			}
		})
	}
}

// TestRunPreGenerateStaleGenerationSkipsWithoutPublishing proves a job whose
// generation no longer matches the brief's current one (superseded by a
// newer reconciliation) completes as a no-op without touching the newer
// state or calling the plugin.
func TestRunPreGenerateStaleGenerationSkipsWithoutPublishing(t *testing.T) {
	f := newPreJobFixture(t)
	f.setDefaultAgent(t)
	ctx := context.Background()
	ev, tmpl, _, job := f.seedReadyPair(t, 2*time.Hour)

	// Advance the brief to generation 2 before the stale (generation-1) job runs.
	h2 := "h2"
	if _, err := f.st.ReconcileEventBriefPair(ctx, ev.ID, tmpl.ID, tmpl.Name, &h2); err != nil {
		t.Fatalf("advance generation: %v", err)
	}

	retryable, err := f.proc.runPreGenerate(ctx, job)
	if err != nil || retryable {
		t.Fatalf("expected a clean no-op for a stale generation, got err=%v retryable=%v", err, retryable)
	}
	if len(f.agent.LastBody()) != 0 {
		t.Fatal("plugin must never be called for a stale generation")
	}

	briefs, err := f.st.EventBriefsForEvents(ctx, f.owner, []string{ev.ID})
	if err != nil {
		t.Fatalf("event briefs: %v", err)
	}
	if briefs[ev.ID][0].Generation != 2 || briefs[ev.ID][0].Status != model.BriefPending {
		t.Fatalf("the newer generation must be untouched: %+v", briefs[ev.ID][0])
	}
}

// TestRunPreGenerateDeletedBriefSkipsCleanly proves a job whose brief row was
// deleted out from under it (e.g. by template-mutation cleanup) completes
// cleanly without error and without calling the plugin.
func TestRunPreGenerateDeletedBriefSkipsCleanly(t *testing.T) {
	f := newPreJobFixture(t)
	f.setDefaultAgent(t)
	ctx := context.Background()
	ev, tmpl, _, job := f.seedReadyPair(t, 2*time.Hour)

	if err := f.st.DeleteTemplate(ctx, f.owner, tmpl.ID); err != nil {
		t.Fatalf("delete template (cascades the brief): %v", err)
	}

	retryable, err := f.proc.runPreGenerate(ctx, job)
	if err != nil || retryable {
		t.Fatalf("expected a clean no-op once the brief is gone, got err=%v retryable=%v", err, retryable)
	}
	if len(f.agent.LastBody()) != 0 {
		t.Fatal("plugin must never be called once the brief is gone")
	}
	_ = ev
}

// TestRunPreGenerateNoAgentIsTerminal proves a missing default agent at
// execution time is a job failure (mapped through ErrPluginNotConfigured),
// distinct from an ineligible pair -- no cleanup runs, and Process's terminal
// path (exercised via handlePreGenerateTerminalFailure) marks the current
// generation failed.
func TestRunPreGenerateNoAgentIsTerminal(t *testing.T) {
	f := newPreJobFixture(t)
	// Deliberately do not call f.setDefaultAgent.
	ev, tmpl, briefID, job := f.seedReadyPair(t, 2*time.Hour)
	_ = tmpl

	retryable, err := f.proc.runPreGenerate(context.Background(), job)
	if err == nil {
		t.Fatal("expected an error with no default agent configured")
	}
	if retryable {
		t.Fatal("no default agent must be terminal, not retryable")
	}

	f.proc.handlePreGenerateTerminalFailure(context.Background(), job)
	briefs, err := f.st.EventBriefsForEvents(context.Background(), f.owner, []string{ev.ID})
	if err != nil {
		t.Fatalf("event briefs: %v", err)
	}
	got := briefs[ev.ID][0]
	if got.ID != briefID || got.Status != model.BriefFailed {
		t.Fatalf("expected the current generation marked failed: %+v", got)
	}
}

// TestRunPreGenerateRetryableFailureLeavesBriefPending proves a retryable
// plugin failure (5xx) does not touch the brief's visible state -- Process
// retries the job, and only exhaustion (handlePreGenerateTerminalFailure)
// marks it failed.
func TestRunPreGenerateRetryableFailureLeavesBriefPending(t *testing.T) {
	f := newPreJobFixture(t)
	f.setDefaultAgent(t)
	f.agent.FailNext(1)
	ev, _, briefID, job := f.seedReadyPair(t, 2*time.Hour)

	retryable, err := f.proc.runPreGenerate(context.Background(), job)
	if err == nil {
		t.Fatal("expected a plugin error")
	}
	if !retryable {
		t.Fatal("a 500 from the plugin must be retryable")
	}

	briefs, err := f.st.EventBriefsForEvents(context.Background(), f.owner, []string{ev.ID})
	if err != nil {
		t.Fatalf("event briefs: %v", err)
	}
	got := briefs[ev.ID][0]
	if got.ID != briefID || got.Status != model.BriefPending {
		t.Fatalf("a retryable failure must leave the brief pending: %+v", got)
	}
}

// TestRunPreGenerateExhaustedRetriesMarksBriefFailed proves that once a
// retryable failure exhausts store.MaxJobAttempts, Process's terminal path
// marks the CURRENT generation's brief failed -- not merely leaves it
// pending forever.
func TestRunPreGenerateExhaustedRetriesMarksBriefFailed(t *testing.T) {
	f := newPreJobFixture(t)
	f.setDefaultAgent(t)
	// seedPendingPair, not seedReadyPair: this test drives its own claim/fail
	// loop across store.MaxJobAttempts, so the job must start unclaimed --
	// otherwise the first claim below finds nothing (the job would still be
	// leased from an earlier claim nobody ever settled).
	ev, _, briefID := f.seedPendingPair(t, 2*time.Hour)

	ctx := context.Background()
	for attempt := 1; attempt <= 3; attempt++ {
		f.agent.FailNext(1)
		claimed, ok, err := f.st.ClaimJob(ctx, time.Minute)
		if err != nil || !ok {
			t.Fatalf("attempt %d: claim: ok=%v err=%v", attempt, ok, err)
		}
		f.proc.Process(ctx, claimed)
		// A retryable failure sets a real backoff lease (see
		// store.RetryBackoff); clear it so the next attempt's ClaimJob can
		// reclaim immediately instead of waiting out the backoff, matching the
		// convention in pipeline_test.go's exhausted-retries tests.
		if _, err := f.st.Pool().Exec(ctx, "UPDATE jobs SET lease_expires_at = NULL WHERE id=$1", claimed.ID); err != nil {
			t.Fatalf("attempt %d: clear retry lease: %v", attempt, err)
		}
	}

	briefs, err := f.st.EventBriefsForEvents(ctx, f.owner, []string{ev.ID})
	if err != nil {
		t.Fatalf("event briefs: %v", err)
	}
	got := briefs[ev.ID][0]
	if got.ID != briefID || got.Status != model.BriefFailed {
		t.Fatalf("expected the brief marked failed after exhausting retries: %+v", got)
	}
}

// TestCleanupIneligiblePreBriefPropagatesDBFailure proves
// cleanupIneligiblePreBrief does not swallow a real store-level failure: it
// returns the error, and -- because the underlying DELETE never committed --
// the brief it was trying to clean up is left completely untouched (not
// silently transitioned into some inconsistent state), so a subsequent
// retry attempt (or the next reconciliation pass) sees exactly the same
// eligible-for-cleanup row it started with.
func TestCleanupIneligiblePreBriefPropagatesDBFailure(t *testing.T) {
	f := newPreJobFixture(t)
	f.setDefaultAgent(t)
	ev, _, briefID, job := f.seedReadyPair(t, 2*time.Hour)
	_ = ev

	before, found, err := f.st.GetEventBriefByID(context.Background(), briefID)
	if err != nil || !found {
		t.Fatalf("expected the seeded brief to exist: found=%v err=%v", found, err)
	}

	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel() // already canceled before any query runs: a real, deterministic DB-layer failure

	if err := f.proc.cleanupIneligiblePreBrief(canceledCtx, job, briefID, before.Generation, "test-injected-failure"); err == nil {
		t.Fatal("expected cleanupIneligiblePreBrief to propagate the store's error, got nil")
	}

	after, found, err := f.st.GetEventBriefByID(context.Background(), briefID)
	if err != nil || !found {
		t.Fatalf("expected the brief to still exist after a failed cleanup: found=%v err=%v", found, err)
	}
	if after.Status != before.Status || after.Generation != before.Generation {
		t.Fatalf("a failed cleanup must leave the brief exactly as it was: before=%+v after=%+v", before, after)
	}
}

// TestRunPreGenerateCleanupFailureIsRetryableNotSuccess proves that when the
// ineligibility-cleanup DB call itself fails, runPreGenerate does NOT settle
// the job as done (which would leave the brief permanently stuck, since
// nothing would ever retry it again): it returns (retryable=true, err), the
// same "genuinely failed, try again" contract as every other DB-error branch
// in this function. The failure here is a real, deterministic Postgres error
// (an invalid uuid literal), not a mock -- see the "not-a-real-uuid" brief id
// below, which the job's own typed BriefID column is deliberately set to
// match so it passes the earlier payload/job-column consistency check and
// actually reaches cleanupIneligiblePreBrief.
func TestRunPreGenerateCleanupFailureIsRetryableNotSuccess(t *testing.T) {
	f := newPreJobFixture(t)
	f.setDefaultAgent(t)
	ev := f.seedEvent(t, "e1", time.Hour)
	tmpl := f.createTemplate(t, "pre", true)

	badBriefID := "not-a-real-uuid"
	job := model.Job{
		ID: "j1", CalendarEventID: ev.ID, BriefID: badBriefID, BriefGeneration: 1, Type: model.JobPreGenerate,
		Payload: json.RawMessage(`{"brief_id":"` + badBriefID + `","template_id":"` + tmpl.ID + `","generation":1}`),
	}
	// Move the fixture clock past the event's start so runPreGenerate takes
	// the "event has started" ineligibility-cleanup branch, which is the
	// very first cleanup call site and requires no template/brief load
	// first.
	preJobClock = func() time.Time { return f.now.Add(2 * time.Hour) }

	retryable, err := f.proc.runPreGenerate(context.Background(), job)
	if err == nil {
		t.Fatal("expected the cleanup DB failure to surface as a job error")
	}
	if !retryable {
		t.Fatalf("expected a cleanup DB failure to be retryable (not settled as success), got retryable=false, err=%v", err)
	}
	if len(f.agent.LastBody()) != 0 {
		t.Fatal("plugin must never be called when cleanup fails")
	}
}

// TestRunPreGenerateBriefMismatchSkipsWithoutMutating is the adversarial
// case for the missing brief<->event/template binding check: a job whose
// typed CalendarEventID names event A, but whose payload brief_id actually
// names a real brief that belongs to a DIFFERENT event B (simulating a
// malformed or tampered job -- a legitimately-enqueued job can never
// disagree, see ReconcileEventBriefPair/enqueuePreGenerateJobTx). Proves
// runPreGenerate rejects it as a clean no-op -- never calls the plugin, and
// crucially never mutates event B's real, unrelated brief -- rather than
// publishing event A's generated content into it.
func TestRunPreGenerateBriefMismatchSkipsWithoutMutating(t *testing.T) {
	f := newPreJobFixture(t)
	f.setDefaultAgent(t)
	ctx := context.Background()

	evA := f.seedEvent(t, "e1", 2*time.Hour)
	tmplA := f.createTemplate(t, "pre", true)
	hA := "hA"
	if _, err := f.st.ReconcileEventBriefPair(ctx, evA.ID, tmplA.ID, tmplA.Name, &hA); err != nil {
		t.Fatalf("reconcile A: %v", err)
	}

	evB := f.seedEvent(t, "e2", 3*time.Hour)
	tmplB, err := f.st.CreateTemplate(ctx, f.owner, "Pre-read 2", "pre",
		[]model.TemplateSection{{Heading: "Context", Instruction: "Summarize the agenda."}}, true, "", "", nil)
	if err != nil {
		t.Fatalf("create template B: %v", err)
	}
	hB := "hB"
	if _, err := f.st.ReconcileEventBriefPair(ctx, evB.ID, tmplB.ID, tmplB.Name, &hB); err != nil {
		t.Fatalf("reconcile B: %v", err)
	}
	briefsB, err := f.st.EventBriefsForEvents(ctx, f.owner, []string{evB.ID})
	if err != nil || len(briefsB[evB.ID]) != 1 {
		t.Fatalf("event B briefs: %+v %v", briefsB, err)
	}
	briefB := briefsB[evB.ID][0]

	// A tampered job: typed CalendarEventID says A, but the payload's
	// brief_id/template_id actually name B's real, unrelated brief/template.
	job := model.Job{
		ID: "tampered", CalendarEventID: evA.ID, BriefID: briefB.ID, BriefGeneration: briefB.Generation, Type: model.JobPreGenerate,
		Payload: json.RawMessage(`{"brief_id":"` + briefB.ID + `","template_id":"` + tmplB.ID + `","generation":` +
			strconv.Itoa(briefB.Generation) + `}`),
	}

	retryable, err := f.proc.runPreGenerate(ctx, job)
	if err != nil || retryable {
		t.Fatalf("expected a clean no-op for a mismatched brief, got err=%v retryable=%v", err, retryable)
	}
	if len(f.agent.LastBody()) != 0 {
		t.Fatal("plugin must never be called for a mismatched brief target")
	}

	afterB, found, err := f.st.GetEventBriefByID(ctx, briefB.ID)
	if err != nil || !found {
		t.Fatalf("expected event B's real brief to still exist untouched: found=%v err=%v", found, err)
	}
	if afterB.Status != briefB.Status || afterB.Generation != briefB.Generation || len(afterB.Sections) != 0 {
		t.Fatalf("event B's brief must be completely untouched by A's mismatched job: before=%+v after=%+v", briefB, afterB)
	}
}

// TestHandlePreGenerateTerminalFailureRefusesMismatchedBrief proves the
// terminal-failure path has the same binding protection as runPreGenerate's
// own pre-execution check: it is reachable with an UNVALIDATED payload (the
// earlier malformed-payload / job-column-mismatch checks in runPreGenerate
// return a terminal error before ever loading a brief), so pl.BriefID here
// may name a brief that belongs to a completely different event. This must
// never mark that unrelated brief failed.
func TestHandlePreGenerateTerminalFailureRefusesMismatchedBrief(t *testing.T) {
	f := newPreJobFixture(t)
	f.setDefaultAgent(t)
	ctx := context.Background()

	evA := f.seedEvent(t, "e1", 2*time.Hour)
	evB := f.seedEvent(t, "e2", 3*time.Hour)
	tmplB, err := f.st.CreateTemplate(ctx, f.owner, "Pre-read 2", "pre",
		[]model.TemplateSection{{Heading: "Context", Instruction: "Summarize the agenda."}}, true, "", "", nil)
	if err != nil {
		t.Fatalf("create template B: %v", err)
	}
	hB := "hB"
	if _, err := f.st.ReconcileEventBriefPair(ctx, evB.ID, tmplB.ID, tmplB.Name, &hB); err != nil {
		t.Fatalf("reconcile B: %v", err)
	}
	briefsB, err := f.st.EventBriefsForEvents(ctx, f.owner, []string{evB.ID})
	if err != nil || len(briefsB[evB.ID]) != 1 {
		t.Fatalf("event B briefs: %+v %v", briefsB, err)
	}
	briefB := briefsB[evB.ID][0]

	// A malformed job "belonging" to event A whose payload names B's real
	// brief -- exactly the shape runPreGenerate's own early malformed-
	// payload checks would reject with a terminal error before ever loading
	// a brief (see TestRunPreGenerateMalformedPayloadNeverCallsPlugin's "j3"
	// case), so Process would route it here with pl.BriefID still unchecked.
	job := model.Job{
		ID: "tampered-terminal", CalendarEventID: evA.ID, BriefID: "mismatched-typed-column", BriefGeneration: briefB.Generation, Type: model.JobPreGenerate,
		Payload: json.RawMessage(`{"brief_id":"` + briefB.ID + `","template_id":"` + tmplB.ID + `","generation":` +
			strconv.Itoa(briefB.Generation) + `}`),
	}

	f.proc.handlePreGenerateTerminalFailure(ctx, job)

	afterB, found, err := f.st.GetEventBriefByID(ctx, briefB.ID)
	if err != nil || !found {
		t.Fatalf("expected event B's real brief to still exist untouched: found=%v err=%v", found, err)
	}
	if afterB.Status == model.BriefFailed {
		t.Fatalf("a mismatched terminal-failure job must never mark an unrelated brief failed: %+v", afterB)
	}
	if afterB.Status != briefB.Status || afterB.Generation != briefB.Generation {
		t.Fatalf("event B's brief must be completely untouched: before=%+v after=%+v", briefB, afterB)
	}
}

// TestRunPreGenerateCrossWiredJobStartedEventNeverDeletesUnrelatedBrief is
// the adversarial case the earlier binding check missed: a cross-wired job
// whose typed CalendarEventID names a REAL event that HAS started (so the
// very first eligibility branch, "event has started", is reachable and
// destructive), while its BriefID/payload actually name a completely
// different, still-eligible, still-pending brief for an unrelated event.
// Before the fix this ran the "event has started" cleanup against the
// job's own (started) event and deleted whatever brief the payload named --
// here, a brief that has nothing to do with that started event and remains
// fully eligible. Proves the binding check runs BEFORE the started-event
// cleanup branch, so the mismatched brief is left completely untouched.
func TestRunPreGenerateCrossWiredJobStartedEventNeverDeletesUnrelatedBrief(t *testing.T) {
	f := newPreJobFixture(t)
	f.setDefaultAgent(t)
	ctx := context.Background()

	// The job's own typed target: a real event that will have started by
	// the time the job runs.
	evStarted := f.seedEvent(t, "e-started", time.Hour)

	// The unrelated, still-eligible, still-pending pair the tampered
	// payload actually names.
	evOther := f.seedEvent(t, "e-other", 5*time.Hour)
	tmplOther := f.createTemplate(t, "pre", true)
	hOther := "h-other"
	if _, err := f.st.ReconcileEventBriefPair(ctx, evOther.ID, tmplOther.ID, tmplOther.Name, &hOther); err != nil {
		t.Fatalf("reconcile other: %v", err)
	}
	briefsOther, err := f.st.EventBriefsForEvents(ctx, f.owner, []string{evOther.ID})
	if err != nil || len(briefsOther[evOther.ID]) != 1 {
		t.Fatalf("event other briefs: %+v %v", briefsOther, err)
	}
	briefOther := briefsOther[evOther.ID][0]

	// Move the clock so evStarted has started but evOther has not.
	preJobClock = func() time.Time { return f.now.Add(2 * time.Hour) }

	// A cross-wired job: typed CalendarEventID names the STARTED event, but
	// BriefID/payload actually name the unrelated, still-eligible brief.
	job := model.Job{
		ID: "cross-wired-started", CalendarEventID: evStarted.ID, BriefID: briefOther.ID, BriefGeneration: briefOther.Generation, Type: model.JobPreGenerate,
		Payload: json.RawMessage(`{"brief_id":"` + briefOther.ID + `","template_id":"` + tmplOther.ID + `","generation":` +
			strconv.Itoa(briefOther.Generation) + `}`),
	}

	retryable, err := f.proc.runPreGenerate(ctx, job)
	if err != nil || retryable {
		t.Fatalf("expected a clean no-op for a cross-wired started-event job, got err=%v retryable=%v", err, retryable)
	}
	if len(f.agent.LastBody()) != 0 {
		t.Fatal("plugin must never be called for a cross-wired job")
	}

	afterOther, found, err := f.st.GetEventBriefByID(ctx, briefOther.ID)
	if err != nil || !found {
		t.Fatalf("expected the unrelated, still-eligible brief to survive untouched: found=%v err=%v", found, err)
	}
	if afterOther.Status != briefOther.Status || afterOther.Generation != briefOther.Generation {
		t.Fatalf("the unrelated brief must be completely untouched by the started event's cleanup: before=%+v after=%+v", briefOther, afterOther)
	}
}

// TestRunPreGenerateCrossWiredJobIneligibleTemplateNeverDeletesUnrelatedBrief
// is the adversarial case for the "template no longer eligible" cleanup
// branch: a single real, eligible event has two templates -- one still
// pre/auto-run (with a real, currently-pending brief that must survive) and
// one that has since had auto_run disabled. A tampered job's typed
// CalendarEventID/BriefID correctly name the GOOD pair, but its payload
// names the BAD (ineligible) template_id. Before the fix this ran the
// "template no longer pre/auto-run" cleanup against the payload's
// ineligible template and deleted the good, unrelated, still-eligible
// brief on the strength of a template it was never actually bound to.
// Proves the binding check runs first, so the good brief is left
// completely untouched.
func TestRunPreGenerateCrossWiredJobIneligibleTemplateNeverDeletesUnrelatedBrief(t *testing.T) {
	f := newPreJobFixture(t)
	f.setDefaultAgent(t)
	ctx := context.Background()

	ev := f.seedEvent(t, "e1", 2*time.Hour)

	tmplGood := f.createTemplate(t, "pre", true)
	hGood := "h-good"
	if _, err := f.st.ReconcileEventBriefPair(ctx, ev.ID, tmplGood.ID, tmplGood.Name, &hGood); err != nil {
		t.Fatalf("reconcile good: %v", err)
	}
	briefsGood, err := f.st.EventBriefsForEvents(ctx, f.owner, []string{ev.ID})
	if err != nil || len(briefsGood[ev.ID]) != 1 {
		t.Fatalf("event good briefs: %+v %v", briefsGood, err)
	}
	briefGood := briefsGood[ev.ID][0]

	// A second real template on the same event, but ineligible (auto_run
	// disabled) -- never reconciled to a brief of its own. A distinct name
	// avoids colliding with tmplGood's (template names are unique per
	// owner), same as TestRunPreGenerateBriefMismatchSkipsWithoutMutating's
	// "Pre-read 2".
	tmplBad, err := f.st.CreateTemplate(ctx, f.owner, "Pre-read (ineligible)", "pre",
		[]model.TemplateSection{{Heading: "Context", Instruction: "Summarize the agenda."}}, false, "", "", nil)
	if err != nil {
		t.Fatalf("create template bad: %v", err)
	}

	// Typed CalendarEventID/BriefID correctly name the good pair, but the
	// payload's template_id names the unrelated, ineligible template.
	job := model.Job{
		ID: "cross-wired-template", CalendarEventID: ev.ID, BriefID: briefGood.ID, BriefGeneration: briefGood.Generation, Type: model.JobPreGenerate,
		Payload: json.RawMessage(`{"brief_id":"` + briefGood.ID + `","template_id":"` + tmplBad.ID + `","generation":` +
			strconv.Itoa(briefGood.Generation) + `}`),
	}

	retryable, err := f.proc.runPreGenerate(ctx, job)
	if err != nil || retryable {
		t.Fatalf("expected a clean no-op for a cross-wired ineligible-template job, got err=%v retryable=%v", err, retryable)
	}
	if len(f.agent.LastBody()) != 0 {
		t.Fatal("plugin must never be called for a cross-wired job")
	}

	afterGood, found, err := f.st.GetEventBriefByID(ctx, briefGood.ID)
	if err != nil || !found {
		t.Fatalf("expected the good, still-eligible brief to survive untouched: found=%v err=%v", found, err)
	}
	if afterGood.Status != briefGood.Status || afterGood.Generation != briefGood.Generation {
		t.Fatalf("the good brief must be completely untouched by the ineligible template's cleanup: before=%+v after=%+v", briefGood, afterGood)
	}
}

// blockingAgentStub is a minimal fake /generate endpoint that blocks until
// explicitly released, letting a test provoke a REAL concurrent race
// against an in-flight plugin call -- as opposed to simulating one by
// simply reordering calls around a plugin invocation that already
// returned. Generate() (see internal/plugin/client.go) only ever POSTs
// /generate, so that is the only route this stub needs.
type blockingAgentStub struct {
	srv     *httptest.Server
	started chan struct{}
	release chan struct{}
}

func newBlockingAgentStub(t *testing.T) *blockingAgentStub {
	t.Helper()
	b := &blockingAgentStub{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/generate", func(w http.ResponseWriter, r *http.Request) {
		close(b.started) // signals the test that the plugin call has genuinely begun
		<-b.release      // blocks here until the test releases it
		var req plugin.GenerateRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		var sections []model.SummarySection
		for _, sec := range req.Template.Sections {
			sections = append(sections, model.SummarySection{Heading: sec.Heading, ContentMarkdown: "Stub summary for " + sec.Heading + "."})
		}
		_ = json.NewEncoder(w).Encode(plugin.GenerateResponse{Summary: plugin.SummaryPayload{Sections: sections}, Model: "stub"})
	})
	b.srv = httptest.NewServer(mux)
	t.Cleanup(b.srv.Close)
	return b
}

func (b *blockingAgentStub) URL() string { return b.srv.URL }

// waitStarted blocks until the in-flight /generate request has actually
// reached the handler (not merely been dispatched), so the test's
// concurrent mutation below is guaranteed to race against a plugin call
// that is genuinely already in progress.
func (b *blockingAgentStub) waitStarted(t *testing.T) {
	t.Helper()
	select {
	case <-b.started:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the blocking agent to receive the /generate call")
	}
}

func (b *blockingAgentStub) unblock() { close(b.release) }

type runPreGenerateResult struct {
	retryable bool
	err       error
}

// runPreGenerateAsync runs runPreGenerate on its own goroutine so the
// calling test can perform a concurrent store mutation while the plugin
// call it triggers is blocked inside blockingAgentStub.
func runPreGenerateAsync(proc *Processor, ctx context.Context, job model.Job) <-chan runPreGenerateResult {
	ch := make(chan runPreGenerateResult, 1)
	go func() {
		retryable, err := proc.runPreGenerate(ctx, job)
		ch <- runPreGenerateResult{retryable: retryable, err: err}
	}()
	return ch
}

// TestRunPreGenerateInFlightGenerationAdvanceDiscardsPublish is the
// genuinely-concurrent counterpart to TestRunPreGenerateStaleGenerationSkipsWithoutPublishing:
// that test proves runPreGenerate's early generation check rejects an
// ALREADY-stale job before ever calling the plugin. This test proves the
// separate guard that matters once the plugin call is already underway --
// generation 1's plugin call is genuinely in flight when reconciliation
// concurrently advances the SAME pair to generation 2 (e.g. the agenda
// changed mid-generation); when generation 1's response finally arrives,
// PublishEventBrief's `WHERE id=? AND generation=?` guard discards it as
// zero affected rows rather than overwriting generation 2's now-current
// (pending) state.
func TestRunPreGenerateInFlightGenerationAdvanceDiscardsPublish(t *testing.T) {
	f := newPreJobFixture(t)
	ctx := context.Background()

	agent := newBlockingAgentStub(t)
	if err := f.st.EnsureDefaultPlugin(ctx, f.proc.crypto, model.PluginAgent, "agent", agent.URL(), "tok", "{}"); err != nil {
		t.Fatalf("ensure default agent: %v", err)
	}
	ev, tmpl, briefID, job := f.seedReadyPair(t, 2*time.Hour)

	resultCh := runPreGenerateAsync(f.proc, ctx, job)
	agent.waitStarted(t)

	// While generation 1's plugin call is genuinely in flight, reconcile
	// the same pair again with a changed hash -- advancing the brief to
	// generation 2, exactly as a concurrent calendar sync would.
	h2 := "h2"
	if _, err := f.st.ReconcileEventBriefPair(ctx, ev.ID, tmpl.ID, tmpl.Name, &h2); err != nil {
		t.Fatalf("reconcile gen 2: %v", err)
	}
	advanced, found, err := f.st.GetEventBriefByID(ctx, briefID)
	if err != nil || !found || advanced.Generation != 2 {
		t.Fatalf("expected the brief advanced to generation 2 while generation 1 was in flight: found=%v err=%v brief=%+v", found, err, advanced)
	}

	// Now let generation 1's (now-stale) plugin call complete with real
	// output.
	agent.unblock()
	res := <-resultCh
	if res.err != nil || res.retryable {
		t.Fatalf("expected generation 1's job to settle cleanly once its publish is discarded, got err=%v retryable=%v", res.err, res.retryable)
	}

	after, found, err := f.st.GetEventBriefByID(ctx, briefID)
	if err != nil || !found {
		t.Fatalf("expected the brief to still exist at generation 2: found=%v err=%v", found, err)
	}
	if after.Generation != 2 || after.Status != model.BriefPending {
		t.Fatalf("generation 1's late-arriving publish must not affect generation 2's state: %+v", after)
	}
	if len(after.Sections) != 0 {
		t.Fatalf("generation 1's stale output must never appear in generation 2's sections: %+v", after)
	}
	_ = tmpl
}

// TestRunPreGenerateInFlightBriefDeletionDiscardsPublish is the deletion
// counterpart: generation 1's plugin call is genuinely in flight when the
// brief is deleted out from under it (e.g. concurrent template-mutation
// cleanup, or a second reconciliation pass finding the pair ineligible).
// When generation 1's response finally arrives, the same guarded publish
// affects zero rows because the row no longer exists at all -- it must not
// resurrect the deleted brief.
func TestRunPreGenerateInFlightBriefDeletionDiscardsPublish(t *testing.T) {
	f := newPreJobFixture(t)
	ctx := context.Background()

	agent := newBlockingAgentStub(t)
	if err := f.st.EnsureDefaultPlugin(ctx, f.proc.crypto, model.PluginAgent, "agent", agent.URL(), "tok", "{}"); err != nil {
		t.Fatalf("ensure default agent: %v", err)
	}
	_, _, briefID, job := f.seedReadyPair(t, 2*time.Hour)

	resultCh := runPreGenerateAsync(f.proc, ctx, job)
	agent.waitStarted(t)

	// While generation 1's plugin call is genuinely in flight, the brief is
	// deleted -- the same generation-guarded cleanup runPreGenerate itself
	// uses for ineligibility, called here directly to model a concurrent
	// cleanup (e.g. a race with template-mutation cleanup or another
	// reconciliation pass).
	deleted, err := f.st.CleanupIneligibleEventBriefIfCurrent(ctx, briefID, 1)
	if err != nil || !deleted {
		t.Fatalf("expected the concurrent cleanup to delete the brief: deleted=%v err=%v", deleted, err)
	}
	if _, found, err := f.st.GetEventBriefByID(ctx, briefID); err != nil || found {
		t.Fatalf("expected the brief gone before generation 1's plugin call returns: found=%v err=%v", found, err)
	}

	agent.unblock()
	res := <-resultCh
	if res.err != nil || res.retryable {
		t.Fatalf("expected generation 1's job to settle cleanly once its publish is discarded, got err=%v retryable=%v", res.err, res.retryable)
	}

	if _, found, err := f.st.GetEventBriefByID(ctx, briefID); err != nil || found {
		t.Fatalf("generation 1's late-arriving publish must not resurrect the deleted brief: found=%v err=%v", found, err)
	}
}
