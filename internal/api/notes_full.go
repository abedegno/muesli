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
// summaries) the desktop client polls. One of the three shared-readable
// routes (issue #12): GetReadableNoteFull returns a note the requester owns
// OR can read via a live shared folder membership (CanReadNote), with
// is_owner and folder_ids (filtered to what the requester can see)
// populated. The authorization check and every payload load (body,
// transcript, alias map, summaries) run inside one guarded transaction in
// the store layer, so an unshare between them can never expose content after
// access was revoked. Speaker labels in transcript segments are substituted
// with user-defined aliases at read time; the stored transcript_segments
// rows are never modified.
func (s *Server) handleGetNoteFull(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	noteID := chi.URLParam(r, "id")
	if !validNoteID(noteID) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	payload, err := s.deps.Store.GetReadableNoteFull(r.Context(), uid, noteID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	note := payload.Note
	if note.Tags == nil {
		note.Tags = []string{}
	}
	if note.FolderIDs == nil {
		note.FolderIDs = []string{}
	}

	resp := fullNoteResponse{Note: note, BodyMarkdown: payload.Body, Summaries: payload.Summaries}

	if payload.Transcript != nil {
		// Build speaker alias map and apply substitution to response segments only.
		// The transcript_segments table is never modified.
		segments := make([]model.Segment, len(payload.Transcript.Segments))
		copy(segments, payload.Transcript.Segments)
		if len(payload.AliasMap) > 0 {
			for i := range segments {
				if alias, ok := payload.AliasMap[segments[i].Speaker]; ok {
					segments[i].Speaker = alias
				}
			}
		}
		resp.Transcript = &transcriptView{Segments: segments}
	}

	writeJSON(w, http.StatusOK, resp)
}
