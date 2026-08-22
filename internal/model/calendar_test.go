package model

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestAttendeeJSONRoundTrip(t *testing.T) {
	t.Parallel()

	value := Attendee{
		Email:    "jane@example.com",
		Name:     "Jane Example",
		Response: "accepted",
	}

	assertJSONRoundTrip(t, value, []string{"email", "name", "response"})
}

func TestCalendarSourceJSONRoundTrip(t *testing.T) {
	t.Parallel()

	lastSyncedAt := time.Date(2026, time.July, 11, 12, 30, 0, 0, time.UTC)
	value := CalendarSource{
		ID:          "source_1",
		OwnerID:     "owner_1",
		Kind:        "google",
		DisplayName: "Work calendar",
		SelectedCalendars: map[string]bool{
			"primary": true,
			"team":    false,
		},
		Status:       "connected",
		LastSyncedAt: &lastSyncedAt,
		CreatedAt:    time.Date(2026, time.July, 11, 8, 15, 0, 0, time.UTC),
	}

	assertJSONRoundTrip(t, value, []string{"id", "owner_id", "kind", "display_name", "selected_calendars", "status", "last_synced_at", "created_at"})
}

func TestCalendarEventJSONRoundTrip(t *testing.T) {
	t.Parallel()

	value := CalendarEvent{
		ID:              "event_1",
		OwnerID:         "owner_1",
		SourceID:        "source_1",
		ExternalID:      "external_1",
		Title:           "Planning",
		StartsAt:        time.Date(2026, time.July, 12, 9, 0, 0, 0, time.UTC),
		EndsAt:          time.Date(2026, time.July, 12, 10, 0, 0, 0, time.UTC),
		Description:     "Sprint planning",
		Location:        "Room 2",
		ConferencingURL: "https://meet.example.com/abc",
		Attendees: []Attendee{
			{
				Email:    "jane@example.com",
				Name:     "Jane Example",
				Response: "accepted",
			},
		},
		UpdatedAt: time.Date(2026, time.July, 11, 13, 0, 0, 0, time.UTC),
	}

	assertJSONRoundTrip(t, value, []string{"id", "owner_id", "source_id", "external_id", "title", "starts_at", "ends_at", "description", "location", "conferencing_url", "attendees", "updated_at"})
}

func TestNoteJSONRoundTrip(t *testing.T) {
	t.Parallel()

	startsAt := time.Date(2026, time.July, 12, 9, 0, 0, 0, time.UTC)
	endsAt := time.Date(2026, time.July, 12, 10, 0, 0, 0, time.UTC)
	lastSyncedAt := time.Date(2026, time.July, 11, 12, 30, 0, 0, time.UTC)

	value := Note{
		ID:                  "note_1",
		OwnerID:             "owner_1",
		Title:               "Weekly sync",
		Status:              NoteReady,
		Pinned:              true,
		StartedAt:           &startsAt,
		EndedAt:             &endsAt,
		AudioHash:           strPtr("audio-hash"),
		NormalizedAudioHash: strPtr("normalized-hash"),
		CreatedAt:           time.Date(2026, time.July, 11, 8, 15, 0, 0, time.UTC),
		UpdatedAt:           time.Date(2026, time.July, 11, 13, 0, 0, 0, time.UTC),
		DeletedAt:           &lastSyncedAt,
		Snippet:             "Discussed roadmap and risks.",
		Tags:                []string{"planning", "engineering"},
		FolderIDs:           []string{"folder_a", "folder_b"},
		PartialTranscript:   true,
		EventID:             strPtr("event_1"),
	}

	assertJSONRoundTrip(t, value, []string{"id", "owner_id", "title", "status", "pinned", "started_at", "ended_at", "audio_hash", "normalized_audio_hash", "created_at", "updated_at", "deleted_at", "snippet", "tags", "folder_ids", "partial_transcript", "event_id"})
}

func TestTranscriptJSONRoundTrip(t *testing.T) {
	t.Parallel()

	value := Transcript{
		ID:                "transcript_1",
		NoteID:            "note_1",
		TranscriberPlugin: "whisper",
		Model:             "large-v3",
		Segments: []Segment{
			{
				ID:      "seg_1",
				StartMS: 0,
				EndMS:   1500,
				Text:    "Hello world",
				Source:  "transcript",
				Speaker: "speaker_1",
				Words: []Word{
					{Text: "Hello", StartMS: 0, EndMS: 500},
					{Text: "world", StartMS: 500, EndMS: 1500},
				},
				Confidence: float64Ptr(0.98),
			},
		},
		ReviewState: "reviewed",
	}

	assertJSONRoundTrip(t, value, []string{"id", "note_id", "transcriber_plugin", "model", "segments", "review_state", "sealed", "generation"})
}

