package embedded

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/abedegno/muesli/internal/config"
)

func TestDetectOllama(t *testing.T) {
	t.Parallel()

	t.Run("up", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/version" {
				t.Fatalf("path = %q, want /api/version", r.URL.Path)
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"version":"0.1.0"}`))
		}))
		defer srv.Close()

		if got := DetectOllama(context.Background(), srv.URL); !got {
			t.Fatal("DetectOllama() = false, want true")
		}
	})

	t.Run("down", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.NewServeMux())
		url := srv.URL
		srv.Close()

		if got := DetectOllama(context.Background(), url); got {
			t.Fatal("DetectOllama() = true, want false for closed server")
		}
	})

	t.Run("slow", func(t *testing.T) {
		t.Parallel()

		entered := make(chan struct{})
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			close(entered)
			<-r.Context().Done()
		}))
		defer srv.Close()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		result := make(chan bool, 1)
		go func() {
			result <- DetectOllama(ctx, srv.URL)
		}()

		<-entered
		cancel()

		if got := <-result; got {
			t.Fatal("DetectOllama() = true, want false for timeout or cancellation")
		}

		select {
		case <-entered:
		default:
			t.Fatal("slow handler was not entered")
		}
	})
}

func TestPullEmbeddingModel(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		var gotName string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				t.Fatalf("method = %s, want POST", r.Method)
			}
			if r.URL.Path != "/api/pull" {
				t.Fatalf("path = %q, want /api/pull", r.URL.Path)
			}
			defer r.Body.Close()

			var reqBody map[string]string
			if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
				t.Fatalf("decode request body: %v", err)
			}
			gotName = reqBody["name"]

			w.Header().Set("Content-Type", "application/x-ndjson")
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"status":"pulling"}`+"\n")
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			_, _ = io.WriteString(w, `{"status":"success"}`+"\n")
		}))
		defer srv.Close()

		if err := PullEmbeddingModel(context.Background(), srv.URL, "nomic-embed-text"); err != nil {
			t.Fatalf("PullEmbeddingModel() error: %v", err)
		}
		if gotName != "nomic-embed-text" {
			t.Fatalf("model name = %q, want %q", gotName, "nomic-embed-text")
		}
	})

	t.Run("failure", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		if err := PullEmbeddingModel(context.Background(), srv.URL, "nomic-embed-text"); err == nil {
			t.Fatal("PullEmbeddingModel() error = nil, want non-nil")
		}
	})
}

func TestConfigureEmbeddedOllama(t *testing.T) {
	t.Parallel()

	t.Run("detected", func(t *testing.T) {
		t.Parallel()

		cfg := config.Config{EmbeddingsURL: "http://old", EmbeddedDegraded: true, EmbeddedDegradedReason: "old"}
		ConfigureEmbeddedOllama(&cfg, " http://127.0.0.1:11434/ ", true)

		if cfg.EmbeddingsURL != "http://127.0.0.1:11434" {
			t.Fatalf("EmbeddingsURL = %q, want bare Ollama base URL", cfg.EmbeddingsURL)
		}
		if !cfg.EmbeddedOllamaDetected {
			t.Fatal("EmbeddedOllamaDetected = false, want true")
		}
		if cfg.EmbeddedDegraded {
			t.Fatal("EmbeddedDegraded = true, want false")
		}
		if cfg.EmbeddedDegradedReason != "" {
			t.Fatalf("EmbeddedDegradedReason = %q, want empty", cfg.EmbeddedDegradedReason)
		}
	})

	t.Run("degraded", func(t *testing.T) {
		t.Parallel()

		cfg := config.Config{EmbeddingsURL: "http://old", EmbeddedOllamaDetected: true}
		ConfigureEmbeddedOllama(&cfg, "http://127.0.0.1:11434", false)

		if cfg.EmbeddingsURL != "" {
			t.Fatalf("EmbeddingsURL = %q, want empty", cfg.EmbeddingsURL)
		}
		if cfg.EmbeddedOllamaDetected {
			t.Fatal("EmbeddedOllamaDetected = true, want false")
		}
		if !cfg.EmbeddedDegraded {
			t.Fatal("EmbeddedDegraded = false, want true")
		}
		if cfg.EmbeddedDegradedReason != embeddedDegradedReason {
			t.Fatalf("EmbeddedDegradedReason = %q, want %q", cfg.EmbeddedDegradedReason, embeddedDegradedReason)
		}
	})
}
