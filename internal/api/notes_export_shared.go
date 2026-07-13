package api

import (
	"context"
	"errors"
	"strings"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/noteexport"
	"github.com/abedegno/muesli/internal/store"
)

type noteExportParts struct {
	segments        []model.Segment
	aliases         map[string]string
	summarySections []model.SummarySection
}

func (s *Server) loadNoteExportParts(ctx context.Context, ownerID string, note model.Note) (noteExportParts, error) {
	parts := noteExportParts{
		segments: []model.Segment{},
		aliases:  map[string]string{},
	}

	if tr, err := s.deps.Store.GetTranscript(ctx, note.ID); err == nil {
		parts.segments = tr.Segments
		aliases, err := s.deps.Store.SpeakerAliasMap(ctx, ownerID, note.ID)
		if err != nil {
			return noteExportParts{}, err
		}
		parts.aliases = aliases
	} else if !errors.Is(err, store.ErrNotFound) {
		return noteExportParts{}, err
	}

	if note.Status == model.NoteReady {
		summaries, err := s.deps.Store.GetSummaries(ctx, note.ID)
		if err != nil {
			return noteExportParts{}, err
		}
		for _, summary := range summaries {
			if summary.Status != model.SummaryReady {
				continue
			}
			parts.summarySections = append(parts.summarySections, summary.Sections...)
		}
	}

	return parts, nil
}

func renderNoteExport(note model.Note, parts noteExportParts, format string, opts noteexport.Options) ([]byte, string, string, error) {
	filename := noteexport.SlugifyFilename(note.Title)
	switch strings.ToLower(format) {
	case "txt":
		return []byte(noteexport.RenderNoteText(note, parts.summarySections, parts.segments, parts.aliases, opts)),
			"text/plain; charset=utf-8", filename + ".txt", nil
	case "docx":
		rendered, err := noteexport.RenderNoteDocx(note, parts.summarySections, parts.segments, parts.aliases, opts)
		if err != nil {
			return nil, "", "", err
		}
		return rendered,
			"application/vnd.openxmlformats-officedocument.wordprocessingml.document",
			filename + ".docx", nil
	case "pdf":
		rendered, err := noteexport.RenderNotePdf(note, parts.summarySections, parts.segments, parts.aliases, opts)
		if err != nil {
			return nil, "", "", err
		}
		return rendered, "application/pdf", filename + ".pdf", nil
	default:
		return []byte(noteexport.RenderNoteMarkdown(note, parts.summarySections, parts.segments, parts.aliases, opts)),
			"text/markdown; charset=utf-8", filename + ".md", nil
	}
}
