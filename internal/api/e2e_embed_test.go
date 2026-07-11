package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/api"
	"github.com/abedegno/muesli/internal/config"
	"github.com/abedegno/muesli/internal/embed"
	"github.com/abedegno/muesli/internal/plugintest"
	"github.com/abedegno/muesli/internal/storage"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
	"github.com/abedegno/muesli/internal/worker"
)

// e2eFakeEmbedder is a deterministic, call-counting embedder for E2E tests.
// It returns an all-0.1 vector of fixed length and tracks how many times
// Embed has been called.
type e2eFakeEmbedder struct {
	calls atomic.Int32
	dim   int
}

func newE2EFakeEmbedder(dim int) *e2eFakeEmbedder { return &e2eFakeEmbedder{dim: dim} }

func (f *e2eFakeEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	f.calls.Add(1)
	v := make([]float32, f.dim)
	for i := range v {
		v[i] = 0.1
	}
	return v, nil
}

func (f *e2eFakeEmbedder) Dim() int { return f.dim }

// Verify e2eFakeEmbedder implements embed.Embedder at compile time.
var _ embed.Embedder = (*e2eFakeEmbedder)(nil)

// setupE2EWithEmbedder is a clone of setupE2E that accepts an embed.Embedder
// and wires it into both the worker processor and the API server.
// The config for both uses EmbeddingsModel: "test-model" and
// EmbeddingsMinScore: 0.0 so the cosine similarity cutoff is bypassed.
func setupE2EWithEmbedder(t *testing.T, transcriber, agent *plugintest.Stub, emb embed.Embedder) (*api.Server, map[string]string, string) {
	t.Helper()
	ctx := context.Background()
	pool := testutil.NewPool(t)
	st := store.New(pool)
	cr := e2eCrypto(t)
	prov, _ := storage.NewLocal(t.TempDir(), "http://example.test", "http://example.test", []byte("test-signing-key-0123456789"))
	_ = st.SeedBuiltInTemplates(ctx)

	cfg := config.Config{
		AudioRetention:     "keep",
		EmbeddingsModel:    "test-model",
		EmbeddingsMinScore: 0.0,
	}

	proc := worker.NewProcessor(st, cr, prov, cfg, emb)
	pool2 := worker.NewPool(st, proc, 2)
	wctx, cancel := context.WithCancel(ctx)
	go pool2.Run(wctx)
	t.Cleanup(func() {
		cancel()
		pool2.Stop()
	})

	srv := api.NewServer(api.Deps{
		Store:    st,
		Storage:  prov,
		Crypto:   cr,
		Config:   cfg,
		Embedder: emb,
	})

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

func TestE2E_SemanticSearch(t *testing.T) {
	t.Parallel()
	tr := plugintest.NewTranscriber()
	defer tr.Close()
	ag := plugintest.NewAgent()
	defer ag.Close()

	fe := newE2EFakeEmbedder(4) // small dim, deterministic
	srv, hdr, noteID := setupE2EWithEmbedder(t, tr, ag, fe)

	// Drive the pipeline to ready.
	pollFullUntilReady(t, srv, noteID, hdr)

	// (a) Embed was called at least once — poll until the embed job runs.
	// The embed job is enqueued AFTER the note reaches ready, so we need
	// to wait a little for the worker to pick it up.
	deadline := time.Now().Add(5 * time.Second)
	for {
		if fe.calls.Load() > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("embed job never ran: e2eFakeEmbedder.Embed was never called")
		}
		time.Sleep(20 * time.Millisecond)
	}

	// (b) Search returns 200.
	rec := doJSON(t, srv, http.MethodGet, "/api/search?q=standup", nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("search: expected 200, got %d body %s", rec.Code, rec.Body)
	}

	// (c) Note ID appears in the results.
	var matches []api.SearchMatch
	if err := json.Unmarshal(rec.Body.Bytes(), &matches); err != nil {
		t.Fatalf("search: decode response: %v", err)
	}
	found := false
	for _, m := range matches {
		if m.NoteID == noteID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("search: note %s not in results %v", noteID, matches)
	}
}
