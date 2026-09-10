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

// maxResolveFolderIDs bounds a single /api/folders/resolve request to a small,
// constant-cost lookup regardless of how large the calling rule tree is; the
// renderer batches larger rule-derived id sets across multiple requests.
const maxResolveFolderIDs = 100

type folderRequest struct {
	Name     string  `json:"name"`
	ParentID *string `json:"parent_id"`
}

func (s *Server) handleResolveFolders(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	var req struct {
		IDs *[]any `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.IDs == nil {
		writeError(w, http.StatusBadRequest, "ids is required")
		return
	}
	seen := make(map[string]bool, len(*req.IDs))
	ids := make([]string, 0, len(*req.IDs))
	for _, raw := range *req.IDs {
		idStr, ok := raw.(string)
		if !ok || idStr == "" {
			writeError(w, http.StatusBadRequest, "ids must be non-empty strings")
			return
		}
		if _, err := uuid.Parse(idStr); err != nil {
			writeError(w, http.StatusBadRequest, "ids must be valid uuids")
			return
		}
		if !seen[idStr] {
			seen[idStr] = true
			ids = append(ids, idStr)
		}
	}
	if len(ids) > maxResolveFolderIDs {
		writeError(w, http.StatusBadRequest, "too many ids")
		return
	}
	folders, err := s.deps.Store.ResolveFolders(r.Context(), uid, ids)
	if err != nil {
		log.Printf("handleResolveFolders: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if folders == nil {
		folders = []model.Folder{}
	}
	writeJSON(w, http.StatusOK, folders)
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
	// Ownership is checked before request-body decode/validation (issue
	// #12): a non-owner must get the same 404/403 denial regardless of
	// whether their body is otherwise well-formed.
	if err := s.deps.Store.CheckFolderOwnerMutation(r.Context(), uid, id); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if errors.Is(err, store.ErrForbidden) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
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
	} else if errors.Is(err, store.ErrForbidden) {
		writeError(w, http.StatusForbidden, "forbidden")
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
	// Ownership is checked before request-body decode/validation (issue
	// #12): a non-owner must get the same 404/403 denial regardless of
	// whether their body is otherwise well-formed.
	if err := s.deps.Store.CheckFolderOwnerMutation(r.Context(), uid, id); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if errors.Is(err, store.ErrForbidden) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
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
	} else if errors.Is(err, store.ErrForbidden) {
		writeError(w, http.StatusForbidden, "forbidden")
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
	} else if errors.Is(err, store.ErrForbidden) {
		writeError(w, http.StatusForbidden, "forbidden")
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

// folderVisibilityRequest is the strict body for PUT /api/folders/{id}/visibility:
// exactly {"visibility":"private"} or {"visibility":"shared"} — unknown fields
// and unknown/missing values are rejected (400).
type folderVisibilityRequest struct {
	Visibility string `json:"visibility"`
}

// handleSetFolderVisibility implements PUT /api/folders/{id}/visibility (issue
// #12). The body is decoded strictly: unknown fields and trailing content
// after the JSON value are both 400, matching the "complete body" contract in
// the design doc.
func (s *Server) handleSetFolderVisibility(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	id := chi.URLParam(r, "id")
	if !validID(w, r, id) {
		return
	}
	// Ownership is checked before request-body decode/validation (issue
	// #12): a non-owner must get the same 404/403 denial regardless of
	// whether their body is otherwise well-formed.
	if err := s.deps.Store.CheckFolderOwnerMutation(r.Context(), uid, id); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if errors.Is(err, store.ErrForbidden) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	var req folderVisibilityRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if dec.More() {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.Visibility != model.FolderPrivate && req.Visibility != model.FolderShared {
		writeError(w, http.StatusBadRequest, "invalid visibility")
		return
	}
	f, err := s.deps.Store.SetFolderVisibility(r.Context(), uid, id, req.Visibility)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if errors.Is(err, store.ErrForbidden) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	} else if ve := (store.ValidationError("")); errors.As(err, &ve) {
		writeError(w, http.StatusBadRequest, ve.Error())
		return
	} else if err != nil {
		log.Printf("handleSetFolderVisibility: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, f)
}
