package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
	"github.com/go-chi/chi/v5"
)

// handleGetDiarizationReview returns the diarization review payload for a
// note's transcript. Segments are sorted by confidence ascending (NULLs last)
// so the lowest-confidence turns appear first for human review.
func (s *Server) handleGetDiarizationReview(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	noteID := chi.URLParam(r, "id")
	if !validNoteID(noteID) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	review, err := s.deps.Store.GetDiarizationReview(r.Context(), uid, noteID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, review)
}

// reviewUpdateRequest is the body accepted by POST /api/notes/{id}/transcript/review.
// At least one of SegmentID or ReviewState must be set.
type reviewUpdateRequest struct {
	SegmentID   string `json:"segment_id"`   // optional: confirm a segment's speaker
	Speaker     string `json:"speaker"`      // optional: used together with SegmentID
	ReviewState string `json:"review_state"` // optional: advance the review lifecycle
}

// handlePostDiarizationReview updates a note's diarization review state and/or
// confirms a segment's speaker label. Returns the updated review on success.
func (s *Server) handlePostDiarizationReview(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	noteID := chi.URLParam(r, "id")
	if !validNoteID(noteID) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	var req reviewUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.SegmentID == "" && req.ReviewState == "" {
		writeError(w, http.StatusBadRequest, "segment_id or review_state required")
		return
	}

	ctx := r.Context()

	if req.SegmentID != "" {
		if err := s.deps.Store.ConfirmSegmentSpeaker(ctx, uid, noteID, req.SegmentID, req.Speaker); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}

	if req.ReviewState != "" {
		if err := s.deps.Store.UpdateReviewState(ctx, uid, noteID, req.ReviewState); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			if errors.Is(err, store.ErrInvalidTransition) {
				writeError(w, http.StatusUnprocessableEntity, "invalid state transition")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if req.ReviewState == model.ReviewStateCompleted {
			if err := s.deps.Store.DeleteNoteSummaries(ctx, uid, noteID); err != nil {
				writeError(w, http.StatusInternalServerError, "internal error")
				return
			}
			if err := s.deps.Store.EnqueueSummarizeJobs(ctx, uid, noteID); err != nil {
				writeError(w, http.StatusInternalServerError, "internal error")
				return
			}
		}
	}

	// Return the updated review payload.
	review, err := s.deps.Store.GetDiarizationReview(ctx, uid, noteID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, review)
}
