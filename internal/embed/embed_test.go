package embed

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/abedegno/muesli/internal/config"
)

func almostEqual(a, b float32) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < 1e-6
}

func TestEmbedHappyPath(t *testing.T) {
	var gotModel, gotPrompt string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embeddings" {
			t.Errorf("path = %q, want /api/embeddings", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Model  string `json:"model"`
			Prompt string `json:"prompt"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		gotModel, gotPrompt = req.Model, req.Prompt
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"embedding":[0.1,0.2,0.3]}`))
	}))
	defer srv.Close()

	// Set dim to 3 to match the mock response.
	e := New(config.Config{EmbeddingsURL: srv.URL, EmbeddingsModel: "nomic-embed-text", EmbeddingsDim: 3})
	if e == nil {
		t.Fatal("New returned nil for a configured embedder")
	}
	vec, err := e.Embed(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	want := []float32{0.1, 0.2, 0.3}
	if len(vec) != len(want) {
		t.Fatalf("len(vec) = %d, want %d", len(vec), len(want))
	}
	for i := range want {
		if !almostEqual(vec[i], want[i]) {
			t.Fatalf("vec[%d] = %v, want %v", i, vec[i], want[i])
		}
	}
	if gotModel != "nomic-embed-text" {
		t.Fatalf("request model = %q, want nomic-embed-text", gotModel)
	}
	if gotPrompt != "hello world" {
		t.Fatalf("request prompt = %q, want hello world", gotPrompt)
	}
}

func TestEmbedNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	e := New(config.Config{EmbeddingsURL: srv.URL, EmbeddingsModel: "m", EmbeddingsDim: 768})
	if _, err := e.Embed(context.Background(), "x"); err == nil {
		t.Fatal("expected error on non-200 response")
	}
}

func TestEmbedEmptyEmbedding(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"embedding":[]}`))
	}))
	defer srv.Close()

	e := New(config.Config{EmbeddingsURL: srv.URL, EmbeddingsModel: "m", EmbeddingsDim: 768})
	if _, err := e.Embed(context.Background(), "x"); err == nil {
		t.Fatal("expected error on empty embedding")
	}
}

func TestEmbedRejectsNonFinite(t *testing.T) {
	// An over-range JSON number decodes to ±Inf in Go; the finite-guard must reject
	// it (rather than letting the bad value reach pgvector, which forbids NaN/Inf).
	for _, body := range []string{`{"embedding":[0.1,1e400,0.3]}`, `{"embedding":[-1e400]}`} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		}))
		e := New(config.Config{EmbeddingsURL: srv.URL, EmbeddingsModel: "m", EmbeddingsDim: 3})
		if _, err := e.Embed(context.Background(), "x"); err == nil {
			t.Fatalf("expected error for non-finite embedding %q", body)
		}
		srv.Close()
	}
}

func TestNewDisabledWhenURLEmpty(t *testing.T) {
	if e := New(config.Config{}); e != nil {
		t.Fatalf("New(empty URL) = %v, want nil", e)
	}
}

func TestNewEnabledHasDim768(t *testing.T) {
	e := New(config.Config{EmbeddingsURL: "http://x", EmbeddingsModel: "m"})
	if e == nil {
		t.Fatal("New(configured) = nil, want non-nil")
	}
	if e.Dim() != 768 {
		t.Fatalf("Dim() = %d, want 768", e.Dim())
	}
}

func TestNewCustomDim(t *testing.T) {
	e := New(config.Config{EmbeddingsURL: "http://x", EmbeddingsModel: "m", EmbeddingsDim: 1536})
	if e == nil {
		t.Fatal("New(configured) = nil, want non-nil")
	}
	if e.Dim() != 1536 {
		t.Fatalf("Dim() = %d, want 1536", e.Dim())
	}
}

func TestEmbedDimensionMismatch(t *testing.T) {
	// Mock returns a 3-dim vector, but embedder is configured for 768.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"embedding":[0.1,0.2,0.3]}`))
	}))
	defer srv.Close()

	e := New(config.Config{EmbeddingsURL: srv.URL, EmbeddingsModel: "test-model", EmbeddingsDim: 768})
	if e == nil {
		t.Fatal("New returned nil for a configured embedder")
	}
	_, err := e.Embed(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error on dimension mismatch, got nil")
	}
	// Check that the error message is clear and mentions the config var.
	if !contains(err.Error(), "test-model") || !contains(err.Error(), "dim 3") || !contains(err.Error(), "want 768") || !contains(err.Error(), "MUESLI_EMBEDDINGS_DIM") {
		t.Fatalf("error message missing expected details: %v", err)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || indexString(s, substr) >= 0)
}

func indexString(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
