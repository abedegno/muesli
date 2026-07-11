package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/abedegno/muesli/internal/api"
	"github.com/abedegno/muesli/internal/config"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
)

// newRateLimitedTestServer creates a test server with login rate limit burst=1
// and an extremely low RPS so the bucket cannot refill between two sequential
// requests, regardless of DB latency. Skips when TEST_DATABASE_URL is unset
// (the first request must reach the DB handler to return 401).
func newRateLimitedTestServer(t *testing.T) *api.Server {
	t.Helper()
	st := store.New(testutil.NewPool(t)) // skips if TEST_DATABASE_URL is unset
	return api.NewServer(api.Deps{
		Store: st,
		Config: config.Config{
			// 0.001 rps = 1 token per 1000 s; the bucket cannot refill during
			// the test regardless of how long the first DB round-trip takes.
			RateLoginRPS:   0.001,
			RateLoginBurst: 1,
		},
	})
}

// loginRequest builds and fires a POST /api/login request directly against
// srv.Handler() using a fixed RemoteAddr so both calls share the same IP bucket.
func loginRequest(t *testing.T, srv *api.Server, creds map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	_ = json.NewEncoder(&buf).Encode(creds)
	req := httptest.NewRequest(http.MethodPost, "/api/login", &buf)
	req.Header.Set("Content-Type", "application/json")
	// httptest.NewRequest already sets RemoteAddr = "192.0.2.1:1234"; be
	// explicit to make the intent clear: both calls MUST share the same IP key.
	req.RemoteAddr = "192.0.2.1:1234"
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

// TestRateLimitLogin verifies that after the first login attempt consumes the
// single token in the bucket, an immediate second attempt from the same IP
// receives 429 rather than reaching the handler.
func TestRateLimitLogin(t *testing.T) {
	t.Parallel()
	srv := newRateLimitedTestServer(t)

	creds := map[string]string{"email": "nobody@example.com", "password": "wrong"}

	// First request: rate-limit allows it through; bad credentials → 401.
	rec1 := loginRequest(t, srv, creds)
	if rec1.Code != http.StatusUnauthorized {
		t.Fatalf("first login: got %d, want 401", rec1.Code)
	}

	// Second immediate request from the same IP: bucket exhausted → 429.
	// With rps=0.001 the bucket takes ~1000 s to refill one token, so this
	// check is deterministic regardless of DB latency in CI.
	rec2 := loginRequest(t, srv, creds)
	if rec2.Code != http.StatusTooManyRequests {
		t.Fatalf("second login: got %d, want 429", rec2.Code)
	}
}
