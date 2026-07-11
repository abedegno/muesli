package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
)

// ---------------------------------------------------------------------------
// SUM03: per-note in-flight summarize guard.
//
// These tests live in package api (not api_test) so they can reach the
// unexported resummarizeGuard field / inFlightGuard methods directly, per
// the acceptance contract. Helpers below are local, minimal copies of the
// api_test package's newTestServer/doJSON (that package is not importable
// from here without a cycle).
// ---------------------------------------------------------------------------

func newGuardTestServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	st := store.New(testutil.NewPool(t))
	return NewServer(Deps{Store: st}), st
}

func doGuardJSON(t *testing.T, srv *Server, method, path string, body any, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

// setupGuardTestUser runs /api/setup + /api/login and returns an auth header.
func setupGuardTestUser(t *testing.T, srv *Server, email string) map[string]string {
	t.Helper()
	_ = doGuardJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": email, "password": "password123"}, nil)
	rec := doGuardJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": email, "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	return map[string]string{"Authorization": "Bearer " + login.Token}
}

// TestResummarizeGuardRejectsConcurrentCall simulates a first request
// already holding the in-flight guard for a note id, and asserts a
// concurrent/subsequent handleResummarize call for the SAME note id is
// rejected with 409 "already in progress" and never touches the store.
func TestResummarizeGuardRejectsConcurrentCall(t *testing.T) {
	t.Parallel()
	srv, st := newGuardTestServer(t)
	ctx := context.Background()
	if err := st.SeedBuiltInTemplates(ctx); err != nil {
		t.Fatalf("seed: %v", err)
	}
	hdr := setupGuardTestUser(t, srv, "guard-concurrent@example.com")

	rec := doGuardJSON(t, srv, http.MethodPost, "/api/notes",
		map[string]string{"title": "Guard test"}, hdr)
	var note struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &note)
	if note.ID == "" {
		t.Fatalf("no note id; body %s", rec.Body)
	}

	// Give it a transcript + an existing summary so we can prove the store
	// was left untouched by the rejected call.
	if _, err := st.SaveTranscript(ctx, model.Transcript{
		NoteID:            note.ID,
		TranscriberPlugin: "test",
		Model:             "m",
		Segments:          []model.Segment{{StartMS: 0, EndMS: 1000, Text: "hi", Source: "mic"}},
	}); err != nil {
		t.Fatalf("SaveTranscript: %v", err)
	}
	tmpls, _ := st.BuiltInTemplates(ctx)
	existingID, err := st.CreatePendingSummary(ctx, note.ID, tmpls[0].ID)
	if err != nil {
		t.Fatalf("CreatePendingSummary: %v", err)
	}

	// Simulate a first, still-in-flight request by acquiring the guard
	// directly (this is what handleResummarize does as its first step).
	if !srv.resummarizeGuard.tryAcquire(note.ID) {
		t.Fatalf("expected to acquire guard for a fresh note id")
	}
	defer srv.resummarizeGuard.release(note.ID)

	// A concurrent/subsequent call for the same note id must be rejected
	// with a 409 distinct from the "no transcript" 409, and must not touch
	// the store at all.
	rec = doGuardJSON(t, srv, http.MethodPost, "/api/notes/"+note.ID+"/resummarize", nil, hdr)
	if rec.Code != http.StatusConflict {
		t.Fatalf("in-flight resummarize status %d, want 409; body %s", rec.Code, rec.Body)
	}
	var body struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Error != "summarize already in progress" {
		t.Fatalf("in-flight resummarize error = %q, want %q; body %s", body.Error, "summarize already in progress", rec.Body)
	}

	sums, err := st.GetSummaries(ctx, note.ID)
	if err != nil {
		t.Fatalf("GetSummaries: %v", err)
	}
	if len(sums) != 1 || sums[0].ID != existingID {
		t.Fatalf("store was touched by rejected request: summaries=%v, want only %s", sums, existingID)
	}
}

