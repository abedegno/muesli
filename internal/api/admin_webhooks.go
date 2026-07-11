package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const (
	defaultWebhookDeliveriesLimit = 50
	maxWebhookDeliveriesLimit     = 200
)

// handleListWebhookDeliveries lists the caller's webhook deliveries (joined
// to their owning webhook), most recent first. Optional ?limit= caps the
// page size (default 50, capped at 200).
func (s *Server) handleListWebhookDeliveries(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())

	limit := defaultWebhookDeliveriesLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > maxWebhookDeliveriesLimit {
		limit = maxWebhookDeliveriesLimit
	}

	deliveries, err := s.deps.Store.ListDeliveriesForOwner(r.Context(), uid, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, deliveries)
}

// handleRetryWebhookDelivery manually retries a webhook delivery:
//   - delivered  -> idempotent no-op, 200 {"status":"already_delivered"}
//   - failed     -> re-enqueued,      202 {"status":"queued"}
//   - pending/in_flight -> already in progress, 409
//   - not found / not owned by caller -> 404
func (s *Server) handleRetryWebhookDelivery(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())

	idStr := chi.URLParam(r, "id")
	if !validID(w, r, idStr) {
		return
	}
	deliveryID, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	status, err := s.deps.Store.RetryDelivery(r.Context(), uid, deliveryID)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrNotFound):
			writeError(w, http.StatusNotFound, "delivery not found")
		case errors.Is(err, store.ErrInvalidState):
			writeError(w, http.StatusConflict, "delivery is already queued or in progress")
		default:
			writeError(w, http.StatusInternalServerError, "internal error")
		}
		return
	}

	if status == model.DeliveryDelivered {
		writeJSON(w, http.StatusOK, map[string]string{"status": "already_delivered"})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "queued"})
}
