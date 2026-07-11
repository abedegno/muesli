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
	if !validNoteID(chi.URLParam(r, "id")) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	var req addFolderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !validNoteID(req.FolderID) {
		writeError(w, http.StatusBadRequest, "folder_id required")
		return
	}
	err := s.deps.Store.AddNoteFolder(r.Context(), uid, chi.URLParam(r, "id"), req.FolderID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleRemoveNoteFolder(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	if !validNoteID(chi.URLParam(r, "id")) || !validNoteID(chi.URLParam(r, "folderID")) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	err := s.deps.Store.RemoveNoteFolder(r.Context(), uid, chi.URLParam(r, "id"), chi.URLParam(r, "folderID"))
	if err != nil {
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
