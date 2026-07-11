package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/abedegno/muesli/internal/store"
	"github.com/go-chi/chi/v5"
)

type upsertAliasRequest struct {
	AliasName string `json:"alias_name"`
}

// handleListSpeakerAliases returns all speaker aliases for a note as
// {"aliases": [...]}. Returns 404 if the note does not exist or does not
// belong to the authenticated user.
func (s *Server) handleListSpeakerAliases(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	noteID := chi.URLParam(r, "id")
	if !validNoteID(noteID) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	// Ownership gate: return 404 if the note doesn't belong to this user.
	owned, err := s.deps.Store.NoteOwnedBy(r.Context(), uid, noteID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !owned {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	aliases, err := s.deps.Store.ListSpeakerAliases(r.Context(), uid, noteID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"aliases": aliases})
}

// handleUpsertSpeakerAlias creates or updates a speaker alias for a note.
func (s *Server) handleUpsertSpeakerAlias(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	noteID := chi.URLParam(r, "id")
	label := chi.URLParam(r, "label")
	if !validNoteID(noteID) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	var req upsertAliasRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.AliasName == "" {
		writeError(w, http.StatusBadRequest, "alias_name required")
		return
	}

	if err := s.deps.Store.UpsertSpeakerAlias(r.Context(), uid, noteID, label, req.AliasName); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"note_id":       noteID,
		"speaker_label": label,
		"alias_name":    req.AliasName,
	})
}

// handleDeleteSpeakerAlias removes a speaker alias for a note.
func (s *Server) handleDeleteSpeakerAlias(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	noteID := chi.URLParam(r, "id")
	label := chi.URLParam(r, "label")
	if !validNoteID(noteID) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	if err := s.deps.Store.DeleteSpeakerAlias(r.Context(), uid, noteID, label); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
