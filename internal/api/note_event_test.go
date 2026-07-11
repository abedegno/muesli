package api_test

// This file is DB-backed and CI-only: testutil.NewPool (via newTestServer)
// skips when TEST_DATABASE_URL is unset, so it must not be run on the local
// CI runner.

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/calendar"
	"github.com/abedegno/muesli/internal/testutil"
)

// noteEventHandlerTestBase is a fixed, deterministic instant used instead
// of the wall clock in this file (see scripts/check-test-determinism.sh).
var noteEventHandlerTestBase = testutil.NewFakeClock(time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)).Now()

func TestSetAndClearNoteEventHandlers(t *testing.T) {
	t.Parallel()
	srv, st := newCalendarTestServer(t)
	hdr := calendarAuthHeader(t, srv, "note-event-owner@example.com")

	// Look up the owner id via a fresh note create (owner_id is on the note).
	rec := doJSON(t, srv, http.MethodPost, "/api/notes", map[string]string{"title": "Standup notes"}, hdr)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create note status %d body %s", rec.Code, rec.Body)
	}
	var note struct {
		ID      string `json:"id"`
		OwnerID string `json:"owner_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &note); err != nil {
		t.Fatalf("unmarshal note: %v", err)
	}

	ctx := context.Background()
	src, err := st.CreateSource(ctx, note.OwnerID, "ics", "Team Calendar", "sealed-creds")
	if err != nil {
		t.Fatalf("create source: %v", err)
	}
	if err := st.UpsertEvents(ctx, note.OwnerID, src.ID, []calendar.NormalizedEvent{
		{ExternalID: "evt-1", Title: "Standup", StartsAt: noteEventHandlerTestBase, EndsAt: noteEventHandlerTestBase.Add(30 * time.Minute)},
	}); err != nil {
		t.Fatalf("upsert events: %v", err)
	}
	evs, err := st.ListEvents(ctx, note.OwnerID, noteEventHandlerTestBase.Add(-time.Hour), noteEventHandlerTestBase.Add(time.Hour))
	if err != nil || len(evs) != 1 {
		t.Fatalf("list events: %v (%d)", err, len(evs))
	}
	eventID := evs[0].ID

	// Link.
	rec = doJSON(t, srv, http.MethodPost, "/api/notes/"+note.ID+"/event",
		map[string]string{"event_id": eventID}, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("set note event status %d body %s", rec.Code, rec.Body)
	}

	rec = doJSON(t, srv, http.MethodGet, "/api/notes/"+note.ID, nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("get note status %d", rec.Code)
	}
	var got struct {
		EventID *string `json:"event_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.EventID == nil || *got.EventID != eventID {
		t.Fatalf("expected event_id=%q, got %v", eventID, got.EventID)
	}

	// Unlink.
	rec = doJSON(t, srv, http.MethodDelete, "/api/notes/"+note.ID+"/event", nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("clear note event status %d body %s", rec.Code, rec.Body)
	}
	rec = doJSON(t, srv, http.MethodGet, "/api/notes/"+note.ID, nil, hdr)
	got.EventID = nil
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.EventID != nil {
		t.Fatalf("expected event_id nil after clear, got %v", *got.EventID)
	}

	// 404: note does not exist / not owner's.
	otherHdr := calendarAuthHeaderForOtherUser(t, st, "note-event-other@example.com")
	rec = doJSON(t, srv, http.MethodPost, "/api/notes/"+note.ID+"/event",
		map[string]string{"event_id": eventID}, otherHdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("set note event on foreign note status %d, want 404", rec.Code)
	}
	rec = doJSON(t, srv, http.MethodDelete, "/api/notes/"+note.ID+"/event", nil, otherHdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("clear note event on foreign note status %d, want 404", rec.Code)
	}

	// 400: event missing / not the owner's, but note exists and is owned.
	rec = doJSON(t, srv, http.MethodPost, "/api/notes/"+note.ID+"/event",
		map[string]string{"event_id": "00000000-0000-0000-0000-000000000000"}, hdr)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("set note event with missing event status %d, want 400", rec.Code)
	}

	// 400: malformed event_id.
	rec = doJSON(t, srv, http.MethodPost, "/api/notes/"+note.ID+"/event",
		map[string]string{"event_id": "not-a-uuid"}, hdr)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("set note event with malformed event_id status %d, want 400", rec.Code)
	}

	// 404: malformed note id in path.
	rec = doJSON(t, srv, http.MethodPost, "/api/notes/not-a-uuid/event",
		map[string]string{"event_id": eventID}, hdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("set note event with malformed note id status %d, want 404", rec.Code)
	}
}
