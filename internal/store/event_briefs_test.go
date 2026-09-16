package store_test

// This file is DB-backed and CI-only: testutil.NewPool skips when
// TEST_DATABASE_URL is unset.

import (
	"context"
	"encoding/json"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/calendar"
	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
)

// briefTestFixture is a store + owner + calendar source, with events seeded
// relative to a fixed "now" so eligibility-window assertions are
// deterministic instead of racing the wall clock.
type briefTestFixture struct {
	st     *store.Store
	owner  string
	source string
	now    time.Time
}

func newBriefTestFixture(t *testing.T) briefTestFixture {
	t.Helper()
	ctx := context.Background()
	st := store.New(testutil.NewPool(t))
	u, err := st.CreateUser(ctx, "brief-owner-"+uniqueSuffix()+"@example.com", "h")
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	src, err := st.CreateSource(ctx, u.ID, "ics", "Cal", "sealed")
	if err != nil {
		t.Fatalf("create source: %v", err)
	}
	return briefTestFixture{st: st, owner: u.ID, source: src.ID, now: time.Date(2026, 9, 20, 8, 0, 0, 0, time.UTC)}
}

func uniqueSuffix() string {
	return strconv.FormatInt(seedUserCounter.Add(1), 10)
}

// seedEvent creates one event starting startsIn after the fixture's now and
// returns its id.
func (f briefTestFixture) seedEvent(t *testing.T, externalID string, startsIn time.Duration) string {
	t.Helper()
	ctx := context.Background()
	starts := f.now.Add(startsIn)
	if err := f.st.UpsertEvents(ctx, f.owner, f.source, []calendar.NormalizedEvent{
		{ExternalID: externalID, Title: "Meeting " + externalID, StartsAt: starts, EndsAt: starts.Add(30 * time.Minute)},
	}); err != nil {
		t.Fatalf("upsert event %s: %v", externalID, err)
	}
	evs, err := f.st.ListEvents(ctx, f.owner, starts.Add(-time.Minute), starts.Add(time.Minute))
	if err != nil || len(evs) != 1 {
		t.Fatalf("list seeded event %s: %+v %v", externalID, evs, err)
	}
	return evs[0].ID
}

func (f briefTestFixture) createTemplate(t *testing.T, name, phase string, autoRun bool) model.Template {
	t.Helper()
	tmpl, err := f.st.CreateTemplate(context.Background(), f.owner, name, phase,
		[]model.TemplateSection{{Heading: "Context", Instruction: "Summarize."}}, autoRun, "", "", nil)
	if err != nil {
		t.Fatalf("create template %s: %v", name, err)
	}
	return tmpl
}

func hashPtr(s string) *string { return &s }

