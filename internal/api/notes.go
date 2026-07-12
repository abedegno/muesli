package api

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/notelinks"
	"github.com/abedegno/muesli/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type createNoteRequest struct {
	Title string `json:"title"`
}

type retranscribeRequest struct {
	Model    string `json:"model"`
	Language string `json:"language"`
}

// validNoteStatuses is the set of accepted values for the ?status= query param.
var validNoteStatuses = map[string]bool{
	"recording":    true,
	"uploaded":     true,
	"transcribing": true,
	"summarizing":  true,
	"ready":        true,
	"failed":       true,
}

func decodeOptionalJSONBody(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
	return nil
}

func buildTranscribeJobPayload(audioKey, modelOverride, languageOverride string) (json.RawMessage, error) {
	payload := map[string]string{"audio_key": audioKey}
	if modelOverride != "" {
		payload["model"] = modelOverride
	}
	if languageOverride != "" {
		payload["language"] = languageOverride
	}
	return json.Marshal(payload)
}

func retranscribeConflictReason(note model.Note) (string, bool) {
	if note.AudioObjectKey == "" || note.RetentionState == "discarded" {
		return "no stored audio to retranscribe", true
	}
	if note.Status != model.NoteReady && note.Status != model.NoteFailed {
		return "note is not ready to retranscribe", true
	}
	return "", false
}

func (s *Server) handleCreateNote(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	var req createNoteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		writeError(w, http.StatusBadRequest, "title required")
		return
	}
	n, err := s.deps.Store.CreateNote(r.Context(), uid, title)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	n.Tags = []string{}
	n.FolderIDs = []string{}
	writeJSON(w, http.StatusCreated, n)
}

