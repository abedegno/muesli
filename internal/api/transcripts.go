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
// At least one of SegmentID or ReviewState must be set. Generation is always
// required: it is the transcript generation the client rendered its review
// payload from, echoed back from model.DiarizationReview.Generation, so a
// submission against a transcript that has since been replaced is rejected
// instead of silently mutating the one that replaced it.
type reviewUpdateRequest struct {
	SegmentID   string `json:"segment_id"`   // optional: confirm a segment's speaker
	Speaker     string `json:"speaker"`      // optional: used together with SegmentID
	ReviewState string `json:"review_state"` // optional: advance the review lifecycle
	Generation  int    `json:"generation"`   // required: the generation this review was rendered from
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
	// A zero generation means the client never rendered a versioned review
	// payload to submit against — accepting it would reintroduce the
	// read-by-note/update-by-note bug this task removes.
	if req.Generation == 0 {
		writeError(w, http.StatusBadRequest, "generation required")
		return
	}

	ctx := r.Context()

	if req.SegmentID != "" {
		if err := s.deps.Store.ConfirmSegmentSpeaker(ctx, uid, noteID, req.SegmentID, req.Speaker, req.Generation); err != nil {
			switch {
			case errors.Is(err, store.ErrNotFound):
				writeError(w, http.StatusNotFound, "not found")
			case errors.Is(err, store.ErrGenerationMismatch):
				writeError(w, http.StatusConflict, "transcript changed, refetch and retry")
			default:
				writeError(w, http.StatusInternalServerError, "internal error")
			}
			return
		}
	}

	if req.ReviewState != "" {
		if err := s.deps.Store.UpdateReviewState(ctx, uid, noteID, req.ReviewState, req.Generation); err != nil {
			switch {
			case errors.Is(err, store.ErrNotFound):
				writeError(w, http.StatusNotFound, "not found")
			case errors.Is(err, store.ErrInvalidTransition):
				writeError(w, http.StatusUnprocessableEntity, "invalid state transition")
			case errors.Is(err, store.ErrGenerationMismatch):
				writeError(w, http.StatusConflict, "transcript changed, refetch and retry")
			default:
				writeError(w, http.StatusInternalServerError, "internal error")
			}
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
