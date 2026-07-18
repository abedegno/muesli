package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/abedegno/muesli/internal/chat"
	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
	"github.com/go-chi/chi/v5"
)

// conversationRequest is the body of POST /api/conversations. Content is
// OPTIONAL: when present (non-empty), this single request both creates the
// conversation AND immediately sends that first message (CHT04's
// create-and-send variant), still returning 201 -- but with a
// conversationWithMessage body (conversation fields plus "message"/"sources")
// instead of the plain conversation returned when Content is omitted. This
// keeps the existing plain-create response shape unchanged for callers that
// don't send Content, while avoiding a second near-duplicate endpoint for
// "create then immediately ask a question" (the common case for a
// note-scoped chat started from a note's page).
type conversationRequest struct {
	NoteID        *string `json:"note_id"`
	Title         string  `json:"title"`
	ModelOverride *string `json:"model_override"`
	Content       string  `json:"content,omitempty"`
}

// conversationWithMessage is the create-and-send response body: the created
// conversation's own fields (embedded), plus the assistant's reply message
// and the sources it cited for that first turn.
type conversationWithMessage struct {
	model.Conversation
	Message model.Message `json:"message"`
	Sources []chat.Source `json:"sources"`
}

func (s *Server) handleCreateConversation(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	var req conversationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	c, err := s.deps.Store.CreateConversation(r.Context(), uid, req.NoteID, req.Title, req.ModelOverride)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		log.Printf("handleCreateConversation: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if strings.TrimSpace(req.Content) == "" {
		writeJSON(w, http.StatusCreated, c)
		return
	}

	// Create-and-send: the conversation was just created above, so its
	// ModelOverride (if any) already came from this same request -- no
	// separate per-request override is passed here (see sendChatMessage's
	// requestOverride doc comment).
	msg, sources, err := s.sendChatMessage(r.Context(), uid, c, req.Content, nil)
	switch {
	case errors.Is(err, errChatSendInFlight):
		writeError(w, http.StatusConflict, "message send already in progress")
		return
	case errors.Is(err, errNoDefaultAgentPlugin):
		log.Printf("handleCreateConversation: send: %v", err)
		writeError(w, http.StatusUnprocessableEntity, "no default agent configured")
		return
	case err != nil:
		log.Printf("handleCreateConversation: send: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, conversationWithMessage{Conversation: c, Message: msg, Sources: sources})
}

func (s *Server) handleListConversations(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	var noteID *string
	if v := r.URL.Query().Get("note_id"); v != "" {
		noteID = &v
	}
	conversations, err := s.deps.Store.ListConversations(r.Context(), uid, noteID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, conversations)
}

func (s *Server) handleGetConversation(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	id := chi.URLParam(r, "id")
	if !validID(w, r, id) {
		return
	}
	c, err := s.deps.Store.GetConversation(r.Context(), uid, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) handleDeleteConversation(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	id := chi.URLParam(r, "id")
	if !validID(w, r, id) {
		return
	}
	err := s.deps.Store.DeleteConversation(r.Context(), uid, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListMessages(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	id := chi.URLParam(r, "id")
	if !validID(w, r, id) {
		return
	}
	messages, err := s.deps.Store.ListMessages(r.Context(), uid, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, messages)
}
