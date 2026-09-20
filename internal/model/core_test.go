package model

import (
	"testing"
	"time"
)

func TestNoteCoreJSONRoundTrip(t *testing.T) {
	t.Parallel()

	startedAt := time.Date(2026, time.July, 12, 9, 0, 0, 0, time.UTC)
	endedAt := time.Date(2026, time.July, 12, 10, 0, 0, 0, time.UTC)
	deletedAt := time.Date(2026, time.July, 12, 11, 0, 0, 0, time.UTC)
	eventID := "event_1"
	audioHash := "audio-hash"
	normalizedAudioHash := "normalized-audio-hash"

	cases := []struct {
		name     string
		value    Note
		wantKeys []string
	}{
		{
			name: "fully populated",
			value: Note{
				ID:                  "note_1",
				OwnerID:             "owner_1",
				Title:               "Weekly sync",
				Status:              NoteReady,
				Pinned:              true,
				StartedAt:           &startedAt,
				EndedAt:             &endedAt,
				AudioHash:           &audioHash,
				NormalizedAudioHash: &normalizedAudioHash,
				CreatedAt:           time.Date(2026, time.July, 11, 8, 15, 0, 0, time.UTC),
				UpdatedAt:           time.Date(2026, time.July, 11, 13, 0, 0, 0, time.UTC),
				DeletedAt:           &deletedAt,
				Snippet:             "Discussed roadmap and risks.",
				Tags:                []string{"planning", "engineering"},
				FolderIDs:           []string{"folder_a", "folder_b"},
				PartialTranscript:   true,
				EventID:             &eventID,
			},
			wantKeys: []string{
				"id",
				"owner_id",
				"title",
				"status",
				"pinned",
				"started_at",
				"ended_at",
				"audio_hash",
				"normalized_audio_hash",
				"created_at",
				"updated_at",
				"deleted_at",
				"snippet",
				"tags",
				"folder_ids",
				"partial_transcript",
				"event_id",
			},
		},
		{
			name:     "zero omitted optional",
			value:    Note{},
			wantKeys: []string{"id", "owner_id", "title", "status", "pinned", "created_at", "updated_at", "tags", "folder_ids", "partial_transcript"},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assertJSONRoundTrip(t, tc.value, tc.wantKeys)
		})
	}
}

func TestNoteLinkCoreJSONRoundTrip(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, time.July, 12, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name     string
		value    NoteLink
		wantKeys []string
	}{
		{
			name: "fully populated",
			value: NoteLink{
				ID:         "link_1",
				OwnerID:    "owner_1",
				FromNoteID: "note_a",
				ToNoteID:   "note_b",
				CreatedAt:  createdAt,
			},
			wantKeys: []string{"id", "owner_id", "from_note_id", "to_note_id", "created_at"},
		},
		{
			name:     "zero value",
			value:    NoteLink{},
			wantKeys: []string{"id", "owner_id", "from_note_id", "to_note_id", "created_at"},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assertJSONRoundTrip(t, tc.value, tc.wantKeys)
		})
	}
}

func TestSegmentCoreJSONRoundTrip(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		value    Segment
		wantKeys []string
	}{
		{
			name: "fully populated",
			value: Segment{
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
			wantKeys: []string{"id", "start_ms", "end_ms", "text", "source", "speaker", "words", "confidence", "provisional"},
		},
		{
			name:     "zero omitted optional",
			value:    Segment{},
			wantKeys: []string{"start_ms", "end_ms", "text", "source", "provisional"},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assertJSONRoundTrip(t, tc.value, tc.wantKeys)
		})
	}
}

