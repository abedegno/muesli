package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/api"
	"github.com/abedegno/muesli/internal/config"
	"github.com/abedegno/muesli/internal/crypto"
	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/plugintest"
	"github.com/abedegno/muesli/internal/storage"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
	"github.com/abedegno/muesli/internal/worker"
)

func e2eCrypto(t *testing.T) *crypto.Crypto {
	t.Helper()
	c, err := crypto.New("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// pollFullUntilReady polls /full up to ~5s for the note to reach ready.
func pollFullUntilReady(t *testing.T, srv *api.Server, noteID string, hdr map[string]string) map[string]any {
	t.Helper()
	for i := 0; i < 100; i++ {
		rec := doJSON(t, srv, http.MethodGet, "/api/notes/"+noteID+"/full", nil, hdr)
		var full struct {
			Note       map[string]any   `json:"note"`
			Transcript json.RawMessage  `json:"transcript"`
			Summaries  []map[string]any `json:"summaries"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &full)
		if full.Note["status"] == "ready" {
			return map[string]any{"transcript": full.Transcript, "summaries": full.Summaries}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("note never reached ready")
	return nil
}

func setupE2E(t *testing.T, transcriber, agent *plugintest.Stub) (*api.Server, map[string]string, string) {
	t.Helper()
	ctx := context.Background()
	pool := testutil.NewPool(t)
	st := store.New(pool)
	cr := e2eCrypto(t)
	prov, _ := storage.NewLocal(t.TempDir(), "http://example.test", "http://example.test", []byte("test-signing-key-0123456789"))
	_ = st.SeedBuiltInTemplates(ctx)

	proc := worker.NewProcessor(st, cr, prov, config.Config{AudioRetention: "keep"}, nil)
	pool2 := worker.NewPool(st, proc, 2)
	wctx, cancel := context.WithCancel(ctx)
	go pool2.Run(wctx)
	// Block teardown on the workers exiting so the pool can't still be claiming
	// jobs while the test's tempdir/DB pool is torn down. Stop cancels + waits;
	// cancel() is a belt-and-braces signal in case Stop races Run's startup.
	t.Cleanup(func() {
		cancel()
		pool2.Stop()
	})

	srv := api.NewServer(api.Deps{Store: st, Storage: prov, Crypto: cr,
		Config: config.Config{AudioRetention: "keep"}})

	// Account + token.
	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "o@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "o@example.com", "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	hdr := map[string]string{"Authorization": "Bearer " + login.Token}

	// Register stub plugins as defaults via the admin API.
	for _, p := range []map[string]any{
		{"kind": "transcriber", "name": "stub-t", "endpoint_url": transcriber.URL(), "token": "x", "enabled": true, "is_default": true},
		{"kind": "agent", "name": "stub-a", "endpoint_url": agent.URL(), "token": "x", "enabled": true, "is_default": true},
	} {
		rec = doJSON(t, srv, http.MethodPost, "/api/admin/plugins", p, hdr)
		if rec.Code != http.StatusCreated {
			t.Fatalf("register plugin %v: status %d body %s", p["kind"], rec.Code, rec.Body)
		}
	}

	// Create note, upload audio.
	rec = doJSON(t, srv, http.MethodPost, "/api/notes", map[string]string{"title": "Standup"}, hdr)
	var note struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &note)
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
	return srv, hdr, note.ID
}

func TestEndToEndHappyPath(t *testing.T) {
	t.Parallel()
	tr := plugintest.NewTranscriber()
	defer tr.Close()
	ag := plugintest.NewAgent()
	defer ag.Close()

	srv, hdr, noteID := setupE2E(t, tr, ag)
	result := pollFullUntilReady(t, srv, noteID, hdr)

	if string(result["transcript"].(json.RawMessage)) == "null" {
		t.Fatal("expected a transcript")
	}
	sums := result["summaries"].([]map[string]any)
	if len(sums) != 2 {
		t.Fatalf("summaries = %d, want 2", len(sums))
	}
	for _, s := range sums {
		if s["status"] != "ready" {
			t.Fatalf("summary not ready: %+v", s)
		}
	}
}

func TestEndToEndTranscriberRetriesThenSucceeds(t *testing.T) {
	t.Parallel()
	tr := plugintest.NewTranscriber()
	defer tr.Close()
	ag := plugintest.NewAgent()
	defer ag.Close()
	tr.FailNext(1) // first transcribe attempt 500s; the leased job retries and succeeds

	srv, hdr, noteID := setupE2E(t, tr, ag)
	result := pollFullUntilReady(t, srv, noteID, hdr)
	if string(result["transcript"].(json.RawMessage)) == "null" {
		t.Fatal("expected a transcript after retry")
	}
}

func TestEndToEndTranscriberTerminalFailure(t *testing.T) {
	t.Parallel()
	tr := plugintest.NewTranscriber()
	defer tr.Close()
	ag := plugintest.NewAgent()
	defer ag.Close()
	tr.FailNext(10) // exceed MaxJobAttempts → transcribe job terminally fails

	srv, hdr, noteID := setupE2E(t, tr, ag)

	// The note never becomes ready; the transcribe job ends 'failed' and surfaces
	// in the admin monitor, and the note itself is set to 'failed' (terminal
	// transcribe failure → note failed, never ready, never stuck in transcribing).
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		rec := doJSON(t, srv, http.MethodGet, "/api/admin/jobs?status=failed", nil, hdr)
		var jobs []map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &jobs)
		if len(jobs) == 1 && jobs[0]["type"] == "transcribe" && jobs[0]["note_id"] == noteID {
			full := doJSON(t, srv, http.MethodGet, "/api/notes/"+noteID+"/full", nil, hdr)
			var v struct {
				Note map[string]any `json:"note"`
			}
			_ = json.Unmarshal(full.Body.Bytes(), &v)
			if v.Note["status"] == "ready" {
				t.Fatal("note should NOT be ready when transcription fails terminally")
			}
			// The job row settles a moment before Process sets the note status, so
			// allow the note to converge to 'failed' (never 'ready'/'transcribing').
			if v.Note["status"] == model.NoteFailed {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("note never reached terminal failed state after transcribe failure")
}

func TestE2E_Resummarize(t *testing.T) {
	tr := plugintest.NewTranscriber()
	defer tr.Close()
	ag := plugintest.NewAgent()
	defer ag.Close()

	srv, hdr, noteID := setupE2E(t, tr, ag)

	// 1. Wait for the initial pipeline to complete.
	pollFullUntilReady(t, srv, noteID, hdr)

	// 2. Call POST /api/notes/{id}/resummarize — handler returns 202 Accepted.
	rec := doJSON(t, srv, http.MethodPost, "/api/notes/"+noteID+"/resummarize", nil, hdr)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("resummarize status %d, want 202; body %s", rec.Code, rec.Body)
	}

	// 3. Poll /full until the note is ready again (deadline ~5 s via 100 × 50 ms).
	result := pollFullUntilReady(t, srv, noteID, hdr)

	// 4b. Note status must be ready after resummarization.
	// (pollFullUntilReady already guarantees this, but assert explicitly.)
	sums, ok := result["summaries"].([]map[string]any)
	if !ok {
		t.Fatalf("summaries not a []map[string]any: %T", result["summaries"])
	}

	// 4c. At least one summary section must have non-empty content_markdown.
	found := false
	for _, s := range sums {
		sections, _ := s["sections"].([]any)
		for _, sec := range sections {
			m, _ := sec.(map[string]any)
			if cm, _ := m["content_markdown"].(string); cm != "" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("expected at least one summary section with non-empty content_markdown; summaries=%v", sums)
	}
}
