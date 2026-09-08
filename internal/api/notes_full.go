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
// summaries) the desktop client polls. Viewer-readable (issue #12): an
// explicit folder member may open a note directly assigned to a live
// folder they are granted on, exactly like handleGetNote. Every component
// is fetched through its own readable method, which reauthorizes within its
// own query rather than trusting the initial GetReadableNote call -- so a
// revocation or deletion racing this handler yields either a complete
// pre-change response or an empty 404, never a partial one. Owners retain
// speaker-alias substitution; viewers see stored speaker labels as-is and
// this handler never calls SpeakerAliasMap for them. The stored
// transcript_segments rows are never modified either way.
func (s *Server) handleGetNoteFull(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	noteID := chi.URLParam(r, "id")
	if !validNoteID(noteID) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	note, err := s.deps.Store.GetReadableNote(r.Context(), uid, noteID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	isOwner := note.OwnerID == uid

	tags, err := s.deps.Store.GetReadableNoteTags(r.Context(), uid, noteID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	note.Tags = tags

	// Never the unscoped NoteFolderIDs: only independently visible live
	// folders are ever projected onto a readable response.
	folderIDs, err := s.deps.Store.ReadableNoteFolderIDs(r.Context(), uid, noteID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	note.FolderIDs = folderIDs

	body, err := s.deps.Store.GetReadableNoteBody(r.Context(), uid, noteID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	resp := fullNoteResponse{Note: note, BodyMarkdown: body, Summaries: []model.Summary{}}

	tr, err := s.deps.Store.GetReadableTranscript(r.Context(), uid, noteID)
	if err == nil {
		segments := make([]model.Segment, len(tr.Segments))
		copy(segments, tr.Segments)
		if isOwner {
			// Build speaker alias map and apply substitution to response segments
			// only. The transcript_segments table is never modified. Viewers never
			// reach this branch, so they never call SpeakerAliasMap.
			aliasMap, aliasErr := s.deps.Store.SpeakerAliasMap(r.Context(), uid, noteID)
			if aliasErr != nil {
				writeError(w, http.StatusInternalServerError, "internal error")
				return
			}
			if len(aliasMap) > 0 {
				for i := range segments {
					if alias, ok := aliasMap[segments[i].Speaker]; ok {
						segments[i].Speaker = alias
					}
				}
			}
		}
		resp.Transcript = &transcriptView{Segments: segments}
	} else if !errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	// A GetReadableTranscript ErrNotFound is ambiguous between "no transcript
	// yet" (the common case) and a mid-request revocation; GetTranscript
	// already collapsed those before this feature, so this preserves existing
	// behaviour rather than introducing a new distinction here.

	sums, err := s.deps.Store.GetReadableSummaries(r.Context(), uid, noteID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if sums != nil {
		resp.Summaries = sums
	}

	writeJSON(w, http.StatusOK, resp)
}
