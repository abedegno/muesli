package store_test

// This file is DB-backed and CI-only: testutil.NewPool skips when
// TEST_DATABASE_URL is unset.

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/calendar"
	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
)

// cleanupFixture is a store + owner + one eligible pre/auto-run template with
// one event, ready to be driven into pending/ready/failed brief states.
type cleanupFixture struct {
	st      *store.Store
	owner   string
	eventID string
	tmpl    model.Template
}

func newCleanupFixture(t *testing.T) cleanupFixture {
	t.Helper()
	return newCleanupFixtureInStore(t, store.New(testutil.NewPool(t)))
}

// newCleanupFixtureInStore is newCleanupFixture against a caller-supplied
// store, so two-owner isolation tests can share one store/pool between
// owners (a fresh store per fixture would trivially "isolate" by never being
// the same database).
func newCleanupFixtureInStore(t *testing.T, st *store.Store) cleanupFixture {
	t.Helper()
	ctx := context.Background()
	u, err := st.CreateUser(ctx, fmt.Sprintf("cleanup-owner-%d@example.com", seedUserCounter.Add(1)), "h")
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	src, err := st.CreateSource(ctx, u.ID, "ics", "Cal", "sealed")
	if err != nil {
		t.Fatalf("create source: %v", err)
	}
	starts := time.Now().Add(time.Hour)
	if err := st.UpsertEvents(ctx, u.ID, src.ID, []calendar.NormalizedEvent{
		{ExternalID: "e1", Title: "Planning", StartsAt: starts, EndsAt: starts.Add(time.Hour)},
	}); err != nil {
		t.Fatalf("upsert event: %v", err)
	}
	evs, err := st.ListEvents(ctx, u.ID, starts.Add(-time.Minute), starts.Add(time.Minute))
	if err != nil || len(evs) != 1 {
		t.Fatalf("list event: %+v %v", evs, err)
	}
	tmpl, err := st.CreateTemplate(ctx, u.ID, "Pre-read", "pre",
		[]model.TemplateSection{{Heading: "Context", Instruction: "Summarize."}}, true, "", "", nil)
	if err != nil {
		t.Fatalf("create template: %v", err)
	}
	return cleanupFixture{st: st, owner: u.ID, eventID: evs[0].ID, tmpl: tmpl}
}

// briefState drives a freshly-created brief into the named state ("pending",
// "ready", "failed") and returns its id. "pending" leaves the job queued;
// "ready"/"failed" settle the job first (matching what the real worker does).
func (f cleanupFixture) briefState(t *testing.T, state string) string {
	t.Helper()
	ctx := context.Background()
	h := "h1"
	if _, err := f.st.ReconcileEventBriefPair(ctx, f.eventID, f.tmpl.ID, f.tmpl.Name, &h); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	briefs, err := f.st.EventBriefsForEvents(ctx, f.owner, []string{f.eventID})
	if err != nil || len(briefs[f.eventID]) != 1 {
		t.Fatalf("event briefs: %+v %v", briefs, err)
	}
	briefID := briefs[f.eventID][0].ID

	switch state {
	case "pending":
		// Leave as-is: pending with a queued job.
	case "ready":
		job, ok, err := f.st.ClaimJob(ctx, time.Minute)
		if err != nil || !ok {
			t.Fatalf("claim: ok=%v err=%v", ok, err)
		}
		if _, err := f.st.PublishEventBrief(ctx, briefID, 1, "agent", "model", []model.SummarySection{{Heading: "Context", ContentMarkdown: "Done."}}); err != nil {
			t.Fatalf("publish: %v", err)
		}
		if err := f.st.CompleteJob(ctx, job.ID); err != nil {
			t.Fatalf("complete job: %v", err)
		}
	case "failed":
		job, ok, err := f.st.ClaimJob(ctx, time.Minute)
		if err != nil || !ok {
			t.Fatalf("claim: ok=%v err=%v", ok, err)
		}
		if _, err := f.st.FailEventBriefIfCurrent(ctx, briefID, 1); err != nil {
			t.Fatalf("fail: %v", err)
		}
		if err := f.st.FailJob(ctx, job.ID, "injected", false); err != nil {
			t.Fatalf("fail job: %v", err)
		}
	default:
		t.Fatalf("unknown state %q", state)
	}
	return briefID
}

func TestUpdateTemplateAutoRunDisabledRemovesBriefForEveryState(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"pending", "ready", "failed"} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			f := newCleanupFixture(t)
			ctx := context.Background()
			briefID := f.briefState(t, state)

			if err := f.st.UpdateTemplate(ctx, f.owner, f.tmpl.ID, f.tmpl.Name, "pre", f.tmpl.Sections, false, "", "", nil); err != nil {
				t.Fatalf("update template (disable auto_run): %v", err)
			}

			if _, found, err := f.st.GetEventBriefByID(ctx, briefID); err != nil || found {
				t.Fatalf("expected the brief removed: found=%v err=%v", found, err)
			}
		})
	}
}

