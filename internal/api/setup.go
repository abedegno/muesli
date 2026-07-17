package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/abedegno/muesli/internal/auth"
	"github.com/abedegno/muesli/internal/store"
)

type setupRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// handleSetup creates the single first-run account. Rejected once any user exists.
func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	var req setupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Email == "" || len(req.Password) < 8 {
		writeError(w, http.StatusBadRequest, "email and password (min 8 chars) required")
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	u, err := s.deps.Store.CreateFirstUser(r.Context(), req.Email, hash)
	if err != nil {
		if errors.Is(err, store.ErrAlreadySetUp) {
			writeError(w, http.StatusConflict, "account already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": u.ID, "email": u.Email})
}

// handleSetupStatus reports whether first-run setup is still needed (no auth).
func (s *Server) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	n, err := s.deps.Store.CountUsers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"needs_setup": n == 0})
}
