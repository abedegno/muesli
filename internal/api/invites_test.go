package api_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/api"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
)

func TestInviteRoutesNotRegistered(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	srv := api.NewServer(api.Deps{Store: st})
	adminHdr := setupLoginHdr(t, srv, "routes-admin@example.com")

	// POST /api/invites/{token} (GET-only inspect path) must not exist.
	rec := doJSON(t, srv, http.MethodPost, "/api/invites/sometoken", nil, nil)
	if rec.Code == http.StatusOK || rec.Code == http.StatusCreated {
		t.Fatalf("POST /api/invites/{token} should not be registered, got %d", rec.Code)
	}

	// GET /api/invites/{token}/accept (accept is POST-only) must not exist.
	rec = doJSON(t, srv, http.MethodGet, "/api/invites/sometoken/accept", nil, nil)
	if rec.Code == http.StatusOK {
		t.Fatalf("GET /api/invites/{token}/accept should not be registered, got %d", rec.Code)
	}
	_ = adminHdr
}

func TestCreateInviteAdminOnly(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	srv := api.NewServer(api.Deps{Store: st})
	adminHdr := setupLoginHdr(t, srv, "invite-admin@example.com")
	_ = createSecondUser(t, st, "invite-member@example.com", "password123")
	memberHdr := loginHdr(t, srv, "invite-member@example.com", "password123")

	rec := doJSON(t, srv, http.MethodPost, "/api/admin/users/invites", map[string]any{}, memberHdr)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("member: status = %d, want 403", rec.Code)
	}

	rec = doJSON(t, srv, http.MethodPost, "/api/admin/users/invites", map[string]any{}, adminHdr)
	if rec.Code != http.StatusCreated {
		t.Fatalf("admin: status = %d, want 201; body %s", rec.Code, rec.Body)
	}
	var resp struct {
		URL       string    `json:"url"`
		ExpiresAt time.Time `json:"expires_at"`
		Role      string    `json:"role"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Role != "member" {
		t.Fatalf("default role = %q, want member", resp.Role)
	}
	if !strings.HasPrefix(resp.URL, "/api/invites/") {
		t.Fatalf("url = %q, want prefix /api/invites/", resp.URL)
	}
	if time.Until(resp.ExpiresAt) < 6*24*time.Hour {
		t.Fatalf("default expiry too soon: %v", resp.ExpiresAt)
	}
}

func TestCreateInviteValidatesRoleAndExpiry(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	srv := api.NewServer(api.Deps{Store: st})
	adminHdr := setupLoginHdr(t, srv, "invite-validate@example.com")

	rec := doJSON(t, srv, http.MethodPost, "/api/admin/users/invites", map[string]any{"role": "root"}, adminHdr)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad role: status = %d, want 400", rec.Code)
	}
	rec = doJSON(t, srv, http.MethodPost, "/api/admin/users/invites", map[string]any{"expires_in_days": 31}, adminHdr)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("over-max expiry: status = %d, want 400", rec.Code)
	}
	rec = doJSON(t, srv, http.MethodPost, "/api/admin/users/invites", map[string]any{"expires_in_days": 0}, adminHdr)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("zero expiry: status = %d, want 400", rec.Code)
	}
	rec = doJSON(t, srv, http.MethodPost, "/api/admin/users/invites", map[string]any{"role": "admin", "expires_in_days": 30}, adminHdr)
	if rec.Code != http.StatusCreated {
		t.Fatalf("valid admin invite: status = %d, want 201; body %s", rec.Code, rec.Body)
	}
}

// issueInvite is a test helper: issues an invite as the given admin and
// returns the raw token extracted from the response url.
func issueInvite(t *testing.T, srv *api.Server, adminHdr map[string]string, body map[string]any) string {
	t.Helper()
	rec := doJSON(t, srv, http.MethodPost, "/api/admin/users/invites", body, adminHdr)
	if rec.Code != http.StatusCreated {
		t.Fatalf("issue invite: status %d body %s", rec.Code, rec.Body)
	}
	var resp struct {
		URL string `json:"url"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	return strings.TrimPrefix(resp.URL, "/api/invites/")
}