func TestTranscriptCoreJSONRoundTrip(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		value    Transcript
		wantKeys []string
	}{
		{
			name: "fully populated",
			value: Transcript{
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
			},
			// sealed and generation are always on the wire (no omitempty) — the
			// live-continuity design deliberately exposes them, not internal-only.
			wantKeys: []string{"id", "note_id", "transcriber_plugin", "model", "segments", "review_state", "sealed", "generation"},
		},
		{
			name:  "zero omitted optional",
			value: Transcript{},
			// sealed and generation are always on the wire (no omitempty) — the
			// live-continuity design deliberately exposes them, not internal-only.
			wantKeys: []string{"id", "note_id", "transcriber_plugin", "model", "segments", "review_state", "sealed", "generation"},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assertJSONRoundTrip(t, tc.value, tc.wantKeys)
		})
	}
}

func TestCrossAnalysisNoteLimits(t *testing.T) {
	t.Parallel()
	if CrossAnalysisMinNotes != 2 {
		t.Fatalf("CrossAnalysisMinNotes = %d, want 2", CrossAnalysisMinNotes)
	}
	if CrossAnalysisMaxNotes != 40 {
		t.Fatalf("CrossAnalysisMaxNotes = %d, want 40", CrossAnalysisMaxNotes)
	}
}

func TestMessageSourceJSONRoundTrip(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		value    MessageSource
		wantKeys []string
	}{
		{
			name: "fully populated with note id",
			value: MessageSource{
				N:                    1,
				NoteID:               strPtr("note_1"),
				TranscriptGeneration: 3,
				SegmentIndex:         7,
				Timestamp:            65000,
				Snippet:              "We agreed to ship next week.",
			},
			wantKeys: []string{"n", "note_id", "transcript_generation", "segment_index", "timestamp", "snippet"},
		},
		{
			name: "note id explicitly null (deleted note)",
			value: MessageSource{
				N:                    2,
				NoteID:               nil,
				TranscriptGeneration: 1,
				SegmentIndex:         0,
				Timestamp:            0,
				Snippet:              "Historical snippet survives.",
			},
			// note_id is NOT omitempty: a deleted note's citation must still
			// serialize an explicit JSON null, distinguishable from a missing
			// field, so the renderer can render it unavailable/non-clickable.
			wantKeys: []string{"n", "note_id", "transcript_generation", "segment_index", "timestamp", "snippet"},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertJSONRoundTrip(t, tc.value, tc.wantKeys)
		})
	}
}

func TestMessageSourcesOnMessage(t *testing.T) {
	t.Parallel()

	t.Run("nil sources omitted from JSON", func(t *testing.T) {
		t.Parallel()
		msg := Message{
			ID:             "msg_1",
			ConversationID: "conv_1",
			Role:           "assistant",
			Content:        "Hello",
			Model:          "gpt-test",
			CreatedAt:      time.Date(2026, time.July, 12, 9, 0, 0, 0, time.UTC),
		}
		assertJSONRoundTrip(t, msg, []string{"id", "conversation_id", "role", "content", "model", "created_at"})
	})

	t.Run("populated sources round-trip in template order", func(t *testing.T) {
		t.Parallel()
		msg := Message{
			ID:             "msg_2",
			ConversationID: "conv_1",
			Role:           "assistant",
			Content:        "Across both meetings [1][2] the team agreed to ship.",
			Model:          "gpt-test",
			CreatedAt:      time.Date(2026, time.July, 12, 9, 0, 0, 0, time.UTC),
			Sources: []MessageSource{
				{N: 1, NoteID: strPtr("note_1"), TranscriptGeneration: 1, SegmentIndex: 0, Timestamp: 0, Snippet: "Meeting one decision."},
				{N: 2, NoteID: strPtr("note_2"), TranscriptGeneration: 2, SegmentIndex: 4, Timestamp: 12000, Snippet: "Meeting two decision."},
			},
		}
		got := assertJSONRoundTrip(t, msg, []string{"id", "conversation_id", "role", "content", "model", "created_at", "sources"})
		if len(got.Sources) != 2 || got.Sources[0].N != 1 || got.Sources[1].N != 2 {
			t.Fatalf("Sources round-trip mismatch: %+v", got.Sources)
		}
	})
}
