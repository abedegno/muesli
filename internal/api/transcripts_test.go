package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/abedegno/muesli/internal/model"
)

// TestHandleGetDiarizationReview verifies the GET endpoint returns 200 with the
// correct payload and 404 for an unknown note.
func TestHandleGetDiarizationReview(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)
	ctx := context.Background()

	// Register + login user.
	doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "rev1@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "rev1@example.com", "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	hdr := map[string]string{"Authorization": "Bearer " + login.Token}

	// Create note.
	noteRec := doJSON(t, srv, http.MethodPost, "/api/notes", map[string]string{"title": "Meeting"}, hdr)
	var note struct{ ID string }
	_ = json.Unmarshal(noteRec.Body.Bytes(), &note)

	// Save transcript with speakers directly through the store.
	conf := 0.7
	tr := model.Transcript{
		NoteID:            note.ID,
		TranscriberPlugin: "whisper",
		Model:             "base",
		Segments: []model.Segment{
			{StartMS: 0, EndMS: 1000, Text: "hello", Source: "mic", Speaker: "SPEAKER_00", Confidence: &conf},
			{StartMS: 1000, EndMS: 2000, Text: "world", Source: "mic", Speaker: "SPEAKER_01"},
		},
	}
	if _, err := st.SaveTranscript(ctx, tr); err != nil {
		t.Fatalf("save transcript: %v", err)
	}

	// GET review — should be 200 with pending state and 2 turns.
	r := doJSON(t, srv, http.MethodGet, "/api/notes/"+note.ID+"/transcript/review", nil, hdr)
	if r.Code != http.StatusOK {
		t.Fatalf("GET review: status=%d body=%s", r.Code, r.Body)
	}
	var review model.DiarizationReview
	if err := json.Unmarshal(r.Body.Bytes(), &review); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if review.NoteID != note.ID {
		t.Errorf("note_id: want %q, got %q", note.ID, review.NoteID)
	}
	if review.ReviewState != model.ReviewStatePending {
		t.Errorf("review_state: want %q, got %q", model.ReviewStatePending, review.ReviewState)
	}
	if len(review.Turns) != 2 {
		t.Errorf("turns: want 2, got %d", len(review.Turns))
	}

	// 404 for a note that has no transcript (create a bare note).
	bareRec := doJSON(t, srv, http.MethodPost, "/api/notes", map[string]string{"title": "Bare"}, hdr)
	var bare struct{ ID string }
	_ = json.Unmarshal(bareRec.Body.Bytes(), &bare)
	r404 := doJSON(t, srv, http.MethodGet, "/api/notes/"+bare.ID+"/transcript/review", nil, hdr)
	if r404.Code != http.StatusNotFound {
		t.Errorf("no transcript: want 404, got %d", r404.Code)
	}
}

// TestHandlePostDiarizationReview_confirmSpeaker verifies that POSTing a
// segment_id + speaker updates the speaker and returns 200 with updated payload.
func TestHandlePostDiarizationReview_confirmSpeaker(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)
	ctx := context.Background()

	// Setup user + note.
	doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "rev2@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "rev2@example.com", "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	hdr := map[string]string{"Authorization": "Bearer " + login.Token}

	noteRec := doJSON(t, srv, http.MethodPost, "/api/notes", map[string]string{"title": "M"}, hdr)
	var note struct{ ID string }
	_ = json.Unmarshal(noteRec.Body.Bytes(), &note)

	tr := model.Transcript{
		NoteID:            note.ID,
		TranscriberPlugin: "whisper",
		Model:             "base",
		Segments: []model.Segment{
			{StartMS: 0, EndMS: 1000, Text: "hi", Source: "mic", Speaker: "SPEAKER_00"},
		},
	}
	saved, err := st.SaveTranscript(ctx, tr)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	segID := saved.Segments[0].ID

	// POST confirm speaker.
	body := map[string]string{"segment_id": segID, "speaker": "Alice"}
	r := doJSON(t, srv, http.MethodPost, "/api/notes/"+note.ID+"/transcript/review", body, hdr)
	if r.Code != http.StatusOK {
		t.Fatalf("POST confirm speaker: status=%d body=%s", r.Code, r.Body)
	}
	var review model.DiarizationReview
	if err := json.Unmarshal(r.Body.Bytes(), &review); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// The review returns turns sorted by confidence (NULLs last); since there's
	// only one turn we can check index 0 directly.
	if len(review.Turns) == 0 {
		t.Fatal("expected at least 1 turn")
	}
	if review.Turns[0].Speaker != "Alice" {
		t.Errorf("speaker: want %q, got %q", "Alice", review.Turns[0].Speaker)
	}
}

