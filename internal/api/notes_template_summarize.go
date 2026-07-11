package api

import (
	"errors"
	"net/http"

	"github.com/abedegno/muesli/internal/store"
	"github.com/go-chi/chi/v5"
)

// handleSummarizeTemplate (re)generates a single template's summary for a note
// without touching the note's other summaries and without re-transcribing. It
// mirrors handleResummarize's error mapping, scoped to one (note, template) pair.
// Precedence matters here: invalid/malformed ids -> 404; note not found/not
// owned -> 404; template not found/not visible to owner -> 404; no transcript
// yet -> 409; success -> 202. The template-visibility check must run BEFORE
// the transcript check so an unknown/foreign templateID 404s even on a note
// that also has no transcript yet, rather than 409ing first.
func (s *Server) handleSummarizeTemplate(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	id := chi.URLParam(r, "id")
	templateID := chi.URLParam(r, "templateID")
	if !validNoteID(id) || !validNoteID(templateID) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if _, err := s.deps.Store.GetNote(r.Context(), uid, id); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	templates, err := s.deps.Store.TemplatesForSummary(r.Context(), uid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	visible := false
	for _, tmpl := range templates {
		if tmpl.ID == templateID {
			visible = true
			break
		}
	}
	if !visible {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	// Need a transcript to (re)summarize.
	if _, err := s.deps.Store.GetTranscript(r.Context(), id); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusConflict, "no transcript to summarize")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := s.deps.Store.EnqueueTemplateSummarizeJob(r.Context(), uid, id, templateID); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "summarizing"})
}
