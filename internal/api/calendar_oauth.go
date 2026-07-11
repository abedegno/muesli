package api

import (
	"encoding/json"
	"fmt"
	"html"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/abedegno/muesli/internal/auth"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const (
	googleOAuthStateTTL     = 5 * time.Minute
	googleOAuthSuccessTitle = "Google Calendar connected"
	googleOAuthErrorTitle   = "Google Calendar connection failed"
	googleOAuthScope        = "https://www.googleapis.com/auth/calendar.readonly"
	googleOAuthStateCookie  = "muesli_google_oauth_state"
)

type googleOAuthStateRecord struct {
	userID    string
	tokenHash string
	expiresAt time.Time
}

type googleOAuthStateStore struct {
	mu     sync.Mutex
	states map[string]googleOAuthStateRecord
}

func (s *googleOAuthStateStore) issue(userID, tokenHash string, ttl time.Duration) (string, error) {
	raw, _, err := auth.GenerateToken()
	if err != nil {
		return "", err
	}
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.states == nil {
		s.states = make(map[string]googleOAuthStateRecord)
	}
	for state, rec := range s.states {
		if now.After(rec.expiresAt) {
			delete(s.states, state)
		}
	}
	s.states[raw] = googleOAuthStateRecord{
		userID:    userID,
		tokenHash: tokenHash,
		expiresAt: now.Add(ttl),
	}
	return raw, nil
}

func (s *googleOAuthStateStore) consume(state string) (googleOAuthStateRecord, bool) {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.states == nil {
		return googleOAuthStateRecord{}, false
	}
	rec, ok := s.states[state]
	if !ok {
		return googleOAuthStateRecord{}, false
	}
	delete(s.states, state)
	if now.After(rec.expiresAt) {
		return googleOAuthStateRecord{}, false
	}
	return rec, true
}

func (s *Server) googleOAuthStateStore() *googleOAuthStateStore {
	return &s.googleOAuthStates
}

func (s *Server) googleOAuthConfigured() bool {
	return s.deps.Config.GoogleOAuthClientID != "" &&
		s.deps.Config.GoogleOAuthClientSecret != "" &&
		s.deps.Config.GoogleOAuthRedirectURL != ""
}

func (s *Server) googleOAuthConfig() (*oauth2.Config, bool) {
	if !s.googleOAuthConfigured() {
		return nil, false
	}
	return &oauth2.Config{
		ClientID:     s.deps.Config.GoogleOAuthClientID,
		ClientSecret: s.deps.Config.GoogleOAuthClientSecret,
		RedirectURL:  s.deps.Config.GoogleOAuthRedirectURL,
		Endpoint:     google.Endpoint,
		Scopes:       []string{googleOAuthScope},
	}, true
}

func requestToken(r *http.Request) string {
	if raw := r.URL.Query().Get("token"); raw != "" {
		return raw
	}
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	if c, err := r.Cookie("muesli_session"); err == nil {
		return c.Value
	}
	return ""
}

func writeGoogleOAuthPage(w http.ResponseWriter, code int, title, message string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	_, _ = fmt.Fprintf(w, `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>%s</title>
  <style>
    body { font-family: system-ui, sans-serif; margin: 0; padding: 3rem 1.5rem; background: #f7f7f8; color: #111827; }
    main { max-width: 32rem; margin: 0 auto; background: white; border-radius: 16px; padding: 2rem; box-shadow: 0 10px 35px rgba(15, 23, 42, 0.1); }
    h1 { margin: 0 0 1rem; font-size: 1.5rem; }
    p { line-height: 1.5; margin: 0; }
  </style>
</head>
<body>
  <main>
    <h1>%s</h1>
    <p>%s</p>
  </main>
</body>
</html>`, html.EscapeString(title), html.EscapeString(title), html.EscapeString(message))
}

func writeGoogleOAuthError(w http.ResponseWriter, code int, message string) {
	writeGoogleOAuthPage(w, code, googleOAuthErrorTitle, message)
}

func writeGoogleOAuthSuccess(w http.ResponseWriter) {
	writeGoogleOAuthPage(w, http.StatusOK, googleOAuthSuccessTitle, "You can close this tab and return to Muesli.")
}

func googleOAuthCookie(state string, maxAge int, secure bool) *http.Cookie {
	return &http.Cookie{
		Name:     googleOAuthStateCookie,
		Value:    state,
		Path:     "/api/calendar/oauth/google",
		MaxAge:   maxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
	}
}

