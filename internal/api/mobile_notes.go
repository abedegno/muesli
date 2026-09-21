package api

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/abedegno/muesli/internal/store"
	"github.com/go-chi/chi/v5"
)

// Mobile API (issue #767): a field-minimized, bounded, keyset-paginated
// notes contract for the native iOS client. Both the desktop loopback
// listener and the additional private iOS TLS listener (internal/api's
// listener_controller) register these same handlers against the same
// router — there is exactly one implementation of the contract.

const (
	mobileNotesDefaultLimit = 30
	mobileNotesMaxLimit     = 100
	// mobileCursorVersion is bumped only if the cursor's encoded shape ever
	// changes incompatibly; any other value in a decoded cursor is rejected
	// as invalid rather than guessed at.
	mobileCursorVersion = 1
)

// mobileNoteItem is the field-minimized list-item DTO. Arrays are always
// non-null; owner id, folders, event/audio metadata, transcripts, and
// summary bodies are never included.
type mobileNoteItem struct {
	ID        string     `json:"id"`
	Title     string     `json:"title"`
	Status    string     `json:"status"`
	Pinned    bool       `json:"pinned"`
	StartedAt *time.Time `json:"started_at,omitempty"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	Snippet   string     `json:"snippet"`
	Tags      []string   `json:"tags"`
}

type mobileNotesListResponse struct {
	Items      []mobileNoteItem `json:"items"`
	NextCursor string           `json:"next_cursor,omitempty"`
}

// mobileCursorPayload is the JSON shape encoded/decoded inside the opaque,
// versioned, base64url cursor. It carries the exact ordering tuple of the
// last row of the previous page.
type mobileCursorPayload struct {
	V         int       `json:"v"`
	Pinned    bool      `json:"p"`
	CreatedAt time.Time `json:"c"`
	ID        string    `json:"i"`
}

func encodeMobileCursor(pinned bool, createdAt time.Time, id string) string {
	payload := mobileCursorPayload{V: mobileCursorVersion, Pinned: pinned, CreatedAt: createdAt, ID: id}
	raw, err := json.Marshal(payload)
	if err != nil {
		// Marshaling a struct of concrete, always-valid fields cannot fail;
		// this defends future fields rather than a case reachable today.
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

// decodeMobileCursor validates and decodes an opaque cursor string into a
// store.MobileNoteCursor. It rejects anything malformed, wrong-versioned, or
// missing a required field — never partially trusting a client-supplied
// value.
func decodeMobileCursor(raw string) (*store.MobileNoteCursor, error) {
	if raw == "" {
		return nil, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, errInvalidCursor
	}
	var payload mobileCursorPayload
	if err := json.Unmarshal(decoded, &payload); err != nil {
		return nil, errInvalidCursor
	}
	if payload.V != mobileCursorVersion {
		return nil, errInvalidCursor
	}
	if payload.ID == "" || payload.CreatedAt.IsZero() {
		return nil, errInvalidCursor
	}
	return &store.MobileNoteCursor{Pinned: payload.Pinned, CreatedAt: payload.CreatedAt, ID: payload.ID}, nil
}

var errInvalidCursor = errors.New("invalid cursor")

// parseMobileLimit validates the ?limit= query parameter per the accepted
// spec: defaults to 30, accepts integers 1-100 inclusive, anything else is a
// 400.
func parseMobileLimit(raw string) (int, bool) {
	if raw == "" {
		return mobileNotesDefaultLimit, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > mobileNotesMaxLimit {
		return 0, false
	}
	return n, true
}

func toMobileNoteItem(n store.MobileNote) mobileNoteItem {
	tags := n.Tags
	if tags == nil {
		tags = []string{}
	}
	return mobileNoteItem{
		ID:        n.ID,
		Title:     n.Title,
		Status:    n.Status,
		Pinned:    n.Pinned,
		StartedAt: n.StartedAt,
		EndedAt:   n.EndedAt,
		CreatedAt: n.CreatedAt,
		UpdatedAt: n.UpdatedAt,
		Snippet:   n.Snippet,
		Tags:      tags,
	}
}

// handleListMobileNotes implements GET /api/mobile/v1/notes?limit=&cursor=.
func (s *Server) handleListMobileNotes(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())

	limit, ok := parseMobileLimit(r.URL.Query().Get("limit"))
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid limit")
		return
	}

	cursor, err := decodeMobileCursor(r.URL.Query().Get("cursor"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid cursor")
		return
	}

	notes, hasMore, err := s.deps.Store.ListMobileNotes(r.Context(), uid, limit, cursor)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	items := make([]mobileNoteItem, len(notes))
	for i, n := range notes {
		items[i] = toMobileNoteItem(n)
	}
	resp := mobileNotesListResponse{Items: items}
	if resp.Items == nil {
		resp.Items = []mobileNoteItem{}
	}
	if hasMore && len(notes) > 0 {
		last := notes[len(notes)-1]
		resp.NextCursor = encodeMobileCursor(last.Pinned, last.CreatedAt, last.ID)
	}
	writeJSON(w, http.StatusOK, resp)
}

// mobileNoteDetailNote is the note-metadata shape for the detail response:
// the list item shape minus snippet (per the accepted spec).
type mobileNoteDetailNote struct {
	ID        string     `json:"id"`
	Title     string     `json:"title"`
	Status    string     `json:"status"`
	Pinned    bool       `json:"pinned"`
	StartedAt *time.Time `json:"started_at,omitempty"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	Tags      []string   `json:"tags"`
}

