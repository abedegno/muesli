package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
	"github.com/go-chi/chi/v5"
)

// Live-prompt SSE timers (issue #764's production values). Variables rather
// than constants so the package's own tests can shorten them and drive the
// heartbeat and renewal paths within milliseconds, without sleeping; see
// live_prompts_release_test.go.
var (
	liveSubscriberRenewInterval = 20 * time.Second
	liveSubscriberLeaseTTL      = 60 * time.Second
	liveHeartbeatInterval       = 15 * time.Second
)

// liveStore is the store surface handleLiveNotePrompts uses from admission
// onward. Production uses *store.Store; the package's own tests wrap it to
// force a failure after the lease is acquired and prove the lease is still
// released (the resource-release requirement: failure paths, not only the
// happy path).
type liveStore interface {
	AcquireLiveNoteSubscription(ctx context.Context, noteID, ownerID string, ttl time.Duration) (string, bool, error)
	RenewLiveNoteSubscription(ctx context.Context, connectionID string, ttl time.Duration) (bool, error)
	ReleaseLiveNoteSubscription(ctx context.Context, connectionID string) error
	LiveNoteSnapshot(ctx context.Context, ownerID, noteID string) (store.LiveNoteSnapshot, error)
}

func (s *Server) liveStoreOrDefault() liveStore {
	if s.liveStoreOverride != nil {
		return s.liveStoreOverride
	}
	return s.deps.Store
}

// liveNoteHub fans out PostgreSQL note-ID notifications to every local
// handler subscribed to that note, via a one-item coalescing mailbox per
// handler (issue #764). It is process-local; cross-process capacity is
// enforced entirely by the database (see AcquireLiveNoteSubscription).
type liveNoteHub struct {
	mu   sync.Mutex
	subs map[string]map[int]chan struct{}
	next int
}

func newLiveNoteHub() *liveNoteHub {
	return &liveNoteHub{subs: map[string]map[int]chan struct{}{}}
}

// register returns a one-item mailbox for noteID and an unregister func.
func (h *liveNoteHub) register(noteID string) (chan struct{}, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	id := h.next
	h.next++
	ch := make(chan struct{}, 1)
	if h.subs[noteID] == nil {
		h.subs[noteID] = map[int]chan struct{}{}
	}
	h.subs[noteID][id] = ch
	return ch, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		delete(h.subs[noteID], id)
		if len(h.subs[noteID]) == 0 {
			delete(h.subs, noteID)
		}
	}
}

