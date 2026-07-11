package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/abedegno/muesli/internal/api"
	"github.com/abedegno/muesli/internal/crypto"
	"github.com/abedegno/muesli/internal/storage"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestUploadEnqueuesJobAndAdminLists(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	cr, _ := crypto.New("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	prov, _ := storage.NewLocal(t.TempDir(), "http://example.test", "http://example.test", []byte("test-signing-key-0123456789"))
	srv := api.NewServer(api.Deps{Store: st, Storage: prov, Crypto: cr})

	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "o@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "o@example.com", "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	hdr := map[string]string{"Authorization": "Bearer " + login.Token}

	rec = doJSON(t, srv, http.MethodPost, "/api/notes", map[string]string{"title": "M"}, hdr)
	var note struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &note)

	// Presign + PUT + notify.
	rec = doJSON(t, srv, http.MethodPost, "/api/notes/"+note.ID+"/audio-upload-url", nil, hdr)
	var grant struct{ URL, Key string }
	_ = json.Unmarshal(rec.Body.Bytes(), &grant)
	_ = doRaw(t, srv, http.MethodPut, strings.TrimPrefix(grant.URL, "http://example.test"), "audio-bytes",
		map[string]string{"Content-Type": "audio/webm"})
	rec = doJSON(t, srv, http.MethodPost, "/api/notes/"+note.ID+"/audio-uploaded",
		map[string]string{"key": grant.Key}, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("uploaded notify status %d body %s", rec.Code, rec.Body)
	}

	// The admin job monitor shows one pending transcribe job for the note.
	rec = doJSON(t, srv, http.MethodGet, "/api/admin/jobs?status=pending", nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("jobs status %d", rec.Code)
	}
	var jobs []map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &jobs)
	if len(jobs) != 1 || jobs[0]["type"] != "transcribe" || jobs[0]["note_id"] != note.ID {
		t.Fatalf("unexpected jobs: %s", rec.Body)
	}
}

// newRetryTestServer returns a server, store and the raw pool for direct SQL.
func newRetryTestServer(t *testing.T) (*api.Server, *store.Store, *pgxpool.Pool) {
	t.Helper()
	pool := testutil.NewPool(t)
	st := store.New(pool)
	return api.NewServer(api.Deps{Store: st}), st, pool
}

// setupLoginHdr creates the first user via /api/setup, logs in, and returns auth headers.
func setupLoginHdr(t *testing.T, srv *api.Server, email string) map[string]string {
	t.Helper()
	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": email, "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": email, "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	return map[string]string{"Authorization": "Bearer " + login.Token}
}

// TestRetryJobSuccess creates a failed transcribe job, retries it, and verifies
// a new pending job appears for the same note.
func TestRetryJobSuccess(t *testing.T) {
	srv, st, pool := newRetryTestServer(t)
	ctx := context.Background()

	hdr := setupLoginHdr(t, srv, "admin@example.com")

	// Create a user + note via store methods.
	u, err := st.CreateUser(ctx, "owner@example.com", "hash")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	note, err := st.CreateNote(ctx, u.ID, "test note")
	if err != nil {
		t.Fatalf("create note: %v", err)
	}

	// Enqueue a transcribe job then mark it failed directly.
	jobID, err := st.EnqueueJob(ctx, note.ID, "transcribe", nil)
	if err != nil {
		t.Fatalf("enqueue job: %v", err)
	}
	_, err = pool.Exec(ctx, `UPDATE jobs SET status='failed' WHERE id=$1`, jobID)
	if err != nil {
		t.Fatalf("mark failed: %v", err)
	}

	// Call the retry endpoint.
	rec := doJSON(t, srv, http.MethodPost, "/api/admin/jobs/"+jobID+"/retry", nil, hdr)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("retry status %d body %s", rec.Code, rec.Body)
	}
	var resp map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["status"] != "queued" {
		t.Fatalf("unexpected response: %s", rec.Body)
	}

	// A new pending job should exist for the same note.
	rec = doJSON(t, srv, http.MethodGet, "/api/admin/jobs?status=pending", nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("list jobs status %d", rec.Code)
	}
	var jobs []map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &jobs)
	found := false
	for _, j := range jobs {
		if j["note_id"] == note.ID && j["type"] == "transcribe" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected pending transcribe job for note %s, got: %s", note.ID, rec.Body)
	}
}

