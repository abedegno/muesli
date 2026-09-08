// Package auth — internal test file so bearerToken (unexported) is reachable.
package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/abedegno/muesli/internal/store"
)

// testResolver implements UserResolver via a function field, making it easy to
// inject arbitrary resolver behaviour from each test.
type testResolver struct {
	fn func(ctx context.Context, hash string) (string, error)
}

func (r *testResolver) UserIDByTokenHash(ctx context.Context, hash string) (string, error) {
	return r.fn(ctx, hash)
}

// testCtxKey is a distinct key type for test-only context values so we can
// verify that the uid was injected into the context by the middleware.
type testCtxKey struct{}

// makeTestSetter returns a CtxSetter that stores uid under testCtxKey, and a
// corresponding getter used by next handlers to read it back.
func makeTestSetter() (CtxSetter, func(ctx context.Context) string) {
	setter := func(ctx context.Context, uid string) context.Context {
		return context.WithValue(ctx, testCtxKey{}, uid)
	}
	getter := func(ctx context.Context) string {
		v, _ := ctx.Value(testCtxKey{}).(string)
		return v
	}
	return setter, getter
}

// ── bearerToken unit tests ──────────────────────────────────────────────────

func TestBearerToken_NoHeaders(t *testing.T) {
	req, _ := http.NewRequest("GET", "/", nil)
	if got := bearerToken(req); got != "" {
		t.Errorf("bearerToken() = %q, want empty", got)
	}
}

func TestBearerToken_BearerHeader(t *testing.T) {
	req, _ := http.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer mytoken")
	if got := bearerToken(req); got != "mytoken" {
		t.Errorf("bearerToken() = %q, want %q", got, "mytoken")
	}
}

func TestBearerToken_CookieOnly(t *testing.T) {
	req, _ := http.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "muesli_session", Value: "cookie-tok"})
	if got := bearerToken(req); got != "cookie-tok" {
		t.Errorf("bearerToken() = %q, want %q", got, "cookie-tok")
	}
}

func TestBearerToken_BearerBeatsCookie(t *testing.T) {
	req, _ := http.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer bearer-tok")
	req.AddCookie(&http.Cookie{Name: "muesli_session", Value: "cookie-tok"})
	if got := bearerToken(req); got != "bearer-tok" {
		t.Errorf("bearerToken() = %q, want bearer-tok (not cookie)", got)
	}
}

// TestBearerToken_NonBearerAuthFallsToCookie verifies that an Authorization
// header without the "Bearer " prefix is ignored, and the function falls
// through to the cookie.
func TestBearerToken_NonBearerAuthFallsToCookie(t *testing.T) {
	req, _ := http.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Basic xyz")
	req.AddCookie(&http.Cookie{Name: "muesli_session", Value: "cookie-tok"})
	if got := bearerToken(req); got != "cookie-tok" {
		t.Errorf("bearerToken() = %q, want cookie-tok (Basic auth must not win)", got)
	}
}

// ── Middleware integration tests ────────────────────────────────────────────

func TestMiddleware_NoAuth(t *testing.T) {
	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	resolver := &testResolver{fn: func(_ context.Context, _ string) (string, error) {
		return "uid", nil
	}}
	setter, _ := makeTestSetter()
	mw := Middleware(resolver, setter)(next)

	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	mw.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "unauthorized") {
		t.Errorf("body = %q, want to contain 'unauthorized'", rr.Body.String())
	}
	if nextCalled {
		t.Error("next handler should NOT be called on missing auth")
	}
}

func TestMiddleware_BearerSuccess(t *testing.T) {
	resolver := &testResolver{fn: func(_ context.Context, _ string) (string, error) {
		return "uid-1", nil
	}}
	setter, getter := makeTestSetter()

	var gotUID string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUID = getter(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	mw := Middleware(resolver, setter)(next)
	req, _ := http.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer some-token")
	rr := httptest.NewRecorder()
	mw.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
	if gotUID != "uid-1" {
		t.Errorf("uid in context = %q, want uid-1", gotUID)
	}
}

func TestMiddleware_ResolverError(t *testing.T) {
	resolver := &testResolver{fn: func(_ context.Context, _ string) (string, error) {
		return "", errors.New("connection refused")
	}}
	setter, _ := makeTestSetter()
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mw := Middleware(resolver, setter)(next)
	req, _ := http.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer some-token")
	rr := httptest.NewRecorder()
	mw.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 on resolver error", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "service unavailable") {
		t.Errorf("body = %q, want to contain 'service unavailable'", rr.Body.String())
	}
}

func TestMiddleware_TokenNotFound(t *testing.T) {
	resolver := &testResolver{fn: func(_ context.Context, _ string) (string, error) {
		return "", store.ErrNotFound
	}}
	setter, _ := makeTestSetter()
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mw := Middleware(resolver, setter)(next)
	req, _ := http.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer revoked-token")
	rr := httptest.NewRecorder()
	mw.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 when token is not found", rr.Code)
	}
}