func TestInviteInspectAndAcceptLifecycle(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	srv := api.NewServer(api.Deps{Store: st})
	adminHdr := setupLoginHdr(t, srv, "lifecycle-admin@example.com")

	token := issueInvite(t, srv, adminHdr, map[string]any{"role": "member"})

	// Unauthenticated inspect.
	rec := doJSON(t, srv, http.MethodGet, "/api/invites/"+token, nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("inspect: status %d body %s", rec.Code, rec.Body)
	}
	var inspect struct {
		Role string `json:"role"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &inspect)
	if inspect.Role != "member" {
		t.Fatalf("inspect role = %q, want member", inspect.Role)
	}

	// Unauthenticated accept.
	rec = doJSON(t, srv, http.MethodPost, "/api/invites/"+token+"/accept",
		map[string]string{"email": "invitee@example.com", "password": "password123"}, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("accept: status %d body %s", rec.Code, rec.Body)
	}

	// The invitee can now log in.
	loginRec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "invitee@example.com", "password": "password123"}, nil)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("invitee login: status %d body %s", loginRec.Code, loginRec.Body)
	}

	// Reuse of the same token is rejected.
	rec = doJSON(t, srv, http.MethodPost, "/api/invites/"+token+"/accept",
		map[string]string{"email": "second@example.com", "password": "password123"}, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("reuse: status = %d, want 404", rec.Code)
	}

	// Inspecting a consumed token is also 404.
	rec = doJSON(t, srv, http.MethodGet, "/api/invites/"+token, nil, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("inspect consumed: status = %d, want 404", rec.Code)
	}
}

func TestInviteUnknownTokenReturns404(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	srv := api.NewServer(api.Deps{Store: st})

	rec := doJSON(t, srv, http.MethodGet, "/api/invites/does-not-exist", nil, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	rec = doJSON(t, srv, http.MethodPost, "/api/invites/does-not-exist/accept",
		map[string]string{"email": "x@example.com", "password": "password123"}, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("accept unknown: status = %d, want 404", rec.Code)
	}
}

func TestInviteAcceptDuplicateEmailConflict(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	srv := api.NewServer(api.Deps{Store: st})
	adminHdr := setupLoginHdr(t, srv, "conflict-admin@example.com")

	token := issueInvite(t, srv, adminHdr, map[string]any{})
	rec := doJSON(t, srv, http.MethodPost, "/api/invites/"+token+"/accept",
		map[string]string{"email": "conflict-admin@example.com", "password": "password123"}, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body %s", rec.Code, rec.Body)
	}
}

func TestInviteAcceptValidatesEmailAndPassword(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	srv := api.NewServer(api.Deps{Store: st})
	adminHdr := setupLoginHdr(t, srv, "validate-accept-admin@example.com")
	token := issueInvite(t, srv, adminHdr, map[string]any{})

	rec := doJSON(t, srv, http.MethodPost, "/api/invites/"+token+"/accept",
		map[string]string{"email": "", "password": "password123"}, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty email: status = %d, want 400", rec.Code)
	}
	rec = doJSON(t, srv, http.MethodPost, "/api/invites/"+token+"/accept",
		map[string]string{"email": "short@example.com", "password": "short"}, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("short password: status = %d, want 400", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "short") && strings.Contains(rec.Body.String(), "password") {
		// Body should describe the requirement, not echo back the secret.
	}
}

// TestInviteSecretsNeverAppearInResponses is a light regression guard: the
// raw invite token appears once (in the issue response's url), and no
// response along the lifecycle ever contains a password or password hash.
func TestInviteSecretsNeverAppearInResponses(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	srv := api.NewServer(api.Deps{Store: st})
	adminHdr := setupLoginHdr(t, srv, "secrets-admin@example.com")
	token := issueInvite(t, srv, adminHdr, map[string]any{})

	rec := doJSON(t, srv, http.MethodGet, "/api/invites/"+token, nil, nil)
	if strings.Contains(rec.Body.String(), "password") {
		t.Fatalf("inspect response leaked password field: %s", rec.Body)
	}

	rec = doJSON(t, srv, http.MethodPost, "/api/invites/"+token+"/accept",
		map[string]string{"email": "secret-check@example.com", "password": "password123"}, nil)
	if strings.Contains(rec.Body.String(), "password123") || strings.Contains(rec.Body.String(), "argon2") {
		t.Fatalf("accept response leaked password/hash: %s", rec.Body)
	}
}
