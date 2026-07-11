package api_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestProtectedRouteRequiresToken(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)

	// Create account + login to obtain a session token.
	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "o@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "o@example.com", "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)

	// No token → 401.
	rec = doJSON(t, srv, http.MethodPost, "/api/tokens",
		map[string]string{"name": "desktop"}, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no-token status %d, want 401", rec.Code)
	}

	// With token → 201 and a new app token.
	rec = doJSON(t, srv, http.MethodPost, "/api/tokens",
		map[string]string{"name": "desktop"},
		map[string]string{"Authorization": "Bearer " + login.Token})
	if rec.Code != http.StatusCreated {
		t.Fatalf("with-token status %d body %s", rec.Code, rec.Body)
	}
}
