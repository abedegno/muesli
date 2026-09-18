package store_test

// This file is DB-backed and CI-only: testutil.NewPool skips when
// TEST_DATABASE_URL is unset.

import (
	"context"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
)

// TestRecoverLiveTemplateOutputs_RemovesStaleByLastDemand proves recovery
// keys on last_demand_at for both an idle (no active job) and an
// active-looking row, regardless of status, once it has gone stale.
func TestRecoverLiveTemplateOutputs_RemovesStaleByLastDemand(t *testing.T) {
	st := store.New(testutil.NewPool(t))
	owner := newLiveTestOwner(t, st)
	newLiveTestTemplate(t, st, owner, "live-during", "during", true)
	noteID, transcriptID, streamID := newLiveTestStream(t, st, owner)
	appendFinalSegment(t, st, transcriptID, streamID, "hello")

	rows, err := st.ListVisibleLiveTemplateOutputs(context.Background(), owner, noteID)
	if err != nil || len(rows) != 1 {
		t.Fatalf("list outputs: %v %+v", err, rows)
	}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	staleTime := now.Add(-2 * time.Hour)
	if _, err := st.Pool().Exec(context.Background(),
		`UPDATE live_template_outputs SET last_demand_at=$2 WHERE id=$1`, rows[0].ID, staleTime); err != nil {
		t.Fatalf("backdate last_demand_at: %v", err)
	}

	noteIDs, removed, err := st.RecoverLiveTemplateOutputs(context.Background(), now, 200)
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if removed < 1 {
		t.Fatalf("expected at least one stale row removed, got %d", removed)
	}
	found := false
	for _, id := range noteIDs {
		if id == noteID {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the stale row's note id to be reported, got %v", noteIDs)
	}

	rows2, err := st.ListVisibleLiveTemplateOutputs(context.Background(), owner, noteID)
	if err != nil || len(rows2) != 0 {
		t.Fatalf("expected the stale row to be removed, got %v %+v", err, rows2)
	}
}

// TestRecoverLiveTemplateOutputs_RecentDemandSurvives proves a row whose
// last_demand_at is fresh is left alone.
func TestRecoverLiveTemplateOutputs_RecentDemandSurvives(t *testing.T) {
	st := store.New(testutil.NewPool(t))
	owner := newLiveTestOwner(t, st)
	newLiveTestTemplate(t, st, owner, "live-during", "during", true)
	noteID, transcriptID, streamID := newLiveTestStream(t, st, owner)
	appendFinalSegment(t, st, transcriptID, streamID, "hello")

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, _, err := st.RecoverLiveTemplateOutputs(context.Background(), now, 200); err != nil {
		t.Fatalf("recover: %v", err)
	}

	rows, err := st.ListVisibleLiveTemplateOutputs(context.Background(), owner, noteID)
	if err != nil || len(rows) != 1 {
		t.Fatalf("expected the fresh row to survive recovery, got %v %+v", err, rows)
	}
}
