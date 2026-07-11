// Package ratelimit provides per-key token-bucket rate-limiting middlewares
// for chi-based HTTP servers.
package ratelimit

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// entry holds a rate.Limiter together with the last time it was accessed, so
// the background sweeper can evict stale entries.
type entry struct {
	limiter  *rate.Limiter
	lastUsed time.Time
}

// limiterStore is a concurrent map of key → limiter entry.
type limiterStore struct {
	mu      sync.Mutex
	entries map[string]*entry
}

func newLimiterStore() *limiterStore {
	return &limiterStore{entries: make(map[string]*entry)}
}

// get returns the existing limiter for key, or creates one with the given rps
// and burst. It also updates the entry's lastUsed timestamp.
func (s *limiterStore) get(key string, rps float64, burst int) *rate.Limiter {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[key]
	if !ok {
		e = &entry{limiter: rate.NewLimiter(rate.Limit(rps), burst)}
		s.entries[key] = e
	}
	e.lastUsed = time.Now()
	return e.limiter
}

// sweep removes entries whose lastUsed is older than ttl.
func (s *limiterStore) sweep(ttl time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := time.Now().Add(-ttl)
	for k, e := range s.entries {
		if e.lastUsed.Before(cutoff) {
			delete(s.entries, k)
		}
	}
}

// startSweeper launches a background goroutine that calls sweep every interval,
// evicting entries idle for longer than ttl. The goroutine runs for the lifetime
// of the process (same lifetime as the HTTP server).
func (s *limiterStore) startSweeper(interval, ttl time.Duration) {
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for range t.C {
			s.sweep(ttl)
		}
	}()
}

// clientIP extracts the client IP from the TCP RemoteAddr, stripping the port.
//
// X-Forwarded-For is intentionally NOT consulted: it is a client-controlled
// header and any attacker can set it to an arbitrary value, which would let
// them obtain a fresh rate-limit bucket on every request and trivially bypass
// the protection. RemoteAddr reflects the actual TCP connection address and
// cannot be spoofed by the remote client.
func clientIP(r *http.Request) string {
	// RemoteAddr is "host:port" or "[::1]:port"; strip the port.
	addr := r.RemoteAddr
	if i := strings.LastIndexByte(addr, ':'); i != -1 {
		addr = addr[:i]
	}
	return addr
}

// denyRateLimit writes a 429 JSON response.
func denyRateLimit(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusTooManyRequests)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": "rate limit exceeded"})
}

// NewIPLimiter returns a chi-compatible middleware that enforces a per-client-IP
// token-bucket rate limit. rps is the sustained rate in requests per second;
// burst is the maximum bucket depth (initial tokens).
//
// When rps or burst is zero the middleware is disabled (all requests pass through),
// which allows test servers created without explicit rate-limit config to work
// unaffected.
//
// A background goroutine evicts buckets that have been idle for more than 5 minutes,
// checked every minute, preventing unbounded memory growth.
func NewIPLimiter(rps float64, burst int) func(http.Handler) http.Handler {
	if rps == 0 || burst == 0 {
		// Disabled — return a no-op middleware.
		return func(next http.Handler) http.Handler { return next }
	}
	s := newLimiterStore()
	s.startSweeper(time.Minute, 5*time.Minute)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := clientIP(r)
			lim := s.get(key, rps, burst)
			if !lim.Allow() {
				denyRateLimit(w)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// NewUserLimiter returns a chi-compatible middleware that enforces a per-user-ID
// token-bucket rate limit. userFromCtx is a function that extracts the
// authenticated user ID from the request context (e.g. the api package's
// userIDFromContext). When no user ID is present the request is forwarded
// unchanged (unauthenticated requests are handled at the auth layer).
//
// When rps or burst is zero the middleware is disabled.
//
// A background goroutine evicts stale buckets to prevent unbounded growth.
func NewUserLimiter(rps float64, burst int, userFromCtx func(context.Context) (string, bool)) func(http.Handler) http.Handler {
	if rps == 0 || burst == 0 {
		return func(next http.Handler) http.Handler { return next }
	}
	s := newLimiterStore()
	s.startSweeper(time.Minute, 5*time.Minute)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			uid, ok := userFromCtx(r.Context())
			if !ok || uid == "" {
				// No authenticated user — pass through; the auth middleware will
				// handle the 401.
				next.ServeHTTP(w, r)
				return
			}
			lim := s.get(uid, rps, burst)
			if !lim.Allow() {
				denyRateLimit(w)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