// TestHandlePostDiarizationReview_advanceState verifies state advancement and
// that illegal transitions return 422.
func TestHandlePostDiarizationReview_advanceState(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)
	ctx := context.Background()

	// Setup user + note.
	doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "rev3@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "rev3@example.com", "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	hdr := map[string]string{"Authorization": "Bearer " + login.Token}

	noteRec := doJSON(t, srv, http.MethodPost, "/api/notes", map[string]string{"title": "M"}, hdr)
	var note struct{ ID string }
	_ = json.Unmarshal(noteRec.Body.Bytes(), &note)

	tr := model.Transcript{
		NoteID:            note.ID,
		TranscriberPlugin: "whisper",
		Model:             "base",
		Segments: []model.Segment{
			{StartMS: 0, EndMS: 1000, Text: "hi", Source: "mic", Speaker: "SPEAKER_00"},
		},
	}
	if _, err := st.SaveTranscript(ctx, tr); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Advance pending → in_review (legal).
	r := doJSON(t, srv, http.MethodPost, "/api/notes/"+note.ID+"/transcript/review",
		map[string]string{"review_state": model.ReviewStateInReview}, hdr)
	if r.Code != http.StatusOK {
		t.Fatalf("advance state: status=%d body=%s", r.Code, r.Body)
	}
	var review model.DiarizationReview
	_ = json.Unmarshal(r.Body.Bytes(), &review)
	if review.ReviewState != model.ReviewStateInReview {
		t.Errorf("review_state: want %q, got %q", model.ReviewStateInReview, review.ReviewState)
	}

	// Same→same (in_review → in_review) is illegal → 422.
	r422 := doJSON(t, srv, http.MethodPost, "/api/notes/"+note.ID+"/transcript/review",
		map[string]string{"review_state": model.ReviewStateInReview}, hdr)
	if r422.Code != http.StatusUnprocessableEntity {
		t.Errorf("illegal transition: want 422, got %d body=%s", r422.Code, r422.Body)
	}
}

// TestHandlePostDiarizationReview_emptyBody verifies a POST with neither
// segment_id nor review_state returns 400. Also checks auth required.
func TestHandlePostDiarizationReview_emptyBody(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)
	ctx := context.Background()

	// Setup user + note.
	doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "rev4@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "rev4@example.com", "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	hdr := map[string]string{"Authorization": "Bearer " + login.Token}

	noteRec := doJSON(t, srv, http.MethodPost, "/api/notes", map[string]string{"title": "M"}, hdr)
	var note struct{ ID string }
	_ = json.Unmarshal(noteRec.Body.Bytes(), &note)

	// Save transcript so the note has one.
	tr := model.Transcript{
		NoteID:            note.ID,
		TranscriberPlugin: "whisper",
		Model:             "base",
		Segments: []model.Segment{
			{StartMS: 0, EndMS: 1000, Text: "hi", Source: "mic", Speaker: "SPEAKER_00"},
		},
	}
	if _, err := st.SaveTranscript(ctx, tr); err != nil {
		t.Fatalf("save: %v", err)
	}

	// POST with empty body (neither field set) → 400.
	r := doJSON(t, srv, http.MethodPost, "/api/notes/"+note.ID+"/transcript/review",
		map[string]string{}, hdr)
	if r.Code != http.StatusBadRequest {
		t.Errorf("empty body: want 400, got %d body=%s", r.Code, r.Body)
	}

	// GET without auth → 401.
	rNoAuth := doJSON(t, srv, http.MethodGet, "/api/notes/"+note.ID+"/transcript/review", nil, nil)
	if rNoAuth.Code != http.StatusUnauthorized {
		t.Errorf("no auth GET: want 401, got %d", rNoAuth.Code)
	}

	// POST without auth → 401.
	rPostNoAuth := doJSON(t, srv, http.MethodPost, "/api/notes/"+note.ID+"/transcript/review",
		map[string]string{"review_state": model.ReviewStateInReview}, nil)
	if rPostNoAuth.Code != http.StatusUnauthorized {
		t.Errorf("no auth POST: want 401, got %d", rPostNoAuth.Code)
	}

	// suppress unused imports
	_ = ctx
	_ = st
}