func (s *Server) handleGoogleOAuthStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"configured": s.googleOAuthConfigured()})
}

func (s *Server) handleGoogleOAuthStart(w http.ResponseWriter, r *http.Request) {
	cfg, ok := s.googleOAuthConfig()
	if !ok {
		writeGoogleOAuthError(w, http.StatusNotFound, "Google Calendar is not configured on this server.")
		return
	}

	rawToken := requestToken(r)
	if rawToken == "" {
		writeGoogleOAuthError(w, http.StatusUnauthorized, "This Google Calendar connection request is missing authentication. Please open it from Muesli again.")
		return
	}

	uid, err := s.deps.Store.UserIDByTokenHash(r.Context(), auth.HashToken(rawToken))
	if err != nil || uid == "" {
		writeGoogleOAuthError(w, http.StatusUnauthorized, "This Google Calendar connection request is no longer valid. Please open it from Muesli again.")
		return
	}

	state, err := s.googleOAuthStateStore().issue(uid, auth.HashToken(rawToken), googleOAuthStateTTL)
	if err != nil {
		log.Printf("handleGoogleOAuthStart: issue state: %T", err)
		writeGoogleOAuthError(w, http.StatusInternalServerError, "Internal error. Please try again.")
		return
	}

	authURL := cfg.AuthCodeURL(
		state,
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("prompt", "consent"),
	)
	secure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
	http.SetCookie(w, googleOAuthCookie(state, int(googleOAuthStateTTL.Seconds()), secure))
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, authURL, http.StatusFound)
}

func (s *Server) handleGoogleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	cfg, ok := s.googleOAuthConfig()
	if !ok {
		writeGoogleOAuthError(w, http.StatusNotFound, "Google Calendar is not configured on this server.")
		return
	}

	state := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")
	if state == "" {
		writeGoogleOAuthError(w, http.StatusBadRequest, "Missing OAuth state. Please try connecting again.")
		return
	}
	if c, err := r.Cookie(googleOAuthStateCookie); err != nil || c.Value != state {
		_, _ = s.googleOAuthStateStore().consume(state)
		writeGoogleOAuthError(w, http.StatusBadRequest, "This Google Calendar connection request expired or is invalid. Please start again.")
		return
	}
	pending, ok := s.googleOAuthStateStore().consume(state)
	if !ok {
		writeGoogleOAuthError(w, http.StatusBadRequest, "This Google Calendar connection request expired or is invalid. Please start again.")
		return
	}
	if code == "" {
		writeGoogleOAuthError(w, http.StatusBadRequest, "Google did not return an authorization code. Please try again.")
		return
	}

	token, err := cfg.Exchange(r.Context(), code)
	if err != nil {
		log.Printf("handleGoogleOAuthCallback: exchange failed: %T", err)
		writeGoogleOAuthError(w, http.StatusBadGateway, "Google Calendar authorization failed. Please try again.")
		return
	}
	if token.RefreshToken == "" {
		writeGoogleOAuthError(w, http.StatusBadGateway, "Google did not return a refresh token. Please reconnect and approve calendar access again.")
		return
	}

	plaintext, err := json.Marshal(map[string]string{"refresh_token": token.RefreshToken})
	if err != nil {
		log.Printf("handleGoogleOAuthCallback: marshal creds: %T", err)
		writeGoogleOAuthError(w, http.StatusInternalServerError, "internal error")
		return
	}

	sealed, err := s.deps.Crypto.Seal(plaintext)
	if err != nil {
		log.Printf("handleGoogleOAuthCallback: seal creds: %T", err)
		writeGoogleOAuthError(w, http.StatusInternalServerError, "internal error")
		return
	}

	src, err := s.deps.Store.CreateSource(r.Context(), pending.userID, "google", "Google Calendar", sealed)
	if err != nil {
		log.Printf("handleGoogleOAuthCallback: create source: %T", err)
		writeGoogleOAuthError(w, http.StatusInternalServerError, "internal error")
		return
	}

	http.SetCookie(w, googleOAuthCookie("", -1, false))
	kickCalendarSync(s.deps.Store, s.deps.Crypto, s.deps.Config.GoogleOAuthClientID, s.deps.Config.GoogleOAuthClientSecret, s.deps.Config.MicrosoftOAuthClientID, s.deps.Config.MicrosoftOAuthClientSecret, src.ID)
	writeGoogleOAuthSuccess(w)
}