func TestReconcileEventBriefPairInsertsAndEnqueues(t *testing.T) {
	t.Parallel()
	f := newBriefTestFixture(t)
	eventID := f.seedEvent(t, "e1", time.Hour)
	tmpl := f.createTemplate(t, "Pre-read", "pre", true)
	ctx := context.Background()

	enqueued, err := f.st.ReconcileEventBriefPair(ctx, eventID, tmpl.ID, tmpl.Name, hashPtr("h1"))
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if !enqueued {
		t.Fatal("expected a new pair to enqueue")
	}

	briefs, err := f.st.EventBriefsForEvents(ctx, f.owner, []string{eventID})
	if err != nil {
		t.Fatalf("event briefs: %v", err)
	}
	got := briefs[eventID]
	if len(got) != 1 || got[0].TemplateID != tmpl.ID || got[0].Status != model.BriefPending {
		t.Fatalf("unexpected briefs: %+v", got)
	}

	job, ok, err := f.st.ClaimJob(ctx, 30*time.Second)
	if err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	if job.CalendarEventID != eventID || job.BriefID != got[0].ID || job.BriefGeneration != 1 || job.Type != model.JobPreGenerate {
		t.Fatalf("unexpected job: %+v", job)
	}
	var payload struct {
		BriefID    string `json:"brief_id"`
		TemplateID string `json:"template_id"`
		Generation int    `json:"generation"`
	}
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.BriefID != got[0].ID || payload.TemplateID != tmpl.ID || payload.Generation != 1 {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

func TestReconcileEventBriefPairNoOpWhenHashUnchanged(t *testing.T) {
	t.Parallel()
	f := newBriefTestFixture(t)
	eventID := f.seedEvent(t, "e1", time.Hour)
	tmpl := f.createTemplate(t, "Pre-read", "pre", true)
	ctx := context.Background()

	if _, err := f.st.ReconcileEventBriefPair(ctx, eventID, tmpl.ID, tmpl.Name, hashPtr("h1")); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	enqueued, err := f.st.ReconcileEventBriefPair(ctx, eventID, tmpl.ID, tmpl.Name, hashPtr("h1"))
	if err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	if enqueued {
		t.Fatal("an unchanged hash must be a no-op")
	}

	briefs, _ := f.st.EventBriefsForEvents(ctx, f.owner, []string{eventID})
	if briefs[eventID][0].Generation != 1 {
		t.Fatalf("generation should not have advanced: %+v", briefs[eventID][0])
	}
}

func TestReconcileEventBriefPairAdvancesGenerationOnHashChange(t *testing.T) {
	t.Parallel()
	f := newBriefTestFixture(t)
	eventID := f.seedEvent(t, "e1", time.Hour)
	tmpl := f.createTemplate(t, "Pre-read", "pre", true)
	ctx := context.Background()

	if _, err := f.st.ReconcileEventBriefPair(ctx, eventID, tmpl.ID, tmpl.Name, hashPtr("h1")); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	enqueued, err := f.st.ReconcileEventBriefPair(ctx, eventID, tmpl.ID, tmpl.Name, hashPtr("h2"))
	if err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	if !enqueued {
		t.Fatal("a changed hash must enqueue")
	}

	briefs, _ := f.st.EventBriefsForEvents(ctx, f.owner, []string{eventID})
	if briefs[eventID][0].Generation != 2 || briefs[eventID][0].Status != model.BriefPending {
		t.Fatalf("expected generation 2 pending: %+v", briefs[eventID][0])
	}

	jobs, err := f.st.ListJobsByNoteID(ctx, "") // sanity: note-scoped listing must never see event jobs
	if err != nil {
		t.Fatalf("list note jobs: %v", err)
	}
	if len(jobs) != 0 {
		t.Fatalf("note job listing leaked event jobs: %+v", jobs)
	}
}

func TestReconcileEventBriefPairNilHashSetsFailedWithoutJob(t *testing.T) {
	t.Parallel()
	f := newBriefTestFixture(t)
	eventID := f.seedEvent(t, "e1", time.Hour)
	tmpl := f.createTemplate(t, "Pre-read", "pre", true)
	ctx := context.Background()

	enqueued, err := f.st.ReconcileEventBriefPair(ctx, eventID, tmpl.ID, tmpl.Name, nil)
	if err != nil {
		t.Fatalf("reconcile with no agent: %v", err)
	}
	if enqueued {
		t.Fatal("a missing default agent must never enqueue")
	}

	briefs, _ := f.st.EventBriefsForEvents(ctx, f.owner, []string{eventID})
	got := briefs[eventID]
	if len(got) != 1 || got[0].Status != model.BriefFailed {
		t.Fatalf("expected a failed placeholder brief: %+v", got)
	}

	if _, ok, err := f.st.ClaimJob(ctx, 30*time.Second); err != nil || ok {
		t.Fatalf("expected no job to have been enqueued: ok=%v err=%v", ok, err)
	}
}

func TestReconcileEventBriefPairNullToRealHashEnqueues(t *testing.T) {
	t.Parallel()
	f := newBriefTestFixture(t)
	eventID := f.seedEvent(t, "e1", time.Hour)
	tmpl := f.createTemplate(t, "Pre-read", "pre", true)
	ctx := context.Background()

	if _, err := f.st.ReconcileEventBriefPair(ctx, eventID, tmpl.ID, tmpl.Name, nil); err != nil {
		t.Fatalf("reconcile with no agent: %v", err)
	}
	enqueued, err := f.st.ReconcileEventBriefPair(ctx, eventID, tmpl.ID, tmpl.Name, hashPtr("h1"))
	if err != nil {
		t.Fatalf("reconcile once an agent exists: %v", err)
	}
	if !enqueued {
		t.Fatal("gaining a hash after none must enqueue")
	}

	briefs, _ := f.st.EventBriefsForEvents(ctx, f.owner, []string{eventID})
	got := briefs[eventID][0]
	if got.Status != model.BriefPending || got.Generation != 2 {
		t.Fatalf("expected pending generation 2: %+v", got)
	}
}

// TestReconcileEventBriefPairConcurrentInsertRace proves two reconcilers
// racing to create the SAME brand-new (event, template) pair converge on one
// brief row and one job, instead of erroring or double-enqueuing.
func TestReconcileEventBriefPairConcurrentInsertRace(t *testing.T) {
	t.Parallel()
	f := newBriefTestFixture(t)
	eventID := f.seedEvent(t, "e1", time.Hour)
	tmpl := f.createTemplate(t, "Pre-read", "pre", true)
	ctx := context.Background()

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = f.st.ReconcileEventBriefPair(ctx, eventID, tmpl.ID, tmpl.Name, hashPtr("h1"))
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("reconciler %d failed: %v", i, err)
		}
	}

	briefs, err := f.st.EventBriefsForEvents(ctx, f.owner, []string{eventID})
	if err != nil {
		t.Fatalf("event briefs: %v", err)
	}
	if len(briefs[eventID]) != 1 {
		t.Fatalf("expected exactly one brief row after the race, got %+v", briefs[eventID])
	}
	if briefs[eventID][0].Generation != 1 {
		t.Fatalf("expected generation 1 after the race: %+v", briefs[eventID][0])
	}

	var jobCount int
	for {
		_, ok, err := f.st.ClaimJob(ctx, 30*time.Second)
		if err != nil {
			t.Fatalf("claim: %v", err)
		}
		if !ok {
			break
		}
		jobCount++
	}
	if jobCount != 1 {
		t.Fatalf("expected exactly one enqueued job after the race, got %d", jobCount)
	}
}