type mobileSummarySectionDTO struct {
	Heading         string `json:"heading"`
	ContentMarkdown string `json:"content_markdown"`
}

type mobileSummaryDTO struct {
	ID           string                    `json:"id"`
	TemplateName string                    `json:"template_name"`
	Status       string                    `json:"status"`
	Truncated    bool                      `json:"truncated"`
	Sections     []mobileSummarySectionDTO `json:"sections"`
}

type mobileNoteDetailResponse struct {
	Note         mobileNoteDetailNote `json:"note"`
	BodyMarkdown string               `json:"body_markdown"`
	Summaries    []mobileSummaryDTO   `json:"summaries"`
}

// handleGetMobileNoteDetail implements GET /api/mobile/v1/notes/{id}.
func (s *Server) handleGetMobileNoteDetail(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	id := chi.URLParam(r, "id")
	// Invalid UUID, deleted, absent, and other-owner notes must all be
	// indistinguishable 404s (issue #767) — validate the UUID shape before
	// ever touching the store, exactly like the desktop note routes.
	if !validID(w, r, id) {
		return
	}

	detail, err := s.deps.Store.GetMobileNoteDetail(r.Context(), uid, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	tags := detail.Note.Tags
	if tags == nil {
		tags = []string{}
	}
	summaries := make([]mobileSummaryDTO, len(detail.Summaries))
	for i, sm := range detail.Summaries {
		sections := make([]mobileSummarySectionDTO, len(sm.Sections))
		for j, sec := range sm.Sections {
			sections[j] = mobileSummarySectionDTO{Heading: sec.Heading, ContentMarkdown: sec.ContentMarkdown}
		}
		if sections == nil {
			sections = []mobileSummarySectionDTO{}
		}
		summaries[i] = mobileSummaryDTO{
			ID:           sm.ID,
			TemplateName: sm.TemplateName,
			Status:       sm.Status,
			Truncated:    sm.Truncated,
			Sections:     sections,
		}
	}
	if summaries == nil {
		summaries = []mobileSummaryDTO{}
	}

	resp := mobileNoteDetailResponse{
		Note: mobileNoteDetailNote{
			ID:        detail.Note.ID,
			Title:     detail.Note.Title,
			Status:    detail.Note.Status,
			Pinned:    detail.Note.Pinned,
			StartedAt: detail.Note.StartedAt,
			EndedAt:   detail.Note.EndedAt,
			CreatedAt: detail.Note.CreatedAt,
			UpdatedAt: detail.Note.UpdatedAt,
			Tags:      tags,
		},
		BodyMarkdown: detail.BodyMarkdown,
		Summaries:    summaries,
	}
	writeJSON(w, http.StatusOK, resp)
}
