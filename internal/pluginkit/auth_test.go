package pluginkit

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestBearerTokenAndMiddleware(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if _, ok := bearerToken(req); ok {
		t.Fatal("expected missing auth to fail")
	}

	req.Header.Set("Authorization", "Bearer tok")
	got, ok := bearerToken(req)
	if !ok || got != "tok" {
		t.Fatalf("bearerToken = %q ok=%v", got, ok)
	}

	h := requireBearer("tok")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("authorized code = %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing token code = %d", rec.Code)
	}

	bad := httptest.NewRequest(http.MethodGet, "/", nil)
	bad.Header.Set("Authorization", "Bearer wrong")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, bad)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token code = %d", rec.Code)
	}
}

func TestRequireBearerConstantTime(t *testing.T) {
	if testing.Short() {
		t.Skip("timing-sensitive test")
	}

	const token = "0123456789abcdef0123456789abcdef"
	h := requireBearer(token)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	measure := func(guess string) time.Duration {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer "+guess)
		start := time.Now()
		for i := 0; i < 2000; i++ {
			h.ServeHTTP(httptest.NewRecorder(), req)
		}
		return time.Since(start)
	}

	wrongEarly := measure("X123456789abcdef0123456789abcdef")
	wrongLate := measure("0123456789abcdef0123456789abcdeX")

	ratio := float64(wrongLate) / float64(wrongEarly)
	if ratio > 1.5 || ratio < 0.66 {
		t.Fatalf("comparison timing correlates with match length (ratio=%.2f) - not constant-time", ratio)
	}
}
