package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/abedegno/muesli/internal/auth"
	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
	"github.com/go-chi/chi/v5"
)

const (
	inviteDefaultExpiryDays = 7
	inviteMaxExpiryDays     = 30
)

type createInviteRequest struct {
	Role          string `json:"role"`
	ExpiresInDays int    `json:"expires_in_days"`
}

type createInviteResponse struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
	Role      string    `json:"role"`
}

// handleCreateInvite is POST /api/admin/users/invites, admin-only (mounted
// behind RequireRole("admin") in server.go). It performs no email delivery
// -- the raw token appears exactly once, in this response's url.
func (s *Server) handleCreateInvite(w http.ResponseWriter, r *http.Request) {
	adminID, _ := userIDFromContext(r.Context())

	var req createInviteRequest
	if err := decodeOptionalJSONBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	role := req.Role
	if role == "" {
		role = model.RoleMember
	}
	if role != model.RoleAdmin && role != model.RoleMember {
		writeError(w, http.StatusBadRequest, "role must be \"admin\" or \"member\"")
		return
	}
	days := req.ExpiresInDays
	if days == 0 {
		days = inviteDefaultExpiryDays
	}
	if days < 1 || days > inviteMaxExpiryDays {
		writeError(w, http.StatusBadRequest, "expires_in_days must be between 1 and 30")
		return
	}

	raw, hash, err := auth.GenerateToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	expiresAt := time.Now().Add(time.Duration(days) * 24 * time.Hour)

	inv, err := s.deps.Store.CreateInvite(r.Context(), hash, role, adminID, expiresAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusCreated, createInviteResponse{
		URL:       "/api/invites/" + raw,
		ExpiresAt: inv.ExpiresAt,
		Role:      inv.Role,
	})
}

type inviteInspectResponse struct {
	Role string `json:"role"`
}

// handleGetInvite is the unauthenticated GET /api/invites/{token}. It
// returns only the invite's role for a live, unused token; an unknown,
// expired, or consumed token returns 404 with no distinguishing detail.
func (s *Server) handleGetInvite(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	inv, err := s.deps.Store.GetLiveInviteByHash(r.Context(), auth.HashToken(token))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, inviteInspectResponse{Role: inv.Role})
}

type acceptInviteRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// handleAcceptInvite is the unauthenticated POST /api/invites/{token}/accept.
// It validates and hashes the password using the same rule as first-run
// setup BEFORE entering the store's locked acceptance transaction, so a
// malformed request never even attempts to consume the invite.
func (s *Server) handleAcceptInvite(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")

	var req acceptInviteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Email == "" || len(req.Password) < 8 {
		writeError(w, http.StatusBadRequest, "email and password (min 8 chars) required")
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	u, err := s.deps.Store.AcceptInvite(r.Context(), auth.HashToken(token), req.Email, hash)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not found")
		return
	case errors.Is(err, store.ErrDuplicate):
		writeError(w, http.StatusConflict, "account already exists")
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": u.ID, "email": u.Email, "role": u.Role})
}