func (s *Server) handleDuplicateNote(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	id := chi.URLParam(r, "id")
	if !validNoteID(id) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	n, err := s.deps.Store.DuplicateNote(r.Context(), uid, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if n.Tags == nil {
		n.Tags = []string{}
	}
	if n.FolderIDs == nil {
		n.FolderIDs = []string{}
	}
	writeJSON(w, http.StatusCreated, n)
}

func (s *Server) handleGetNote(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	if !validNoteID(chi.URLParam(r, "id")) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	n, err := s.deps.Store.GetNote(r.Context(), uid, chi.URLParam(r, "id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if n.Tags == nil {
		n.Tags = []string{}
	}
	if n.FolderIDs == nil {
		n.FolderIDs = []string{}
	}
	writeJSON(w, http.StatusOK, n)
}

func (s *Server) handleListNotes(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())

	q := r.URL.Query()
	f := store.ListNotesFilter{
		Tag:    q.Get("tag"),
		Status: q.Get("status"),
	}

	// Validate status enum: only the six known values (or empty) are accepted.
	if f.Status != "" && !validNoteStatuses[f.Status] {
		writeError(w, http.StatusBadRequest, "invalid status")
		return
	}

	if folderIDStr := q.Get("folder_id"); folderIDStr != "" {
		if _, err := uuid.Parse(folderIDStr); err != nil {
			writeError(w, http.StatusBadRequest, "invalid folder_id")
			return
		}
		f.FolderID = folderIDStr
		f.FolderIDSet = true
	}

	notes, err := s.deps.Store.ListNotes(r.Context(), uid, f)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if notes == nil {
		notes = []model.Note{}
	}
	writeJSON(w, http.StatusOK, notes)
}

type updateNoteTitleRequest struct {
	Title string `json:"title"`
}

func (s *Server) handleUpdateNoteTitle(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	if !validNoteID(chi.URLParam(r, "id")) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	var req updateNoteTitleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		writeError(w, http.StatusBadRequest, "title required")
		return
	}
	noteID := chi.URLParam(r, "id")
	err := s.deps.Store.UpdateNoteTitle(r.Context(), uid, noteID, title)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	n, err := s.deps.Store.GetNote(r.Context(), uid, noteID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if n.Tags == nil {
		n.Tags = []string{}
	}
	if n.FolderIDs == nil {
		n.FolderIDs = []string{}
	}
	writeJSON(w, http.StatusOK, n)
}

type updateNoteBodyRequest struct {
	Content string `json:"content"`
}

func (s *Server) handleUpdateNoteBody(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	if !validNoteID(chi.URLParam(r, "id")) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	var req updateNoteBodyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	err := s.deps.Store.UpdateNoteBody(r.Context(), uid, chi.URLParam(r, "id"), req.Content)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	fromNoteID := chi.URLParam(r, "id")
	for _, title := range notelinks.ParseMentions(req.Content) {
		target, err := s.deps.Store.FindNoteByTitleCI(r.Context(), uid, title)
		if err != nil {
			continue
		}
		if target.ID == fromNoteID {
			continue
		}
		if _, err := s.deps.Store.AddLink(r.Context(), uid, fromNoteID, target.ID); err != nil && !errors.Is(err, store.ErrDuplicate) {
			continue
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handlePinNote(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	noteID := chi.URLParam(r, "id")
	if !validNoteID(noteID) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err := s.deps.Store.SetNotePinned(r.Context(), uid, noteID, true); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleUnpinNote(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	noteID := chi.URLParam(r, "id")
	if !validNoteID(noteID) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err := s.deps.Store.SetNotePinned(r.Context(), uid, noteID, false); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type setNoteEventRequest struct {
	EventID string `json:"event_id"`
}

// handleSetNoteEvent links a note to a calendar event. The note is verified
// to exist and belong to the caller first (404 if not); the event_id in the
// body is then verified to belong to the same owner via the store (400 if
// missing/not the owner's), mirroring handlePinNote/handleUnpinNote's
// errors.Is/writeError conventions.
func (s *Server) handleSetNoteEvent(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	noteID := chi.URLParam(r, "id")
	if !validNoteID(noteID) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	var req setNoteEventRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if !validNoteID(req.EventID) {
		writeError(w, http.StatusBadRequest, "invalid event_id")
		return
	}
	if _, err := s.deps.Store.GetNote(r.Context(), uid, noteID); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := s.deps.Store.SetNoteEvent(r.Context(), uid, noteID, req.EventID); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusBadRequest, "event not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleClearNoteEvent unlinks a note's calendar event, owner-scoped.
func (s *Server) handleClearNoteEvent(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	noteID := chi.URLParam(r, "id")
	if !validNoteID(noteID) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err := s.deps.Store.ClearNoteEvent(r.Context(), uid, noteID); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleResummarize re-generates a note's summaries with the owner's current
// templates: it deletes the existing summaries then fans out fresh summarize jobs
// (shared store path with the post-transcription pipeline). Requires a transcript.
func (s *Server) handleResummarize(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	id := chi.URLParam(r, "id")
	if !validNoteID(id) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	// Per-note in-flight guard (SUM03): reject a concurrent resummarize call
	// for the same note id before touching the store at all. Acquired first,
	// released via defer on every exit path (not-found, no-transcript,
	// store error, success, or client disconnect).
	if !s.resummarizeGuard.tryAcquire(id) {
		writeError(w, http.StatusConflict, "summarize already in progress")
		return
	}
	defer s.resummarizeGuard.release(id)
	if _, err := s.deps.Store.GetNote(r.Context(), uid, id); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
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
	if err := s.deps.Store.DeleteNoteSummaries(r.Context(), uid, id); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := s.deps.Store.EnqueueSummarizeJobs(r.Context(), uid, id); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "summarizing"})
}

// handleRetranscribe re-runs transcription on retained audio, optionally
// overriding the transcriber model and language. It returns 409 Conflict when
// the note has no retained audio or is not in a safe terminal state: only
// notes already marked ready or failed are eligible, while recording/uploaded/
// transcribing/summarizing all count as mid-pipeline and are rejected.
func (s *Server) handleRetranscribe(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	id := chi.URLParam(r, "id")
	if !validNoteID(id) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	// Per-note in-flight guard: reject a concurrent retranscribe call for the
	// same note id before touching the store. Acquired first, released via defer
	// on every exit path (not-found, conflict, decode error, store error, success,
	// or client disconnect).
	if !s.retranscribeGuard.tryAcquire(id) {
		writeError(w, http.StatusConflict, "retranscribe already in progress")
		return
	}
	defer s.retranscribeGuard.release(id)

	note, err := s.deps.Store.GetNote(r.Context(), uid, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	var req retranscribeRequest
	if err := decodeOptionalJSONBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}

	if reason, conflict := retranscribeConflictReason(note); conflict {
		writeError(w, http.StatusConflict, reason)
		return
	}

	payload, err := buildTranscribeJobPayload(note.AudioObjectKey, req.Model, req.Language)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if _, err := s.deps.Store.EnqueueJob(r.Context(), id, model.JobTranscribe, payload); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "transcribing"})
}

func (s *Server) handleDeleteNote(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	id := chi.URLParam(r, "id")
	if !validNoteID(id) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err := s.deps.Store.DeleteNote(r.Context(), uid, id); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "trashed"})
}

func (s *Server) handleListTrash(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	notes, err := s.deps.Store.ListTrash(r.Context(), uid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if notes == nil {
		notes = []model.Note{}
	}
	writeJSON(w, http.StatusOK, notes)
}

func (s *Server) handleRestoreNote(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	id := chi.URLParam(r, "id")
	if !validNoteID(id) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err := s.deps.Store.RestoreNote(r.Context(), uid, id); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "restored"})
}

func (s *Server) handlePurgeNote(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	id := chi.URLParam(r, "id")
	if !validNoteID(id) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	audioKey, err := s.deps.Store.PurgeNote(r.Context(), uid, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if audioKey != "" {
		if derr := s.deps.Storage.Delete(audioKey); derr != nil {
			slog.ErrorContext(r.Context(), "purge note: audio blob", "audio_key", audioKey, "error", derr)
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// handleRetryNote re-queues the pipeline for a note that failed processing.
// It finds the most recent failed job, enqueues a new copy, and resets the
// note status to "uploaded" so the worker picks it up.
func (s *Server) handleRetryNote(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	id := chi.URLParam(r, "id")
	if !validNoteID(id) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	note, err := s.deps.Store.GetNote(r.Context(), uid, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if note.Status != model.NoteFailed {
		writeError(w, http.StatusConflict, "note is not in failed state")
		return
	}
	job, err := s.deps.Store.GetLatestFailedJobByNoteID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no failed job found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := s.deps.Store.RetryNote(r.Context(), id, job.Type, job.Payload); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "queued"})
}

// handleProcessNextNote lets a user prioritize a note's queued pipeline work:
// it bumps the priority of the note's still-PENDING jobs so ClaimJob dequeues
// them ahead of every other pending job, without touching (or preempting)
// anything already running. Returns 409 if the note has no pending job to
// bump (nothing to prioritize).
func (s *Server) handleProcessNextNote(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	id := chi.URLParam(r, "id")
	if !validNoteID(id) {
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
	n, err := s.deps.Store.BumpNoteJobPriority(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if n == 0 {
		writeError(w, http.StatusConflict, "no pending job to prioritize")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "bumped", "jobs_bumped": n})
}
