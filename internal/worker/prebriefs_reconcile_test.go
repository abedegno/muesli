package worker

// This file is DB-backed and CI-only: testutil.NewPool skips when
// TEST_DATABASE_URL is unset.

import (
	"context"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/calendar"
	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
)

func seedPreBriefEvent(t *testing.T, st *store.Store, ownerID, sourceID, externalID string, now time.Time, startsIn time.Duration) string {
	t.Helper()
	ctx := context.Background()
	starts := now.Add(startsIn)
	if err := st.UpsertEvents(ctx, ownerID, sourceID, []calendar.NormalizedEvent{
		{ExternalID: externalID, Title: "Meeting", StartsAt: starts, EndsAt: starts.Add(30 * time.Minute)},
	}); err != nil {
		t.Fatalf("upsert event: %v", err)
	}
	evs, err := st.ListEvents(ctx, ownerID, starts.Add(-time.Minute), starts.Add(time.Minute))
	if err != nil || len(evs) != 1 {
		t.Fatalf("list seeded event: %+v %v", evs, err)
	}
	return evs[0].ID
}

// TestReconcilePreBriefsEnqueuesForEligiblePairAndSkipsAfterOnce proves a
// fresh eligible (event, template) pair gets exactly one job on the first
// pass and none on an unchanged second pass.
func TestReconcilePreBriefsEnqueuesForEligiblePairAndSkipsOnRepeat(t *testing.T) {
	st, cr, owner := newCalendarSyncTestState(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 20, 8, 0, 0, 0, time.UTC)

	src, err := st.CreateSource(ctx, owner, "ics", "Cal", "sealed")
	if err != nil {
		t.Fatalf("create source: %v", err)
	}
	eventID := seedPreBriefEvent(t, st, owner, src.ID, "e1", now, time.Hour)

	tmpl, err := st.CreateTemplate(ctx, owner, "Pre-read", "pre",
		[]model.TemplateSection{{Heading: "Context", Instruction: "Summarize."}}, true, "", "", nil)
	if err != nil {
		t.Fatalf("create template: %v", err)
	}
	if err := st.EnsureDefaultPlugin(ctx, cr, model.PluginAgent, "agent", "http://127.0.0.1:0", "tok", "{}"); err != nil {
		t.Fatalf("ensure default agent: %v", err)
	}

	if err := ReconcilePreBriefs(ctx, st, cr, owner, src.ID, now); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	briefs, err := st.EventBriefsForEvents(ctx, owner, []string{eventID})
	if err != nil {
		t.Fatalf("event briefs: %v", err)
	}
	got := briefs[eventID]
	if len(got) != 1 || got[0].TemplateID != tmpl.ID || got[0].Status != model.BriefPending {
		t.Fatalf("unexpected briefs after first reconcile: %+v", got)
	}
	job, ok, err := st.ClaimJob(ctx, 30*time.Second)
	if err != nil || !ok || job.CalendarEventID != eventID {
		t.Fatalf("expected a claimable pre_generate job: ok=%v err=%v job=%+v", ok, err, job)
	}
	if err := st.CompleteJob(ctx, job.ID); err != nil {
		t.Fatalf("complete job: %v", err)
	}

	if err := ReconcilePreBriefs(ctx, st, cr, owner, src.ID, now); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	if _, ok, err := st.ClaimJob(ctx, 30*time.Second); err != nil || ok {
		t.Fatalf("expected no new job on an unchanged repeat pass: ok=%v err=%v", ok, err)
	}
}

// TestReconcilePreBriefsNoAgentFailsWithoutJob proves reconciliation with no
// default agent configured marks applicable briefs failed with a null hash
// and enqueues nothing (rather than erroring or leaving pending forever).
func TestReconcilePreBriefsNoAgentFailsWithoutJob(t *testing.T) {
	st, cr, owner := newCalendarSyncTestState(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 20, 8, 0, 0, 0, time.UTC)

	src, err := st.CreateSource(ctx, owner, "ics", "Cal", "sealed")
	if err != nil {
		t.Fatalf("create source: %v", err)
	}
	eventID := seedPreBriefEvent(t, st, owner, src.ID, "e1", now, time.Hour)
	if _, err := st.CreateTemplate(ctx, owner, "Pre-read", "pre",
		[]model.TemplateSection{{Heading: "Context", Instruction: "Summarize."}}, true, "", "", nil); err != nil {
		t.Fatalf("create template: %v", err)
	}

	if err := ReconcilePreBriefs(ctx, st, cr, owner, src.ID, now); err != nil {
		t.Fatalf("reconcile with no agent: %v", err)
	}

	briefs, err := st.EventBriefsForEvents(ctx, owner, []string{eventID})
	if err != nil {
		t.Fatalf("event briefs: %v", err)
	}
	got := briefs[eventID]
	if len(got) != 1 || got[0].Status != model.BriefFailed {
		t.Fatalf("expected a failed placeholder: %+v", got)
	}
	if _, ok, err := st.ClaimJob(ctx, 30*time.Second); err != nil || ok {
		t.Fatalf("no agent must never enqueue: ok=%v err=%v", ok, err)
	}
}

