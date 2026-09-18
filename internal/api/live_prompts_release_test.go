package api

// This file is DB-backed and CI-only: testutil.NewPool skips when
// TEST_DATABASE_URL is unset. package api (not api_test) because it injects
// failures behind handleLiveNotePrompts's liveStore boundary and shortens
// its timers, neither of which is exported.
//
// The property under test (issue #764, cross-review finding on PR #769): a
// subscriber lease acquired at admission is released on EVERY post-admission
// failure path, not only when a healthy stream closes. Each test forces one
// class of failure -- the initial snapshot query, the SSE write, the
// heartbeat reread, the lease renewal -- and asserts the lease row is gone.
// The injected failures leave the row in place themselves, so a passing
// assertion can only come from the handler's own release.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/crypto"
	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
	"github.com/go-chi/chi/v5"
)

var errInjected = errors.New("injected live store failure")

// faultyLiveStore delegates to the real store and fails one chosen call.
type faultyLiveStore struct {
	*store.Store
	failSnapshot  bool  // CurrentLiveStreamID, the initial snapshot query
	failListAfter int32 // ListVisibleLiveTemplateOutputs fails once this many calls succeeded (0 = never)
	failRenew     bool  // RenewLiveNoteSubscription errors; the lease row is left untouched

	lists    atomic.Int32
	released atomic.Int32
	mu       sync.Mutex
	connID   string
}

func (f *faultyLiveStore) AcquireLiveNoteSubscription(ctx context.Context, noteID, ownerID string, ttl time.Duration) (string, bool, error) {
	id, ok, err := f.Store.AcquireLiveNoteSubscription(ctx, noteID, ownerID, ttl)
	if ok {
		f.mu.Lock()
		f.connID = id
		f.mu.Unlock()
	}
	return id, ok, err
}

func (f *faultyLiveStore) ReleaseLiveNoteSubscription(ctx context.Context, id string) error {
	f.released.Add(1)
	return f.Store.ReleaseLiveNoteSubscription(ctx, id)
}

func (f *faultyLiveStore) RenewLiveNoteSubscription(ctx context.Context, id string, ttl time.Duration) (bool, error) {
	if f.failRenew {
		return false, errInjected
	}
	return f.Store.RenewLiveNoteSubscription(ctx, id, ttl)
}

func (f *faultyLiveStore) CurrentLiveStreamID(ctx context.Context, noteID string) (string, bool, error) {
	if f.failSnapshot {
		return "", false, errInjected
	}
	return f.Store.CurrentLiveStreamID(ctx, noteID)
}

func (f *faultyLiveStore) ListVisibleLiveTemplateOutputs(ctx context.Context, ownerID, noteID string) ([]model.LiveTemplateOutput, error) {
	n := f.lists.Add(1)
	if f.failListAfter > 0 && n > f.failListAfter {
		return nil, errInjected
	}
	return f.Store.ListVisibleLiveTemplateOutputs(ctx, ownerID, noteID)
}

func (f *faultyLiveStore) admitted() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.connID
}

// failingWriter is a recorder whose Write fails once allow writes have
// succeeded, modelling a peer that went away mid-stream. Flush is promoted
// from the recorder, so the handler still sees an http.Flusher.
type failingWriter struct {
	*httptest.ResponseRecorder
	allow  int
	writes int
}

func (w *failingWriter) Write(b []byte) (int, error) {
	if w.writes >= w.allow {
		return 0, errInjected
	}
	w.writes++
	return w.ResponseRecorder.Write(b)
}

type releaseFixture struct {
	srv    *Server
	st     *store.Store
	fs     *faultyLiveStore
	uid    string
	noteID string
}

