package store_test

import (
	"context"
	"errors"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
	"github.com/google/uuid"
	"testing"
)

func TestCaptureLeaseProtectsReplacement(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testutil.NewPool(t)
	st := store.New(pool)
	owner, err := st.CreateUser(ctx, "lease@example.com", "unused")
	if err != nil {
		t.Fatal(err)
	}
	n, err := st.CreateNote(ctx, owner.ID, "Leased recording")
	if err != nil {
		t.Fatal(err)
	}
	first, second := uuid.NewString(), uuid.NewString()
	if _, err = st.ClaimCapture(ctx, owner.ID, n.ID, first); err != nil {
		t.Fatal(err)
	}
	if _, err = st.ClaimCapture(ctx, owner.ID, n.ID, second); !errors.Is(err, store.ErrCaptureActive) {
		t.Fatalf("active lease replaced: %v", err)
	}
	if _, err = st.StopCaptureLease(ctx, owner.ID, n.ID, "", true); !errors.Is(err, store.ErrCaptureActive) {
		t.Fatalf("active recording recovered: %v", err)
	}
	if _, err = pool.Exec(ctx, `UPDATE notes SET capture_expires_at=now()-interval '1 second' WHERE id=$1`, n.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = st.ClaimCapture(ctx, owner.ID, n.ID, second); err != nil {
		t.Fatal(err)
	}
	if _, err = st.StopCaptureLease(ctx, owner.ID, n.ID, first, false); !errors.Is(err, store.ErrCaptureActive) {
		t.Fatalf("late stop ended replacement: %v", err)
	}
	if err = st.HeartbeatCapture(ctx, owner.ID, n.ID, first); !errors.Is(err, store.ErrCaptureActive) {
		t.Fatalf("stale heartbeat renewed: %v", err)
	}
	if err = st.HeartbeatCapture(ctx, owner.ID, n.ID, second); err != nil {
		t.Fatal(err)
	}
	stopped, err := st.StopCaptureLease(ctx, owner.ID, n.ID, second, false)
	if err != nil || stopped.Status != "draft" {
		t.Fatalf("owned stop: %+v %v", stopped, err)
	}
}