// TestResummarizeGuardReleasedAfterSuccess asserts the guard slot is freed
// once a resummarize call completes successfully, so an immediately
// following call for the same note id is not rejected by the guard.
func TestResummarizeGuardReleasedAfterSuccess(t *testing.T) {
	t.Parallel()
	srv, st := newGuardTestServer(t)
	ctx := context.Background()
	if err := st.SeedBuiltInTemplates(ctx); err != nil {
		t.Fatalf("seed: %v", err)
	}
	hdr := setupGuardTestUser(t, srv, "guard-success@example.com")

	rec := doGuardJSON(t, srv, http.MethodPost, "/api/notes",
		map[string]string{"title": "Guard success"}, hdr)
	var note struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &note)

	if _, err := st.SaveTranscript(ctx, model.Transcript{
		NoteID:            note.ID,
		TranscriberPlugin: "test",
		Model:             "m",
		Segments:          []model.Segment{{StartMS: 0, EndMS: 1000, Text: "hi", Source: "mic"}},
	}); err != nil {
		t.Fatalf("SaveTranscript: %v", err)
	}

	// First call succeeds (202) and, on the way out, releases the slot.
	rec = doGuardJSON(t, srv, http.MethodPost, "/api/notes/"+note.ID+"/resummarize", nil, hdr)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("first resummarize status %d, want 202; body %s", rec.Code, rec.Body)
	}

	// A second, immediate call for the same note id must NOT be rejected by
	// the guard (it may 202 again or fail for an unrelated reason, but must
	// not be the "already in progress" 409).
	rec = doGuardJSON(t, srv, http.MethodPost, "/api/notes/"+note.ID+"/resummarize", nil, hdr)
	var body struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code == http.StatusConflict && body.Error == "summarize already in progress" {
		t.Fatalf("guard slot was not released after success; got 409 %q", body.Error)
	}
	if rec.Code != http.StatusAccepted {
		t.Fatalf("second resummarize status %d, want 202 (transcript still present); body %s", rec.Code, rec.Body)
	}
}

// TestResummarizeGuardReleasedAfterFailure asserts the guard slot is freed
// even when the handler exits via an existing failure path (no transcript),
// so an immediately following call reaches that same failure path again
// rather than the guard's "already in progress" 409.
func TestResummarizeGuardReleasedAfterFailure(t *testing.T) {
	t.Parallel()
	srv, _ := newGuardTestServer(t)
	hdr := setupGuardTestUser(t, srv, "guard-failure@example.com")

	rec := doGuardJSON(t, srv, http.MethodPost, "/api/notes",
		map[string]string{"title": "Guard failure"}, hdr)
	var note struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &note)

	// No transcript saved -> existing "no transcript to summarize" 409.
	rec = doGuardJSON(t, srv, http.MethodPost, "/api/notes/"+note.ID+"/resummarize", nil, hdr)
	if rec.Code != http.StatusConflict {
		t.Fatalf("first resummarize status %d, want 409; body %s", rec.Code, rec.Body)
	}
	var first struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &first)
	if first.Error != "no transcript to summarize" {
		t.Fatalf("first resummarize error = %q, want %q", first.Error, "no transcript to summarize")
	}

	// Immediately calling again must reach the SAME no-transcript 409, not
	// the guard's "already in progress" 409 - proving release happened on
	// this failure path.
	rec = doGuardJSON(t, srv, http.MethodPost, "/api/notes/"+note.ID+"/resummarize", nil, hdr)
	if rec.Code != http.StatusConflict {
		t.Fatalf("second resummarize status %d, want 409; body %s", rec.Code, rec.Body)
	}
	var second struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &second)
	if second.Error != "no transcript to summarize" {
		t.Fatalf("second resummarize error = %q, want %q (guard slot leaked on failure path)", second.Error, "no transcript to summarize")
	}
}

// TestInFlightGuardAcquireRelease is a direct unit test of the guard type
// itself: acquiring twice fails the second time, and release then acquire
// succeeds again.
func TestInFlightGuardAcquireRelease(t *testing.T) {
	t.Parallel()
	var g inFlightGuard

	if !g.tryAcquire("note-1") {
		t.Fatalf("first acquire of a fresh key should succeed")
	}
	if g.tryAcquire("note-1") {
		t.Fatalf("second acquire of an already-held key should fail")
	}
	// A different key is unaffected.
	if !g.tryAcquire("note-2") {
		t.Fatalf("acquire of a distinct key should succeed independently")
	}

	g.release("note-1")
	if !g.tryAcquire("note-1") {
		t.Fatalf("acquire after release should succeed")
	}
	g.release("note-1")
	g.release("note-2")
}
