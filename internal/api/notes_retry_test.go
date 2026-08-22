package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/abedegno/muesli/internal/auth"
	"github.com/abedegno/muesli/internal/model"
)

func TestRetryNoteAPI(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)
	ctx := context.Background()

	// Setup owner and log in.
	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "retry@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "retry@example.com", "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	hdr := map[string]string{"Authorization": "Bearer " + login.Token}

	// Create a note via the API (starts as "draft").
	rec = doJSON(t, srv, http.MethodPost, "/api/notes",
		map[string]string{"title": "Failed note"}, hdr)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create note: status %d body %s", rec.Code, rec.Body)
	}
	var note struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &note)
	if note.ID == "" {
		t.Fatal("no note id")
	}

	// 409: note not in failed state (still "draft").
	rec = doJSON(t, srv, http.MethodPost, "/api/notes/"+note.ID+"/retry", nil, hdr)
	if rec.Code != http.StatusConflict {
		t.Fatalf("retry non-failed note: got %d, want 409; body %s", rec.Code, rec.Body)
	}

	// Advance note to failed state with no failed jobs yet.
	if err := st.SetNoteStatus(ctx, note.ID, model.NoteFailed); err != nil {
		t.Fatalf("set note failed status: %v", err)
	}

	// 404: note is failed but no failed job in DB.
	rec = doJSON(t, srv, http.MethodPost, "/api/notes/"+note.ID+"/retry", nil, hdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("retry with no failed job: got %d, want 404; body %s", rec.Code, rec.Body)
	}

	// Enqueue a transcribe job and fail it terminally.
	jobID, err := st.EnqueueJob(ctx, note.ID, model.JobTranscribe, nil)
	if err != nil {
		t.Fatalf("enqueue job: %v", err)
	}
	if err := st.FailJob(ctx, jobID, "transcriber error", false); err != nil {
		t.Fatalf("fail job: %v", err)
	}

	// 202 happy path: note is failed, has a failed job.
	rec = doJSON(t, srv, http.MethodPost, "/api/notes/"+note.ID+"/retry", nil, hdr)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("retry happy path: got %d, want 202; body %s", rec.Code, rec.Body)
	}
	var resp struct{ Status string }
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Status != "queued" {
		t.Fatalf("retry response status = %q, want queued", resp.Status)
	}

	// Note status must have been reset to "uploaded".
	retried, err := st.GetNoteAdmin(ctx, note.ID)
	if err != nil {
		t.Fatalf("get note after retry: %v", err)
	}
	if retried.Status != model.NoteUploaded {
		t.Fatalf("note status after retry = %q, want uploaded", retried.Status)
	}

	// 404: wrong owner cannot retry.
	other, _ := st.CreateUser(ctx, "other@example.com", "hash")
	raw, hash, _ := auth.GenerateToken()
	_ = st.CreateToken(ctx, other.ID, "session", hash, "session")
	otherHdr := map[string]string{"Authorization": "Bearer " + raw}
	// Reset to failed so the owner check fires (not the status check).
	if err := st.SetNoteStatus(ctx, note.ID, model.NoteFailed); err != nil {
		t.Fatalf("reset note status: %v", err)
	}
	rec = doJSON(t, srv, http.MethodPost, "/api/notes/"+note.ID+"/retry", nil, otherHdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-owner retry: got %d, want 404; body %s", rec.Code, rec.Body)
	}
}

// TestRetryNoteRefreshesStaleExpectedGeneration is the regression test for
// H6: handleRetryNote does not just forward the failed job's stored payload —
// for a transcribe job it must overwrite expected_generation with the note's
// CURRENT transcript generation. This exercises the unmarshal/overwrite/
// remarshal logic end to end (via the real HTTP handler, not the store
// directly) and asserts the concrete re-enqueued value, not just "the retry
// didn't crash".
func TestRetryNoteRefreshesStaleExpectedGeneration(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)
	ctx := context.Background()

	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "retrygen@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "retrygen@example.com", "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	hdr := map[string]string{"Authorization": "Bearer " + login.Token}

	rec = doJSON(t, srv, http.MethodPost, "/api/notes", map[string]string{"title": "Retry gen"}, hdr)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create note: status %d body %s", rec.Code, rec.Body)
	}
	var note struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &note)
	if note.ID == "" {
		t.Fatal("no note id")
	}

	// Seed an existing transcript directly through the store, so the note's
	// CURRENT transcript generation is 1.
	if _, err := st.SaveTranscript(ctx, model.Transcript{
		NoteID:            note.ID,
		TranscriberPlugin: "whisper",
		Segments:          []model.Segment{{StartMS: 0, EndMS: 10, Text: "hi", Source: "mic"}},
	}, 0); err != nil {
		t.Fatalf("seed transcript: %v", err)
	}

	// The failed job's OWN stored payload is stale: expected_generation 0, as
	// if it had been enqueued before that transcript ever existed.
	audioKey := "notes/" + note.ID + "/audio/a.webm"
	staleJobID, err := st.EnqueueJob(ctx, note.ID, model.JobTranscribe,
		json.RawMessage(`{"audio_key":"`+audioKey+`","expected_generation":0}`))
	if err != nil {
		t.Fatalf("enqueue stale job: %v", err)
	}
	if err := st.FailJob(ctx, staleJobID, "transcriber error", false); err != nil {
		t.Fatalf("fail job: %v", err)
	}
	if err := st.SetNoteStatus(ctx, note.ID, model.NoteFailed); err != nil {
		t.Fatalf("set note failed: %v", err)
	}

	rec = doJSON(t, srv, http.MethodPost, "/api/notes/"+note.ID+"/retry", nil, hdr)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("retry: status %d body %s", rec.Code, rec.Body)
	}

	// Find the freshly re-enqueued pending job for this note (the stale job
	// stays "failed"; RetryNote inserts a new row) and inspect its payload.
	var newJobID string
	if err := st.Pool().QueryRow(ctx,
		`SELECT id FROM jobs WHERE note_id=$1 AND status=$2 AND id != $3`,
		note.ID, model.JobPending, staleJobID).Scan(&newJobID); err != nil {
		t.Fatalf("find retried job: %v", err)
	}
	newJob, err := st.GetJob(ctx, newJobID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	var payload struct {
		AudioKey           string `json:"audio_key"`
		ExpectedGeneration int    `json:"expected_generation"`
	}
	if err := json.Unmarshal(newJob.Payload, &payload); err != nil {
		t.Fatalf("unmarshal retried payload: %v", err)
	}
	if payload.ExpectedGeneration != 1 {
		t.Fatalf("retried job expected_generation = %d, want 1 (fresh, not the stale 0 carried by the failed job)", payload.ExpectedGeneration)
	}
	if payload.AudioKey != audioKey {
		t.Fatalf("retried job audio_key = %q, want %q (other fields must survive the refresh)", payload.AudioKey, audioKey)
	}
}