// TestRetryJobNotFound verifies 404 when the job ID doesn't exist.
func TestRetryJobNotFound(t *testing.T) {
	srv, _, _ := newRetryTestServer(t)
	hdr := setupLoginHdr(t, srv, "admin2@example.com")

	rec := doJSON(t, srv, http.MethodPost, "/api/admin/jobs/00000000-0000-0000-0000-000000000001/retry", nil, hdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d body %s", rec.Code, rec.Body)
	}
}

// TestRetryJobNotFailed verifies 409 when the job exists but is not in failed state.
func TestRetryJobNotFailed(t *testing.T) {
	srv, st, _ := newRetryTestServer(t)
	ctx := context.Background()
	hdr := setupLoginHdr(t, srv, "admin3@example.com")

	u, err := st.CreateUser(ctx, "owner3@example.com", "hash")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	note, err := st.CreateNote(ctx, u.ID, "pending note")
	if err != nil {
		t.Fatalf("create note: %v", err)
	}
	// Job stays in pending state.
	jobID, err := st.EnqueueJob(ctx, note.ID, "transcribe", nil)
	if err != nil {
		t.Fatalf("enqueue job: %v", err)
	}

	rec := doJSON(t, srv, http.MethodPost, "/api/admin/jobs/"+jobID+"/retry", nil, hdr)
	if rec.Code != http.StatusConflict {
		t.Fatalf("want 409, got %d body %s", rec.Code, rec.Body)
	}
}

// TestRetryJobNoteDeleted verifies 404 when the job's note has been soft-deleted.
func TestRetryJobNoteDeleted(t *testing.T) {
	srv, st, pool := newRetryTestServer(t)
	ctx := context.Background()
	hdr := setupLoginHdr(t, srv, "admin4@example.com")

	u, err := st.CreateUser(ctx, "owner4@example.com", "hash")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	note, err := st.CreateNote(ctx, u.ID, "deleted note")
	if err != nil {
		t.Fatalf("create note: %v", err)
	}
	jobID, err := st.EnqueueJob(ctx, note.ID, "transcribe", nil)
	if err != nil {
		t.Fatalf("enqueue job: %v", err)
	}
	// Mark job failed and soft-delete the note.
	_, err = pool.Exec(ctx, `UPDATE jobs SET status='failed' WHERE id=$1`, jobID)
	if err != nil {
		t.Fatalf("mark failed: %v", err)
	}
	if err := st.DeleteNote(ctx, u.ID, note.ID); err != nil {
		t.Fatalf("delete note: %v", err)
	}

	rec := doJSON(t, srv, http.MethodPost, "/api/admin/jobs/"+jobID+"/retry", nil, hdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d body %s", rec.Code, rec.Body)
	}
}

