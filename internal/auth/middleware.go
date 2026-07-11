package auth

import (
	"context"
	"net/http"
	"strings"
)

// UserResolver looks up a user ID from a token hash.
type UserResolver interface {
	UserIDByTokenHash(ctx context.Context, tokenHash string) (string, error)
}

// CtxSetter injects the resolved user ID into the request context.
type CtxSetter func(ctx context.Context, uid string) context.Context

// Middleware authenticates requests via `Authorization: Bearer <token>` or a
// `muesli_session` cookie. On success it sets the user ID in context; on
// failure it responds 401.
func Middleware(resolver UserResolver, set CtxSetter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := bearerToken(r)
			if raw == "" {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
			uid, err := resolver.UserIDByTokenHash(r.Context(), HashToken(raw))
			if err != nil || uid == "" {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
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
