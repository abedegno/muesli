package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// validNoteID reports whether id is a syntactically valid UUID. Handlers use it to
// return 404 (not 500) for malformed path ids before hitting the store.
func validNoteID(id string) bool {
	_, err := uuid.Parse(id)
	return err == nil
}

// validID validates id as a UUID; if invalid it writes a 404 and returns false.
// Use as: if !validID(w, r, id) { return }
func validID(w http.ResponseWriter, _ *http.Request, id string) bool {
	if !validNoteID(id) {
		writeError(w, http.StatusNotFound, "not found")
		return false
	}
	return true
}

// handleAudioUploadURL issues a presigned PUT URL scoped to this note's audio object.
func (s *Server) handleAudioUploadURL(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	noteID := chi.URLParam(r, "id")
	if !validNoteID(noteID) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	// Confirm the note exists and is owned by the caller before issuing a grant.
	if _, err := s.deps.Store.GetNote(r.Context(), uid, noteID); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	key := "notes/" + noteID + "/audio/" + uuid.NewString()
	grant, err := s.deps.Storage.PresignUpload(key, 15*time.Minute)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, grant)
}

type audioURLGrant struct {
	URL       string `json:"url"`
	ExpiresAt string `json:"expires_at"`
}

// handleAudioDownloadURL issues a presigned GET URL for this note's audio object.
func (s *Server) handleAudioDownloadURL(w http.ResponseWriter, r *http.Request) {
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
	if note.AudioObjectKey == "" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	const ttl = time.Hour
	url, err := s.deps.Storage.PresignDownload(note.AudioObjectKey, ttl)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, audioURLGrant{
		URL:       url,
		ExpiresAt: time.Now().Add(ttl).UTC().Format(time.RFC3339),
	})
}

type audioUploadedRequest struct {
	Key string `json:"key"`
}

// handleAudioUploaded verifies the uploaded object then advances note status.
func (s *Server) handleAudioUploaded(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	noteID := chi.URLParam(r, "id")
	if !validNoteID(noteID) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	var req audioUploadedRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Key == "" {
		writeError(w, http.StatusBadRequest, "key required")
		return
	}
	// The key must belong to this note (prevents pointing at someone else's object).
	if !isKeyForNote(req.Key, noteID) {
		writeError(w, http.StatusBadRequest, "key does not match note")
		return
	}
	exists, size, err := s.deps.Storage.Verify(req.Key)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !exists || size == 0 {
		writeError(w, http.StatusBadRequest, "object not found or empty")
		return
	}
	if err := s.deps.Store.SetNoteAudio(r.Context(), uid, noteID, req.Key); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Enqueue transcription. The worker pool (if running) picks it up. Carry the
	// transcript generation observed now so a stale job (e.g. superseded by a
	// live-stream finalize before the worker gets to it) is rejected rather than
	// overwriting a newer transcript.
	expectedGeneration, err := s.deps.Store.CurrentTranscriptGeneration(r.Context(), noteID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	payload, _ := buildTranscribeJobPayload(req.Key, "", "", expectedGeneration)
	if _, err := s.deps.Store.EnqueueJob(r.Context(), noteID, model.JobTranscribe, payload); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "uploaded"})
}

func isKeyForNote(key, noteID string) bool {
	prefix := "notes/" + noteID + "/audio/"
	return strings.HasPrefix(key, prefix) && len(key) > len(prefix) && !strings.Contains(key, "..")
}
