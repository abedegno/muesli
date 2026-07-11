package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/abedegno/muesli/internal/model"
)

type audioMatch struct {
	NoteID    string    `json:"note_id"`
	Title     string    `json:"title"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

func firstAudioMatch(notes []model.Note) (audioMatch, bool) {
	if len(notes) == 0 {
		return audioMatch{}, false
	}
	n := notes[0]
	return audioMatch{
		NoteID:    n.ID,
		Title:     n.Title,
		Status:    n.Status,
		CreatedAt: n.CreatedAt,
	}, true
}

// handleAudioDedupCheck is a read-only endpoint that checks whether any
// existing note for the authenticated user matches the supplied audio hash(es).
// At least one of audio_hash or normalized_audio_hash must be non-empty.
//
// POST /api/audio/dedup-check
// Body: {"audio_hash":"...","normalized_audio_hash":"..."}
// 200:  {"matches":[{"note_id":"...","title":"...","status":"...","created_at":"..."}]}
// 400:  when both hash fields are empty
func (s *Server) handleAudioDedupCheck(w http.ResponseWriter, r *http.Request) {
	ownerID, _ := userIDFromContext(r.Context())

	var req struct {
		AudioHash           string `json:"audio_hash"`
		NormalizedAudioHash string `json:"normalized_audio_hash"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if req.AudioHash == "" && req.NormalizedAudioHash == "" {
		writeError(w, http.StatusBadRequest, "at least one of audio_hash or normalized_audio_hash must be provided")
		return
	}

	notes, err := s.deps.Store.FindNotesByAudioHash(r.Context(), ownerID, req.AudioHash, req.NormalizedAudioHash)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	matches := make([]audioMatch, 0, len(notes))
	for _, n := range notes {
		matches = append(matches, audioMatch{
			NoteID:    n.ID,
			Title:     n.Title,
			Status:    n.Status,
			CreatedAt: n.CreatedAt,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"matches": matches})
}
