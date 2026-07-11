package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/abedegno/muesli/internal/auth"
	"github.com/abedegno/muesli/internal/store"
)

// dummyHash is a valid argon2 hash verified on the user-not-found login path so
// that path does the same work (and takes ~the same time) as a real verify,
// mitigating user-enumeration via response timing.
var dummyHash, _ = auth.HashPassword("muesli-login-timing-equalizer")

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	// Validate required fields before any store/crypto work.
	if strings.TrimSpace(req.Email) == "" || strings.TrimSpace(req.Password) == "" {
		writeError(w, http.StatusBadRequest, "email and password required")
		return
	}
	u, err := s.deps.Store.GetUserByEmail(r.Context(), req.Email)
	if errors.Is(err, store.ErrNotFound) {
		// Equalize timing with the found-but-wrong-password path (anti-enumeration).
		_, _ = auth.VerifyPassword(req.Password, dummyHash)
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	ok, err := auth.VerifyPassword(req.Password, u.PasswordHash)
	if err != nil || !ok {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	raw, hash, err := auth.GenerateToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := s.deps.Store.CreateToken(r.Context(), u.ID, "session", hash, "session"); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Set a session cookie so browser clients can authenticate without
	// managing the bearer token manually. Secure is only set when the
	// connection is already HTTPS (r.TLS != nil) or the reverse proxy has
	// signalled HTTPS via X-Forwarded-Proto, so local HTTP dev still works.
	secure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
	http.SetCookie(w, &http.Cookie{
		Name:     "muesli_session",
		Value:    raw,
		Path:     "/",
		MaxAge:   30 * 24 * 60 * 60, // 30 days
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})

	writeJSON(w, http.StatusOK, map[string]string{"token": raw})
}

type createTokenRequest struct {
	Name string `json:"name"`
}

// handleCreateToken mints a named app token for the authenticated user.
func (s *Server) handleCreateToken(w http.ResponseWriter, r *http.Request) {
	uid, ok := userIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req createTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "name required")
		return
	}
	raw, hash, err := auth.GenerateToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := s.deps.Store.CreateToken(r.Context(), uid, req.Name, hash, "app"); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"token": raw})
}
