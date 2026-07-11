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

	// Create a note via the API (starts as "recording").
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

	// 409: note not in failed state (still "recording").
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
