package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
	"github.com/go-chi/chi/v5"
)

type addNoteLinkRequest struct {
	ToNoteID string `json:"to_note_id"`
}

func (s *Server) handleAddNoteLink(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	fromID := chi.URLParam(r, "id")
	if !validNoteID(fromID) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	var req addNoteLinkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !validNoteID(req.ToNoteID) {
		writeError(w, http.StatusBadRequest, "to_note_id required")
		return
	}
	link, err := s.deps.Store.AddLink(r.Context(), uid, fromID, req.ToNoteID)
	switch {
	case errors.Is(err, store.ErrSelfLink):
		writeError(w, http.StatusBadRequest, "cannot link a note to itself")
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not found")
	case errors.Is(err, store.ErrDuplicate):
		writeError(w, http.StatusConflict, "link already exists")
	case err != nil:
		writeError(w, http.StatusInternalServerError, "internal error")
	default:
		writeJSON(w, http.StatusCreated, link)
	}
}

func (s *Server) handleRemoveNoteLink(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	fromID := chi.URLParam(r, "id")
	if !validNoteID(fromID) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	var req addNoteLinkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !validNoteID(req.ToNoteID) {
		writeError(w, http.StatusBadRequest, "to_note_id required")
		return
	}
	err := s.deps.Store.RemoveLink(r.Context(), uid, fromID, req.ToNoteID)
	switch {
	case errors.Is(err, store.ErrSelfLink):
		writeError(w, http.StatusBadRequest, "cannot link a note to itself")
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not found")
	case err != nil:
		writeError(w, http.StatusInternalServerError, "internal error")
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) handleListNoteLinks(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	noteID := chi.URLParam(r, "id")
	if !validNoteID(noteID) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	outgoing, err := s.deps.Store.OutgoingLinks(r.Context(), uid, noteID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	backlinks, err := s.deps.Store.Backlinks(r.Context(), uid, noteID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Outgoing  []model.NoteLink `json:"outgoing"`
		Backlinks []model.NoteLink `json:"backlinks"`
	}{
		Outgoing:  outgoing,
		Backlinks: backlinks,
	})
}
