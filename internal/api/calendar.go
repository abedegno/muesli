package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/abedegno/muesli/internal/crypto"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/worker"
	"github.com/go-chi/chi/v5"
)

// calendarSyncKickTimeout bounds each fire-and-forget background sync kicked
// off by these handlers (create/refresh), so one hung upstream fetch can
// never leak a goroutine forever.
const calendarSyncKickTimeout = 2 * time.Minute

// calendarSourceRequest is the request body for POST /api/calendar/sources.
// URL/User/Pass are plaintext on the wire only for the duration of this
// request; the handler seals them before they ever reach the store, and
// they are never echoed back in any response.
type calendarSourceRequest struct {
	Kind        string `json:"kind"`
	DisplayName string `json:"display_name"`
	URL         string `json:"url"`
	User        string `json:"user"`
	Pass        string `json:"pass"`
}

// calendarSelectRequest is the request body for
// POST /api/calendar/sources/{id}/select.
type calendarSelectRequest struct {
	Selected map[string]bool `json:"selected"`
}

// kickCalendarSync runs worker.SyncSource for sourceID in the background,
// logging and swallowing its error: the HTTP response has already gone out
// by the time this runs, so there is no one left to report the error to.
// It intentionally does NOT take the handler's request context - that
// context is cancelled the moment the handler returns, which would abort
// the sync before it ever starts. Instead it builds a fresh, bounded
// context of its own.
func kickCalendarSync(st *store.Store, cr *crypto.Crypto, googleClientID, googleClientSecret, microsoftClientID, microsoftClientSecret, sourceID string) {
	go func(sourceID string) {
		ctx, cancel := context.WithTimeout(context.Background(), calendarSyncKickTimeout)
		defer cancel()
		if err := worker.SyncSource(ctx, st, cr, googleClientID, googleClientSecret, microsoftClientID, microsoftClientSecret, sourceID); err != nil {
			log.Printf("calendar sync %s: %v", sourceID, err)
		}
	}(sourceID)
}

// handleListCalendarSources lists the caller's own calendar sources. The
// store's ListSources never selects the credentials column, so there is
// nothing to redact here.
func (s *Server) handleListCalendarSources(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	sources, err := s.deps.Store.ListSources(r.Context(), uid)
	if err != nil {
		log.Printf("handleListCalendarSources: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, sources)
}

// handleCreateCalendarSource creates a new ICS or CalDAV calendar source,
// sealing its credentials before they ever reach the store, then kicks an
// async sync so the caller doesn't wait on the upstream fetch.
func (s *Server) handleCreateCalendarSource(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	var req calendarSourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.Kind != "ics" && req.Kind != "caldav" {
		writeError(w, http.StatusBadRequest, "invalid kind")
		return
	}
	if req.URL == "" {
		writeError(w, http.StatusBadRequest, "url is required")
		return
	}

	var (
		plaintext []byte
		err       error
	)
	switch req.Kind {
	case "ics":
		plaintext, err = json.Marshal(map[string]string{"url": req.URL})
	case "caldav":
		plaintext, err = json.Marshal(map[string]string{"url": req.URL, "user": req.User, "pass": req.Pass})
	}
	if err != nil {
		log.Printf("handleCreateCalendarSource: marshal creds: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	sealed, err := s.deps.Crypto.Seal(plaintext)
	if err != nil {
		log.Printf("handleCreateCalendarSource: seal creds: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	src, err := s.deps.Store.CreateSource(r.Context(), uid, req.Kind, req.DisplayName, sealed)
	if err != nil {
		log.Printf("handleCreateCalendarSource: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	kickCalendarSync(s.deps.Store, s.deps.Crypto, s.deps.Config.GoogleOAuthClientID, s.deps.Config.GoogleOAuthClientSecret, s.deps.Config.MicrosoftOAuthClientID, s.deps.Config.MicrosoftOAuthClientSecret, src.ID)

	writeJSON(w, http.StatusCreated, src)
}

// handleSelectCalendarSource updates which of a source's upstream calendars
// are selected for sync.
func (s *Server) handleSelectCalendarSource(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	id := chi.URLParam(r, "id")
	if !validID(w, r, id) {
		return
	}
	var req calendarSelectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	err := s.deps.Store.SetSelectedCalendars(r.Context(), uid, id, req.Selected)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		log.Printf("handleSelectCalendarSource: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleDeleteCalendarSource deletes a calendar source (and, via the
// store's cascading FK, its events).
func (s *Server) handleDeleteCalendarSource(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	id := chi.URLParam(r, "id")
	if !validID(w, r, id) {
		return
	}
	err := s.deps.Store.DeleteSource(r.Context(), uid, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		log.Printf("handleDeleteCalendarSource: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// handleRefreshCalendar kicks an async re-sync of every one of the calling
// user's own calendar sources (owner-scoped via ListSources - this is a
// user-triggered refresh, not the background scheduler's
// ListAllSourceIDs sweep across every owner). It responds immediately;
// each source syncs independently so one bad source can't block or fail
// the others.
func (s *Server) handleRefreshCalendar(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	sources, err := s.deps.Store.ListSources(r.Context(), uid)
	if err != nil {
		log.Printf("handleRefreshCalendar: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	for _, src := range sources {
		kickCalendarSync(s.deps.Store, s.deps.Crypto, s.deps.Config.GoogleOAuthClientID, s.deps.Config.GoogleOAuthClientSecret, s.deps.Config.MicrosoftOAuthClientID, s.deps.Config.MicrosoftOAuthClientSecret, src.ID)
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}

// handleListCalendarEvents lists the caller's own calendar events within
// [from, to], defaulting to a 7-day window starting now when the
// respective query param is absent or empty.
func (s *Server) handleListCalendarEvents(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())

	from := time.Now()
	if raw := r.URL.Query().Get("from"); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid from")
			return
		}
		from = parsed
	}

	to := from.Add(7 * 24 * time.Hour)
	if raw := r.URL.Query().Get("to"); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid to")
			return
		}
		to = parsed
	}

	events, err := s.deps.Store.ListEvents(r.Context(), uid, from, to)
	if err != nil {
		log.Printf("handleListCalendarEvents: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, events)
}
