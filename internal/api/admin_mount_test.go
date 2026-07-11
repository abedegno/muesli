package api_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/abedegno/muesli/internal/api"
)

func TestAdminUIMounted(t *testing.T) {
	t.Parallel()
	// The admin UI is served without DB/auth deps; an empty Deps is enough.
	srv := api.NewServer(api.Deps{})

	for _, path := range []string{"/admin", "/admin/", "/admin/plugins"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s status %d, want 200", path, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), `<div id="root">`) {
			t.Fatalf("GET %s did not return admin index: %q", path, rec.Body.String())
		}
	}

	// An embedded asset is served too.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/assets/app.js", nil)
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /admin/assets/app.js status %d, want 200", rec.Code)
	}
}
