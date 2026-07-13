package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/abedegno/muesli/internal/store"
)

type digestConfigRequest struct {
	Cadence string `json:"cadence"`
}

type digestConfigResponse struct {
	OwnerID    string     `json:"owner_id"`
	Cadence    string     `json:"cadence"`
	LastSentAt *time.Time `json:"last_sent_at,omitempty"`
	UpdatedAt  *time.Time `json:"updated_at,omitempty"`
}

func (s *Server) handleGetDigestConfig(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	cfg, err := s.deps.Store.GetDigestConfig(r.Context(), uid)
	if err != nil {
		log.Printf("handleGetDigestConfig: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, digestConfigResponse{
		OwnerID:    cfg.OwnerID,
		Cadence:    cfg.Cadence,
		LastSentAt: cfg.LastSentAt,
		UpdatedAt:  timePtr(cfg.UpdatedAt),
	})
}

func (s *Server) handleUpdateDigestConfig(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	var req digestConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	cfg, err := s.deps.Store.SetDigestConfig(r.Context(), uid, req.Cadence)
	switch {
	case errors.Is(err, store.ErrInvalidState):
		writeError(w, http.StatusBadRequest, "invalid cadence")
		return
	case err != nil:
		log.Printf("handleUpdateDigestConfig: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, digestConfigResponse{
		OwnerID:    cfg.OwnerID,
		Cadence:    cfg.Cadence,
		LastSentAt: cfg.LastSentAt,
		UpdatedAt:  timePtr(cfg.UpdatedAt),
	})
}

func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}