func TestUpcomingEventsForSourceBatchWindowAndPaging(t *testing.T) {
	t.Parallel()
	f := newBriefTestFixture(t)
	ctx := context.Background()

	tooSoonID := f.seedEvent(t, "already-started", -time.Hour)  // started -- not eligible
	inWindowID := f.seedEvent(t, "in-window", 2*time.Hour)      // eligible
	tooFarID := f.seedEvent(t, "too-far", 8*24*time.Hour)       // beyond 7d -- not eligible
	boundaryID := f.seedEvent(t, "at-boundary", 7*24*time.Hour) // exactly at the boundary -- eligible (<=)

	got, err := f.st.UpcomingEventsForSourceBatch(ctx, f.owner, f.source, f.now, "", 100)
	if err != nil {
		t.Fatalf("upcoming events: %v", err)
	}
	ids := map[string]bool{}
	for _, ev := range got {
		ids[ev.ID] = true
	}
	if !ids[inWindowID] || !ids[boundaryID] {
		t.Fatalf("expected the in-window and at-boundary events, got %+v", got)
	}
	if ids[tooSoonID] || ids[tooFarID] {
		t.Fatalf("expected already-started/too-far events excluded, got %+v", got)
	}

	// A second owner's source never leaks in (owner/source scoping).
	other := newBriefTestFixture(t)
	other.seedEvent(t, "other-owner-event", time.Hour)
	otherGot, err := f.st.UpcomingEventsForSourceBatch(ctx, other.owner, other.source, other.now, "", 100)
	if err != nil {
		t.Fatalf("other owner upcoming events: %v", err)
	}
	for _, ev := range otherGot {
		if ev.ID == inWindowID || ev.ID == boundaryID {
			t.Fatalf("owner/source scoping leaked another owner's event: %+v", ev)
		}
	}

	// Paging: limit=1 must eventually enumerate every eligible event across
	// pages, strictly after the previous page's last id, without duplicates.
	seen := map[string]bool{}
	afterID := ""
	for i := 0; i < 10; i++ {
		page, err := f.st.UpcomingEventsForSourceBatch(ctx, f.owner, f.source, f.now, afterID, 1)
		if err != nil {
			t.Fatalf("page %d: %v", i, err)
		}
		if len(page) == 0 {
			break
		}
		for _, ev := range page {
			if seen[ev.ID] {
				t.Fatalf("event %s returned twice across pages", ev.ID)
			}
			seen[ev.ID] = true
		}
		afterID = page[len(page)-1].ID
	}
	if !seen[inWindowID] || !seen[boundaryID] {
		t.Fatalf("paging did not enumerate every eligible event: %+v", seen)
	}
}

