package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
	"github.com/go-chi/chi/v5"
)

type createShareRequest struct {
	ExpiresAt string `json:"expires_at"`
}

type shareResponse struct {
	Token string `json:"token"`
	URL   string `json:"url"`
}

func parseShareExpiresAt(raw string) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func shareURL(publicURL, token string) string {
	return strings.TrimRight(publicURL, "/") + "/shared/" + token
}

func (s *Server) handleCreateShare(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	noteID := chi.URLParam(r, "id")
	if !validNoteID(noteID) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	var req createShareRequest
	if err := decodeOptionalJSONBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	expiresAt, err := parseShareExpiresAt(req.ExpiresAt)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid expires_at")
		return
	}

	share, err := s.deps.Store.CreateShare(r.Context(), uid, noteID, expiresAt)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusCreated, shareResponse{
		Token: share.Token,
		URL:   shareURL(s.deps.Config.PublicURL, share.Token),
	})
}

func (s *Server) handleListNoteShares(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	noteID := chi.URLParam(r, "id")
	if !validNoteID(noteID) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	shares, err := s.deps.Store.ListSharesForNote(r.Context(), uid, noteID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if shares == nil {
		shares = []model.Share{}
	}
	writeJSON(w, http.StatusOK, shares)
}

func (s *Server) handleRevokeShare(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	token := chi.URLParam(r, "token")
	if token == "" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	if err := s.deps.Store.RevokeShareByToken(r.Context(), uid, token); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
