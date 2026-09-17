package worker

// This file is DB-backed and CI-only: testutil.NewPool skips when
// TEST_DATABASE_URL is unset.
//
// Cross-review finding on PR #766: ReconcilePreBriefs cleaned up only among
// the events it had just loaded -- those inside the eligibility window
// (now, now+7d]. A brief whose event was rescheduled OUT of that window, or
// had already started, was never in the batch and so was never cleaned: the
// stale brief stayed attached and retrievable through the calendar API.
// These tests reschedule an event that already has a brief and prove the
// brief and its queued job are gone on the next pass.

import (
	"context"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/calendar"
	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
)

// reschedulePreBriefEvent moves an already-seeded event (same source and
// external id, so the upsert updates the row in place) to start at
// now+startsIn.
func reschedulePreBriefEvent(t *testing.T, st *store.Store, ownerID, sourceID, externalID string, now time.Time, startsIn time.Duration) {
	t.Helper()
	starts := now.Add(startsIn)
	if err := st.UpsertEvents(context.Background(), ownerID, sourceID, []calendar.NormalizedEvent{
		{ExternalID: externalID, Title: "Meeting", StartsAt: starts, EndsAt: starts.Add(30 * time.Minute)},
	}); err != nil {
		t.Fatalf("reschedule event: %v", err)
	}
}

// briefedEvent seeds one eligible event with a pre template and a default
// agent, reconciles once, and returns the event id with its brief in place
// and its pre_generate job still queued (nothing claims it).
func briefedEvent(t *testing.T, st *store.Store, owner, sourceID string, now time.Time) string {
	t.Helper()
	ctx := context.Background()
	eventID := seedPreBriefEvent(t, st, owner, sourceID, "e1", now, time.Hour)
	if _, err := st.CreateTemplate(ctx, owner, "Pre-read", "pre",
		[]model.TemplateSection{{Heading: "Context", Instruction: "Summarize."}}, true, "", "", nil); err != nil {
		t.Fatalf("create template: %v", err)
	}
	return eventID
}

func TestReconcilePreBriefsRemovesBriefWhenEventRescheduledBeyondWindow(t *testing.T) {
	st, cr, owner := newCalendarSyncTestState(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 20, 8, 0, 0, 0, time.UTC)

	src, err := st.CreateSource(ctx, owner, "ics", "Cal", "sealed")
	if err != nil {
		t.Fatalf("create source: %v", err)
	}
	eventID := briefedEvent(t, st, owner, src.ID, now)
	if err := st.EnsureDefaultPlugin(ctx, cr, model.PluginAgent, "agent", "http://127.0.0.1:0", "tok", "{}"); err != nil {
		t.Fatalf("ensure default agent: %v", err)
	}
	if err := ReconcilePreBriefs(ctx, st, cr, owner, src.ID, now); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	briefs, err := st.EventBriefsForEvents(ctx, owner, []string{eventID})
	if err != nil || len(briefs[eventID]) != 1 {
		t.Fatalf("expected one brief before rescheduling: %+v %v", briefs, err)
	}

	// The event moves to eight days out: outside (now, now+7d], so no longer
	// eligible -- but also no longer in the batch the reconciler loads.
	reschedulePreBriefEvent(t, st, owner, src.ID, "e1", now, 8*24*time.Hour)
	if err := ReconcilePreBriefs(ctx, st, cr, owner, src.ID, now); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}

	briefs, err = st.EventBriefsForEvents(ctx, owner, []string{eventID})
	if err != nil {
		t.Fatalf("event briefs after reschedule: %v", err)
	}
	if got := briefs[eventID]; len(got) != 0 {
		t.Fatalf("a brief for an event rescheduled beyond the window must be removed, got %+v", got)
	}
	// Its still-queued job goes with it: nothing is left to claim.
	if job, ok, err := st.ClaimJob(ctx, 30*time.Second); err != nil || ok {
		t.Fatalf("expected the queued pre job to be deleted with its brief: ok=%v err=%v job=%+v", ok, err, job)
	}
}