// wake non-blockingly signals every mailbox registered for noteID. A full
// mailbox (already holding one pending wake) is left alone -- the pending
// wake's own reread is authoritative and already covers this notification.
func (h *liveNoteHub) wake(noteID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, ch := range h.subs[noteID] {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// liveHub lazily starts this process's note-update listener and hub the
// first time any live-prompts connection is served, so every one of the
// package's many pre-existing api.NewServer() call sites in tests never
// opens a database LISTEN connection unless they actually exercise this
// endpoint.
func (s *Server) liveHubInstance() *liveNoteHub {
	s.liveOnce.Do(func() {
		s.liveHubField = newLiveNoteHub()
		if s.deps.Store != nil && s.deps.Store.Pool() != nil {
			hub := s.liveHubField
			s.liveMu.Lock()
			defer s.liveMu.Unlock()
			if !s.liveClosed {
				s.liveListener = store.NewLiveNoteListener(s.deps.Store.Pool(), hub.wake)
			}
		}
	})
	return s.liveHubField
}

// Close releases the process-wide resources the server acquired lazily:
// currently the live-prompts LISTEN connection, which otherwise stays
// acquired from the pool for the life of the process and blocks
// pgxpool.Close forever (the store's pool cannot close while a connection is
// checked out). Run calls it on return; tests call it before their pool's
// cleanup. Safe to call when nothing was started, and idempotent.
func (s *Server) Close() {
	s.liveMu.Lock()
	l := s.liveListener
	s.liveListener = nil
	s.liveClosed = true
	s.liveMu.Unlock()
	if l != nil {
		l.Close()
	}
}

// liveSnapshotItem is one template's entry in the SSE snapshot/update
// payload -- the accepted spec's "ID/name, status, row event_version,
// rendered/desired revisions, sections, and safe error code".
type liveSnapshotItem struct {
	TemplateID       string                 `json:"template_id"`
	TemplateName     string                 `json:"template_name"`
	StreamID         string                 `json:"stream_id"`
	Status           string                 `json:"status"`
	EventVersion     int                    `json:"event_version"`
	RenderedRevision int                    `json:"rendered_revision"`
	DesiredRevision  int                    `json:"desired_revision"`
	Sections         []model.SummarySection `json:"sections"`
	ErrorCode        string                 `json:"error_code,omitempty"`
}

type liveSnapshotEvent struct {
	NoteID   string             `json:"note_id"`
	StreamID string             `json:"stream_id,omitempty"`
	Active   bool               `json:"active"`
	Items    []liveSnapshotItem `json:"items"`
}

type liveUpdateEvent struct {
	Item liveSnapshotItem `json:"item"`
}

type liveEndedEvent struct {
	TemplateID string `json:"template_id"`
	StreamID   string `json:"stream_id"`
}

func toLiveSnapshotItem(o model.LiveTemplateOutput) liveSnapshotItem {
	return liveSnapshotItem{
		TemplateID: o.TemplateID, TemplateName: o.TemplateName, StreamID: o.StreamID,
		Status: o.Status, EventVersion: o.EventVersion, RenderedRevision: o.RenderedRevision,
		DesiredRevision: o.DesiredRevision, Sections: o.Sections, ErrorCode: o.ErrorCode,
	}
}

type liveOutputKey struct{ streamID, templateID string }

func writeSSE(w http.ResponseWriter, flusher http.Flusher, event string, payload any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

// handleLiveNotePrompts serves GET /api/notes/{id}/live-prompts: an
// authenticated text/event-stream of live in-meeting prompt state (issue
// #764). Ownership/readability is checked before capacity allocation.
// Missing, malformed, foreign, deleted, or trashed notes are indistinguishable
// 404s.
func (s *Server) handleLiveNotePrompts(w http.ResponseWriter, r *http.Request) {
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
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	ls := s.liveStoreOrDefault()
	connID, admitted, err := ls.AcquireLiveNoteSubscription(r.Context(), noteID, note.OwnerID, liveSubscriberLeaseTTL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !admitted {
		writeError(w, http.StatusTooManyRequests, "too many subscribers")
		return
	}
	defer func() {
		if err := ls.ReleaseLiveNoteSubscription(context.Background(), connID); err != nil {
			slog.Error("live prompts: release subscription failed", "error", err, "note_id", noteID)
		}
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	hub := s.liveHubInstance()
	mailbox, unregister := hub.register(noteID)
	defer unregister()

	sent := map[liveOutputKey]int{}

	// endAll tells the viewer every card it holds is gone. Used when the note
	// stops being readable after admission (trashed or deleted): the accepted
	// spec ends the stream on trash, and a viewer must not keep displaying
	// prompts for a note nobody can read.
	endAll := func() error {
		for key := range sent {
			if err := writeSSE(w, flusher, "ended", liveEndedEvent{TemplateID: key.templateID, StreamID: key.streamID}); err != nil {
				return err
			}
			delete(sent, key)
		}
		return nil
	}

	reread := func() error {
		snap, err := ls.LiveNoteSnapshot(r.Context(), note.OwnerID, noteID)
		if errors.Is(err, store.ErrNotFound) {
			_ = endAll()
			return err // closes the stream; the deferred release frees the lease
		}
		if err != nil {
			return err
		}
		current := map[liveOutputKey]model.LiveTemplateOutput{}
		for _, row := range snap.Items {
			current[liveOutputKey{row.StreamID, row.TemplateID}] = row
		}
		for key := range sent {
			if _, ok := current[key]; !ok {
				if err := writeSSE(w, flusher, "ended", liveEndedEvent{TemplateID: key.templateID, StreamID: key.streamID}); err != nil {
					return err
				}
				delete(sent, key)
			}
		}
		for key, row := range current {
			if v, ok := sent[key]; !ok || v != row.EventVersion {
				if err := writeSSE(w, flusher, "update", liveUpdateEvent{Item: toLiveSnapshotItem(row)}); err != nil {
					return err
				}
			}
		}
		newSent := make(map[liveOutputKey]int, len(current))
		for key, row := range current {
			newSent[key] = row.EventVersion
		}
		sent = newSent
		return nil
	}

	snap, err := ls.LiveNoteSnapshot(r.Context(), note.OwnerID, noteID)
	if err != nil {
		return // a note trashed between admission and here has nothing to show; the lease is released
	}
	items := make([]liveSnapshotItem, 0, len(snap.Items))
	for _, row := range snap.Items {
		items = append(items, toLiveSnapshotItem(row))
		sent[liveOutputKey{row.StreamID, row.TemplateID}] = row.EventVersion
	}
	if err := writeSSE(w, flusher, "snapshot", liveSnapshotEvent{NoteID: noteID, StreamID: snap.StreamID, Active: snap.Active, Items: items}); err != nil {
		return
	}

	renewTicker := time.NewTicker(liveSubscriberRenewInterval)
	defer renewTicker.Stop()
	heartbeatTicker := time.NewTicker(liveHeartbeatInterval)
	defer heartbeatTicker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-mailbox:
			if err := reread(); err != nil {
				return
			}
		case <-heartbeatTicker.C:
			if err := reread(); err != nil {
				return
			}
			if err := writeSSE(w, flusher, "heartbeat", map[string]string{}); err != nil {
				return
			}
		case <-renewTicker.C:
			ok, err := ls.RenewLiveNoteSubscription(r.Context(), connID, liveSubscriberLeaseTTL)
			if err != nil || !ok {
				// Renewal cannot complete before expiry (or the lease is
				// already gone): close the stream rather than serve without
				// capacity (see the accepted spec).
				return
			}
		}
	}
}
