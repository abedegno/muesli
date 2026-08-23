package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/api"
	"github.com/abedegno/muesli/internal/config"
	"github.com/abedegno/muesli/internal/crypto"
	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/plugin"
	"github.com/abedegno/muesli/internal/storage"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
	"github.com/abedegno/muesli/internal/worker"
)

// newFlakyPartialTranscriber returns a stub whose FIRST /transcribe call
// returns a partial result (segments plus partial=true, the shape that fails
// runTranscribe terminally after publishing) and whose every call after that
// returns a full, non-partial result with DIFFERENT segment text — so a test
// can tell "the retry re-transcribed" from "the retry resumed with the old
// partial content" by inspecting which segments ended up saved, not just by
// counting calls.
func newFlakyPartialTranscriber(t *testing.T) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var calls atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/info", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(plugin.Info{Name: "stub-flaky-partial", Version: "0", PluginAPI: 1, Kind: "transcriber"})
	})
	mux.HandleFunc("/transcribe", func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			_ = json.NewEncoder(w).Encode(plugin.TranscribeResponse{
				Segments:      []model.Segment{{StartMS: 0, EndMS: 1000, Text: "cut off mid-sen", Source: "mic"}},
				Language:      "en",
				Model:         "stub-partial",
				DurationMS:    1000,
				Partial:       true,
				PartialReason: "gpu oom",
			})
			return
		}
		_ = json.NewEncoder(w).Encode(plugin.TranscribeResponse{
			Segments: []model.Segment{
				{StartMS: 0, EndMS: 1000, Text: "Welcome everyone.", Source: "mic"},
				{StartMS: 1000, EndMS: 2000, Text: "Full transcript this time.", Source: "mic"},
			},
			Language: "en", Model: "stub-full", DurationMS: 2000,
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &calls
}

// partialRetryFixture wires a real api.Server and worker.Processor against the
// same store, so a test can drive retries through the actual HTTP handlers
// (handleRetryNote / handleRetryJob) and then process the re-enqueued job with
// the actual pipeline — exercising the exact path the finding describes rather
// than asserting against the store or the payload-building helper in isolation.
type partialRetryFixture struct {
	srv      *api.Server
	proc     *worker.Processor
	st       *store.Store
	hdr      map[string]string
	noteID   string
	audioKey string
	calls    *atomic.Int64
}

func newPartialRetryFixture(t *testing.T, emailPrefix string) *partialRetryFixture {
	t.Helper()
	ctx := context.Background()
	pool := testutil.NewPool(t)
	st := store.New(pool)
	cr, err := crypto.New("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	if err != nil {
		t.Fatal(err)
	}
	_ = st.SeedBuiltInTemplates(ctx)

	root := t.TempDir()
	prov, err := storage.NewLocal(root, "http://example.test", "http://example.test", []byte("test-signing-key-0123456789"))
	if err != nil {
		t.Fatal(err)
	}

	trSrv, calls := newFlakyPartialTranscriber(t)
	agSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/info" {
			_ = json.NewEncoder(w).Encode(plugin.Info{Name: "stub-agent", Version: "0", PluginAPI: 1, Kind: "agent"})
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(agSrv.Close)

	tp, err := st.CreatePlugin(ctx, cr, model.Plugin{Kind: model.PluginTranscriber, Name: "t", EndpointURL: trSrv.URL, Token: "x", Enabled: true, Config: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatalf("create transcriber plugin: %v", err)
	}
	ap, err := st.CreatePlugin(ctx, cr, model.Plugin{Kind: model.PluginAgent, Name: "a", EndpointURL: agSrv.URL, Token: "x", Enabled: true, Config: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatalf("create agent plugin: %v", err)
	}
	if err := st.SetDefaultPlugin(ctx, tp.ID); err != nil {
		t.Fatalf("set default transcriber: %v", err)
	}
	if err := st.SetDefaultPlugin(ctx, ap.ID); err != nil {
		t.Fatalf("set default agent: %v", err)
	}

	cfg := config.Config{AudioRetention: "keep"}
	srv := api.NewServer(api.Deps{Store: st, Storage: prov, Crypto: cr, Config: cfg})
	proc := worker.NewProcessor(st, cr, prov, cfg, nil)

	rec := doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": emailPrefix + "@example.com", "password": "password123"}, nil)
	var setup struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &setup)
	if setup.ID == "" {
		t.Fatal("no user id from setup")
	}
	rec = doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": emailPrefix + "@example.com", "password": "password123"}, nil)
	var login struct{ Token string }
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	hdr := map[string]string{"Authorization": "Bearer " + login.Token}

	rec = doJSON(t, srv, http.MethodPost, "/api/notes", map[string]string{"title": "Partial retry"}, hdr)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create note: status %d body %s", rec.Code, rec.Body)
	}
	var note struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &note)
	if note.ID == "" {
		t.Fatal("no note id")
	}

	audioKey := "notes/" + note.ID + "/audio/a.webm"
	full := filepath.Join(root, filepath.FromSlash(audioKey))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir audio dir: %v", err)
	}
	if err := os.WriteFile(full, []byte("audio-bytes"), 0o644); err != nil {
		t.Fatalf("write audio object: %v", err)
	}
	if err := st.SetNoteAudio(ctx, setup.ID, note.ID, audioKey); err != nil {
		t.Fatalf("set note audio: %v", err)
	}

	if _, err := st.EnqueueJob(ctx, note.ID, model.JobTranscribe,
		json.RawMessage(`{"audio_key":"`+audioKey+`","expected_generation":0}`)); err != nil {
		t.Fatalf("enqueue transcribe job: %v", err)
	}

	return &partialRetryFixture{srv: srv, proc: proc, st: st, hdr: hdr, noteID: note.ID, audioKey: audioKey, calls: calls}
}

