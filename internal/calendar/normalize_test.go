package calendar

import (
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/model"
)

func TestNormalizedEventZeroValue(t *testing.T) {
	var got NormalizedEvent

	if got.ExternalID != "" {
		t.Fatalf("ExternalID = %q, want empty string", got.ExternalID)
	}
	if got.Title != "" {
		t.Fatalf("Title = %q, want empty string", got.Title)
	}
	if !got.StartsAt.IsZero() {
		t.Fatalf("StartsAt = %v, want zero time", got.StartsAt)
	}
	if !got.EndsAt.IsZero() {
		t.Fatalf("EndsAt = %v, want zero time", got.EndsAt)
	}
	if got.Description != "" {
		t.Fatalf("Description = %q, want empty string", got.Description)
	}
	if got.Location != "" {
		t.Fatalf("Location = %q, want empty string", got.Location)
	}
	if got.ConferencingURL != "" {
		t.Fatalf("ConferencingURL = %q, want empty string", got.ConferencingURL)
	}
	if got.Attendees != nil {
		t.Fatalf("Attendees = %#v, want nil slice", got.Attendees)
	}
}

func TestNormalizedEventConstruction(t *testing.T) {
	startsAt := time.Date(2026, 7, 10, 15, 4, 5, 0, time.UTC)
	endsAt := startsAt.Add(45 * time.Minute)
	wantAttendees := []model.Attendee{
		{Email: "alice@example.com", Name: "Alice", Response: "accepted"},
	}

	got := NormalizedEvent{
		ExternalID:      "evt-1",
		Title:           "Standup",
		StartsAt:        startsAt,
		EndsAt:          endsAt,
		Description:     "Daily team sync",
		Location:        "Room 1",
		ConferencingURL: "https://meet.example.com/abc",
		Attendees:       wantAttendees,
	}

	if got.ExternalID != "evt-1" {
		t.Fatalf("ExternalID = %q, want %q", got.ExternalID, "evt-1")
	}
	if got.Title != "Standup" {
		t.Fatalf("Title = %q, want %q", got.Title, "Standup")
	}
	if !got.StartsAt.Equal(startsAt) {
		t.Fatalf("StartsAt = %v, want %v", got.StartsAt, startsAt)
	}
	if !got.EndsAt.Equal(endsAt) {
		t.Fatalf("EndsAt = %v, want %v", got.EndsAt, endsAt)
	}
	if got.Description != "Daily team sync" {
		t.Fatalf("Description = %q, want %q", got.Description, "Daily team sync")
	}
	if got.Location != "Room 1" {
		t.Fatalf("Location = %q, want %q", got.Location, "Room 1")
	}
	if got.ConferencingURL != "https://meet.example.com/abc" {
		t.Fatalf("ConferencingURL = %q, want %q", got.ConferencingURL, "https://meet.example.com/abc")
	}
	if len(got.Attendees) != len(wantAttendees) {
		t.Fatalf("Attendees length = %d, want %d", len(got.Attendees), len(wantAttendees))
	}
	if got.Attendees[0] != wantAttendees[0] {
		t.Fatalf("Attendees[0] = %#v, want %#v", got.Attendees[0], wantAttendees[0])
	}
}