func TestMiddleware_EmptyUID(t *testing.T) {
	resolver := &testResolver{fn: func(_ context.Context, _ string) (string, error) {
		return "", nil // resolver returned empty uid (token not found in DB)
	}}
	setter, _ := makeTestSetter()
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mw := Middleware(resolver, setter)(next)
	req, _ := http.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer unknown-token")
	rr := httptest.NewRecorder()
	mw.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 when resolver returns empty uid", rr.Code)
	}
}

// TestMiddleware_HashPassedToResolver verifies that the middleware passes
// HashToken(rawToken) to the resolver, not the raw token itself.
func TestMiddleware_HashPassedToResolver(t *testing.T) {
	rawToken := "my-test-raw-token"
	expectedHash := HashToken(rawToken)

	var receivedHash string
	resolver := &testResolver{fn: func(_ context.Context, hash string) (string, error) {
		receivedHash = hash
		return "uid-1", nil
	}}
	setter, _ := makeTestSetter()
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mw := Middleware(resolver, setter)(next)
	req, _ := http.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)
	rr := httptest.NewRecorder()
	mw.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if receivedHash != expectedHash {
		t.Errorf("resolver received hash %q, want HashToken(%q) = %q", receivedHash, rawToken, expectedHash)
	}
	if receivedHash == rawToken {
		t.Error("resolver received raw token instead of its hash — secrets would be stored unhashed")
	}
}

// ── RequireRole tests ──────────────────────────────────────────────────

// testRoleResolver implements RoleResolver via a function field.
type testRoleResolver struct {
	fn func(ctx context.Context, userID string) (string, error)
}

func (r *testRoleResolver) GetUserRole(ctx context.Context, userID string) (string, error) {
	return r.fn(ctx, userID)
}

func TestRequireRole_NoUserInContext(t *testing.T) {
	resolver := &testRoleResolver{fn: func(_ context.Context, _ string) (string, error) {
		t.Fatal("resolver should not be called when no user is in context")
		return "", nil
	}}
	get := func(ctx context.Context) (string, bool) { return "", false }
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	mw := RequireRole(resolver, get, "admin")(next)
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	mw.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rr.Code)
	}
}

func TestRequireRole_AdminAllowed(t *testing.T) {
	resolver := &testRoleResolver{fn: func(_ context.Context, uid string) (string, error) {
		if uid != "u1" {
			t.Errorf("resolver called with %q, want u1", uid)
		}
		return "admin", nil
	}}
	get := func(ctx context.Context) (string, bool) { return "u1", true }
	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	mw := RequireRole(resolver, get, "admin")(next)
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	mw.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
	if !nextCalled {
		t.Error("expected next handler to be called for a matching role")
	}
}

func TestRequireRole_MemberForbidden(t *testing.T) {
	resolver := &testRoleResolver{fn: func(_ context.Context, _ string) (string, error) {
		return "member", nil
	}}
	get := func(ctx context.Context) (string, bool) { return "u1", true }
	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	mw := RequireRole(resolver, get, "admin")(next)
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	mw.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rr.Code)
	}
	if nextCalled {
		t.Error("next handler must not be called for a mismatched role")
	}
}

func TestRequireRole_UnknownUser(t *testing.T) {
	resolver := &testRoleResolver{fn: func(_ context.Context, _ string) (string, error) {
		return "", store.ErrNotFound
	}}
	get := func(ctx context.Context) (string, bool) { return "ghost", true }
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	mw := RequireRole(resolver, get, "admin")(next)
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	mw.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rr.Code)
	}
}

func TestRequireRole_ResolverError(t *testing.T) {
	resolver := &testRoleResolver{fn: func(_ context.Context, _ string) (string, error) {
		return "", errors.New("db down")
	}}
	get := func(ctx context.Context) (string, bool) { return "u1", true }
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	mw := RequireRole(resolver, get, "admin")(next)
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	mw.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rr.Code)
	}
}

// TestRequireRole_NoCache proves the role is loaded fresh on every request:
// two requests through the SAME middleware instance for the SAME user see
// different outcomes once the backing "database" (a mutable var the fake
// resolver reads) changes between them -- there is no package-level or
// per-middleware cache to go stale.
func TestRequireRole_NoCache(t *testing.T) {
	currentRole := "member"
	resolver := &testRoleResolver{fn: func(_ context.Context, _ string) (string, error) {
		return currentRole, nil
	}}
	get := func(ctx context.Context) (string, bool) { return "u1", true }
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	mw := RequireRole(resolver, get, "admin")(next)

	req1, _ := http.NewRequest("GET", "/", nil)
	rr1 := httptest.NewRecorder()
	mw.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusForbidden {
		t.Fatalf("first request status = %d, want 403 (member)", rr1.Code)
	}

	// The user's role changes in the database between the two requests --
	// e.g. an admin just promoted them via PATCH /api/admin/users/{id}.
	currentRole = "admin"

	req2, _ := http.NewRequest("GET", "/", nil)
	rr2 := httptest.NewRecorder()
	mw.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("second request status = %d, want 200 (promoted to admin, no cache)", rr2.Code)
	}
}
