package pluginkit

import (
	"net/http"
	"net/http/httptest"
	"testing"
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
