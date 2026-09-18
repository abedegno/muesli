package api_test

// This file is DB-backed and CI-only: testutil.NewPool skips when
// TEST_DATABASE_URL is unset.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/store"
)

// TestLiveNotePrompts_CapacityRejects41st proves the note-scoped 40-
// subscriber cap is enforced globally: 40 pre-existing leases (simulating
// other subscribers, possibly on another process sharing the database) make
// the 41st live connection attempt receive 429, and releasing one frees
// capacity for a subsequent connection.
func TestLiveNotePrompts_CapacityRejects41st(t *testing.T) {
	t.Parallel()
	apiSrv, st2 := newTestServer(t)
	httpSrv := httptest.NewServer(apiSrv.Handler())
	defer httpSrv.Close()
	client := httpSrv.Client()
	token := liveTestLogin(t, client, httpSrv.URL)

	ctx := context.Background()
	u, err := st2.GetUserByEmail(ctx, "live-owner@example.com")
	if err != nil {
		t.Fatalf("lookup owner: %v", err)
	}
	n, err := st2.CreateNote(ctx, u.ID, "Capacity note")
	if err != nil {
		t.Fatalf("create note: %v", err)
	}

	var connIDs []string
	for i := 0; i < store.MaxLiveNoteSubscribers; i++ {
		connID, ok, err := st2.AcquireLiveNoteSubscription(ctx, n.ID, u.ID, time.Minute)
		if err != nil || !ok {
			t.Fatalf("seed subscriber %d: ok=%v err=%v", i, ok, err)
		}
		connIDs = append(connIDs, connID)
	}

	req, _ := http.NewRequest(http.MethodGet, httpSrv.URL+"/api/notes/"+n.ID+"/live-prompts", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 at cap", resp.StatusCode)
	}

	// Releasing one frees capacity for the next connection.
	if err := st2.ReleaseLiveNoteSubscription(ctx, connIDs[0]); err != nil {
		t.Fatalf("release: %v", err)
	}
	connID, ok, err := st2.AcquireLiveNoteSubscription(ctx, n.ID, u.ID, time.Minute)
	if err != nil || !ok {
		t.Fatalf("expected capacity freed after release: ok=%v err=%v", ok, err)
	}
	_ = connID
}

// TestLiveNoteSubscription_CrashedLeaseReclaimedAfterExpiry proves a
// process-crash lease (never explicitly released) becomes reusable once its
// TTL elapses, via a fake short TTL rather than a sleep.
func TestLiveNoteSubscription_CrashedLeaseReclaimedAfterExpiry(t *testing.T) {
	t.Parallel()
	_, st2 := newTestServer(t)
	ctx := context.Background()
	u, err := st2.CreateUser(ctx, "crash-lease-owner@example.com", "h")
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	n, err := st2.CreateNote(ctx, u.ID, "Crash note")
	if err != nil {
		t.Fatalf("create note: %v", err)
	}

	for i := 0; i < store.MaxLiveNoteSubscribers; i++ {
		if _, ok, err := st2.AcquireLiveNoteSubscription(ctx, n.ID, u.ID, -time.Second); err != nil || !ok {
			// A negative TTL immediately expires the lease -- simulating a
			// crashed process that never renewed nor released it.
			t.Fatalf("seed expired subscriber %d: ok=%v err=%v", i, ok, err)
		}
	}

	// All 40 leases are already expired; a fresh acquire must succeed
	// because AcquireLiveNoteSubscription deletes expired leases first.
	if _, ok, err := st2.AcquireLiveNoteSubscription(ctx, n.ID, u.ID, time.Minute); err != nil || !ok {
		t.Fatalf("expected expired leases to be reclaimed: ok=%v err=%v", ok, err)
	}
}
