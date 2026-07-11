package adminui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServesIndexAtRoot(t *testing.T) {
	h := Handler()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /admin status %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `<div id="root">`) {
		t.Fatalf("GET /admin did not return index.html: %q", rec.Body.String())
	}
}

func TestServesIndexWithTrailingSlash(t *testing.T) {
	h := Handler()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `<div id="root">`) {
		t.Fatalf("GET /admin/ status %d body %q", rec.Code, rec.Body.String())
	}
}

func TestSPAFallbackServesIndexForUnknownPath(t *testing.T) {
	h := Handler()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/plugins/anything/deep", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("SPA fallback status %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `<div id="root">`) {
		t.Fatalf("SPA fallback did not return index.html: %q", rec.Body.String())
	}
}

func TestServesEmbeddedAsset(t *testing.T) {
	h := Handler()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/assets/app.js", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET asset status %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Fatalf("asset content-type %q, want javascript", ct)
	}
	if !strings.Contains(rec.Body.String(), "muesli admin") {
		t.Fatalf("asset body %q", rec.Body.String())
	}
}

func TestMissingAssetFallsBackToIndex(t *testing.T) {
	// A request that *looks* like an asset but doesn't exist still falls back to
	// index (SPA routes can contain dots); only real files under assets/ 404 is
	// avoided to keep client routing robust.
	h := Handler()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/assets/missing.js", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `<div id="root">`) {
		t.Fatalf("missing asset fallback status %d body %q", rec.Code, rec.Body.String())
	}
}
