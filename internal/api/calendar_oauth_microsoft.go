package api

import (
	"encoding/json"
	"fmt"
	"html"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/abedegno/muesli/internal/auth"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/microsoft"
)

const (
	microsoftOAuthStateTTL     = 5 * time.Minute
	microsoftOAuthSuccessTitle = "Microsoft Calendar connected"
	microsoftOAuthErrorTitle   = "Microsoft Calendar connection failed"
	microsoftOAuthScope        = "https://graph.microsoft.com/Calendars.Read"
	microsoftOAuthStateCookie  = "muesli_microsoft_oauth_state"
)

type microsoftOAuthStateRecord struct {
	userID    string
	tokenHash string
	expiresAt time.Time
}

type microsoftOAuthStateStore struct {
	mu     sync.Mutex
	states map[string]microsoftOAuthStateRecord
}

func (s *microsoftOAuthStateStore) issue(userID, tokenHash string, ttl time.Duration) (string, error) {
	raw, _, err := auth.GenerateToken()
	if err != nil {
		return "", err
	}
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.states == nil {
		s.states = make(map[string]microsoftOAuthStateRecord)
	}
	for state, rec := range s.states {
		if now.After(rec.expiresAt) {
			delete(s.states, state)
		}
	}
	s.states[raw] = microsoftOAuthStateRecord{
		userID:    userID,
		tokenHash: tokenHash,
		expiresAt: now.Add(ttl),
	}
	return raw, nil
}

func (s *microsoftOAuthStateStore) consume(state string) (microsoftOAuthStateRecord, bool) {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.states == nil {
		return microsoftOAuthStateRecord{}, false
	}
	rec, ok := s.states[state]
	if !ok {
		return microsoftOAuthStateRecord{}, false
	}
	delete(s.states, state)
	if now.After(rec.expiresAt) {
		return microsoftOAuthStateRecord{}, false
	}
	return rec, true
}

func (s *Server) microsoftOAuthStateStore() *microsoftOAuthStateStore {
	return &s.microsoftOAuthStates
}

func (s *Server) microsoftOAuthConfigured() bool {
	return s.deps.Config.MicrosoftOAuthClientID != "" &&
		s.deps.Config.MicrosoftOAuthClientSecret != "" &&
		s.deps.Config.MicrosoftOAuthRedirectURL != ""
}

func (s *Server) microsoftOAuthConfig() (*oauth2.Config, bool) {
	if !s.microsoftOAuthConfigured() {
		return nil, false
	}
	return &oauth2.Config{
		ClientID:     s.deps.Config.MicrosoftOAuthClientID,
		ClientSecret: s.deps.Config.MicrosoftOAuthClientSecret,
		RedirectURL:  s.deps.Config.MicrosoftOAuthRedirectURL,
		Endpoint:     microsoft.AzureADEndpoint("common"),
		Scopes:       []string{"offline_access", microsoftOAuthScope},
	}, true
}

func writeMicrosoftOAuthPage(w http.ResponseWriter, code int, title, message string) {
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

func writeMicrosoftOAuthError(w http.ResponseWriter, code int, message string) {
	writeMicrosoftOAuthPage(w, code, microsoftOAuthErrorTitle, message)
}

func writeMicrosoftOAuthSuccess(w http.ResponseWriter) {
	writeMicrosoftOAuthPage(w, http.StatusOK, microsoftOAuthSuccessTitle, "You can close this tab and return to Muesli.")
}

func microsoftOAuthCookie(state string, maxAge int, secure bool) *http.Cookie {
	return &http.Cookie{
		Name:     microsoftOAuthStateCookie,
		Value:    state,
		Path:     "/api/calendar/oauth/microsoft",
		MaxAge:   maxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
	}
}

func (s *Server) handleMicrosoftOAuthStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"configured": s.microsoftOAuthConfigured()})
}

