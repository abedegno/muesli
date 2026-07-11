package api_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/abedegno/muesli/internal/api"
	"github.com/abedegno/muesli/internal/config"
	"github.com/abedegno/muesli/internal/crypto"
	"github.com/abedegno/muesli/internal/embed"
	"github.com/abedegno/muesli/internal/storage"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
)

func TestAdminEmbeddingsStatusEnabled(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	cr, _ := crypto.New("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	prov, _ := storage.NewLocal(t.TempDir(), "http://example.test", "http://example.test", []byte("test-signing-key-0123456789"))

	cfg := config.Config{
		EmbeddingsURL:         "http://ollama:11434",
		EmbeddingsModel:       "nomic-embed-text",
		EmbeddingsDim:         768,
		EmbeddingsMinScore:    0.6,
		EmbeddingsDocPrefix:   "search_document: ",
		EmbeddingsQueryPrefix: "search_query: ",
	}
	embedder := embed.New(cfg)
	srv := api.NewServer(api.Deps{Store: st, Storage: prov, Crypto: cr, Config: cfg, Embedder: embedder})

	// Setup user and get auth token
	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "o@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "o@example.com", "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	hdr := map[string]string{"Authorization": "Bearer " + login.Token}

	rec = doJSON(t, srv, http.MethodGet, "/api/admin/embeddings", nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body)
	}

	var status api.EmbeddingsStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !status.Enabled {
		t.Errorf("enabled = false, want true")
	}
	if status.Model != "nomic-embed-text" {
		t.Errorf("model = %q, want nomic-embed-text", status.Model)
	}
	if status.Dim != 768 {
		t.Errorf("dim = %d, want 768", status.Dim)
	}
	if status.MinScore != 0.6 {
		t.Errorf("minScore = %v, want 0.6", status.MinScore)
	}
	if status.DocPrefix != "search_document: " {
		t.Errorf("docPrefix = %q, want 'search_document: '", status.DocPrefix)
	}
	if status.QueryPrefix != "search_query: " {
		t.Errorf("queryPrefix = %q, want 'search_query: '", status.QueryPrefix)
	}
}

func TestAdminEmbeddingsStatusDisabled(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	cr, _ := crypto.New("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	prov, _ := storage.NewLocal(t.TempDir(), "http://example.test", "http://example.test", []byte("test-signing-key-0123456789"))

	// Leave embeddings disabled (EmbeddingsURL empty), but set model/dim in config.
	cfg := config.Config{
		EmbeddingsURL:   "",
		EmbeddingsModel: "some-model",
		EmbeddingsDim:   1536,
	}
	srv := api.NewServer(api.Deps{Store: st, Storage: prov, Crypto: cr, Config: cfg, Embedder: nil})

	// Setup user and get auth token
	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "o@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "o@example.com", "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	hdr := map[string]string{"Authorization": "Bearer " + login.Token}

	rec = doJSON(t, srv, http.MethodGet, "/api/admin/embeddings", nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body)
	}

	var status api.EmbeddingsStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	// Enabled should be false, but model/dim must still be populated from config.
	if status.Enabled {
		t.Errorf("enabled = true, want false (embeddings disabled)")
	}
	if status.Model != "some-model" {
		t.Errorf("model = %q, want some-model (from config even when disabled)", status.Model)
	}
	if status.Dim != 1536 {
		t.Errorf("dim = %d, want 1536 (from config even when disabled)", status.Dim)
	}
}
