package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const (
	usersDefaultLimit = 50
	usersMaxLimit     = 100
)

// userListItem is the deliberately narrow shape returned by both user list
// endpoints: id, email, and role only. Never the password hash.
type userListItem struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Role  string `json:"role"`
}

type userListResponse struct {
	Items      []userListItem `json:"items"`
	NextCursor string         `json:"next_cursor,omitempty"`
}

// listUsersPage fetches one page of users (limit+1 to detect a next page),
// shared by the ordinary and admin user list handlers -- they differ only in
// authorization (RequireRole gates the admin route in server.go), not in
// shape.
func (s *Server) listUsersPage(w http.ResponseWriter, r *http.Request) {
	var cursor userCursor
	limit, hasCursor, ok := parsePageRequest(w, r.URL.Query(), usersDefaultLimit, usersMaxLimit, userCursorKind, &cursor)
	if !ok {
		return
	}
	if hasCursor && (cursor.CreatedAt.IsZero() || cursor.ID == "") {
		writeError(w, http.StatusBadRequest, "invalid cursor")
		return
	}

	users, err := s.deps.Store.ListUsers(r.Context(), limit+1, cursor.CreatedAt, cursor.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	resp := userListResponse{Items: []userListItem{}}
	for i, u := range users {
		if i == limit {
			resp.NextCursor = encodeCursor(userCursorKind, userCursor{CreatedAt: users[limit-1].CreatedAt, ID: users[limit-1].ID})
			break
		}
		resp.Items = append(resp.Items, userListItem{ID: u.ID, Email: u.Email, Role: u.Role})
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleListUsers is GET /api/users: any authenticated deployment member can
// discover their teammates (for sharing a folder with them), but only their
// id/email/role -- never password hashes or anything else.
func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	s.listUsersPage(w, r)
}

// handleAdminListUsers is GET /api/admin/users: same shape and pagination as
// handleListUsers, mounted behind RequireRole("admin") in server.go.
func (s *Server) handleAdminListUsers(w http.ResponseWriter, r *http.Request) {
	s.listUsersPage(w, r)
}

type setUserRoleRequest struct {
	Role string `json:"role"`
}

// handleAdminSetUserRole is PATCH /api/admin/users/{id}: sets the target
// user's role, admin-only. Reapplying the current role is idempotent;
// demoting the last remaining admin is rejected with 409.
func (s *Server) handleAdminSetUserRole(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, err := uuid.Parse(id); err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	var req setUserRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.Role != model.RoleAdmin && req.Role != model.RoleMember {
		writeError(w, http.StatusBadRequest, "role must be \"admin\" or \"member\"")
		return
	}
	err := s.deps.Store.SetUserRole(r.Context(), id, req.Role)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not found")
		return
	case errors.Is(err, store.ErrLastAdmin):
		writeError(w, http.StatusConflict, "cannot demote the last admin")
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	u, err := s.deps.Store.GetUser(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, userListItem{ID: u.ID, Email: u.Email, Role: u.Role})
}