func TestDeleteIneligibleEventBriefsRemovesStaleRowsAndQueuedJobsOnly(t *testing.T) {
	t.Parallel()
	f := newBriefTestFixture(t)
	ctx := context.Background()
	eventID := f.seedEvent(t, "e1", time.Hour)
	staleTemplate := f.createTemplate(t, "Now ineligible", "pre", true)
	liveTemplate := f.createTemplate(t, "Still eligible", "pre", true)

	if _, err := f.st.ReconcileEventBriefPair(ctx, eventID, staleTemplate.ID, staleTemplate.Name, hashPtr("h1")); err != nil {
		t.Fatalf("reconcile stale: %v", err)
	}
	if _, err := f.st.ReconcileEventBriefPair(ctx, eventID, liveTemplate.ID, liveTemplate.Name, hashPtr("h1")); err != nil {
		t.Fatalf("reconcile live: %v", err)
	}

	// Claim the stale template's job so it looks "running" (guarded
	// publication, not deletion, is what must make it harmless).
	var staleJobID string
	for {
		job, ok, err := f.st.ClaimJob(ctx, time.Minute)
		if err != nil {
			t.Fatalf("claim: %v", err)
		}
		if !ok {
			t.Fatal("expected a claimable job for the stale template")
		}
		if job.BriefID != "" {
			briefs, _ := f.st.EventBriefsForEvents(ctx, f.owner, []string{eventID})
			for _, b := range briefs[eventID] {
				if b.ID == job.BriefID && b.TemplateID == staleTemplate.ID {
					staleJobID = job.ID
				}
			}
		}
		if staleJobID != "" {
			break
		}
	}
	if staleJobID == "" {
		t.Fatal("failed to identify the stale template's job")
	}

	// The eligible-template query now (simulated) omits the stale template
	// entirely -- e.g. it was deleted, or auto_run/phase changed.
	deleted, err := f.st.DeleteIneligibleEventBriefs(ctx, []string{eventID}, []string{liveTemplate.ID})
	if err != nil {
		t.Fatalf("delete ineligible: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("expected exactly one stale brief deleted, got %d", deleted)
	}

	briefs, _ := f.st.EventBriefsForEvents(ctx, f.owner, []string{eventID})
	got := briefs[eventID]
	if len(got) != 1 || got[0].TemplateID != liveTemplate.ID {
		t.Fatalf("expected only the live template's brief to remain: %+v", got)
	}

	// The running job survives (guarded publication, not deletion, protects
	// it) even though its brief is gone.
	stillThere, err := f.st.GetJob(ctx, staleJobID)
	if err != nil {
		t.Fatalf("the running job for the deleted brief must survive: %v", err)
	}
	if stillThere.Status != model.JobRunning {
		t.Fatalf("expected the surviving job to still be running: %+v", stillThere)
	}
}

func TestDeleteIneligibleEventBriefsEmptyEligibleSetRemovesEverything(t *testing.T) {
	t.Parallel()
	f := newBriefTestFixture(t)
	ctx := context.Background()
	eventID := f.seedEvent(t, "e1", time.Hour)
	tmpl := f.createTemplate(t, "Pre-read", "pre", true)

	if _, err := f.st.ReconcileEventBriefPair(ctx, eventID, tmpl.ID, tmpl.Name, hashPtr("h1")); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	deleted, err := f.st.DeleteIneligibleEventBriefs(ctx, []string{eventID}, nil)
	if err != nil {
		t.Fatalf("delete with nil eligible set: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("expected the only brief deleted when nothing is eligible, got %d", deleted)
	}
	if _, ok, err := f.st.ClaimJob(ctx, 30*time.Second); err != nil || ok {
		t.Fatalf("expected the queued job also removed: ok=%v err=%v", ok, err)
	}
}

// TestEventBriefsForEventsJoinDoesNotAmbiguateSharedColumns is a regression
// guard for a real incident: EventBriefsForEvents' join against
// calendar_events used a SELECT list built by string-concatenating "b." onto
// eventBriefColumns, which only qualified the FIRST column and left every
// other column (including created_at/updated_at, which calendar_events also
// has) unqualified. Postgres rejects that as an ambiguous column reference,
// which broke this query -- and therefore ListEvents, which calls it on
// every request -- for any event/brief pair, cascading into unrelated tests
// sharing the pool. Both calendar_events and event_briefs default
// created_at/updated_at to now(), so any real row exercises this; this test
// names the failure mode explicitly so a future refactor of eventBriefColumns
// can't silently reintroduce it. It exercises both the direct join
// (EventBriefsForEvents) and the production entry point (ListEvents).
func TestEventBriefsForEventsJoinDoesNotAmbiguateSharedColumns(t *testing.T) {
	t.Parallel()
	f := newBriefTestFixture(t)
	ctx := context.Background()
	eventID := f.seedEvent(t, "e1", time.Hour)
	tmpl := f.createTemplate(t, "Pre-read", "pre", true)

	if _, err := f.st.ReconcileEventBriefPair(ctx, eventID, tmpl.ID, tmpl.Name, hashPtr("h1")); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	briefs, err := f.st.EventBriefsForEvents(ctx, f.owner, []string{eventID})
	if err != nil {
		t.Fatalf("EventBriefsForEvents must not fail with an ambiguous column error: %v", err)
	}
	got := briefs[eventID]
	if len(got) != 1 || got[0].TemplateID != tmpl.ID {
		t.Fatalf("unexpected briefs from join query: %+v", got)
	}
	if got[0].UpdatedAt.IsZero() || got[0].CreatedAt.IsZero() {
		t.Fatalf("expected the brief's own (event_briefs) timestamps, not zero values: %+v", got[0])
	}

	// Exercise the production path too: ListEvents joins in briefs for every
	// event it returns, on the same query shape.
	evs, err := f.st.ListEvents(ctx, f.owner, f.now, f.now.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("ListEvents must not fail with an ambiguous column error: %v", err)
	}
	var found bool
	for _, ev := range evs {
		if ev.ID != eventID {
			continue
		}
		found = true
		if len(ev.Briefs) != 1 || ev.Briefs[0].TemplateID != tmpl.ID {
			t.Fatalf("expected ListEvents to attach the seeded brief: %+v", ev.Briefs)
		}
	}
	if !found {
		t.Fatalf("expected seeded event in ListEvents result: %+v", evs)
	}
}
