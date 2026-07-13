package api

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/abedegno/muesli/internal/exportutil"
	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/noteexport"
	"github.com/abedegno/muesli/internal/store"
)

type batchExportRequest struct {
	FolderID          *string  `json:"folder_id"`
	NoteIDs           []string `json:"note_ids"`
	Format            string   `json:"format"`
	IncludeTranscript *bool    `json:"include_transcript"`
	RedactSpeakers    bool     `json:"redact_speakers"`
}

func (s *Server) handleBatchExport(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())

	var req batchExportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}

	format := strings.ToLower(strings.TrimSpace(req.Format))
	if format != "md" && format != "txt" && format != "docx" && format != "pdf" {
		writeError(w, http.StatusBadRequest, "invalid format")
		return
	}

	includeTranscript := true
	if req.IncludeTranscript != nil {
		includeTranscript = *req.IncludeTranscript
	}

	hasFolder := req.FolderID != nil
	hasNotes := req.NoteIDs != nil
	if hasFolder == hasNotes {
		writeError(w, http.StatusBadRequest, "folder_id or note_ids required")
		return
	}

	var (
		notes       []model.Note
		archiveName string
	)

	if hasFolder {
		folderID := strings.TrimSpace(*req.FolderID)
		if folderID == "" {
			writeError(w, http.StatusBadRequest, "folder_id required")
			return
		}
		if !validNoteID(folderID) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}

		folder, err := s.deps.Store.GetFolder(r.Context(), uid, folderID)
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		} else if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}

		notes, err = s.deps.Store.ListNotes(r.Context(), uid, store.ListNotesFilter{
			FolderID:    folderID,
			FolderIDSet: true,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		archiveName = noteexport.SlugifyFilename(folder.Name) + "-export.zip"
	}

	if hasNotes {
		if len(req.NoteIDs) == 0 {
			writeError(w, http.StatusBadRequest, "note_ids required")
			return
		}
		notes = make([]model.Note, 0, len(req.NoteIDs))
		for _, noteID := range req.NoteIDs {
			noteID = strings.TrimSpace(noteID)
			if noteID == "" {
				writeError(w, http.StatusBadRequest, "note_ids required")
				return
			}
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
			notes = append(notes, note)
		}
		archiveName = "selected-notes-export.zip"
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	nameCounts := map[string]int{}
	for _, note := range notes {
		parts, err := s.loadNoteExportParts(r.Context(), uid, note)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		rendered, _, filename, err := renderNoteExport(note, parts, format, noteexport.Options{
			IncludeTranscript: includeTranscript,
			RedactSpeakers:    req.RedactSpeakers,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		entryName := exportutil.DedupeFilename(filename, nameCounts)
		entry, err := zw.Create(entryName)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if _, err := entry.Write(rendered); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}
	if err := zw.Close(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", archiveName))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}
