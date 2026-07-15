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

type templateRequest struct {
	Name     string                  `json:"name"`
	Phase    string                  `json:"phase"`
	Sections []model.TemplateSection `json:"sections"`
	AutoRun  *bool                   `json:"auto_run,omitempty"`
	// SystemPrompt, Model, and Temperature are optional per-template agent
	// overrides. Empty/nil means unset (falls back to the agent's default
	// system prompt / plugin Config values).
	SystemPrompt string   `json:"system_prompt,omitempty"`
	Model        string   `json:"model,omitempty"`
	Temperature  *float64 `json:"temperature,omitempty"`
}

func (s *Server) handleListTemplates(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	ts, err := s.deps.Store.ListTemplates(r.Context(), uid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, ts)
}

func (s *Server) handleCreateTemplate(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	var req templateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	autoRun := true
	if req.AutoRun != nil {
		autoRun = *req.AutoRun
	}
	tm, err := s.deps.Store.CreateTemplate(r.Context(), uid, req.Name, req.Phase, req.Sections,
		autoRun, req.SystemPrompt, req.Model, req.Temperature)
	if errors.Is(err, store.ErrDuplicate) {
		writeError(w, http.StatusConflict, "a template with that name already exists")
		return
	} else if ve := (store.ValidationError("")); errors.As(err, &ve) {
		writeError(w, http.StatusBadRequest, ve.Error())
		return
	} else if err != nil {
		log.Printf("handleCreateTemplate: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, tm)
}

func (s *Server) handleUpdateTemplate(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	id := chi.URLParam(r, "id")
	if !validID(w, r, id) {
		return
	}
	current, err := s.deps.Store.GetTemplate(r.Context(), uid, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		log.Printf("handleUpdateTemplate lookup: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	var req templateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	autoRun := current.AutoRun
	if req.AutoRun != nil {
		autoRun = *req.AutoRun
	}
	err = s.deps.Store.UpdateTemplate(r.Context(), uid, id, req.Name, req.Phase, req.Sections,
		autoRun, req.SystemPrompt, req.Model, req.Temperature)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if errors.Is(err, store.ErrDuplicate) {
		writeError(w, http.StatusConflict, "a template with that name already exists")
		return
	} else if ve := (store.ValidationError("")); errors.As(err, &ve) {
		writeError(w, http.StatusBadRequest, ve.Error())
		return
	} else if err != nil {
		log.Printf("handleUpdateTemplate: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleDeleteTemplate(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	id := chi.URLParam(r, "id")
	if !validID(w, r, id) {
		return
	}
	err := s.deps.Store.DeleteTemplate(r.Context(), uid, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
