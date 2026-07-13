package api

import (
	"errors"
	"net/http"

	"github.com/abedegno/muesli/internal/store"
	"github.com/go-chi/chi/v5"
)

// RelatedNoteMatch is one embedding-similarity hit returned by
// GET /api/notes/{id}/related.
type RelatedNoteMatch struct {
	NoteID string  `json:"note_id"`
	Score  float64 `json:"score"`
}

const relatedNotesDefaultLimit = 5

func (s *Server) handleRelatedNotes(w http.ResponseWriter, r *http.Request) {
	uid, ok := userIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	noteID := chi.URLParam(r, "id")
	if !validNoteID(noteID) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if s.deps.Embedder == nil {
		writeJSON(w, http.StatusOK, []RelatedNoteMatch{})
		return
	}

	hits, err := s.deps.Store.RelatedNotes(r.Context(), uid, noteID, s.deps.Config.EmbeddingsModel, s.deps.Embedder.Dim(), relatedNotesDefaultLimit)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not found")
	case err != nil:
		writeError(w, http.StatusInternalServerError, "internal error")
	default:
		out := make([]RelatedNoteMatch, 0, len(hits))
		for _, hit := range hits {
			out = append(out, RelatedNoteMatch{
				NoteID: hit.ID,
				Score:  hit.Score,
			})
		}
		writeJSON(w, http.StatusOK, out)
	}
}
