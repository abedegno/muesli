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