// TestReconcilePreBriefsRepairsStaleRowsEligibleQueryOmits proves a stale
// brief for a template the eligible-template query no longer returns
// (deleted entirely) is still removed by the repair pass, since cleanup
// considers existing rows rather than only the eligible-template query.
func TestReconcilePreBriefsRepairsStaleRowsEligibleQueryOmits(t *testing.T) {
	st, cr, owner := newCalendarSyncTestState(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 20, 8, 0, 0, 0, time.UTC)

	src, err := st.CreateSource(ctx, owner, "ics", "Cal", "sealed")
	if err != nil {
		t.Fatalf("create source: %v", err)
	}
	eventID := seedPreBriefEvent(t, st, owner, src.ID, "e1", now, time.Hour)
	tmpl, err := st.CreateTemplate(ctx, owner, "Soon deleted", "pre",
		[]model.TemplateSection{{Heading: "Context", Instruction: "Summarize."}}, true, "", "", nil)
	if err != nil {
		t.Fatalf("create template: %v", err)
	}

	// Seed a stale row directly (bypassing the normal write path), simulating
	// a brief left behind by a template that is about to be deleted.
	h := "stale-hash"
	if _, err := st.ReconcileEventBriefPair(ctx, eventID, tmpl.ID, tmpl.Name, &h); err != nil {
		t.Fatalf("seed stale brief: %v", err)
	}
	if err := st.DeleteTemplate(ctx, owner, tmpl.ID); err != nil {
		t.Fatalf("delete template: %v", err)
	}

	if err := ReconcilePreBriefs(ctx, st, cr, owner, src.ID, now); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	briefs, err := st.EventBriefsForEvents(ctx, owner, []string{eventID})
	if err != nil {
		t.Fatalf("event briefs: %v", err)
	}
	if len(briefs[eventID]) != 0 {
		t.Fatalf("expected the stale brief removed by the repair pass: %+v", briefs[eventID])
	}
}

// TestReconcilePreBriefsIgnoresIneligibleTemplates proves an after-phase or
// auto-run-disabled template never produces a brief.
func TestReconcilePreBriefsIgnoresIneligibleTemplates(t *testing.T) {
	st, cr, owner := newCalendarSyncTestState(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 20, 8, 0, 0, 0, time.UTC)

	src, err := st.CreateSource(ctx, owner, "ics", "Cal", "sealed")
	if err != nil {
		t.Fatalf("create source: %v", err)
	}
	eventID := seedPreBriefEvent(t, st, owner, src.ID, "e1", now, time.Hour)
	if _, err := st.CreateTemplate(ctx, owner, "After template", "after",
		[]model.TemplateSection{{Heading: "Context", Instruction: "Summarize."}}, true, "", "", nil); err != nil {
		t.Fatalf("create after template: %v", err)
	}
	if _, err := st.CreateTemplate(ctx, owner, "Manual pre template", "pre",
		[]model.TemplateSection{{Heading: "Context", Instruction: "Summarize."}}, false, "", "", nil); err != nil {
		t.Fatalf("create manual pre template: %v", err)
	}
	if err := st.EnsureDefaultPlugin(ctx, cr, model.PluginAgent, "agent", "http://127.0.0.1:0", "tok", "{}"); err != nil {
		t.Fatalf("ensure default agent: %v", err)
	}

	if err := ReconcilePreBriefs(ctx, st, cr, owner, src.ID, now); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	briefs, err := st.EventBriefsForEvents(ctx, owner, []string{eventID})
	if err != nil {
		t.Fatalf("event briefs: %v", err)
	}
	if len(briefs[eventID]) != 0 {
		t.Fatalf("expected no briefs for after/manual templates: %+v", briefs[eventID])
	}
}
