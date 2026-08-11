package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/abedegno/muesli/internal/config"
)

// proberFunc is a DepProber that records calls and returns preset errors.
type proberFunc func(ctx context.Context, url string) error

func TestReadyz_AllConfiguredAndReachable(t *testing.T) {
	t.Parallel()

	prober := DepProber(func(_ context.Context, _ string) error { return nil })
	srv := NewServer(Deps{
		Config: config.Config{
			DefaultTranscriberURL: "http://transcriber:9000",
			DefaultAgentURL:       "http://agent:9001",
			EmbeddingsURL:         "http://embeddings:11434",
		},
		Prober: prober,
	})

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}

	var resp readyzResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Status != "ready" {
		t.Errorf("want status=ready, got %q", resp.Status)
	}
	for _, dep := range []string{"transcriber", "agent", "embeddings"} {
		d, ok := resp.Deps[dep]
		if !ok {
			t.Errorf("missing dep %q", dep)
			continue
		}
		if !d.Configured {
			t.Errorf("dep %q: want configured=true", dep)
		}
		if !d.Ok {
			t.Errorf("dep %q: want ok=true", dep)
		}
		if d.Error != "" {
			t.Errorf("dep %q: want no error, got %q", dep, d.Error)
		}
	}
}

func TestReadyz_DatabaseUnreachable(t *testing.T) {
	t.Parallel()

	srv := NewServer(Deps{
		DBPing: func(context.Context) error { return fmt.Errorf("database unavailable") },
	})

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d", rec.Code)
	}

	var resp readyzResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Status != "starting" {
		t.Errorf("want status=starting, got %q", resp.Status)
	}
	if database := resp.Deps["database"]; database.Ok || database.Error == "" {
		t.Errorf("database: want failed status with error, got %+v", database)
	}
}

func TestReadyz_OneDepUnreachable(t *testing.T) {
	t.Parallel()

	transcriberURL := "http://transcriber:9000"
	probeErr := fmt.Errorf("connection refused")

	prober := DepProber(func(_ context.Context, url string) error {
		if url == transcriberURL {
			return probeErr
		}
		return nil
	})
	srv := NewServer(Deps{
		Config: config.Config{
			DefaultTranscriberURL: transcriberURL,
			DefaultAgentURL:       "http://agent:9001",
			EmbeddingsURL:         "http://embeddings:11434",
		},
		Prober: prober,
	})

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d", rec.Code)
	}

	var resp readyzResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Status != "starting" {
		t.Errorf("want status=starting, got %q", resp.Status)
	}

	td, ok := resp.Deps["transcriber"]
	if !ok {
		t.Fatal("missing transcriber dep")
	}
	if !td.Configured {
		t.Error("transcriber: want configured=true")
	}
	if td.Ok {
		t.Error("transcriber: want ok=false")
	}
	if td.Error == "" {
		t.Error("transcriber: want non-empty error")
	}

	// Other configured deps should still be ok.
	for _, dep := range []string{"agent", "embeddings"} {
		d := resp.Deps[dep]
		if !d.Ok {
			t.Errorf("dep %q: want ok=true", dep)
		}
	}
}

func TestReadyz_NoDepsConfigured(t *testing.T) {
	t.Parallel()

	// Prober should never be called when no URLs are configured.
	prober := DepProber(func(_ context.Context, url string) error {
		t.Errorf("prober called unexpectedly for url %q", url)
		return nil
	})
	srv := NewServer(Deps{
		Config: config.Config{}, // no URLs set
		Prober: prober,
	})

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}

	var resp readyzResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Status != "ready" {
		t.Errorf("want status=ready, got %q", resp.Status)
	}
	for _, dep := range []string{"transcriber", "agent", "embeddings"} {
		d, ok := resp.Deps[dep]
		if !ok {
			t.Errorf("missing dep %q", dep)
			continue
		}
		if d.Configured {
			t.Errorf("dep %q: want configured=false", dep)
		}
		if !d.Ok {
			t.Errorf("dep %q: want ok=true (unconfigured)", dep)
		}
	}
}

func TestReadyz_EmbeddedStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		cfg          config.Config
		wantPresent  bool
		wantDetected bool
		wantDegraded bool
		wantReason   string
	}{
		{
			name: "embedded detected",
			cfg: config.Config{
				Embedded:               true,
				EmbeddedOllamaDetected: true,
				EmbeddingsURL:          "http://ollama:11434",
			},
			wantPresent:  true,
			wantDetected: true,
			wantDegraded: false,
		},
		{
			name: "embedded degraded",
			cfg: config.Config{
				Embedded:               true,
				EmbeddedDegraded:       true,
				EmbeddedDegradedReason: "summaries & semantic search need Ollama",
			},
			wantPresent:  true,
			wantDegraded: true,
			wantReason:   "summaries & semantic search need Ollama",
		},
		{
			name: "non-embedded",
			cfg: config.Config{
				Embedded:               false,
				EmbeddedOllamaDetected: true,
				EmbeddedDegraded:       true,
				EmbeddedDegradedReason: "ignored",
			},
			wantPresent: false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			srv := NewServer(Deps{
				Config: tc.cfg,
				Prober: DepProber(func(_ context.Context, _ string) error { return nil }),
			})

			req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
			rec := httptest.NewRecorder()
			srv.Handler().ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("want 200, got %d", rec.Code)
			}

			var resp readyzResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}

			if tc.wantPresent {
				if resp.Embedded == nil {
					t.Fatal("embedded response missing")
				}
				if resp.Embedded.OllamaDetected != tc.wantDetected {
					t.Fatalf("ollamaDetected = %v, want %v", resp.Embedded.OllamaDetected, tc.wantDetected)
				}
				if resp.Embedded.Degraded != tc.wantDegraded {
					t.Fatalf("degraded = %v, want %v", resp.Embedded.Degraded, tc.wantDegraded)
				}
				if resp.Embedded.DegradedReason != tc.wantReason {
					t.Fatalf("degradedReason = %q, want %q", resp.Embedded.DegradedReason, tc.wantReason)
				}
			} else if resp.Embedded != nil {
				t.Fatalf("embedded response present: %#v", resp.Embedded)
			}
		})
	}
}

// TestReadyz_HealthzStillWorks verifies that the existing /healthz endpoint is
// unaffected by the /readyz changes.
func TestReadyz_HealthzStillWorks(t *testing.T) {
	t.Parallel()
	srv := NewServer(Deps{})
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
}

// TestReadyz_RedirectingDepCountsAsReachable verifies that a dep returning a
// 3xx redirect is treated as reachable (first response wins; redirect not followed).
func TestReadyz_RedirectingDepCountsAsReachable(t *testing.T) {
	t.Parallel()

	// Start a real test server that always returns 302.
	redirectSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://192.0.2.1/nowhere", http.StatusFound)
	}))
	defer redirectSrv.Close()

	// Use the real defaultProber (no mock) so we exercise probeClient directly.
	srv := NewServer(Deps{
		Config: config.Config{
			DefaultTranscriberURL: redirectSrv.URL,
		},
		// Prober nil → defaultProber is used
	})

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (body: %s)", rec.Code, rec.Body.String())
	}

	var resp readyzResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Status != "ready" {
		t.Errorf("want status=ready, got %q", resp.Status)
	}
	td, ok := resp.Deps["transcriber"]
	if !ok {
		t.Fatal("missing transcriber dep")
	}
	if !td.Ok {
		t.Errorf("redirecting dep should be ok=true, got error: %s", td.Error)
	}
}
