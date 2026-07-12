package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/abedegno/muesli/internal/people"
	"github.com/abedegno/muesli/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// noteSpeakerRelinkTimeout bounds each fire-and-forget relink run so a stuck
// store call cannot keep the goroutine alive indefinitely.
const noteSpeakerRelinkTimeout = 2 * time.Minute

type upsertAliasRequest struct {
	AliasName string `json:"alias_name"`
}

type setSpeakerAliasPersonRequest struct {
	PersonID *string `json:"person_id"`
}

// kickNoteSpeakerRelink runs best-effort note speaker relinking in the
// background. It intentionally does not use the handler's request context,
// which is cancelled as soon as the handler returns.
func kickNoteSpeakerRelink(st *store.Store, ownerID, noteID string) {
	go func(ownerID, noteID string) {
		ctx, cancel := context.WithTimeout(context.Background(), noteSpeakerRelinkTimeout)
		defer cancel()
		if err := people.LinkNoteSpeakers(ctx, st, ownerID, noteID); err != nil {
			log.Printf("note speaker relink %s: %v", noteID, err)
		}
	}(ownerID, noteID)
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

// handleListNotePeople returns all distinct people linked to a note's speaker
// aliases as {"people": [...]}. Returns 404 if the note does not exist or does
// not belong to the authenticated user.
func (s *Server) handleListNotePeople(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	noteID := chi.URLParam(r, "id")
	if !validNoteID(noteID) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	owned, err := s.deps.Store.NoteOwnedBy(r.Context(), uid, noteID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !owned {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	peopleList, err := s.deps.Store.PeopleForNoteSpeakers(r.Context(), uid, noteID)
	if err != nil {
		log.Printf("handleListNotePeople: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"people": peopleList})
}

// handleRelinkNoteSpeakers kicks a background relink of speaker aliases to
// people and responds immediately.
func (s *Server) handleRelinkNoteSpeakers(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	noteID := chi.URLParam(r, "id")
	if !validNoteID(noteID) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	owned, err := s.deps.Store.NoteOwnedBy(r.Context(), uid, noteID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !owned {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	kickNoteSpeakerRelink(s.deps.Store, uid, noteID)
	writeJSON(w, http.StatusAccepted, map[string]string{"note_id": noteID, "status": "relinking"})
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

	if err := people.LinkNoteSpeakers(r.Context(), s.deps.Store, uid, noteID); err != nil {
		log.Printf("handleUpsertSpeakerAlias: link speakers: %v", err)
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

// handleSetSpeakerAliasPerson updates the person link for a speaker alias.
func (s *Server) handleSetSpeakerAliasPerson(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	noteID := chi.URLParam(r, "id")
	label := chi.URLParam(r, "label")
	if !validNoteID(noteID) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	var req setSpeakerAliasPersonRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}

	if req.PersonID != nil {
		if _, err := uuid.Parse(*req.PersonID); err != nil {
			writeError(w, http.StatusBadRequest, "invalid id")
			return
		}
		if _, err := s.deps.Store.GetPerson(r.Context(), uid, *req.PersonID); errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		} else if err != nil {
			log.Printf("handleSetSpeakerAliasPerson: get person: %v", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}

	if err := s.deps.Store.SetSpeakerAliasPerson(r.Context(), uid, noteID, label, req.PersonID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		log.Printf("handleSetSpeakerAliasPerson: set person: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	resp := map[string]any{
		"note_id":       noteID,
		"speaker_label": label,
		"person_id":     req.PersonID,
	}
	writeJSON(w, http.StatusOK, resp)
}