func TestCalendarZeroValues(t *testing.T) {
	t.Parallel()

	t.Run("calendar source nil map and pointer", func(t *testing.T) {
		t.Parallel()

		value := CalendarSource{}
		got := assertJSONRoundTrip(t, value, []string{"id", "owner_id", "kind", "display_name", "selected_calendars", "status", "last_synced_at", "created_at"})

		if got.SelectedCalendars != nil {
			t.Fatalf("SelectedCalendars = %#v, want nil", got.SelectedCalendars)
		}
		if got.LastSyncedAt != nil {
			t.Fatalf("LastSyncedAt = %#v, want nil", got.LastSyncedAt)
		}
	})

	t.Run("calendar source empty map stays empty", func(t *testing.T) {
		t.Parallel()

		value := CalendarSource{SelectedCalendars: map[string]bool{}}
		got := assertJSONRoundTrip(t, value, []string{"id", "owner_id", "kind", "display_name", "selected_calendars", "status", "last_synced_at", "created_at"})

		if got.SelectedCalendars == nil {
			t.Fatal("SelectedCalendars = nil, want non-nil empty map")
		}
		if len(got.SelectedCalendars) != 0 {
			t.Fatalf("SelectedCalendars len = %d, want 0", len(got.SelectedCalendars))
		}
	})

	t.Run("calendar event nil attendees", func(t *testing.T) {
		t.Parallel()

		got := assertJSONRoundTrip(t, CalendarEvent{}, []string{"id", "owner_id", "source_id", "external_id", "title", "starts_at", "ends_at", "description", "location", "conferencing_url", "attendees", "updated_at"})

		if got.Attendees != nil {
			t.Fatalf("Attendees = %#v, want nil", got.Attendees)
		}
	})

	t.Run("calendar event empty attendees stays empty", func(t *testing.T) {
		t.Parallel()

		got := assertJSONRoundTrip(t, CalendarEvent{Attendees: []Attendee{}}, []string{"id", "owner_id", "source_id", "external_id", "title", "starts_at", "ends_at", "description", "location", "conferencing_url", "attendees", "updated_at"})

		if got.Attendees == nil {
			t.Fatal("Attendees = nil, want non-nil empty slice")
		}
		if len(got.Attendees) != 0 {
			t.Fatalf("Attendees len = %d, want 0", len(got.Attendees))
		}
	})

	t.Run("note nil slices", func(t *testing.T) {
		t.Parallel()

		got := assertJSONRoundTrip(t, Note{}, []string{"id", "owner_id", "title", "status", "pinned", "created_at", "updated_at", "tags", "folder_ids", "partial_transcript"})

		if got.Tags != nil {
			t.Fatalf("Tags = %#v, want nil", got.Tags)
		}
		if got.FolderIDs != nil {
			t.Fatalf("FolderIDs = %#v, want nil", got.FolderIDs)
		}
	})

	t.Run("note empty slices stay empty", func(t *testing.T) {
		t.Parallel()

		got := assertJSONRoundTrip(t, Note{Tags: []string{}, FolderIDs: []string{}}, []string{"id", "owner_id", "title", "status", "pinned", "created_at", "updated_at", "tags", "folder_ids", "partial_transcript"})

		if got.Tags == nil {
			t.Fatal("Tags = nil, want non-nil empty slice")
		}
		if len(got.Tags) != 0 {
			t.Fatalf("Tags len = %d, want 0", len(got.Tags))
		}
		if got.FolderIDs == nil {
			t.Fatal("FolderIDs = nil, want non-nil empty slice")
		}
		if len(got.FolderIDs) != 0 {
			t.Fatalf("FolderIDs len = %d, want 0", len(got.FolderIDs))
		}
	})

	t.Run("transcript nil segments", func(t *testing.T) {
		t.Parallel()

		got := assertJSONRoundTrip(t, Transcript{}, []string{"id", "note_id", "transcriber_plugin", "model", "segments", "review_state", "sealed", "generation"})

		if got.Segments != nil {
			t.Fatalf("Segments = %#v, want nil", got.Segments)
		}
	})
}

func assertJSONRoundTrip[T any](t *testing.T, value T, wantKeys []string) T {
	t.Helper()

	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal(%T) failed: %v", value, err)
	}

	var keys map[string]json.RawMessage
	if err := json.Unmarshal(data, &keys); err != nil {
		t.Fatalf("json.Unmarshal(%T keys) failed: %v", value, err)
	}
	assertJSONKeys(t, keys, wantKeys)

	var got T
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal(%T) round-trip failed: %v", value, err)
	}

	if !reflect.DeepEqual(value, got) {
		t.Fatalf("round-trip mismatch\nwant: %#v\ngot:  %#v\njson: %s", value, got, data)
	}

	return got
}

func assertJSONKeys(t *testing.T, got map[string]json.RawMessage, want []string) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("json keys = %v, want %v", mapKeys(got), want)
	}

	for _, key := range want {
		if _, ok := got[key]; !ok {
			t.Fatalf("json keys = %v, missing %q", mapKeys(got), key)
		}
	}
}

func mapKeys(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	return keys
}

func strPtr(s string) *string {
	return &s
}

func float64Ptr(v float64) *float64 {
	return &v
}