// runOnce claims and processes exactly the given job, failing the test if a
// different job (or none) is claimed.
func (f *partialRetryFixture) runOnce(t *testing.T, wantJobID string) {
	t.Helper()
	ctx := context.Background()
	job, ok, err := f.st.ClaimJob(ctx, 30*time.Second)
	if err != nil || !ok {
		t.Fatalf("claim job: ok=%v err=%v", ok, err)
	}
	if job.ID != wantJobID {
		t.Fatalf("claimed job %s, want %s", job.ID, wantJobID)
	}
	f.proc.Process(ctx, job)
}

// runFirstPartialAttempt claims and processes the note's initial transcribe
// job, which the flaky stub answers with a partial result, and asserts the
// terminal-failure state the finding starts from: the transcript is saved
// (Published checkpoint set), the note is failed, and partial_transcript is
// set.
func (f *partialRetryFixture) runFirstPartialAttempt(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	job, ok, err := f.st.ClaimJob(ctx, 30*time.Second)
	if err != nil || !ok {
		t.Fatalf("claim first job: ok=%v err=%v", ok, err)
	}
	f.proc.Process(ctx, job)

	if got := f.calls.Load(); got != 1 {
		t.Fatalf("transcriber calls after first attempt = %d, want 1", got)
	}
	note, err := f.st.GetNoteByID(ctx, f.noteID)
	if err != nil {
		t.Fatalf("GetNoteByID after first attempt: %v", err)
	}
	if note.Status != model.NoteFailed {
		t.Fatalf("note status after partial attempt = %q, want %q", note.Status, model.NoteFailed)
	}
	if !note.PartialTranscript {
		t.Fatal("PartialTranscript should be true after a partial result")
	}
	tr, err := f.st.GetTranscript(ctx, f.noteID)
	if err != nil {
		t.Fatalf("GetTranscript after first attempt: %v", err)
	}
	if len(tr.Segments) != 1 || tr.Segments[0].Text != "cut off mid-sen" {
		t.Fatalf("transcript after first attempt = %+v, want the partial segment", tr.Segments)
	}
}

