package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
	"github.com/go-chi/chi/v5"
)

type actionItemsResponse struct {
	ActionItems []model.ActionItem `json:"action_items"`
	Decisions   []model.Decision   `json:"decisions"`
}

type actionItemStatusRequest struct {
	Status string `json:"status"`
}

func validActionItemStatus(status string) bool {
	switch status {
	case model.ActionItemOpen, model.ActionItemDone:
		return true
	default:
		return false
	}
}

func (s *Server) handleListNoteActionItems(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	id := chi.URLParam(r, "id")
	if !validID(w, r, id) {
		return
	}
	items, decisions, err := s.deps.Store.ListForNote(r.Context(), uid, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		log.Printf("handleListNoteActionItems: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if items == nil {
		items = []model.ActionItem{}
	}
	if decisions == nil {
		decisions = []model.Decision{}
	}
	writeJSON(w, http.StatusOK, actionItemsResponse{ActionItems: items, Decisions: decisions})
}

func (s *Server) handleListActionItems(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	status := r.URL.Query().Get("status")
	if status != "" && !validActionItemStatus(status) {
		writeError(w, http.StatusBadRequest, "invalid status")
		return
	}
	items, err := s.deps.Store.ListForOwner(r.Context(), uid, status)
	if err != nil {
		log.Printf("handleListActionItems: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if items == nil {
		items = []model.ActionItem{}
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleUpdateActionItemStatus(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	id := chi.URLParam(r, "id")
	if !validID(w, r, id) {
		return
	}
	var req actionItemStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if !validActionItemStatus(req.Status) {
		writeError(w, http.StatusBadRequest, "invalid status")
		return
	}
	item, err := s.deps.Store.SetStatus(r.Context(), uid, id, req.Status)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		log.Printf("handleUpdateActionItemStatus: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, item)
}
