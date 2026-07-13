package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/abedegno/muesli/internal/noteexport"
	"github.com/abedegno/muesli/internal/store"
	"github.com/go-chi/chi/v5"
)

// handleGetNoteExport returns a note as a downloadable attachment.
// It shares the same owner-scoped note lookup and 404 behavior as /full.
func (s *Server) handleGetNoteExport(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	noteID := chi.URLParam(r, "id")
	if !validNoteID(noteID) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	if format == "" {
		format = "md"
	}
	if format != "md" && format != "txt" && format != "docx" && format != "pdf" {
		writeError(w, http.StatusBadRequest, "invalid format")
		return
	}
	opts := noteexport.Options{
		IncludeTranscript: parseIncludeTranscriptQuery(r.URL.Query().Get("include_transcript")),
		RedactSpeakers:    parseRedactSpeakersQuery(r.URL.Query().Get("redact_speakers")),
	}

	note, err := s.deps.Store.GetNote(r.Context(), uid, noteID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	parts, err := s.loadNoteExportParts(r.Context(), uid, note)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	rendered, contentType, filename, err := renderNoteExport(note, parts, format, opts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(rendered)
}

func parseIncludeTranscriptQuery(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "false", "0":
		return false
	default:
		return true
	}
}

func parseRedactSpeakersQuery(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1":
		return true
	default:
		return false
	}
}
