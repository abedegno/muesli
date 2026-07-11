package store_test

// This file is DB-backed and CI-only: testutil.NewPool skips when
// TEST_DATABASE_URL is unset, so it must not be run on the local CI
// runner.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/calendar"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
)

// noteEventTestBase is a fixed, deterministic instant used instead of the
// wall clock in these tests (see scripts/check-test-determinism.sh).
var noteEventTestBase = testutil.NewFakeClock(time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)).Now()

// seedCalendarEvent creates a calendar source + one event for ownerID and
// returns the event id.
func seedCalendarEvent(t *testing.T, st *store.Store, ownerID string) string {
	t.Helper()
	ctx := context.Background()
	src, err := st.CreateSource(ctx, ownerID, "ics", "Team Calendar", "sealed-creds")
	if err != nil {
		t.Fatalf("create source: %v", err)
	}
	if err := st.UpsertEvents(ctx, ownerID, src.ID, []calendar.NormalizedEvent{
		{
			ExternalID: "evt-1",
			Title:      "Standup",
			StartsAt:   noteEventTestBase,
			EndsAt:     noteEventTestBase.Add(30 * time.Minute),
		},
	}); err != nil {
		t.Fatalf("upsert events: %v", err)
	}
	evs, err := st.ListEvents(ctx, ownerID, noteEventTestBase.Add(-time.Hour), noteEventTestBase.Add(time.Hour))
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("expected 1 seeded event, got %d", len(evs))
	}
	return evs[0].ID
}

func TestSetAndClearNoteEvent(t *testing.T) {
	t.Parallel()
	pool := testutil.NewPool(t)
	st := store.New(pool)
	ctx := context.Background()

	u, err := st.CreateUser(ctx, "note-event-owner@example.com", "h")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	n, err := st.CreateNote(ctx, u.ID, "Meeting note")
	if err != nil {
		t.Fatalf("create note: %v", err)
	}
	if n.EventID != nil {
		t.Fatalf("expected new note to have nil EventID, got %v", n.EventID)
	}
	eventID := seedCalendarEvent(t, st, u.ID)

	if err := st.SetNoteEvent(ctx, u.ID, n.ID, eventID); err != nil {
		t.Fatalf("set note event: %v", err)
	}
	got, err := st.GetNote(ctx, u.ID, n.ID)
	if err != nil {
		t.Fatalf("get note: %v", err)
	}
	if got.EventID == nil || *got.EventID != eventID {
		t.Fatalf("expected note.EventID=%q, got %v", eventID, got.EventID)
	}

	if err := st.ClearNoteEvent(ctx, u.ID, n.ID); err != nil {
		t.Fatalf("clear note event: %v", err)
	}
	got, err = st.GetNote(ctx, u.ID, n.ID)
	if err != nil {
		t.Fatalf("get note after clear: %v", err)
	}
	if got.EventID != nil {
		t.Fatalf("expected EventID nil after clear, got %v", *got.EventID)
	}
}

func TestSetNoteEventNotFoundCases(t *testing.T) {
	t.Parallel()
	pool := testutil.NewPool(t)
	st := store.New(pool)
	ctx := context.Background()

	owner, err := st.CreateUser(ctx, "note-event-owner2@example.com", "h")
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	other, err := st.CreateUser(ctx, "note-event-other@example.com", "h")
	if err != nil {
		t.Fatalf("create other: %v", err)
	}
	n, err := st.CreateNote(ctx, owner.ID, "Meeting note")
	if err != nil {
		t.Fatalf("create note: %v", err)
	}
	eventID := seedCalendarEvent(t, st, owner.ID)

	// Event exists but belongs to a different owner.
	if err := st.SetNoteEvent(ctx, other.ID, n.ID, eventID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("set note event with foreign owner: got err %v, want ErrNotFound", err)
	}

	// Event id does not exist at all.
	if err := st.SetNoteEvent(ctx, owner.ID, n.ID, "00000000-0000-0000-0000-000000000000"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("set note event with missing event: got err %v, want ErrNotFound", err)
	}

	// Note does not belong to owner.
	if err := st.SetNoteEvent(ctx, other.ID, "00000000-0000-0000-0000-000000000000", eventID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("set note event with missing note: got err %v, want ErrNotFound", err)
	}

	// Clearing a note that doesn't belong to the owner.
	if err := st.ClearNoteEvent(ctx, other.ID, n.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("clear note event with foreign owner: got err %v, want ErrNotFound", err)
	}
}