// TestCancelJobSuccess cancels a pending job and verifies the response and
// resulting job status.
func TestCancelJobSuccess(t *testing.T) {
	srv, st, _ := newRetryTestServer(t)
	ctx := context.Background()
	hdr := setupLoginHdr(t, srv, "admin5@example.com")

	u, err := st.CreateUser(ctx, "owner5@example.com", "hash")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	note, err := st.CreateNote(ctx, u.ID, "pending note")
	if err != nil {
		t.Fatalf("create note: %v", err)
	}
	jobID, err := st.EnqueueJob(ctx, note.ID, "transcribe", nil)
	if err != nil {
		t.Fatalf("enqueue job: %v", err)
	}

	rec := doJSON(t, srv, http.MethodPost, "/api/admin/jobs/"+jobID+"/cancel", nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("cancel status %d body %s", rec.Code, rec.Body)
	}
	var resp map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["status"] != "cancelled" {
		t.Fatalf("unexpected response: %s", rec.Body)
	}

	job, err := st.GetJob(ctx, jobID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.Status != "cancelled" {
		t.Fatalf("want job status cancelled, got %s", job.Status)
	}
}

// TestCancelJobRunningConflict verifies cancelling a running job is refused
// with 409 and leaves the job's status unchanged (the critical "running jobs
// untouchable" case).
func TestCancelJobRunningConflict(t *testing.T) {
	srv, st, pool := newRetryTestServer(t)
	ctx := context.Background()
	hdr := setupLoginHdr(t, srv, "admin6@example.com")

	u, err := st.CreateUser(ctx, "owner6@example.com", "hash")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	note, err := st.CreateNote(ctx, u.ID, "running note")
	if err != nil {
		t.Fatalf("create note: %v", err)
	}
	jobID, err := st.EnqueueJob(ctx, note.ID, "transcribe", nil)
	if err != nil {
		t.Fatalf("enqueue job: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE jobs SET status='running' WHERE id=$1`, jobID); err != nil {
		t.Fatalf("mark running: %v", err)
	}

	rec := doJSON(t, srv, http.MethodPost, "/api/admin/jobs/"+jobID+"/cancel", nil, hdr)
	if rec.Code != http.StatusConflict {
		t.Fatalf("want 409, got %d body %s", rec.Code, rec.Body)
	}

	job, err := st.GetJob(ctx, jobID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.Status != "running" {
		t.Fatalf("running job status changed to %s", job.Status)
	}
}

// TestCancelJobTerminalConflict verifies cancelling a done or failed job is
// refused with 409.
func TestCancelJobTerminalConflict(t *testing.T) {
	srv, st, pool := newRetryTestServer(t)
	ctx := context.Background()
	hdr := setupLoginHdr(t, srv, "admin7@example.com")

	u, err := st.CreateUser(ctx, "owner7@example.com", "hash")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	note, err := st.CreateNote(ctx, u.ID, "terminal note")
	if err != nil {
		t.Fatalf("create note: %v", err)
	}

	for _, status := range []string{"done", "failed"} {
		jobID, err := st.EnqueueJob(ctx, note.ID, "transcribe", nil)
		if err != nil {
			t.Fatalf("enqueue job: %v", err)
		}
		if _, err := pool.Exec(ctx, `UPDATE jobs SET status=$1 WHERE id=$2`, status, jobID); err != nil {
			t.Fatalf("mark %s: %v", status, err)
		}

		rec := doJSON(t, srv, http.MethodPost, "/api/admin/jobs/"+jobID+"/cancel", nil, hdr)
		if rec.Code != http.StatusConflict {
			t.Fatalf("status %s: want 409, got %d body %s", status, rec.Code, rec.Body)
		}
	}
}

// TestCancelJobNotFound verifies 404 when the job ID doesn't exist.
func TestCancelJobNotFound(t *testing.T) {
	srv, _, _ := newRetryTestServer(t)
	hdr := setupLoginHdr(t, srv, "admin8@example.com")

	rec := doJSON(t, srv, http.MethodPost, "/api/admin/jobs/00000000-0000-0000-0000-000000000002/cancel", nil, hdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d body %s", rec.Code, rec.Body)
	}
}

// TestProcessNextJobSuccess bumps a pending job's priority and verifies the
// response and that the job's priority actually increased.
func TestProcessNextJobSuccess(t *testing.T) {
	srv, st, _ := newRetryTestServer(t)
	ctx := context.Background()
	hdr := setupLoginHdr(t, srv, "admin9@example.com")

	u, err := st.CreateUser(ctx, "owner9@example.com", "hash")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	note, err := st.CreateNote(ctx, u.ID, "queued note")
	if err != nil {
		t.Fatalf("create note: %v", err)
	}
	jobID, err := st.EnqueueJob(ctx, note.ID, "transcribe", nil)
	if err != nil {
		t.Fatalf("enqueue job: %v", err)
	}
	before, err := st.GetJob(ctx, jobID)
	if err != nil {
		t.Fatalf("get before: %v", err)
	}

	rec := doJSON(t, srv, http.MethodPost, "/api/admin/jobs/"+jobID+"/process-next", nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("process-next status %d body %s", rec.Code, rec.Body)
	}
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["status"] != "bumped" {
		t.Fatalf("unexpected response: %s", rec.Body)
	}
	if n, ok := resp["jobs_bumped"].(float64); !ok || n < 1 {
		t.Fatalf("expected jobs_bumped>=1, got: %s", rec.Body)
	}

	after, err := st.GetJob(ctx, jobID)
	if err != nil {
		t.Fatalf("get after: %v", err)
	}
	if after.Priority <= before.Priority {
		t.Fatalf("expected priority to increase: before=%d after=%d", before.Priority, after.Priority)
	}
}

// TestProcessNextJobRunningConflict verifies process-next never bumps a job
// that isn't pending: calling it on a running job's id is refused with 409
// and its priority is unchanged.
func TestProcessNextJobRunningConflict(t *testing.T) {
	srv, st, pool := newRetryTestServer(t)
	ctx := context.Background()
	hdr := setupLoginHdr(t, srv, "admin10@example.com")

	u, err := st.CreateUser(ctx, "owner10@example.com", "hash")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	note, err := st.CreateNote(ctx, u.ID, "running note")
	if err != nil {
		t.Fatalf("create note: %v", err)
	}
	jobID, err := st.EnqueueJob(ctx, note.ID, "transcribe", nil)
	if err != nil {
		t.Fatalf("enqueue job: %v", err)
	}
	before, err := st.GetJob(ctx, jobID)
	if err != nil {
		t.Fatalf("get before: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE jobs SET status='running' WHERE id=$1`, jobID); err != nil {
		t.Fatalf("mark running: %v", err)
	}

	rec := doJSON(t, srv, http.MethodPost, "/api/admin/jobs/"+jobID+"/process-next", nil, hdr)
	if rec.Code != http.StatusConflict {
		t.Fatalf("want 409, got %d body %s", rec.Code, rec.Body)
	}

	after, err := st.GetJob(ctx, jobID)
	if err != nil {
		t.Fatalf("get after: %v", err)
	}
	if after.Priority != before.Priority {
		t.Fatalf("running job priority changed: before=%d after=%d", before.Priority, after.Priority)
	}
}

// TestProcessNextJobNotFound verifies 404 when the job ID doesn't exist.
func TestProcessNextJobNotFound(t *testing.T) {
	srv, _, _ := newRetryTestServer(t)
	hdr := setupLoginHdr(t, srv, "admin11@example.com")

	rec := doJSON(t, srv, http.MethodPost, "/api/admin/jobs/00000000-0000-0000-0000-000000000003/process-next", nil, hdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d body %s", rec.Code, rec.Body)
	}
}

// TestListNoteJobsSuccess verifies GET /api/admin/notes/{id}/jobs returns every
// job for a note, in created_at ASC (pipeline) order, with the started_at/
// finished_at/last_error fields intact for each job's current state.
func TestListNoteJobsSuccess(t *testing.T) {
	srv, st, pool := newRetryTestServer(t)
	ctx := context.Background()
	hdr := setupLoginHdr(t, srv, "admin12@example.com")

	u, err := st.CreateUser(ctx, "owner12@example.com", "hash")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	note, err := st.CreateNote(ctx, u.ID, "pipeline note")
	if err != nil {
		t.Fatalf("create note: %v", err)
	}

	// Enqueue in pipeline order, forcing created_at apart so ordering is
	// deterministic regardless of clock resolution.
	transcribeID, err := st.EnqueueJob(ctx, note.ID, "transcribe", nil)
	if err != nil {
		t.Fatalf("enqueue transcribe: %v", err)
	}
	summarizeID, err := st.EnqueueJob(ctx, note.ID, "summarize", nil)
	if err != nil {
		t.Fatalf("enqueue summarize: %v", err)
	}
	embedID, err := st.EnqueueJob(ctx, note.ID, "embed", nil)
	if err != nil {
		t.Fatalf("enqueue embed: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE jobs SET created_at = now() - interval '2 minutes' WHERE id=$1`, transcribeID); err != nil {
		t.Fatalf("set transcribe created_at: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE jobs SET created_at = now() - interval '1 minute' WHERE id=$1`, summarizeID); err != nil {
		t.Fatalf("set summarize created_at: %v", err)
	}

	// transcribe: done, with a completed timeline.
	if _, err := pool.Exec(ctx,
		`UPDATE jobs SET status='done', attempts=1,
		 started_at = now() - interval '90 seconds', finished_at = now() - interval '85 seconds'
		 WHERE id=$1`, transcribeID); err != nil {
		t.Fatalf("mark transcribe done: %v", err)
	}
	// summarize: failed, with a last_error and a completed (failed) attempt.
	if _, err := pool.Exec(ctx,
		`UPDATE jobs SET status='failed', attempts=2, last_error='agent timeout',
		 started_at = now() - interval '80 seconds', finished_at = now() - interval '70 seconds'
		 WHERE id=$1`, summarizeID); err != nil {
		t.Fatalf("mark summarize failed: %v", err)
	}
	// embed: still pending, no timeline yet.

	rec := doJSON(t, srv, http.MethodGet, "/api/admin/notes/"+note.ID+"/jobs", nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("list note jobs status %d body %s", rec.Code, rec.Body)
	}
	var jobs []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &jobs); err != nil {
		t.Fatalf("unmarshal: %v body %s", err, rec.Body)
	}
	if len(jobs) != 3 {
		t.Fatalf("want 3 jobs, got %d: %s", len(jobs), rec.Body)
	}

	if jobs[0]["id"] != transcribeID || jobs[0]["type"] != "transcribe" || jobs[0]["status"] != "done" {
		t.Fatalf("unexpected first job (want done transcribe): %s", rec.Body)
	}
	if jobs[0]["started_at"] == nil || jobs[0]["finished_at"] == nil {
		t.Fatalf("expected transcribe job to have started_at+finished_at: %s", rec.Body)
	}

	if jobs[1]["id"] != summarizeID || jobs[1]["type"] != "summarize" || jobs[1]["status"] != "failed" {
		t.Fatalf("unexpected second job (want failed summarize): %s", rec.Body)
	}
	if jobs[1]["last_error"] != "agent timeout" {
		t.Fatalf("expected last_error on failed summarize job: %s", rec.Body)
	}
	if jobs[1]["started_at"] == nil || jobs[1]["finished_at"] == nil {
		t.Fatalf("expected summarize job to have started_at+finished_at: %s", rec.Body)
	}

	if jobs[2]["id"] != embedID || jobs[2]["type"] != "embed" || jobs[2]["status"] != "pending" {
		t.Fatalf("unexpected third job (want pending embed): %s", rec.Body)
	}
	if jobs[2]["started_at"] != nil || jobs[2]["finished_at"] != nil {
		t.Fatalf("expected pending embed job to have nil started_at/finished_at: %s", rec.Body)
	}
}

// TestListNoteJobsNotFound verifies 404 when the note ID doesn't exist.
func TestListNoteJobsNotFound(t *testing.T) {
	srv, _, _ := newRetryTestServer(t)
	hdr := setupLoginHdr(t, srv, "admin13@example.com")

	rec := doJSON(t, srv, http.MethodGet, "/api/admin/notes/00000000-0000-0000-0000-000000000004/jobs", nil, hdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d body %s", rec.Code, rec.Body)
	}
}
