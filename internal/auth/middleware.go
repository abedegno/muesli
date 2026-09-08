package auth

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/abedegno/muesli/internal/store"
)

// UserResolver looks up a user ID from a token hash.
type UserResolver interface {
	UserIDByTokenHash(ctx context.Context, tokenHash string) (string, error)
}

// CtxSetter injects the resolved user ID into the request context.
type CtxSetter func(ctx context.Context, uid string) context.Context

// Middleware authenticates requests via `Authorization: Bearer <token>` or a
// `muesli_session` cookie. On success it sets the user ID in context; on
// invalid credentials it responds 401. Resolver failures respond 503.
func Middleware(resolver UserResolver, set CtxSetter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := bearerToken(r)
			if raw == "" {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
			uid, err := resolver.UserIDByTokenHash(r.Context(), HashToken(raw))
			if errors.Is(err, store.ErrNotFound) || (err == nil && uid == "") {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
			if err != nil {
				slog.ErrorContext(r.Context(), "resolve authenticated user", "error", err)
				http.Error(w, `{"error":"service unavailable"}`, http.StatusServiceUnavailable)
				return
			}
			next.ServeHTTP(w, r.WithContext(set(r.Context(), uid)))
		})
	}
}

func bearerToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	if c, err := r.Cookie("muesli_session"); err == nil {
		return c.Value
	}
	return ""
}

// RoleResolver loads a user's current role.
type RoleResolver interface {
	GetUserRole(ctx context.Context, userID string) (string, error)
}

// CtxGetter reads a previously-injected user ID back out of a request
// context (the inverse of CtxSetter).
type CtxGetter func(ctx context.Context) (string, bool)

// RequireRole returns middleware that responds 403 unless the authenticated
// request's user currently holds exactly `role`. It must run after
// Middleware (which resolves and injects the user ID). The role is loaded
// from the database on every request -- there is no cache, so a role change
// takes effect on the very next request, not the next login.
func RequireRole(resolver RoleResolver, get CtxGetter, role string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			uid, ok := get(r.Context())
			if !ok {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
			got, err := resolver.GetUserRole(r.Context(), uid)
			if errors.Is(err, store.ErrNotFound) {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
			if err != nil {
				slog.ErrorContext(r.Context(), "resolve user role", "error", err)
				http.Error(w, `{"error":"service unavailable"}`, http.StatusServiceUnavailable)
				return
			}
			if got != role {
				http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r.WithContext(r.Context()))
		})
	}
}
