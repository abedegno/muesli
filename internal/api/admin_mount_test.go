package api_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/abedegno/muesli/internal/api"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
)

// TestAdminGate_MemberForbidden_AdminReaches proves the whole /api/admin/*
// subtree is mounted behind RequireRole("admin"): an authenticated member
// gets 403 from a representative handler, and an authenticated admin
// reaches it and gets a normal response. This is deliberately separate from
// each admin suite's own endpoint-behaviour tests (whose callers are all
// created via /api/setup, so they are already admin) -- this test is the one
// place member-denial is asserted, per the plan's ruling that gate tests and
// handler-behaviour tests should not be conflated.
func TestAdminGate_MemberForbidden_AdminReaches(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	srv := api.NewServer(api.Deps{Store: st})

	adminHdr := setupLoginHdr(t, srv, "admin@example.com")
	_ = createSecondUser(t, st, "member@example.com", "password123")
	memberHdr := loginHdr(t, srv, "member@example.com", "password123")

	// Representative handler: GET /api/admin/plugins (no config/crypto deps).
	rec := doJSON(t, srv, http.MethodGet, "/api/admin/plugins", nil, memberHdr)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("member: status = %d, want 403; body: %s", rec.Code, rec.Body)
	}

	rec = doJSON(t, srv, http.MethodGet, "/api/admin/plugins", nil, adminHdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin: status = %d, want 200; body: %s", rec.Code, rec.Body)
	}
	var plugins []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &plugins); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

// TestAdminGate_UnauthenticatedUnauthorized proves the admin mount still
// requires session auth before role -- an unauthenticated caller gets 401,
// not 403 (which would leak that the route exists behind a role check).
func TestAdminGate_UnauthenticatedUnauthorized(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	srv := api.NewServer(api.Deps{Store: st})

	rec := doJSON(t, srv, http.MethodGet, "/api/admin/plugins", nil, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body: %s", rec.Code, rec.Body)
	}
}

// loginHdr logs an existing user in and returns their auth header.
func loginHdr(t *testing.T, srv *api.Server, email, password string) map[string]string {
	t.Helper()
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": email, "password": password}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("login %s: status %d body %s", email, rec.Code, rec.Body)
	}
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	return map[string]string{"Authorization": "Bearer " + login.Token}
}
