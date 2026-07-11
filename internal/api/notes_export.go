package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/abedegno/muesli/internal/model"
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

	note, err := s.deps.Store.GetNote(r.Context(), uid, noteID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	segments := []model.Segment{}
	aliases := map[string]string{}
	if tr, err := s.deps.Store.GetTranscript(r.Context(), noteID); err == nil {
		segments = tr.Segments
		aliases, err = s.deps.Store.SpeakerAliasMap(r.Context(), uid, noteID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
	} else if !errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	summarySections := []model.SummarySection{}
	if note.Status == model.NoteReady {
		summaries, err := s.deps.Store.GetSummaries(r.Context(), noteID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		for _, summary := range summaries {
			if summary.Status != model.SummaryReady {
				continue
			}
			summarySections = append(summarySections, summary.Sections...)
		}
	}

	filename := noteexport.SlugifyFilename(note.Title)
	var (
		contentType string
		rendered    []byte
	)
	switch format {
	case "txt":
		filename += ".txt"
		contentType = "text/plain; charset=utf-8"
		rendered = []byte(noteexport.RenderNoteText(note, summarySections, segments, aliases))
	case "docx":
		filename += ".docx"
		contentType = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
		rendered, err = noteexport.RenderNoteDocx(note, summarySections, segments, aliases)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
	case "pdf":
		filename += ".pdf"
		contentType = "application/pdf"
		rendered, err = noteexport.RenderNotePdf(note, summarySections, segments, aliases)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
	default:
		filename += ".md"
		contentType = "text/markdown; charset=utf-8"
		rendered = []byte(noteexport.RenderNoteMarkdown(note, summarySections, segments, aliases))
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(rendered)
}
