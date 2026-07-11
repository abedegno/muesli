package api_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestLoginSetsCookieFlags verifies that a successful login response includes a
// muesli_session cookie with the required security flags.
func TestLoginSetsCookieFlags(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)

	// Register a user via the setup endpoint.
	rec := doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "cookietest@example.com", "password": "s3cret-pass"}, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("setup status %d body %s", rec.Code, rec.Body)
	}

	t.Run("plain HTTP omits Secure flag", func(t *testing.T) {
		rec := doJSON(t, srv, http.MethodPost, "/api/login",
			map[string]string{"email": "cookietest@example.com", "password": "s3cret-pass"}, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("login status %d body %s", rec.Code, rec.Body)
		}

		cookie := findCookie(t, rec, "muesli_session")

		if cookie.Name != "muesli_session" {
			t.Errorf("cookie Name = %q, want muesli_session", cookie.Name)
		}
		if !cookie.HttpOnly {
			t.Error("cookie HttpOnly = false, want true")
		}
		if cookie.SameSite != http.SameSiteLaxMode {
			t.Errorf("cookie SameSite = %v, want Lax", cookie.SameSite)
		}
		if cookie.Path != "/" {
			t.Errorf("cookie Path = %q, want /", cookie.Path)
		}
		if cookie.MaxAge <= 0 {
			t.Errorf("cookie MaxAge = %d, want > 0", cookie.MaxAge)
		}
		if cookie.Secure {
			t.Error("cookie Secure = true on plain HTTP, want false")
		}
	})

	t.Run("X-Forwarded-Proto https sets Secure flag", func(t *testing.T) {
		rec := doJSON(t, srv, http.MethodPost, "/api/login",
			map[string]string{"email": "cookietest@example.com", "password": "s3cret-pass"},
			map[string]string{"X-Forwarded-Proto": "https"})
		if rec.Code != http.StatusOK {
			t.Fatalf("login status %d body %s", rec.Code, rec.Body)
		}

		cookie := findCookie(t, rec, "muesli_session")
		if !cookie.Secure {
			t.Error("cookie Secure = false behind https proxy, want true")
		}
	})
}

// findCookie parses the Set-Cookie headers from rec and returns the cookie with
// the given name, or fails the test if none is found.
func findCookie(t *testing.T, rec *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	// Build a minimal fake response so we can use http.Response.Cookies().
	header := http.Header{"Set-Cookie": rec.Result().Header["Set-Cookie"]}
	resp := &http.Response{Header: header}
	for _, c := range resp.Cookies() {
		if c.Name == name {
			return c
		}
	}
	// Print the raw Set-Cookie headers for debugging.
	t.Logf("Set-Cookie headers: %s", strings.Join(rec.Result().Header["Set-Cookie"], " | "))
	t.Fatalf("cookie %q not found in response", name)
	return nil
}