func TestReconcilePreBriefsRemovesBriefWhenEventHasAlreadyStarted(t *testing.T) {
	st, cr, owner := newCalendarSyncTestState(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 20, 8, 0, 0, 0, time.UTC)

	src, err := st.CreateSource(ctx, owner, "ics", "Cal", "sealed")
	if err != nil {
		t.Fatalf("create source: %v", err)
	}
	eventID := briefedEvent(t, st, owner, src.ID, now)
	if err := st.EnsureDefaultPlugin(ctx, cr, model.PluginAgent, "agent", "http://127.0.0.1:0", "tok", "{}"); err != nil {
		t.Fatalf("ensure default agent: %v", err)
	}
	if err := ReconcilePreBriefs(ctx, st, cr, owner, src.ID, now); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	// Let the job run to completion this time, so the brief is a finished
	// artefact and not merely a pending placeholder when it goes stale.
	job, ok, err := st.ClaimJob(ctx, 30*time.Second)
	if err != nil || !ok || job.CalendarEventID != eventID {
		t.Fatalf("expected a claimable pre_generate job: ok=%v err=%v job=%+v", ok, err, job)
	}
	if err := st.CompleteJob(ctx, job.ID); err != nil {
		t.Fatalf("complete job: %v", err)
	}

	// The event moves into the past: it has started, so it is outside the
	// window on the near side.
	reschedulePreBriefEvent(t, st, owner, src.ID, "e1", now, -time.Hour)
	if err := ReconcilePreBriefs(ctx, st, cr, owner, src.ID, now); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}

	briefs, err := st.EventBriefsForEvents(ctx, owner, []string{eventID})
	if err != nil {
		t.Fatalf("event briefs after reschedule: %v", err)
	}
	if got := briefs[eventID]; len(got) != 0 {
		t.Fatalf("a brief for an event that has already started must be removed, got %+v", got)
	}
}

// TestDeleteEventBriefsOutsideWindowIsSourceScoped proves the cleanup reaches
// only the named source: another source's out-of-window brief is that
// source's own reconciliation's to remove, not this one's.
func TestDeleteEventBriefsOutsideWindowIsSourceScoped(t *testing.T) {
	st, cr, owner := newCalendarSyncTestState(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 20, 8, 0, 0, 0, time.UTC)

	srcA, err := st.CreateSource(ctx, owner, "ics", "A", "sealed")
	if err != nil {
		t.Fatalf("create source A: %v", err)
	}
	srcB, err := st.CreateSource(ctx, owner, "ics", "B", "sealed")
	if err != nil {
		t.Fatalf("create source B: %v", err)
	}
	if _, err := st.CreateTemplate(ctx, owner, "Pre-read", "pre",
		[]model.TemplateSection{{Heading: "Context", Instruction: "Summarize."}}, true, "", "", nil); err != nil {
		t.Fatalf("create template: %v", err)
	}
	if err := st.EnsureDefaultPlugin(ctx, cr, model.PluginAgent, "agent", "http://127.0.0.1:0", "tok", "{}"); err != nil {
		t.Fatalf("ensure default agent: %v", err)
	}
	evA := seedPreBriefEvent(t, st, owner, srcA.ID, "a1", now, time.Hour)
	evB := seedPreBriefEvent(t, st, owner, srcB.ID, "b1", now, time.Hour)
	for _, src := range []string{srcA.ID, srcB.ID} {
		if err := ReconcilePreBriefs(ctx, st, cr, owner, src, now); err != nil {
			t.Fatalf("reconcile %s: %v", src, err)
		}
	}
	// Both events leave the window; only source A is cleaned.
	reschedulePreBriefEvent(t, st, owner, srcA.ID, "a1", now, 8*24*time.Hour)
	reschedulePreBriefEvent(t, st, owner, srcB.ID, "b1", now, 8*24*time.Hour)

	n, err := st.DeleteEventBriefsOutsideWindow(ctx, owner, srcA.ID, now)
	if err != nil {
		t.Fatalf("delete outside window: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected exactly source A's one brief deleted, got %d", n)
	}
	briefs, err := st.EventBriefsForEvents(ctx, owner, []string{evA, evB})
	if err != nil {
		t.Fatalf("event briefs: %v", err)
	}
	if len(briefs[evA]) != 0 || len(briefs[evB]) != 1 {
		t.Fatalf("source scoping violated: A=%+v B=%+v", briefs[evA], briefs[evB])
	}
}
