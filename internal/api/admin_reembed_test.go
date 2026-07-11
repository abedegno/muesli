package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/abedegno/muesli/internal/api"
	"github.com/abedegno/muesli/internal/config"
	"github.com/abedegno/muesli/internal/embed"
	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
)

// TestAdminReembedStatusDisabled verifies the live status endpoint reports
// enabled:false with zeroed fields when no embedder is configured.
func TestAdminReembedStatusDisabled(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	srv := api.NewServer(api.Deps{Store: st})
	hdr := setupLoginHdr(t, srv, "admin@example.com")

	rec := doJSON(t, srv, http.MethodGet, "/api/admin/embeddings/status", nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body)
	}
	var got api.ReembedStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := api.ReembedStatus{Enabled: false}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

// TestAdminReembedStatusEnabled walks progress through 0 notes -> a ready note
// with no embedding -> the same note embedded, confirming done/total track
// live store state via the configured embedder's model + Dim().
func TestAdminReembedStatusEnabled(t *testing.T) {
	t.Parallel()
	pool := testutil.NewPool(t)
	st := store.New(pool)
	cfg := config.Config{
		EmbeddingsURL:   "http://ollama:11434",
		EmbeddingsModel: "nomic-embed-text",
		EmbeddingsDim:   768,
	}
	embedder := embed.New(cfg)
	srv := api.NewServer(api.Deps{Store: st, Config: cfg, Embedder: embedder})
	hdr := setupLoginHdr(t, srv, "admin@example.com")

	// 0 notes -> total 0.
	rec := doJSON(t, srv, http.MethodGet, "/api/admin/embeddings/status", nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body)
	}
	var got api.ReembedStatus
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if !got.Enabled || got.Model != "nomic-embed-text" || got.Dim != 768 {
		t.Fatalf("got %+v, want enabled with model/dim from config", got)
	}
	if got.Total != 0 || got.Done != 0 {
		t.Fatalf("with 0 notes: got %+v, want total=0 done=0", got)
	}

	// Note created then marked ready -> total 1, done 0.
	ctx := context.Background()
	u, err := st.CreateUser(ctx, "owner@example.com", "hash")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	note, err := st.CreateNote(ctx, u.ID, "Test")
	if err != nil {
		t.Fatalf("create note: %v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE notes SET status='ready' WHERE id=$1", note.ID); err != nil {
		t.Fatalf("set ready: %v", err)
	}

	rec = doJSON(t, srv, http.MethodGet, "/api/admin/embeddings/status", nil, hdr)
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Total != 1 || got.Done != 0 {
		t.Fatalf("after ready note: got %+v, want total=1 done=0", got)
	}

	// After UpsertEmbedding -> done 1.
	vec := make([]float32, 768)
	vec[0] = 1
	if err := st.UpsertEmbedding(ctx, note.ID, "nomic-embed-text", vec); err != nil {
		t.Fatalf("upsert embedding: %v", err)
	}

	rec = doJSON(t, srv, http.MethodGet, "/api/admin/embeddings/status", nil, hdr)
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Total != 1 || got.Done != 1 {
		t.Fatalf("after upsert: got %+v, want total=1 done=1", got)
	}
}

// TestAdminReembedAllHappyPath verifies POST reembed clears existing
// embeddings for the current (model, dim), leaves the eligible population
// (total) unchanged, and enqueues a pending embed job for the affected note.
func TestAdminReembedAllHappyPath(t *testing.T) {
	t.Parallel()
	pool := testutil.NewPool(t)
	st := store.New(pool)
	cfg := config.Config{
		EmbeddingsURL:   "http://ollama:11434",
		EmbeddingsModel: "nomic-embed-text",
		EmbeddingsDim:   768,
	}
	embedder := embed.New(cfg)
	srv := api.NewServer(api.Deps{Store: st, Config: cfg, Embedder: embedder})
	hdr := setupLoginHdr(t, srv, "admin@example.com")

	ctx := context.Background()
	u, err := st.CreateUser(ctx, "owner@example.com", "hash")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	note, err := st.CreateNote(ctx, u.ID, "Test")
	if err != nil {
		t.Fatalf("create note: %v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE notes SET status='ready' WHERE id=$1", note.ID); err != nil {
		t.Fatalf("set ready: %v", err)
	}
	vec := make([]float32, 768)
	vec[0] = 1
	if err := st.UpsertEmbedding(ctx, note.ID, "nomic-embed-text", vec); err != nil {
		t.Fatalf("upsert embedding: %v", err)
	}

	rec := doJSON(t, srv, http.MethodPost, "/api/admin/embeddings/reembed", nil, hdr)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body: %s", rec.Code, rec.Body)
	}
	var resp api.ReembedResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "queued" {
		t.Errorf("status = %q, want queued", resp.Status)
	}
	if resp.Enqueued != 1 {
		t.Errorf("enqueued = %d, want 1", resp.Enqueued)
	}

	statusRec := doJSON(t, srv, http.MethodGet, "/api/admin/embeddings/status", nil, hdr)
	var status api.ReembedStatus
	_ = json.Unmarshal(statusRec.Body.Bytes(), &status)
	if status.Done != 0 {
		t.Errorf("done = %d, want 0 after reembed (embedding deleted)", status.Done)
	}
	if status.Total != 1 {
		t.Errorf("total = %d, want 1 (unchanged; note still exists)", status.Total)
	}

	jobs, err := st.ListJobs(ctx, model.JobPending)
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	found := false
	for _, j := range jobs {
		if j.NoteID == note.ID && j.Type == model.JobEmbed {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("no pending embed job found for note %s among %+v", note.ID, jobs)
	}
}

// TestAdminReembedAllDisabled verifies the trigger endpoint refuses to run
// (409) when embeddings are disabled, rather than silently no-oping.
func TestAdminReembedAllDisabled(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	srv := api.NewServer(api.Deps{Store: st})
	hdr := setupLoginHdr(t, srv, "admin@example.com")

	rec := doJSON(t, srv, http.MethodPost, "/api/admin/embeddings/reembed", nil, hdr)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body: %s", rec.Code, rec.Body)
	}
	var body struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Error != "embeddings are disabled" {
		t.Errorf("error = %q, want %q", body.Error, "embeddings are disabled")
	}
}
