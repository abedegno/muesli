package api

import "net/http"

// corsMiddleware returns an HTTP middleware that enforces an explicit
// allowed-origin policy. It is intentionally conservative:
//   - An empty allowed list denies all cross-origin requests.
//   - Only exact string matches are accepted (no wildcards).
//   - Requests without an Origin header (e.g. the Electron desktop client,
//     server-to-server calls) pass through unchanged.
func corsMiddleware(allowed []string) func(http.Handler) http.Handler {
	// Build a set for O(1) lookup.
	set := make(map[string]struct{}, len(allowed))
	for _, o := range allowed {
		set[o] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			// No Origin header — not a cross-origin request; pass through.
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}

			_, originAllowed := set[origin]

			if !originAllowed {
				// Origin is not in the allow-list.
				if r.Method == http.MethodOptions {
					// Preflight: reject with 204 and NO CORS headers so the
					// browser treats the request as blocked.
					w.WriteHeader(http.StatusNoContent)
					return
				}
				// Non-preflight: serve the request without CORS headers.
				// The browser will block the response itself.
				next.ServeHTTP(w, r)
				return
			}

			// Origin is in the allow-list — set the CORS response headers.
			header := w.Header()
			header.Set("Access-Control-Allow-Origin", origin)
			header.Add("Vary", "Origin")

			if r.Method == http.MethodOptions {
				// Preflight response.
				header.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
				header.Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
				header.Set("Access-Control-Allow-Credentials", "true")
				header.Set("Access-Control-Max-Age", "86400")
				w.WriteHeader(http.StatusNoContent)
				return
			}

			// Simple / actual request — CORS headers are already set above.
			next.ServeHTTP(w, r)
		})
	}
}
