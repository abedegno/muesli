package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
	"github.com/go-chi/chi/v5"
)

// sharedNoteResponse is the public read-only payload for a token-scoped share.
// Date uses StartedAt when present, otherwise CreatedAt, so the response stays
// stable even when a note has not been explicitly started yet.
type sharedNoteResponse struct {
	Title      string                    `json:"title"`
	Date       time.Time                 `json:"date"`
	Transcript *sharedTranscriptResponse `json:"transcript,omitempty"`
	Summary    *sharedSummaryResponse    `json:"summary,omitempty"`
}

type sharedTranscriptResponse struct {
	Segments []sharedTranscriptSegment `json:"segments"`
}

type sharedTranscriptSegment struct {
	StartMS    int          `json:"start_ms"`
	EndMS      int          `json:"end_ms"`
	Text       string       `json:"text"`
	Source     string       `json:"source"`
	Speaker    string       `json:"speaker,omitempty"`
	Words      []model.Word `json:"words,omitempty"`
	Confidence *float64     `json:"confidence,omitempty"`
}

// sharedSummaryResponse carries the ready summary sections for the share.
// The handler returns the first ready summary from the store's stable ordering
// and omits summary entirely if none are ready.
type sharedSummaryResponse struct {
	Sections []model.SummarySection `json:"sections"`
}

func copySharedTranscriptSegments(segments []model.Segment) []sharedTranscriptSegment {
	out := make([]sharedTranscriptSegment, 0, len(segments))
	for _, seg := range segments {
		out = append(out, sharedTranscriptSegment{
			StartMS:    seg.StartMS,
			EndMS:      seg.EndMS,
			Text:       seg.Text,
			Source:     seg.Source,
			Speaker:    seg.Speaker,
			Words:      append([]model.Word(nil), seg.Words...),
			Confidence: seg.Confidence,
		})
	}
	return out
}

func (s *Server) handleGetSharedNote(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	share, err := s.deps.Store.GetActiveShare(r.Context(), token)
	if err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	note, err := s.deps.Store.GetNoteByID(r.Context(), share.NoteID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	resp := sharedNoteResponse{
		Title: note.Title,
		Date:  note.CreatedAt,
	}
	if note.StartedAt != nil {
		resp.Date = *note.StartedAt
	}

	if tr, err := s.deps.Store.GetTranscript(r.Context(), share.NoteID); err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
	} else {
		resp.Transcript = &sharedTranscriptResponse{
			Segments: copySharedTranscriptSegments(tr.Segments),
		}
	}

	summaries, err := s.deps.Store.GetSummaries(r.Context(), share.NoteID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	for _, summary := range summaries {
		if summary.Status != model.SummaryReady {
			continue
		}
		resp.Summary = &sharedSummaryResponse{
			Sections: append([]model.SummarySection(nil), summary.Sections...),
		}
		break
	}

	writeJSON(w, http.StatusOK, resp)
}
