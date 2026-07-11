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

type folderRequest struct {
	Name     string  `json:"name"`
	ParentID *string `json:"parent_id"`
}

func (s *Server) handleListFolders(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	folders, err := s.deps.Store.ListFolders(r.Context(), uid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, folders)
}

func (s *Server) handleCreateFolder(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	var req folderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	f, err := s.deps.Store.CreateFolder(r.Context(), uid, req.Name, req.ParentID)
	if errors.Is(err, store.ErrDuplicate) {
		writeError(w, http.StatusConflict, "a folder with that name already exists")
		return
	} else if errors.Is(err, store.ErrInvalidParent) {
		writeError(w, http.StatusBadRequest, "invalid parent folder")
		return
	} else if ve := (store.ValidationError("")); errors.As(err, &ve) {
		writeError(w, http.StatusBadRequest, ve.Error())
		return
	} else if err != nil {
		log.Printf("handleCreateFolder: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, f)
}

func (s *Server) handleUpdateFolder(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	id := chi.URLParam(r, "id")
	if !validID(w, r, id) {
		return
	}
	var req folderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	f, err := s.deps.Store.UpdateFolder(r.Context(), uid, id, req.Name, req.ParentID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if errors.Is(err, store.ErrDuplicate) {
		writeError(w, http.StatusConflict, "a folder with that name already exists")
		return
	} else if errors.Is(err, store.ErrInvalidParent) {
		writeError(w, http.StatusBadRequest, "invalid parent folder")
		return
	} else if ve := (store.ValidationError("")); errors.As(err, &ve) {
		writeError(w, http.StatusBadRequest, ve.Error())
		return
	} else if err != nil {
		log.Printf("handleUpdateFolder: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, f)
}

func (s *Server) handleReorderFolder(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	id := chi.URLParam(r, "id")
	if !validID(w, r, id) {
		return
	}
	var req struct {
		AfterID *string `json:"after_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	err := s.deps.Store.ReorderFolder(r.Context(), uid, id, req.AfterID)
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

func (s *Server) handleDeleteFolder(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	id := chi.URLParam(r, "id")
	if !validID(w, r, id) {
		return
	}
	err := s.deps.Store.DeleteFolder(r.Context(), uid, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "trashed"})
}

func (s *Server) handleListTrashedFolders(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	folders, err := s.deps.Store.ListTrashedFolders(r.Context(), uid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if folders == nil {
		folders = []model.Folder{}
	}
	writeJSON(w, http.StatusOK, folders)
}

func (s *Server) handleRestoreFolder(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	id := chi.URLParam(r, "id")
	if !validID(w, r, id) {
		return
	}
	if err := s.deps.Store.RestoreFolder(r.Context(), uid, id); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "restored"})
}

func (s *Server) handlePurgeFolder(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	id := chi.URLParam(r, "id")
	if !validID(w, r, id) {
		return
	}
	if err := s.deps.Store.PurgeFolder(r.Context(), uid, id); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
