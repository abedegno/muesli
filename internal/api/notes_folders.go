package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/abedegno/muesli/internal/store"
	"github.com/go-chi/chi/v5"
)

type addFolderRequest struct {
	FolderID string `json:"folder_id"`
}

func (s *Server) handleAddNoteFolder(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	noteID := chi.URLParam(r, "id")
	if !validNoteID(noteID) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	// A wholly-invisible note is checked before request-body decode/
	// validation (issue #12): AddNoteFolder always denies with ErrNotFound
	// when the note isn't visible to the requester regardless of the target
	// folder, so that case can be (and is) rejected before the body is even
	// parsed. The full authorization — note ownership AND target-folder
	// visibility together — still runs atomically inside AddNoteFolder once
	// folder_id is known, since it is itself part of the request body.
	if _, err := s.deps.Store.GetReadableNote(r.Context(), uid, noteID); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	var req addFolderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !validNoteID(req.FolderID) {
		writeError(w, http.StatusBadRequest, "folder_id required")
		return
	}
	err := s.deps.Store.AddNoteFolder(r.Context(), uid, noteID, req.FolderID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if errors.Is(err, store.ErrForbidden) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleRemoveNoteFolder implements DELETE /api/notes/{id}/folders/{folderID}.
// Allowed when the requester owns the live note or owns the live folder
// (issue #12): a folder owner may remove any note from their own folder,
// including a teammate's contribution.
func (s *Server) handleRemoveNoteFolder(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	if !validNoteID(chi.URLParam(r, "id")) || !validNoteID(chi.URLParam(r, "folderID")) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	err := s.deps.Store.RemoveNoteFolder(r.Context(), uid, chi.URLParam(r, "id"), chi.URLParam(r, "folderID"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if errors.Is(err, store.ErrForbidden) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleReorderNoteInFolder(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	folderID := chi.URLParam(r, "folderID")
	noteID := chi.URLParam(r, "noteID")
	if !validNoteID(folderID) || !validNoteID(noteID) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	// Folder ownership (the sole authority for this route) is checked before
	// request-body decode/validation (issue #12): a non-owner must get the
	// same 404/403 denial regardless of whether their body is otherwise
	// well-formed.
	if err := s.deps.Store.CheckFolderOwnerMutation(r.Context(), uid, folderID); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if errors.Is(err, store.ErrForbidden) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	var req struct {
		AfterID *string `json:"after_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	err := s.deps.Store.ReorderNoteInFolder(r.Context(), uid, folderID, noteID, req.AfterID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if errors.Is(err, store.ErrInvalidParent) {
		writeError(w, http.StatusBadRequest, "invalid sibling")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
