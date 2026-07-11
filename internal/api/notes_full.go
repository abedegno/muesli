package api

import (
	"errors"
	"net/http"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
	"github.com/go-chi/chi/v5"
)

type transcriptView struct {
	Segments []model.Segment `json:"segments"`
}

type fullNoteResponse struct {
	Note         model.Note      `json:"note"`
	BodyMarkdown string          `json:"body_markdown"`
	Transcript   *transcriptView `json:"transcript"`
	Summaries    []model.Summary `json:"summaries"`
}

// handleGetNoteFull returns the full note (metadata + body + transcript +
// summaries) the desktop client polls. Owner-scoped via GetNote.
// Speaker labels in transcript segments are substituted with user-defined
// aliases at read time; the stored transcript_segments rows are never modified.
func (s *Server) handleGetNoteFull(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	noteID := chi.URLParam(r, "id")
	if !validNoteID(noteID) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	note, err := s.deps.Store.GetNote(r.Context(), uid, noteID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	tags, err := s.deps.Store.NoteTags(r.Context(), noteID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	note.Tags = tags // NoteTags returns [] (non-nil) when empty

	folderIDs, err := s.deps.Store.NoteFolderIDs(r.Context(), noteID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	note.FolderIDs = folderIDs // NoteFolderIDs returns [] (non-nil) when empty

	body, err := s.deps.Store.NoteBody(r.Context(), noteID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	resp := fullNoteResponse{Note: note, BodyMarkdown: body, Summaries: []model.Summary{}}

	tr, err := s.deps.Store.GetTranscript(r.Context(), noteID)
	if err == nil {
		// Build speaker alias map and apply substitution to response segments only.
		// The transcript_segments table is never modified.
		aliasMap, aliasErr := s.deps.Store.SpeakerAliasMap(r.Context(), uid, noteID)
		if aliasErr != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		segments := make([]model.Segment, len(tr.Segments))
		copy(segments, tr.Segments)
		if len(aliasMap) > 0 {
			for i := range segments {
				if alias, ok := aliasMap[segments[i].Speaker]; ok {
					segments[i].Speaker = alias
				}
			}
		}
		resp.Transcript = &transcriptView{Segments: segments}
	} else if !errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	sums, err := s.deps.Store.GetSummaries(r.Context(), noteID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if sums != nil {
		resp.Summaries = sums
	}

	writeJSON(w, http.StatusOK, resp)
}
