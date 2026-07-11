package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/auth"
	"github.com/abedegno/muesli/internal/model"
)

func TestProcessNextNoteAPI(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)
	ctx := context.Background()

	// Setup owner and log in.
	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "processnext@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "processnext@example.com", "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	hdr := map[string]string{"Authorization": "Bearer " + login.Token}

	rec = doJSON(t, srv, http.MethodPost, "/api/notes",
		map[string]string{"title": "Queued note"}, hdr)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create note: status %d body %s", rec.Code, rec.Body)
	}
	var note struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &note)
	if note.ID == "" {
		t.Fatal("no note id")
	}

	// 409: no pending job yet on this note.
	rec = doJSON(t, srv, http.MethodPost, "/api/notes/"+note.ID+"/process-next", nil, hdr)
	if rec.Code != http.StatusConflict {
		t.Fatalf("process-next with no pending job: got %d, want 409; body %s", rec.Code, rec.Body)
	}

	// Enqueue a job on our note and immediately claim it (it is the only
	// claimable job in the whole table at this point, so ClaimJob must pick
	// it) so it becomes RUNNING and must not be touched by the bump below.
	runningJobID, err := st.EnqueueJob(ctx, note.ID, model.JobTranscribe, nil)
	if err != nil {
		t.Fatalf("enqueue running job: %v", err)
	}
	claimedRunning, ok, err := st.ClaimJob(ctx, 10*time.Minute)
	if err != nil || !ok || claimedRunning.ID != runningJobID {
		t.Fatalf("claim running job: ok=%v err=%v claimed=%+v", ok, err, claimedRunning)
	}

	// An older pending job on another note (owned by someone else) that must
	// also be left untouched.
	otherUser, err := st.CreateUser(ctx, "processnext2@example.com", "hash")
	if err != nil {
		t.Fatalf("create other user: %v", err)
	}
	otherNote, err := st.CreateNote(ctx, otherUser.ID, "Other note")
	if err != nil {
		t.Fatalf("create other note: %v", err)
	}
	otherPendingJobID, err := st.EnqueueJob(ctx, otherNote.ID, model.JobTranscribe, nil)
	if err != nil {
		t.Fatalf("enqueue other pending job: %v", err)
	}

	// The only PENDING job belonging to our note.
	pendingJobID, err := st.EnqueueJob(ctx, note.ID, model.JobSummarize, nil)
	if err != nil {
		t.Fatalf("enqueue pending job: %v", err)
	}

	// 200 happy path: note has a pending job to bump.
	rec = doJSON(t, srv, http.MethodPost, "/api/notes/"+note.ID+"/process-next", nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("process-next happy path: got %d, want 200; body %s", rec.Code, rec.Body)
	}
	var resp struct {
		Status     string `json:"status"`
		JobsBumped int    `json:"jobs_bumped"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Status != "bumped" || resp.JobsBumped != 1 {
		t.Fatalf("process-next response = %+v, want status=bumped jobs_bumped=1", resp)
	}

	// Only the pending job's priority moved; the running job and the other
	// note's older pending job are untouched.
	bumped, err := st.GetJob(ctx, pendingJobID)
	if err != nil {
		t.Fatalf("get bumped job: %v", err)
	}
	running, err := st.GetJob(ctx, runningJobID)
	if err != nil {
		t.Fatalf("get running job: %v", err)
	}
	if running.Status != model.JobRunning {
		t.Fatalf("running job status changed to %q", running.Status)
	}
	if running.Priority != 0 {
		t.Fatalf("running job priority changed to %d, want 0", running.Priority)
	}
	otherPending, err := st.GetJob(ctx, otherPendingJobID)
	if err != nil {
		t.Fatalf("get other pending job: %v", err)
	}
	if otherPending.Priority != 0 {
		t.Fatalf("other note's pending job priority changed to %d, want 0", otherPending.Priority)
	}
	if bumped.Priority <= otherPending.Priority {
		t.Fatalf("bumped job priority %d should be > other pending job's priority %d", bumped.Priority, otherPending.Priority)
	}

	// The bumped job now dequeues first, ahead of the older never-bumped job.
	claimed, ok, err := st.ClaimJob(ctx, 10*time.Minute)
	if err != nil || !ok {
		t.Fatalf("claim after bump: ok=%v err=%v", ok, err)
	}
	if claimed.ID != pendingJobID {
		t.Fatalf("expected bumped job %s to dequeue first, got %s", pendingJobID, claimed.ID)
	}

	// 404: wrong owner cannot process-next.
	other, _ := st.CreateUser(ctx, "processnext-other@example.com", "hash")
	raw, hash, _ := auth.GenerateToken()
	_ = st.CreateToken(ctx, other.ID, "session", hash, "session")
	otherHdr := map[string]string{"Authorization": "Bearer " + raw}
	rec = doJSON(t, srv, http.MethodPost, "/api/notes/"+note.ID+"/process-next", nil, otherHdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-owner process-next: got %d, want 404; body %s", rec.Code, rec.Body)
	}
}
