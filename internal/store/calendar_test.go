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
	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
	"github.com/jackc/pgx/v5/pgxpool"
)

func newCalendarStoreWithOwner(t *testing.T) (*store.Store, string, *pgxpool.Pool) {
	t.Helper()
	pool := testutil.NewPool(t)
	st := store.New(pool)
	ctx := context.Background()
	u, err := st.CreateUser(ctx, "calendar-owner@example.com", "h")
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	return st, u.ID, pool
}

func TestCalendarStoreSourcesAndEvents(t *testing.T) {
	st, ownerID, _ := newCalendarStoreWithOwner(t)
	ctx := context.Background()

	src, err := st.CreateSource(ctx, ownerID, "ics", "Team Calendar", "sealed-creds")
	if err != nil {
		t.Fatalf("create source: %v", err)
	}
	if src.ID == "" || src.OwnerID != ownerID || src.Kind != "ics" || src.DisplayName != "Team Calendar" {
		t.Fatalf("unexpected source: %+v", src)
	}
	if _, ok := src.SelectedCalendars["missing"]; ok {
		t.Fatalf("selected calendars should start empty: %+v", src.SelectedCalendars)
	}

	sources, err := st.ListSources(ctx, ownerID)
	if err != nil {
		t.Fatalf("list sources: %v", err)
	}
	if len(sources) != 1 || sources[0].ID != src.ID {
		t.Fatalf("unexpected source list: %+v", sources)
	}
	if sources[0].Kind != "ics" || sources[0].Status != "ok" {
		t.Fatalf("unexpected listed source: %+v", sources[0])
	}

	kind, sealed, owner, err := st.GetSourceCreds(ctx, src.ID)
	if err != nil {
		t.Fatalf("get source creds: %v", err)
	}
	if kind != "ics" || sealed != "sealed-creds" || owner != ownerID {
		t.Fatalf("unexpected creds lookup: kind=%q sealed=%q owner=%q", kind, sealed, owner)
	}

	selected := map[string]bool{"work": true, "personal": false}
	if err := st.SetSelectedCalendars(ctx, ownerID, src.ID, selected); err != nil {
		t.Fatalf("set selected calendars: %v", err)
	}
	sources, err = st.ListSources(ctx, ownerID)
	if err != nil {
		t.Fatalf("relist sources: %v", err)
	}
	if got := sources[0].SelectedCalendars; len(got) != len(selected) || !got["work"] || got["personal"] {
		t.Fatalf("selected calendars mismatch: %+v", got)
	}

	from := time.Date(2026, 7, 9, 9, 0, 0, 0, time.UTC)
	to := time.Date(2026, 7, 9, 11, 0, 0, 0, time.UTC)
	evs := []calendar.NormalizedEvent{
		{
			ExternalID:      "evt-1",
			Title:           "Standup",
			StartsAt:        time.Date(2026, 7, 9, 9, 30, 0, 0, time.UTC),
			EndsAt:          time.Date(2026, 7, 9, 10, 0, 0, 0, time.UTC),
			Description:     "daily",
			Location:        "Room A",
			ConferencingURL: "https://meet.google.com/abc-defg-hij",
			Attendees:       []model.Attendee{},
		},
		{
			ExternalID:      "evt-2",
			Title:           "Retro",
			StartsAt:        time.Date(2026, 7, 9, 10, 30, 0, 0, time.UTC),
			EndsAt:          time.Date(2026, 7, 9, 11, 30, 0, 0, time.UTC),
			Description:     "retro",
			Location:        "Room B",
			ConferencingURL: "",
		},
	}
	if err := st.UpsertEvents(ctx, ownerID, src.ID, evs); err != nil {
		t.Fatalf("upsert events: %v", err)
	}

	listed, err := st.ListEvents(ctx, ownerID, from, to)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(listed) != 2 {
		t.Fatalf("want 2 events, got %d: %+v", len(listed), listed)
	}
	if listed[0].ExternalID != "evt-1" || listed[1].ExternalID != "evt-2" {
		t.Fatalf("unexpected ordering: %+v", listed)
	}

	if err := st.PruneEvents(ctx, src.ID, calendar.DiffExternalIDs([]calendar.NormalizedEvent{evs[1]})); err != nil {
		t.Fatalf("prune events: %v", err)
	}
	listed, err = st.ListEvents(ctx, ownerID, from, to)
	if err != nil {
		t.Fatalf("list after prune: %v", err)
	}
	if len(listed) != 1 || listed[0].ExternalID != "evt-2" {
		t.Fatalf("expected only evt-2 after prune, got %+v", listed)
	}

	if err := st.DeleteSource(ctx, ownerID, src.ID); err != nil {
		t.Fatalf("delete source: %v", err)
	}
	afterDelete, err := st.ListEvents(ctx, ownerID, from, to)
	if err != nil {
		t.Fatalf("list after delete source: %v", err)
	}
	if len(afterDelete) != 0 {
		t.Fatalf("expected no events after source delete, got %+v", afterDelete)
	}
	sources, err = st.ListSources(ctx, ownerID)
	if err != nil {
		t.Fatalf("list sources after delete: %v", err)
	}
	if len(sources) != 0 {
		t.Fatalf("expected no sources after delete, got %+v", sources)
	}

	if err := st.DeleteSource(ctx, ownerID, src.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("delete missing source: got %v, want ErrNotFound", err)
	}
}