// assertRetranscribed asserts the retried job called the transcriber a SECOND
// time and landed the FULL response's segments — proving the retry actually
// re-ran transcription rather than resuming downstream with the incomplete
// transcript the first attempt published.
func (f *partialRetryFixture) assertRetranscribed(t *testing.T, retriedJobID string) {
	t.Helper()
	ctx := context.Background()
	f.runOnce(t, retriedJobID)

	if got := f.calls.Load(); got != 2 {
		t.Fatalf("transcriber calls after retry = %d, want 2 — the retry resumed instead of re-transcribing", got)
	}
	tr, err := f.st.GetTranscript(ctx, f.noteID)
	if err != nil {
		t.Fatalf("GetTranscript after retry: %v", err)
	}
	if len(tr.Segments) != 2 || tr.Segments[0].Text != "Welcome everyone." {
		t.Fatalf("transcript after retry = %+v, want the full response's segments — the retry kept the partial transcript", tr.Segments)
	}
	note, err := f.st.GetNoteByID(ctx, f.noteID)
	if err != nil {
		t.Fatalf("GetNoteByID after retry: %v", err)
	}
	if note.PartialTranscript {
		t.Error("PartialTranscript should be false once the retry lands a full transcript")
	}
	job, err := f.st.GetJob(ctx, retriedJobID)
	if err != nil {
		t.Fatalf("GetJob after retry: %v", err)
	}
	if job.Status != model.JobDone {
		t.Fatalf("retried job status = %q, want %q", job.Status, model.JobDone)
	}
}

// TestRetryNoteAfterPartialFailureRetranscribes is the regression test for the
// PR #729 review finding: runTranscribe checkpoints Published=true the moment
// it saves a transcript, BEFORE it notices the plugin's response was partial
// and fails the job terminally. handleRetryNote must not forward that
// checkpoint onto the new job — doing so has the retry take the pl.Published
// branch, skip the transcriber, and continue with the same incomplete
// transcript, which is exactly the bug this test would have caught.
func TestRetryNoteAfterPartialFailureRetranscribes(t *testing.T) {
	t.Parallel()
	f := newPartialRetryFixture(t, "partialretrynote")
	f.runFirstPartialAttempt(t)

	rec := doJSON(t, f.srv, http.MethodPost, "/api/notes/"+f.noteID+"/retry", nil, f.hdr)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("retry: status %d body %s", rec.Code, rec.Body)
	}
	var retriedJobID string
	if err := f.st.Pool().QueryRow(context.Background(),
		`SELECT id FROM jobs WHERE note_id=$1 AND status=$2 ORDER BY created_at DESC LIMIT 1`,
		f.noteID, model.JobPending).Scan(&retriedJobID); err != nil {
		t.Fatalf("find retried job: %v", err)
	}

	f.assertRetranscribed(t, retriedJobID)
}

// TestRetryJobAfterPartialFailureRetranscribes is the handleRetryJob
// (admin job monitor) counterpart to TestRetryNoteAfterPartialFailureRetranscribes
// — same finding, same fixture, driven through the admin retry endpoint
// instead of the note-owner one.
func TestRetryJobAfterPartialFailureRetranscribes(t *testing.T) {
	t.Parallel()
	f := newPartialRetryFixture(t, "partialretryjob")
	f.runFirstPartialAttempt(t)

	failedJob, err := f.st.GetLatestFailedJobByNoteID(context.Background(), f.noteID)
	if err != nil {
		t.Fatalf("GetLatestFailedJobByNoteID: %v", err)
	}

	rec := doJSON(t, f.srv, http.MethodPost, "/api/admin/jobs/"+failedJob.ID+"/retry", nil, f.hdr)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("retry: status %d body %s", rec.Code, rec.Body)
	}
	var retriedJobID string
	if err := f.st.Pool().QueryRow(context.Background(),
		`SELECT id FROM jobs WHERE note_id=$1 AND status=$2 AND id != $3 ORDER BY created_at DESC LIMIT 1`,
		f.noteID, model.JobPending, failedJob.ID).Scan(&retriedJobID); err != nil {
		t.Fatalf("find retried job: %v", err)
	}

	f.assertRetranscribed(t, retriedJobID)
}