func (s *Server) handleMicrosoftOAuthStart(w http.ResponseWriter, r *http.Request) {
	cfg, ok := s.microsoftOAuthConfig()
	if !ok {
		writeMicrosoftOAuthError(w, http.StatusNotFound, "Microsoft Calendar is not configured on this server.")
		return
	}

	rawToken := requestToken(r)
	if rawToken == "" {
		writeMicrosoftOAuthError(w, http.StatusUnauthorized, "This Microsoft Calendar connection request is missing authentication. Please open it from Muesli again.")
		return
	}

	uid, err := s.deps.Store.UserIDByTokenHash(r.Context(), auth.HashToken(rawToken))
	if err != nil || uid == "" {
		writeMicrosoftOAuthError(w, http.StatusUnauthorized, "This Microsoft Calendar connection request is no longer valid. Please open it from Muesli again.")
		return
	}

	state, err := s.microsoftOAuthStateStore().issue(uid, auth.HashToken(rawToken), microsoftOAuthStateTTL)
	if err != nil {
		log.Printf("handleMicrosoftOAuthStart: issue state: %T", err)
		writeMicrosoftOAuthError(w, http.StatusInternalServerError, "Internal error. Please try again.")
		return
	}

	authURL := cfg.AuthCodeURL(
		state,
		oauth2.SetAuthURLParam("prompt", "consent"),
	)
	secure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
	http.SetCookie(w, microsoftOAuthCookie(state, int(microsoftOAuthStateTTL.Seconds()), secure))
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, authURL, http.StatusFound)
}

func (s *Server) handleMicrosoftOAuthCallback(w http.ResponseWriter, r *http.Request) {
	cfg, ok := s.microsoftOAuthConfig()
	if !ok {
		writeMicrosoftOAuthError(w, http.StatusNotFound, "Microsoft Calendar is not configured on this server.")
		return
	}

	state := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")
	if state == "" {
		writeMicrosoftOAuthError(w, http.StatusBadRequest, "Missing OAuth state. Please try connecting again.")
		return
	}
	if c, err := r.Cookie(microsoftOAuthStateCookie); err != nil || c.Value != state {
		_, _ = s.microsoftOAuthStateStore().consume(state)
		writeMicrosoftOAuthError(w, http.StatusBadRequest, "This Microsoft Calendar connection request expired or is invalid. Please start again.")
		return
	}
	pending, ok := s.microsoftOAuthStateStore().consume(state)
	if !ok {
		writeMicrosoftOAuthError(w, http.StatusBadRequest, "This Microsoft Calendar connection request expired or is invalid. Please start again.")
		return
	}
	if code == "" {
		writeMicrosoftOAuthError(w, http.StatusBadRequest, "Microsoft did not return an authorization code. Please try again.")
		return
	}

	token, err := cfg.Exchange(r.Context(), code)
	if err != nil {
		log.Printf("handleMicrosoftOAuthCallback: exchange failed: %T", err)
		writeMicrosoftOAuthError(w, http.StatusBadGateway, "Microsoft Calendar authorization failed. Please try again.")
		return
	}
	if token.RefreshToken == "" {
		writeMicrosoftOAuthError(w, http.StatusBadGateway, "Microsoft did not return a refresh token. Please reconnect and approve calendar access again.")
		return
	}

	plaintext, err := json.Marshal(map[string]string{"refresh_token": token.RefreshToken})
	if err != nil {
		log.Printf("handleMicrosoftOAuthCallback: marshal creds: %T", err)
		writeMicrosoftOAuthError(w, http.StatusInternalServerError, "internal error")
		return
	}

	sealed, err := s.deps.Crypto.Seal(plaintext)
	if err != nil {
		log.Printf("handleMicrosoftOAuthCallback: seal creds: %T", err)
		writeMicrosoftOAuthError(w, http.StatusInternalServerError, "internal error")
		return
	}

	src, err := s.deps.Store.CreateSource(r.Context(), pending.userID, "microsoft", "Microsoft Calendar", sealed)
	if err != nil {
		log.Printf("handleMicrosoftOAuthCallback: create source: %T", err)
		writeMicrosoftOAuthError(w, http.StatusInternalServerError, "internal error")
		return
	}

	http.SetCookie(w, microsoftOAuthCookie("", -1, false))
	kickCalendarSync(
		s.deps.Store,
		s.deps.Crypto,
		s.deps.Config.GoogleOAuthClientID,
		s.deps.Config.GoogleOAuthClientSecret,
		s.deps.Config.MicrosoftOAuthClientID,
		s.deps.Config.MicrosoftOAuthClientSecret,
		src.ID,
	)
	writeMicrosoftOAuthSuccess(w)
}
