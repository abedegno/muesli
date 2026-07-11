package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// nextOK is a minimal next handler that always returns 200 OK.
var nextOK = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
})

func TestCORSMiddleware_EmptyAllowedList_SimpleGET(t *testing.T) {
	t.Parallel()
	mw := corsMiddleware(nil)
	handler := mw(nextOK)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("expected no Access-Control-Allow-Origin header, got %q", got)
	}
	// Next handler should still be called for non-preflight.
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestCORSMiddleware_AllowedOrigin_SimpleGET(t *testing.T) {
	t.Parallel()
	allowed := []string{"https://app.example.com"}
	mw := corsMiddleware(allowed)
	handler := mw(nextOK)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://app.example.com")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
		t.Errorf("expected Access-Control-Allow-Origin = %q, got %q", "https://app.example.com", got)
	}
	vary := rr.Header().Get("Vary")
	if !strings.Contains(vary, "Origin") {
		t.Errorf("expected Vary to contain 'Origin', got %q", vary)
	}
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestCORSMiddleware_NonMatchingOrigin_SimpleGET(t *testing.T) {
	t.Parallel()
	allowed := []string{"https://app.example.com"}
	mw := corsMiddleware(allowed)
	handler := mw(nextOK)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://other.example.com")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("expected no Access-Control-Allow-Origin header, got %q", got)
	}
	// Next handler is still called for non-preflight even when origin is not allowed.
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestCORSMiddleware_Preflight_AllowedOrigin(t *testing.T) {
	t.Parallel()
	allowed := []string{"https://app.example.com"}
	mw := corsMiddleware(allowed)
	handler := mw(nextOK)

	req := httptest.NewRequest(http.MethodOptions, "/api/notes", nil)
	req.Header.Set("Origin", "https://app.example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d", rr.Code)
	}
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
		t.Errorf("expected Access-Control-Allow-Origin = %q, got %q", "https://app.example.com", got)
	}
	methods := rr.Header().Get("Access-Control-Allow-Methods")
	if !strings.Contains(methods, "GET") {
		t.Errorf("expected Access-Control-Allow-Methods to contain 'GET', got %q", methods)
	}
	headers := rr.Header().Get("Access-Control-Allow-Headers")
	if !strings.Contains(headers, "Authorization") {
		t.Errorf("expected Access-Control-Allow-Headers to contain 'Authorization', got %q", headers)
	}
}

func TestCORSMiddleware_Preflight_NonAllowedOrigin(t *testing.T) {
	t.Parallel()
	allowed := []string{"https://app.example.com"}
	mw := corsMiddleware(allowed)
	handler := mw(nextOK)

	req := httptest.NewRequest(http.MethodOptions, "/api/notes", nil)
	req.Header.Set("Origin", "https://attacker.example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d", rr.Code)
	}
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("expected no Access-Control-Allow-Origin header, got %q", got)
	}
}

func TestCORSMiddleware_NoOriginHeader(t *testing.T) {
	t.Parallel()
	allowed := []string{"https://app.example.com"}
	mw := corsMiddleware(allowed)

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusTeapot) // use a distinct status to confirm next was called
	})
	handler := mw(next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	// No Origin header set.
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if !called {
		t.Error("expected next handler to be called when no Origin header is present")
	}
	if rr.Code != http.StatusTeapot {
		t.Errorf("expected %d from next handler, got %d", http.StatusTeapot, rr.Code)
	}
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("expected no CORS headers, got Access-Control-Allow-Origin = %q", got)
	}
}
