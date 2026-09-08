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
	folderMembersDefaultLimit = 50
	folderMembersMaxLimit     = 100
)

type folderMemberView struct {
	UserID    string `json:"user_id"`
	Email     string `json:"email"`
	Role      string `json:"role"`
	CreatedAt string `json:"created_at"`
}

func newFolderMemberView(fm model.FolderMember) folderMemberView {
	return folderMemberView{
		UserID:    fm.UserID,
		Email:     fm.Email,
		Role:      fm.Role,
		CreatedAt: fm.CreatedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"),
	}
}

type upsertFolderMemberRequest struct {
	UserID string `json:"user_id"`
	Role   string `json:"role"`
}

// handleUpsertFolderMember is POST /api/folders/{id}/members, owner-only.
// Idempotently grants (or updates the role of) userID's read-only access to
// the folder.
func (s *Server) handleUpsertFolderMember(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	folderID := chi.URLParam(r, "id")
	if !validID(w, r, folderID) {
		return
	}
	var req upsertFolderMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if _, err := uuid.Parse(req.UserID); err != nil {
		writeError(w, http.StatusBadRequest, "user_id required")
		return
	}
	if req.Role == "" {
		req.Role = model.FolderRoleViewer
	}
	fm, err := s.deps.Store.UpsertFolderMember(r.Context(), uid, folderID, req.UserID, req.Role)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if ve := (store.ValidationError("")); errors.As(err, &ve) {
		writeError(w, http.StatusBadRequest, ve.Error())
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, newFolderMemberView(fm))
}

// handleDeleteFolderMember is DELETE /api/folders/{id}/members/{user_id},
// owner-only and idempotent.
func (s *Server) handleDeleteFolderMember(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	folderID := chi.URLParam(r, "id")
	targetID := chi.URLParam(r, "user_id")
	if !validID(w, r, folderID) || !validNoteID(targetID) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	err := s.deps.Store.DeleteFolderMember(r.Context(), uid, folderID, targetID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type folderMemberListResponse struct {
	Items      []folderMemberView `json:"items"`
	NextCursor string             `json:"next_cursor,omitempty"`
}

// handleListFolderMembers is GET /api/folders/{id}/members, reachable by the
// folder's owner or any of its current members.
func (s *Server) handleListFolderMembers(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	folderID := chi.URLParam(r, "id")
	if !validID(w, r, folderID) {
		return
	}
	var cursor folderMemberCursor
	limit, hasCursor, ok := parsePageRequest(w, r.URL.Query(), folderMembersDefaultLimit, folderMembersMaxLimit, folderMemberCursorKind, &cursor)
	if !ok {
		return
	}
	if hasCursor && (cursor.CreatedAt.IsZero() || cursor.UserID == "") {
		writeError(w, http.StatusBadRequest, "invalid cursor")
		return
	}

	members, err := s.deps.Store.ListFolderMembers(r.Context(), uid, folderID, limit+1, cursor.CreatedAt, cursor.UserID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	resp := folderMemberListResponse{Items: []folderMemberView{}}
	for i, m := range members {
		if i == limit {
			resp.NextCursor = encodeCursor(folderMemberCursorKind, folderMemberCursor{CreatedAt: members[limit-1].CreatedAt, UserID: members[limit-1].UserID})
			break
		}
		resp.Items = append(resp.Items, newFolderMemberView(m))
	}
	writeJSON(w, http.StatusOK, resp)
}
