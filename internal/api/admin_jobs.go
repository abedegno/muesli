package api

import (
	"errors"
	"net/http"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
	"github.com/go-chi/chi/v5"
)

// handleListJobs lists jobs, optionally filtered by ?status=. Each entry carries
// id, note_id, type, status, attempts, and last_error for the admin monitor.
func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	jobs, err := s.deps.Store.ListJobs(r.Context(), status)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, jobs)
}

// handleRetryJob re-enqueues a failed job so it can be processed again.
func (s *Server) handleRetryJob(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "id")
	if !validID(w, r, jobID) {
		return
	}

	job, err := s.deps.Store.GetJob(r.Context(), jobID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "job not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if job.Status != model.JobFailed {
		writeError(w, http.StatusConflict, "job is not in a failed state")
		return
	}

	if _, err = s.deps.Store.GetNoteAdmin(r.Context(), job.NoteID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "note not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	payload := job.Payload
	if job.Type == model.JobTranscribe {
		// This re-enqueues a failed job's OWN payload verbatim by default, which
		// can carry a stale expected_generation the same way handleRetryNote's
		// did (e.g. the failed run itself partially saved a transcript before
		// failing on a later step). Refresh it to the note's current transcript
		// generation, the same way every other transcribe enqueue site does, so
		// the retry isn't rejected as stale against its own — or someone else's
		// newer — prior work.
		refreshed, rerr := refreshTranscribeExpectedGeneration(r.Context(), s.deps.Store, job.NoteID, job.Payload)
		if rerr != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		payload = refreshed
	}

	if _, err = s.deps.Store.EnqueueJob(r.Context(), job.NoteID, job.Type, payload); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]string{"status": "queued"})
}

// handleCancelJob cancels a job that is still pending. Running, done, and
// failed jobs are never touched (no preemption of in-flight or terminal work).
func (s *Server) handleCancelJob(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "id")
	if !validID(w, r, jobID) {
		return
	}

	job, err := s.deps.Store.GetJob(r.Context(), jobID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "job not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if job.Status != model.JobPending {
		writeError(w, http.StatusConflict, "job is not pending")
		return
	}

	if err := s.deps.Store.CancelJob(r.Context(), jobID); err != nil {
		if errors.Is(err, store.ErrInvalidTransition) {
			writeError(w, http.StatusConflict, "job is not pending")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

// handleListNoteJobs returns every job for a note (all statuses), ordered by
// created_at ASC so the pipeline stages (transcribe/summarize/embed) render in
// the order they were enqueued — the data backing the admin per-note timeline.
func (s *Server) handleListNoteJobs(w http.ResponseWriter, r *http.Request) {
	noteID := chi.URLParam(r, "id")
	if !validID(w, r, noteID) {
		return
	}

	if _, err := s.deps.Store.GetNoteAdmin(r.Context(), noteID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "note not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	jobs, err := s.deps.Store.ListJobsByNoteID(r.Context(), noteID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, jobs)
}

// handleProcessNextJob is the admin-scoped counterpart to handleProcessNextNote:
// it prioritizes a specific PENDING job (keyed by job id, matching the other
// admin job actions) by reusing store.BumpNoteJobPriority to bump every
// still-pending job on that job's note ahead of the rest of the queue, without
// touching anything already running/done/failed.
func (s *Server) handleProcessNextJob(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "id")
	if !validID(w, r, jobID) {
		return
	}

	job, err := s.deps.Store.GetJob(r.Context(), jobID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "job not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if job.Status != model.JobPending {
		writeError(w, http.StatusConflict, "job is not pending")
		return
	}

	if _, err = s.deps.Store.GetNoteAdmin(r.Context(), job.NoteID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "note not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	n, err := s.deps.Store.BumpNoteJobPriority(r.Context(), job.NoteID)
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