func newReleaseFixture(t *testing.T, fs *faultyLiveStore) *releaseFixture {
	t.Helper()
	ctx := context.Background()
	st := store.New(testutil.NewPool(t))
	cr, err := crypto.New("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(Deps{Store: st, Crypto: cr})
	t.Cleanup(srv.Close) // stops the LISTEN connection before the pool's cleanup
	u, err := st.CreateUser(ctx, "release-owner@example.com", "h")
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	n, err := st.CreateNote(ctx, u.ID, "Release note")
	if err != nil {
		t.Fatalf("create note: %v", err)
	}
	fs.Store = st
	srv.liveStoreOverride = fs
	return &releaseFixture{srv: srv, st: st, fs: fs, uid: u.ID, noteID: n.ID}
}

// setTimers shortens the handler's tickers for one test. These tests never
// call t.Parallel, so no other handler observes the shortened values.
func setTimers(t *testing.T, renew, heartbeat time.Duration) {
	t.Helper()
	r, h := liveSubscriberRenewInterval, liveHeartbeatInterval
	liveSubscriberRenewInterval, liveHeartbeatInterval = renew, heartbeat
	t.Cleanup(func() { liveSubscriberRenewInterval, liveHeartbeatInterval = r, h })
}

// serve invokes the handler directly, as the authenticated owner, and
// returns once the handler has returned -- so its deferred release has run.
func (fx *releaseFixture) serve(t *testing.T, w http.ResponseWriter) {
	t.Helper()
	ctx, cancel := context.WithCancel(withUserID(context.Background(), fx.uid))
	defer cancel()
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", fx.noteID)
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req := httptest.NewRequest(http.MethodGet, "/api/notes/"+fx.noteID+"/live-prompts", nil).WithContext(ctx)

	done := make(chan struct{})
	go func() {
		defer close(done)
		fx.srv.handleLiveNotePrompts(w, req)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		cancel()
		<-done
		t.Fatal("handler did not return after the injected failure")
	}
}

// assertReleased proves the lease acquired at admission is gone: released
// exactly once through the boundary, and no row remains for the note.
func (fx *releaseFixture) assertReleased(t *testing.T) {
	t.Helper()
	if fx.fs.admitted() == "" {
		t.Fatal("handler never admitted a subscriber, so the failure path was not reached")
	}
	if got := fx.fs.released.Load(); got != 1 {
		t.Fatalf("release calls = %d, want exactly 1", got)
	}
	var n int
	if err := fx.st.Pool().QueryRow(context.Background(),
		`SELECT count(*) FROM live_note_subscriptions WHERE note_id=$1`, fx.noteID).Scan(&n); err != nil {
		t.Fatalf("count leases: %v", err)
	}
	if n != 0 {
		t.Fatalf("lease rows after the failure = %d, want 0", n)
	}
}

func TestLiveNotePrompts_ReleasesLeaseWhenSnapshotQueryFails(t *testing.T) {
	fx := newReleaseFixture(t, &faultyLiveStore{failSnapshot: true})
	rec := httptest.NewRecorder()
	fx.serve(t, rec)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: the failure must come after admission, not before", rec.Code)
	}
	fx.assertReleased(t)
}

func TestLiveNotePrompts_ReleasesLeaseWhenSSEWriteFails(t *testing.T) {
	fx := newReleaseFixture(t, &faultyLiveStore{})
	w := &failingWriter{ResponseRecorder: httptest.NewRecorder(), allow: 0} // the snapshot write itself fails
	fx.serve(t, w)
	fx.assertReleased(t)
}

func TestLiveNotePrompts_ReleasesLeaseWhenHeartbeatRereadFails(t *testing.T) {
	setTimers(t, time.Hour, 5*time.Millisecond)                    // heartbeat fires; renewal never does
	fx := newReleaseFixture(t, &faultyLiveStore{failListAfter: 1}) // snapshot list succeeds, the reread's fails
	rec := httptest.NewRecorder()
	fx.serve(t, rec)
	if got := fx.fs.lists.Load(); got < 2 {
		t.Fatalf("list calls = %d, want the snapshot and at least one heartbeat reread", got)
	}
	fx.assertReleased(t)
}

func TestLiveNotePrompts_ReleasesLeaseWhenRenewalFails(t *testing.T) {
	setTimers(t, 5*time.Millisecond, time.Hour) // renewal fires; heartbeat never does
	fx := newReleaseFixture(t, &faultyLiveStore{failRenew: true})
	rec := httptest.NewRecorder()
	fx.serve(t, rec)
	fx.assertReleased(t)
}
