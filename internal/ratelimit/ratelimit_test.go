package ratelimit_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/abedegno/muesli/internal/ratelimit"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

// TestIPLimiter_AllowsThenBlocks verifies that the first request succeeds but
// an immediate second request from the same IP is rejected (burst=1).
func TestIPLimiter_AllowsThenBlocks(t *testing.T) {
	t.Parallel()
	mw := ratelimit.NewIPLimiter(100, 1) // rps=100 so the bucket refills quickly, burst=1
	handler := mw(okHandler())

	// First request — bucket has 1 token, should succeed.
	req1 := httptest.NewRequest(http.MethodGet, "/", nil)
	req1.RemoteAddr = "1.2.3.4:1234"
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("first request: got %d, want 200", rec1.Code)
	}

	// Immediate second request — bucket empty, should be rejected.
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.RemoteAddr = "1.2.3.4:5678"
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusTooManyRequests {
		t.Fatalf("second request: got %d, want 429", rec2.Code)
	}
}

// TestIPLimiter_DifferentIPsIndependent verifies that two requests from
// different RemoteAddr IPs have independent buckets (so legitimate users on
// distinct IPs are not blocked by each other).
func TestIPLimiter_DifferentIPsIndependent(t *testing.T) {
	t.Parallel()
	mw := ratelimit.NewIPLimiter(100, 1)
	handler := mw(okHandler())

	// Request from IP A — uses RemoteAddr, which is the only key.
	reqA := httptest.NewRequest(http.MethodGet, "/", nil)
	reqA.RemoteAddr = "10.0.0.1:1234"
	recA := httptest.NewRecorder()
	handler.ServeHTTP(recA, reqA)
	if recA.Code != http.StatusOK {
		t.Fatalf("IP A: got %d, want 200", recA.Code)
	}

	// Request from IP B — independent bucket (different RemoteAddr), should succeed.
	reqB := httptest.NewRequest(http.MethodGet, "/", nil)
	reqB.RemoteAddr = "10.0.0.2:1234"
	recB := httptest.NewRecorder()
	handler.ServeHTTP(recB, reqB)
	if recB.Code != http.StatusOK {
		t.Fatalf("IP B: got %d, want 200", recB.Code)
	}
}

// testCtxKey is a private context key used by the user limiter test.
type testCtxKey int

const testUserKey testCtxKey = iota

// TestUserLimiter_AllowsThenBlocks verifies that the user limiter grants the
// first request and blocks the second immediate request for the same user.
func TestUserLimiter_AllowsThenBlocks(t *testing.T) {
	t.Parallel()
	getUser := func(ctx context.Context) (string, bool) {
		uid, ok := ctx.Value(testUserKey).(string)
		return uid, ok && uid != ""
	}
	mw := ratelimit.NewUserLimiter(100, 1, getUser)
	handler := mw(okHandler())

	ctx := context.WithValue(context.Background(), testUserKey, "user-abc")

	// First request.
	req1 := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("first request: got %d, want 200", rec1.Code)
	}

	// Second immediate request — same user, bucket empty.
	req2 := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusTooManyRequests {
		t.Fatalf("second request: got %d, want 429", rec2.Code)
	}
}
