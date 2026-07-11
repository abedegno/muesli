package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/abedegno/muesli/internal/store"
	"github.com/go-chi/chi/v5"
)

type addTagRequest struct {
	Name string `json:"name"`
}

// handleListTags returns the owner's tags with live-note counts (never null).
func (s *Server) handleListTags(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	tags, err := s.deps.Store.ListTags(r.Context(), uid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, tags)
}

// handleRenameTag renames an owner-scoped tag by id. The new name cascades to
// every note carrying the tag (note_tags joins on tag_id). 200 + renamed tag on
// success; 400 bad body/blank name, 404 not found, 409 duplicate name.
func (s *Server) handleRenameTag(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	id := chi.URLParam(r, "id")
	if !validID(w, r, id) {
		return
	}
	var req addTagRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "tag name required")
		return
	}
	tag, err := s.deps.Store.RenameTag(r.Context(), uid, id, req.Name)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if errors.Is(err, store.ErrDuplicate) {
		writeError(w, http.StatusConflict, "tag name already exists")
		return
	} else if errors.Is(err, store.ErrTagNameInvalid) {
		writeError(w, http.StatusBadRequest, "invalid tag name")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, tag)
}

// handleDeleteTag deletes an owner-scoped tag and all its note associations.
// 204 on success, 404 if the tag is not found, 500 on unexpected error.
func (s *Server) handleDeleteTag(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	id := chi.URLParam(r, "id")
	if !validID(w, r, id) {
		return
	}
	err := s.deps.Store.DeleteTag(r.Context(), uid, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAddNoteTag(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	if !validNoteID(chi.URLParam(r, "id")) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	var req addTagRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "tag name required")
		return
	}
	tag, err := s.deps.Store.AddNoteTag(r.Context(), uid, chi.URLParam(r, "id"), req.Name)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if errors.Is(err, store.ErrTagNameInvalid) {
		writeError(w, http.StatusBadRequest, "invalid tag name")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, tag)
}

func (s *Server) handleRemoveNoteTag(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	if !validNoteID(chi.URLParam(r, "id")) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		writeError(w, http.StatusBadRequest, "tag name required")
		return
	}
	err := s.deps.Store.RemoveNoteTag(r.Context(), uid, chi.URLParam(r, "id"), name)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
