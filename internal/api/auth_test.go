package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/abedegno/muesli/internal/api"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
)

func newTestServer(t *testing.T) (*api.Server, *store.Store) {
	t.Helper()
	st := store.New(testutil.NewPool(t))
	return api.NewServer(api.Deps{Store: st}), st
}

func doJSON(t *testing.T, srv *api.Server, method, path string, body any, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func doRaw(t *testing.T, srv *api.Server, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func TestLoginUnknownEmailReturns401(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	// no setup/user created
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "nobody@example.com", "password": "whatever"}, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}
}

func TestSetupAndLogin(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)

	// First-run setup succeeds.
	rec := doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "owner@example.com", "password": "s3cret-pass"}, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("setup status %d body %s", rec.Code, rec.Body)
	}

	// Second setup is rejected (account already exists).
	rec = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "x@example.com", "password": "another-pass"}, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("second setup status %d, want 409", rec.Code)
	}

	// Login with correct password returns a session token.
	rec = doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "owner@example.com", "password": "s3cret-pass"}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("login status %d", rec.Code)
	}
	var out struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out.Token == "" {
		t.Fatal("expected session token")
	}

	// Wrong password is rejected.
	rec = doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "owner@example.com", "password": "nope"}, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad login status %d, want 401", rec.Code)
	}

	// Sanity: exactly one user exists.
	if n, _ := st.CountUsers(context.Background()); n != 1 {
		t.Fatalf("user count %d", n)
	}
}
