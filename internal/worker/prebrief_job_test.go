package worker

// This file is DB-backed and CI-only: testutil.NewPool skips when
// TEST_DATABASE_URL is unset. package worker (not worker_test) because it
// exercises runPreGenerate and preJobClock directly.

import (
	"context"
	"encoding/base64"
	"encoding/json"
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

// seedReadyPair reconciles a brand-new eligible pair and claims its job,
// returning the event, template, brief id, and claimed job.
func (f *preJobFixture) seedReadyPair(t *testing.T, startsIn time.Duration) (model.CalendarEvent, model.Template, string, model.Job) {
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
	job, ok, err := f.st.ClaimJob(ctx, time.Minute)
	if err != nil || !ok {
		t.Fatalf("claim job: ok=%v err=%v", ok, err)
	}
	return ev, tmpl, briefs[ev.ID][0].ID, job
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
	ev, _, briefID, job := f.seedReadyPair(t, 2*time.Hour)
	_ = job // seedReadyPair already claimed one job; re-claim it fresh below.

	ctx := context.Background()
	for attempt := 1; attempt <= 3; attempt++ {
		f.agent.FailNext(1)
		claimed, ok, err := f.st.ClaimJob(ctx, time.Minute)
		if err != nil || !ok {
			t.Fatalf("attempt %d: claim: ok=%v err=%v", attempt, ok, err)
		}
		f.proc.Process(ctx, claimed)
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