func TestUpdateTemplatePhaseChangedRemovesBrief(t *testing.T) {
	t.Parallel()
	f := newCleanupFixture(t)
	ctx := context.Background()
	briefID := f.briefState(t, "pending")

	if err := f.st.UpdateTemplate(ctx, f.owner, f.tmpl.ID, f.tmpl.Name, "after", f.tmpl.Sections, true, "", "", nil); err != nil {
		t.Fatalf("update template (change phase): %v", err)
	}
	if _, found, err := f.st.GetEventBriefByID(ctx, briefID); err != nil || found {
		t.Fatalf("expected the brief removed: found=%v err=%v", found, err)
	}
}

func TestUpdateTemplateStillEligibleKeepsBrief(t *testing.T) {
	t.Parallel()
	f := newCleanupFixture(t)
	ctx := context.Background()
	briefID := f.briefState(t, "ready")

	// A rename that keeps phase=pre, auto_run=true must not touch the brief.
	if err := f.st.UpdateTemplate(ctx, f.owner, f.tmpl.ID, "Pre-read v2", "pre", f.tmpl.Sections, true, "", "", nil); err != nil {
		t.Fatalf("update template (still eligible): %v", err)
	}
	if _, found, err := f.st.GetEventBriefByID(ctx, briefID); err != nil || !found {
		t.Fatalf("expected the brief kept: found=%v err=%v", found, err)
	}
}

func TestUpdateTemplateDisableAutoRunRemovesQueuedJobButRunningJobSurvivesWithoutPublishing(t *testing.T) {
	t.Parallel()
	f := newCleanupFixture(t)
	ctx := context.Background()

	h := "h1"
	if _, err := f.st.ReconcileEventBriefPair(ctx, f.eventID, f.tmpl.ID, f.tmpl.Name, &h); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	briefs, err := f.st.EventBriefsForEvents(ctx, f.owner, []string{f.eventID})
	if err != nil || len(briefs[f.eventID]) != 1 {
		t.Fatalf("event briefs: %+v %v", briefs, err)
	}
	briefID := briefs[f.eventID][0].ID

	job, ok, err := f.st.ClaimJob(ctx, time.Minute)
	if err != nil || !ok {
		t.Fatalf("claim (simulate running): ok=%v err=%v", ok, err)
	}

	if err := f.st.UpdateTemplate(ctx, f.owner, f.tmpl.ID, f.tmpl.Name, "pre", f.tmpl.Sections, false, "", "", nil); err != nil {
		t.Fatalf("update template (disable auto_run): %v", err)
	}

	if _, found, err := f.st.GetEventBriefByID(ctx, briefID); err != nil || found {
		t.Fatalf("expected the brief removed: found=%v err=%v", found, err)
	}

	// The running job survives -- deletion does not touch it -- but guarded
	// publication now discards its output since the brief is gone.
	stillThere, err := f.st.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("running job must survive brief deletion: %v", err)
	}
	if stillThere.Status != model.JobRunning {
		t.Fatalf("expected the job still running: %+v", stillThere)
	}
	published, err := f.st.PublishEventBrief(ctx, briefID, 1, "agent", "model", []model.SummarySection{{Heading: "Context", ContentMarkdown: "Late."}})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if published {
		t.Fatal("publication must be discarded once the brief row is gone")
	}
}

func TestDeleteTemplateRemovesBriefForEveryState(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"pending", "ready", "failed"} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			f := newCleanupFixture(t)
			ctx := context.Background()
			briefID := f.briefState(t, state)

			if err := f.st.DeleteTemplate(ctx, f.owner, f.tmpl.ID); err != nil {
				t.Fatalf("delete template: %v", err)
			}
			if _, found, err := f.st.GetEventBriefByID(ctx, briefID); err != nil || found {
				t.Fatalf("expected the brief removed: found=%v err=%v", found, err)
			}
		})
	}
}

// TestUpdateTemplateTwoOwnersCleanupIsolated proves disabling auto_run for
// owner A's template never touches owner B's separate (but identically
// eligible) template/brief.
func TestUpdateTemplateTwoOwnersCleanupIsolated(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	a := newCleanupFixtureInStore(t, st)
	b := newCleanupFixtureInStore(t, st)
	briefA := a.briefState(t, "pending")
	briefB := b.briefState(t, "pending")

	if err := a.st.UpdateTemplate(context.Background(), a.owner, a.tmpl.ID, a.tmpl.Name, "pre", a.tmpl.Sections, false, "", "", nil); err != nil {
		t.Fatalf("update owner A template: %v", err)
	}

	if _, found, err := a.st.GetEventBriefByID(context.Background(), briefA); err != nil || found {
		t.Fatalf("expected owner A's brief removed: found=%v err=%v", found, err)
	}
	if _, found, err := b.st.GetEventBriefByID(context.Background(), briefB); err != nil || !found {
		t.Fatalf("owner B's brief must be untouched: found=%v err=%v", found, err)
	}
}
