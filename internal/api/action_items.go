package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type actionItemsResponse struct {
	ActionItems []model.ActionItem `json:"action_items"`
	Decisions   []model.Decision   `json:"decisions"`
}

type actionItemStatusRequest struct {
	Status string `json:"status"`
}

type actionItemPatchRequest struct {
	Text             *string
	Status           *string
	OwnerPersonID    *string
	HasText          bool
	HasStatus        bool
	HasOwnerPersonID bool
}

var (
	errActionItemPatchNoFields      = errors.New("no fields to update")
	errActionItemPatchInvalidBody   = errors.New("invalid body")
	errActionItemPatchInvalidText   = errors.New("invalid text")
	errActionItemPatchInvalidStatus = errors.New("invalid status")
	errActionItemPatchInvalidOwner  = errors.New("invalid owner")
)

func validActionItemStatus(status string) bool {
	switch status {
	case model.ActionItemOpen, model.ActionItemDone:
		return true
	default:
		return false
	}
}

func parseActionItemPatch(raw map[string]json.RawMessage) (actionItemPatchRequest, error) {
	var req actionItemPatchRequest

	if v, ok := raw["text"]; ok {
		req.HasText = true
		if string(v) == "null" {
			return actionItemPatchRequest{}, errActionItemPatchInvalidText
		}
		var text string
		if err := json.Unmarshal(v, &text); err != nil {
			return actionItemPatchRequest{}, errActionItemPatchInvalidBody
		}
		if text == "" {
			return actionItemPatchRequest{}, errActionItemPatchInvalidText
		}
		req.Text = &text
	}

	if v, ok := raw["status"]; ok {
		req.HasStatus = true
		if string(v) == "null" {
			return actionItemPatchRequest{}, errActionItemPatchInvalidStatus
		}
		var status string
		if err := json.Unmarshal(v, &status); err != nil {
			return actionItemPatchRequest{}, errActionItemPatchInvalidBody
		}
		if !validActionItemStatus(status) {
			return actionItemPatchRequest{}, errActionItemPatchInvalidStatus
		}
		req.Status = &status
	}

	if v, ok := raw["owner_person_id"]; ok {
		req.HasOwnerPersonID = true
		if string(v) == "null" {
			req.OwnerPersonID = nil
		} else {
			var ownerPersonID string
			if err := json.Unmarshal(v, &ownerPersonID); err != nil {
				return actionItemPatchRequest{}, errActionItemPatchInvalidBody
			}
			if ownerPersonID == "" {
				return actionItemPatchRequest{}, errActionItemPatchInvalidOwner
			}
			if _, err := uuid.Parse(ownerPersonID); err != nil {
				return actionItemPatchRequest{}, errActionItemPatchInvalidOwner
			}
			req.OwnerPersonID = &ownerPersonID
		}
	}

	if !req.HasText && !req.HasStatus && !req.HasOwnerPersonID {
		return actionItemPatchRequest{}, errActionItemPatchNoFields
	}

	return req, nil
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
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	req, err := parseActionItemPatch(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.HasOwnerPersonID && req.OwnerPersonID != nil {
		if _, err := s.deps.Store.GetPerson(r.Context(), uid, *req.OwnerPersonID); errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusBadRequest, "invalid owner")
			return
		} else if err != nil {
			log.Printf("handleUpdateActionItemStatus: validate owner: %v", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}

	var item model.ActionItem
	var updated bool
	if req.HasText || req.HasStatus {
		item, err = s.deps.Store.UpdateActionItem(r.Context(), uid, id, req.Text, req.Status)
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		} else if err != nil {
			log.Printf("handleUpdateActionItemStatus: update item: %v", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		updated = true
	}

	if req.HasOwnerPersonID {
		item, err = s.deps.Store.AssignOwner(r.Context(), uid, id, req.OwnerPersonID)
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		} else if errors.Is(err, store.ErrInvalidOwner) {
			writeError(w, http.StatusBadRequest, "invalid owner")
			return
		} else if err != nil {
			log.Printf("handleUpdateActionItemStatus: assign owner: %v", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		updated = true
	}

	if !updated {
		writeError(w, http.StatusBadRequest, "no fields to update")
		return
	}

	writeJSON(w, http.StatusOK, item)
}
