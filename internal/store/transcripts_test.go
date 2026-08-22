package store_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
)

func TestTranscriptStore(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	noteID := seedNote(t, st)

	tr := model.Transcript{
		NoteID:            noteID,
		TranscriberPlugin: "whisper",
		Model:             "base",
		Segments: []model.Segment{
			{StartMS: 0, EndMS: 1000, Text: "hello", Source: "mic"},
			{StartMS: 1000, EndMS: 2000, Text: "world", Source: "system", Speaker: "spk1"},
		},
	}
	saved, err := st.SaveTranscript(ctx, tr, 0)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if saved.ID == "" || len(saved.Segments) != 2 {
		t.Fatalf("unexpected saved %+v", saved)
	}
	if saved.Segments[0].ID == "" {
		t.Fatal("segments should get ids")
	}

	got, err := st.GetTranscript(ctx, noteID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got.Segments) != 2 || got.Segments[1].Speaker != "spk1" {
		t.Fatalf("unexpected transcript %+v", got)
	}

	// No transcript for a different note.
	if _, err := st.GetTranscript(ctx, seedNote(t, st)); err != store.ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestSetNoteStatusAndGetByID(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	noteID := seedNote(t, st)

	if err := st.SetNoteStatus(ctx, noteID, model.NoteTranscribing); err != nil {
		t.Fatalf("set status: %v", err)
	}
	n, err := st.GetNoteByID(ctx, noteID)
	if err != nil || n.Status != model.NoteTranscribing {
		t.Fatalf("got %+v err=%v", n, err)
	}
	if n.AudioObjectKey == "" {
		// seedNote does not set audio; AudioObjectKey is empty — acceptable.
	}
}

// TestTranscriptWordsRoundTrip verifies that Word-level timing is stored and
// retrieved correctly via the JSONB words column, and that transcripts without
// words are unaffected (backward compat).
func TestTranscriptWordsRoundTrip(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()

	// --- sub-test: round-trip with words ---
	t.Run("with_words", func(t *testing.T) {
		noteID := seedNote(t, st)
		tr := model.Transcript{
			NoteID:            noteID,
			TranscriberPlugin: "whisper",
			Model:             "base",
			Segments: []model.Segment{
				{
					StartMS: 0,
					EndMS:   700,
					Text:    "hello world",
					Source:  "mixed",
					Words: []model.Word{
						{Text: "hello", StartMS: 0, EndMS: 300},
						{Text: "world", StartMS: 350, EndMS: 700},
					},
				},
			},
		}
		saved, err := st.SaveTranscript(ctx, tr, 0)
		if err != nil {
			t.Fatalf("save: %v", err)
		}
		if len(saved.Segments[0].Words) != 2 {
			t.Fatalf("saved segment should have 2 words, got %d", len(saved.Segments[0].Words))
		}

		got, err := st.GetTranscript(ctx, noteID)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if len(got.Segments) != 1 {
			t.Fatalf("want 1 segment, got %d", len(got.Segments))
		}
		seg := got.Segments[0]
		if len(seg.Words) != 2 {
			t.Fatalf("want 2 words, got %d", len(seg.Words))
		}
		if seg.Words[0].Text != "hello" || seg.Words[0].StartMS != 0 || seg.Words[0].EndMS != 300 {
			t.Errorf("word 0: got %+v", seg.Words[0])
		}
		if seg.Words[1].Text != "world" || seg.Words[1].StartMS != 350 || seg.Words[1].EndMS != 700 {
			t.Errorf("word 1: got %+v", seg.Words[1])
		}
	})

	// --- sub-test: backward compat (no words) ---
	t.Run("no_words", func(t *testing.T) {
		noteID := seedNote(t, st)
		tr := model.Transcript{
			NoteID:            noteID,
			TranscriberPlugin: "whisper",
			Model:             "base",
			Segments: []model.Segment{
				{StartMS: 0, EndMS: 1000, Text: "hello", Source: "mic"},
			},
		}
		saved, err := st.SaveTranscript(ctx, tr, 0)
		if err != nil {
			t.Fatalf("save: %v", err)
		}
		if saved.Segments[0].Words != nil {
			t.Fatalf("expected nil Words, got %v", saved.Segments[0].Words)
		}

		got, err := st.GetTranscript(ctx, noteID)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if got.Segments[0].Words != nil {
			t.Errorf("expected nil Words on round-trip, got %v", got.Segments[0].Words)
		}
	})
}

// floatPtr returns a pointer to f; used in confidence round-trip tests.
func floatPtr(f float64) *float64 { v := f; return &v }

// TestTranscriptConfidenceRoundTrip verifies that the optional per-segment
// confidence score is persisted and retrieved correctly, and that segments
// without confidence behave exactly as before (backward compat).
func TestTranscriptConfidenceRoundTrip(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()

	// --- sub-test: confidence value round-trips ---
	t.Run("with_confidence", func(t *testing.T) {
		noteID := seedNote(t, st)
		tr := model.Transcript{
			NoteID:            noteID,
			TranscriberPlugin: "whisper",
			Model:             "base",
			Segments: []model.Segment{
				{StartMS: 0, EndMS: 1000, Text: "hello", Source: "mixed", Confidence: floatPtr(0.87)},
			},
		}
		_, err := st.SaveTranscript(ctx, tr, 0)
		if err != nil {
			t.Fatalf("save: %v", err)
		}
		got, err := st.GetTranscript(ctx, noteID)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if got.Segments[0].Confidence == nil {
			t.Fatal("expected non-nil Confidence")
		}
		if *got.Segments[0].Confidence != 0.87 {
			t.Errorf("want 0.87, got %v", *got.Segments[0].Confidence)
		}
	})

	// --- sub-test: nil confidence stays nil (backward compat) ---
	t.Run("without_confidence", func(t *testing.T) {
		noteID := seedNote(t, st)
		tr := model.Transcript{
			NoteID:            noteID,
			TranscriberPlugin: "whisper",
			Model:             "base",
			Segments: []model.Segment{
				{StartMS: 0, EndMS: 1000, Text: "hello", Source: "mixed"},
			},
		}
		_, err := st.SaveTranscript(ctx, tr, 0)
		if err != nil {
			t.Fatalf("save: %v", err)
		}
		got, err := st.GetTranscript(ctx, noteID)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if got.Segments[0].Confidence != nil {
			t.Errorf("expected nil Confidence, got %v", got.Segments[0].Confidence)
		}
	})

	// --- sub-test: zero confidence is stored and not confused with nil ---
	t.Run("confidence_zero", func(t *testing.T) {
		noteID := seedNote(t, st)
		tr := model.Transcript{
			NoteID:            noteID,
			TranscriberPlugin: "whisper",
			Model:             "base",
			Segments: []model.Segment{
				{StartMS: 0, EndMS: 1000, Text: "hello", Source: "mixed", Confidence: floatPtr(0.0)},
			},
		}
		_, err := st.SaveTranscript(ctx, tr, 0)
		if err != nil {
			t.Fatalf("save: %v", err)
		}
		got, err := st.GetTranscript(ctx, noteID)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if got.Segments[0].Confidence == nil {
			t.Fatal("expected non-nil Confidence for zero value")
		}
		if *got.Segments[0].Confidence != 0.0 {
			t.Errorf("want 0.0, got %v", *got.Segments[0].Confidence)
		}
	})
}

// seedNoteWithOwner seeds a user+note and returns both the ownerID and noteID.
func seedNoteWithOwner(t *testing.T, st *store.Store) (ownerID, noteID string) {
	t.Helper()
	ctx := context.Background()
	email := fmt.Sprintf("dz%d@example.com", seedUserCounter.Add(1))
	u, err := st.CreateUser(ctx, email, "h")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	n, err := st.CreateNote(ctx, u.ID, "Review note")
	if err != nil {
		t.Fatalf("create note: %v", err)
	}
	return u.ID, n.ID
}

// TestSaveTranscript_reviewState verifies that SaveTranscript sets review_state
// to "pending" for guessed (acoustic) speaker labels, and "completed" when there
// are no speakers or only deterministic channel-based You/Them labels.
func TestSaveTranscript_reviewState(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()

	t.Run("with_speakers_pending", func(t *testing.T) {
		_, noteID := seedNoteWithOwner(t, st)
		tr := model.Transcript{
			NoteID:            noteID,
			TranscriberPlugin: "whisper",
			Model:             "base",
			Segments: []model.Segment{
				{StartMS: 0, EndMS: 1000, Text: "hello", Source: "mic", Speaker: "SPEAKER_00"},
				{StartMS: 1000, EndMS: 2000, Text: "world", Source: "mic"},
			},
		}
		saved, err := st.SaveTranscript(ctx, tr, 0)
		if err != nil {
			t.Fatalf("save: %v", err)
		}
		if saved.ReviewState != model.ReviewStatePending {
			t.Errorf("want review_state=%q, got %q", model.ReviewStatePending, saved.ReviewState)
		}
		got, err := st.GetTranscript(ctx, noteID)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if got.ReviewState != model.ReviewStatePending {
			t.Errorf("persisted review_state: want %q, got %q", model.ReviewStatePending, got.ReviewState)
		}
	})

	t.Run("no_speakers_completed", func(t *testing.T) {
		_, noteID := seedNoteWithOwner(t, st)
		tr := model.Transcript{
			NoteID:            noteID,
			TranscriberPlugin: "whisper",
			Model:             "base",
			Segments: []model.Segment{
				{StartMS: 0, EndMS: 1000, Text: "hello", Source: "mic"},
				{StartMS: 1000, EndMS: 2000, Text: "world", Source: "mic"},
			},
		}
		saved, err := st.SaveTranscript(ctx, tr, 0)
		if err != nil {
			t.Fatalf("save: %v", err)
		}
		if saved.ReviewState != model.ReviewStateCompleted {
			t.Errorf("want review_state=%q, got %q", model.ReviewStateCompleted, saved.ReviewState)
		}
		got, err := st.GetTranscript(ctx, noteID)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if got.ReviewState != model.ReviewStateCompleted {
			t.Errorf("persisted review_state: want %q, got %q", model.ReviewStateCompleted, got.ReviewState)
		}
	})

	t.Run("multitrack_youthem_completed", func(t *testing.T) {
		_, noteID := seedNoteWithOwner(t, st)
		tr := model.Transcript{
			NoteID:            noteID,
			TranscriberPlugin: "whisper",
			Model:             "base",
			Segments: []model.Segment{
				{StartMS: 0, EndMS: 1000, Text: "hi", Source: "mic", Speaker: model.SpeakerYou},
				{StartMS: 1000, EndMS: 2000, Text: "hello", Source: "system", Speaker: model.SpeakerThem},
			},
		}
		saved, err := st.SaveTranscript(ctx, tr, 0)
		if err != nil {
			t.Fatalf("save: %v", err)
		}
		if saved.ReviewState != model.ReviewStateCompleted {
			t.Errorf("channel-based You/Them needs no review: want %q, got %q", model.ReviewStateCompleted, saved.ReviewState)
		}
	})

	t.Run("multitrack_three_channels_completed", func(t *testing.T) {
		_, noteID := seedNoteWithOwner(t, st)
		tr := model.Transcript{
			NoteID:            noteID,
			TranscriberPlugin: "whisper",
			Model:             "base",
			Segments: []model.Segment{
				{StartMS: 0, EndMS: 1000, Text: "hi", Source: "mic", Speaker: model.SpeakerYou},
				{StartMS: 1000, EndMS: 2000, Text: "hello", Source: "system", Speaker: model.SpeakerThem},
				{StartMS: 2000, EndMS: 3000, Text: "third", Source: "channel 2", Speaker: "Speaker 3"},
			},
		}
		saved, err := st.SaveTranscript(ctx, tr, 0)
		if err != nil {
			t.Fatalf("save: %v", err)
		}
		if saved.ReviewState != model.ReviewStateCompleted {
			t.Errorf("multitrack channel labels need no review: want %q, got %q", model.ReviewStateCompleted, saved.ReviewState)
		}
		got, err := st.GetTranscript(ctx, noteID)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if got.ReviewState != model.ReviewStateCompleted {
			t.Errorf("persisted review_state: want %q, got %q", model.ReviewStateCompleted, got.ReviewState)
		}
	})
}

// TestGetDiarizationReview verifies segments are returned sorted by confidence
// ascending (NULLs last).
func TestGetDiarizationReview(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()

	ownerID, noteID := seedNoteWithOwner(t, st)
	highConf := floatPtr(0.9)
	lowConf := floatPtr(0.3)
	tr := model.Transcript{
		NoteID:            noteID,
		TranscriberPlugin: "whisper",
		Model:             "base",
		Segments: []model.Segment{
			{StartMS: 0, EndMS: 1000, Text: "high confidence", Source: "mic", Speaker: "SPEAKER_00", Confidence: highConf},
			{StartMS: 1000, EndMS: 2000, Text: "low confidence", Source: "mic", Speaker: "SPEAKER_01", Confidence: lowConf},
			{StartMS: 2000, EndMS: 3000, Text: "no confidence", Source: "mic", Speaker: "SPEAKER_00"},
		},
	}
	_, err := st.SaveTranscript(ctx, tr, 0)
	if err != nil {
		t.Fatalf("save: %v", err)
	}

	review, err := st.GetDiarizationReview(ctx, ownerID, noteID)
	if err != nil {
		t.Fatalf("get review: %v", err)
	}
	if review.NoteID != noteID {
		t.Errorf("note_id: want %q, got %q", noteID, review.NoteID)
	}
	if review.ReviewState != model.ReviewStatePending {
		t.Errorf("review_state: want %q, got %q", model.ReviewStatePending, review.ReviewState)
	}
	if len(review.Turns) != 3 {
		t.Fatalf("want 3 turns, got %d", len(review.Turns))
	}
	// Sorted: low (0.3) → high (0.9) → nil NULLS LAST.
	if *review.Turns[0].Confidence != 0.3 {
		t.Errorf("turn[0] confidence: want 0.3, got %v", review.Turns[0].Confidence)
	}
	if *review.Turns[1].Confidence != 0.9 {
		t.Errorf("turn[1] confidence: want 0.9, got %v", review.Turns[1].Confidence)
	}
	if review.Turns[2].Confidence != nil {
		t.Errorf("turn[2] confidence: want nil (NULLS LAST), got %v", review.Turns[2].Confidence)
	}

	// Wrong owner returns ErrNotFound.
	_, err = st.GetDiarizationReview(ctx, "00000000-0000-0000-0000-000000000001", noteID)
	if err != store.ErrNotFound {
		t.Errorf("wrong owner: want ErrNotFound, got %v", err)
	}
}

// TestConfirmSegmentSpeaker verifies speaker label updates and ownership enforcement.
func TestConfirmSegmentSpeaker(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()

	ownerID, noteID := seedNoteWithOwner(t, st)
	tr := model.Transcript{
		NoteID:            noteID,
		TranscriberPlugin: "whisper",
		Model:             "base",
		Segments: []model.Segment{
			{StartMS: 0, EndMS: 1000, Text: "hello", Source: "mic", Speaker: "SPEAKER_00"},
		},
	}
	saved, err := st.SaveTranscript(ctx, tr, 0)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	segID := saved.Segments[0].ID

	// Confirm the speaker.
	if err := st.ConfirmSegmentSpeaker(ctx, ownerID, noteID, segID, "Alice"); err != nil {
		t.Fatalf("confirm: %v", err)
	}

	// Verify via GetTranscript.
	got, err := st.GetTranscript(ctx, noteID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Segments[0].Speaker != "Alice" {
		t.Errorf("speaker: want %q, got %q", "Alice", got.Segments[0].Speaker)
	}

	// Wrong owner returns ErrNotFound.
	if err := st.ConfirmSegmentSpeaker(ctx, "00000000-0000-0000-0000-000000000001", noteID, segID, "Hack"); err != store.ErrNotFound {
		t.Errorf("wrong owner: want ErrNotFound, got %v", err)
	}

	// Wrong segment ID returns ErrNotFound.
	if err := st.ConfirmSegmentSpeaker(ctx, ownerID, noteID, "00000000-0000-0000-0000-000000000002", "Alice"); err != store.ErrNotFound {
		t.Errorf("bad segment id: want ErrNotFound, got %v", err)
	}
}

// TestUpdateReviewState_valid verifies all four legal state transitions.
func TestUpdateReviewState_valid(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()

	transitions := []struct {
		from string
		to   string
	}{
		{model.ReviewStatePending, model.ReviewStateInReview},
		{model.ReviewStateInReview, model.ReviewStateCompleted},
		{model.ReviewStateInReview, model.ReviewStatePending},
		{model.ReviewStateCompleted, model.ReviewStateInReview},
	}

	for _, tc := range transitions {
		tc := tc
		t.Run(tc.from+"→"+tc.to, func(t *testing.T) {
			t.Parallel()
			ownerID, noteID := seedNoteWithOwner(t, st)
			// Save a transcript with a speaker so review_state starts as "pending".
			tr := model.Transcript{
				NoteID:            noteID,
				TranscriberPlugin: "whisper",
				Model:             "base",
				Segments:          []model.Segment{{StartMS: 0, EndMS: 1000, Text: "hi", Source: "mic", Speaker: "SPEAKER_00"}},
			}
			if _, err := st.SaveTranscript(ctx, tr, 0); err != nil {
				t.Fatalf("save: %v", err)
			}

			// Drive to the `from` state if it isn't already "pending".
			if tc.from != model.ReviewStatePending {
				if err := forceReviewState(ctx, st, ownerID, noteID, tc.from); err != nil {
					t.Fatalf("force state %q: %v", tc.from, err)
				}
			}

			if err := st.UpdateReviewState(ctx, ownerID, noteID, tc.to); err != nil {
				t.Errorf("%s→%s: unexpected error: %v", tc.from, tc.to, err)
				return
			}

			rev, err := st.GetDiarizationReview(ctx, ownerID, noteID)
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			if rev.ReviewState != tc.to {
				t.Errorf("after transition: want %q, got %q", tc.to, rev.ReviewState)
			}
		})
	}
}

// forceReviewState drives the review state by applying legal transitions
// from "pending" to the desired state, used only in tests.
func forceReviewState(ctx context.Context, st *store.Store, ownerID, noteID, target string) error {
	switch target {
	case model.ReviewStatePending:
		return nil
	case model.ReviewStateInReview:
		return st.UpdateReviewState(ctx, ownerID, noteID, model.ReviewStateInReview)
	case model.ReviewStateCompleted:
		if err := st.UpdateReviewState(ctx, ownerID, noteID, model.ReviewStateInReview); err != nil {
			return err
		}
		return st.UpdateReviewState(ctx, ownerID, noteID, model.ReviewStateCompleted)
	}
	return nil
}

// TestUpdateReviewState_illegal verifies that illegal transitions return ErrInvalidTransition.
func TestUpdateReviewState_illegal(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()

	illegalTransitions := []struct {
		from string
		to   string
	}{
		{model.ReviewStatePending, model.ReviewStateCompleted},   // skip in_review
		{model.ReviewStateCompleted, model.ReviewStatePending},   // not allowed
		{model.ReviewStatePending, model.ReviewStatePending},     // same→same
		{model.ReviewStateInReview, model.ReviewStateInReview},   // same→same
		{model.ReviewStateCompleted, model.ReviewStateCompleted}, // same→same
	}

	for _, tc := range illegalTransitions {
		tc := tc
		t.Run(tc.from+"→"+tc.to, func(t *testing.T) {
			t.Parallel()
			ownerID, noteID := seedNoteWithOwner(t, st)
			tr := model.Transcript{
				NoteID:            noteID,
				TranscriberPlugin: "whisper",
				Model:             "base",
				Segments:          []model.Segment{{StartMS: 0, EndMS: 1000, Text: "hi", Source: "mic", Speaker: "SPEAKER_00"}},
			}
			if _, err := st.SaveTranscript(ctx, tr, 0); err != nil {
				t.Fatalf("save: %v", err)
			}

			// Drive to `from` state.
			if err := forceReviewState(ctx, st, ownerID, noteID, tc.from); err != nil {
				t.Fatalf("force state %q: %v", tc.from, err)
			}

			err := st.UpdateReviewState(ctx, ownerID, noteID, tc.to)
			if err != store.ErrInvalidTransition {
				t.Errorf("%s→%s: want ErrInvalidTransition, got %v", tc.from, tc.to, err)
			}
		})
	}
}

func TestTranscriptGenerationDefaultsToOne(t *testing.T) {
	t.Parallel()
	pool := testutil.NewPool(t)
	st := store.New(pool)
	ctx := context.Background()
	noteID := seedNote(t, st)

	saved, err := st.SaveTranscript(ctx, model.Transcript{
		NoteID:            noteID,
		TranscriberPlugin: "whisper",
		Segments:          []model.Segment{{StartMS: 0, EndMS: 10, Text: "hi", Source: "mic"}},
	}, 0)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if saved.Generation != 1 {
		t.Fatalf("Generation = %d, want 1", saved.Generation)
	}
	if saved.Sealed {
		t.Fatal("new transcript should not be sealed")
	}

	// Verify the actual database defaults by reading back the row.
	var generation int
	var sealed bool
	var streamID *string
	if err := pool.QueryRow(ctx, `SELECT generation, sealed, stream_id FROM transcripts WHERE id = $1`, saved.ID).
		Scan(&generation, &sealed, &streamID); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if generation != 1 {
		t.Fatalf("database generation = %d, want 1", generation)
	}
	if sealed {
		t.Fatal("database sealed should be false")
	}
	if streamID != nil {
		t.Fatalf("database stream_id should be NULL for batch transcript, got %v", streamID)
	}
}

func TestTranscriptGenerationIncrementsOnReplacement(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	noteID := seedNote(t, st)

	base := model.Transcript{
		NoteID:            noteID,
		TranscriberPlugin: "whisper",
		Segments:          []model.Segment{{StartMS: 0, EndMS: 10, Text: "one", Source: "mic"}},
	}
	first, err := st.SaveTranscript(ctx, base, 0)
	if err != nil {
		t.Fatalf("first save: %v", err)
	}
	second, err := st.SaveTranscript(ctx, base, first.Generation)
	if err != nil {
		t.Fatalf("second save: %v", err)
	}
	if first.Generation != 1 || second.Generation != 2 {
		t.Fatalf("generations = %d, %d; want 1, 2", first.Generation, second.Generation)
	}
	if second.ID == first.ID {
		t.Fatal("replacement should be a new transcript row")
	}
}

// TestSaveTranscriptRejectsStaleExpectedGeneration verifies that SaveTranscript
// rejects a caller carrying a transcript generation the note has since moved
// past — the mechanism that stops an older transcribe job from overwriting a
// newer retranscription (spec §7).
func TestSaveTranscriptRejectsStaleExpectedGeneration(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	noteID := seedNote(t, st)

	base := model.Transcript{
		NoteID:            noteID,
		TranscriberPlugin: "whisper",
		Segments:          []model.Segment{{StartMS: 0, EndMS: 10, Text: "one", Source: "mic"}},
	}
	// First job: expects no transcript.
	first, err := st.SaveTranscript(ctx, base, 0)
	if err != nil {
		t.Fatalf("first save: %v", err)
	}
	// A retranscribe enqueued against generation 1 publishes generation 2.
	if _, err := st.SaveTranscript(ctx, base, first.Generation); err != nil {
		t.Fatalf("retranscribe save: %v", err)
	}
	// An older job, enqueued when no transcript existed, must not publish.
	if _, err := st.SaveTranscript(ctx, base, 0); !errors.Is(err, store.ErrGenerationMismatch) {
		t.Fatalf("stale job error = %v, want ErrGenerationMismatch", err)
	}

	// GetTranscript does not select generation until Task 6, so confirm the
	// stale save left the note on generation 2 via CurrentTranscriptGeneration
	// (the same read path production callers use), not GetTranscript.
	generation, err := st.CurrentTranscriptGeneration(ctx, noteID)
	if err != nil {
		t.Fatalf("current generation: %v", err)
	}
	if generation != 2 {
		t.Fatalf("Generation = %d, want 2", generation)
	}
}

// TestAppendStreamSegmentRejectsSupersededStream verifies the bug #724 fixes:
// a live final that arrives after batch transcription has replaced the
// transcript must not land in the batch transcript it no longer owns.
func TestAppendStreamSegmentRejectsSupersededStream(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	noteID := seedNote(t, st)

	live, err := st.CreateStreamTranscript(ctx, noteID, "stream-a", "whisper-live", "tiny", 0)
	if err != nil {
		t.Fatalf("create stream transcript: %v", err)
	}
	if err := st.AppendStreamSegment(ctx, live.ID, "stream-a", model.Segment{
		StartMS: 0, EndMS: 500, Text: "live text", Source: "mic",
	}); err != nil {
		t.Fatalf("append before replacement: %v", err)
	}

	if _, err := st.SaveTranscript(ctx, model.Transcript{
		NoteID:            noteID,
		TranscriberPlugin: "whisper",
		Segments:          []model.Segment{{StartMS: 0, EndMS: 500, Text: "batch text", Source: "mic"}},
	}, live.Generation); err != nil {
		t.Fatalf("batch save: %v", err)
	}

	// A final still in flight now arrives. It must not land anywhere.
	if err := st.AppendStreamSegment(ctx, live.ID, "stream-a", model.Segment{
		StartMS: 500, EndMS: 900, Text: "late live text", Source: "mic",
	}); !errors.Is(err, store.ErrStreamSuperseded) {
		t.Fatalf("late append error = %v, want ErrStreamSuperseded", err)
	}

	got, err := st.GetTranscript(ctx, noteID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got.Segments) != 1 || got.Segments[0].Text != "batch text" {
		t.Fatalf("segments = %+v, want exactly the batch segment", got.Segments)
	}
}

// TestAppendStreamSegmentRejectsWrongStreamID binds the stream_id predicate in
// AppendStreamSegment's guard on its own: the transcript is present and
// unsealed, so only a stream identity mismatch can reject the write.
func TestAppendStreamSegmentRejectsWrongStreamID(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	noteID := seedNote(t, st)

	live, err := st.CreateStreamTranscript(ctx, noteID, "stream-a", "whisper-live", "tiny", 0)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// The transcript is present and unsealed. Only the stream identity is wrong,
	// so only the stream_id predicate can reject this.
	if err := st.AppendStreamSegment(ctx, live.ID, "stream-b", model.Segment{
		StartMS: 0, EndMS: 500, Text: "other stream", Source: "mic",
	}); !errors.Is(err, store.ErrStreamSuperseded) {
		t.Fatalf("error = %v, want ErrStreamSuperseded", err)
	}

	got, err := st.GetTranscript(ctx, noteID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got.Segments) != 0 {
		t.Fatalf("segments = %+v, want none written", got.Segments)
	}
}

// TestAppendStreamSegmentRejectsSealedTranscript binds the sealed predicate on
// its own. A committed sealed row is never observable through SaveTranscript,
// because its UPDATE and DELETE commit together, so the fixture seals directly
// — legitimate setup for testing a predicate production reaches only
// mid-transaction.
func TestAppendStreamSegmentRejectsSealedTranscript(t *testing.T) {
	t.Parallel()
	pool := testutil.NewPool(t)
	st := store.New(pool)
	ctx := context.Background()
	noteID := seedNote(t, st)

	live, err := st.CreateStreamTranscript(ctx, noteID, "stream-a", "whisper-live", "tiny", 0)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE transcripts SET sealed=TRUE WHERE id=$1`, live.ID); err != nil {
		t.Fatalf("seal: %v", err)
	}
	// The transcript is present and the stream id matches. Only sealed can
	// reject this.
	if err := st.AppendStreamSegment(ctx, live.ID, "stream-a", model.Segment{
		StartMS: 0, EndMS: 500, Text: "after seal", Source: "mic",
	}); !errors.Is(err, store.ErrStreamSuperseded) {
		t.Fatalf("error = %v, want ErrStreamSuperseded", err)
	}
}

// TestAppendStreamSegmentRejectsWrongTranscriptID binds the id=$1 predicate on
// its own. Dropping id=$1 is not caught by either test above: with only
// stream_id and sealed matching, a different live transcript carrying the same
// stream id can satisfy the lookup while the insert still targets the
// original. Two notes are needed, because transcripts(note_id) is unique.
func TestAppendStreamSegmentRejectsWrongTranscriptID(t *testing.T) {
	t.Parallel()
	pool := testutil.NewPool(t)
	st := store.New(pool)
	ctx := context.Background()
	noteA := seedNote(t, st)
	noteB := seedNote(t, st)

	liveA, err := st.CreateStreamTranscript(ctx, noteA, "stream-a", "whisper-live", "tiny", 0)
	if err != nil {
		t.Fatalf("create A: %v", err)
	}
	// A second note carrying the SAME stream id, left unsealed. If the guard
	// stops matching on transcript id, this row satisfies the lookup.
	if _, err := st.CreateStreamTranscript(ctx, noteB, "stream-a", "whisper-live", "tiny", 0); err != nil {
		t.Fatalf("create B: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE transcripts SET sealed=TRUE WHERE id=$1`, liveA.ID); err != nil {
		t.Fatalf("seal A: %v", err)
	}

	if err := st.AppendStreamSegment(ctx, liveA.ID, "stream-a", model.Segment{
		StartMS: 0, EndMS: 500, Text: "should not land", Source: "mic",
	}); !errors.Is(err, store.ErrStreamSuperseded) {
		t.Fatalf("error = %v, want ErrStreamSuperseded", err)
	}

	got, err := st.GetTranscript(ctx, noteA)
	if err != nil {
		t.Fatalf("get A: %v", err)
	}
	if len(got.Segments) != 0 {
		t.Fatalf("segments on A = %+v, want none", got.Segments)
	}
}

// TestConcurrentFirstWritersDoNotRaceTheUniqueIndex binds the note-row lock in
// SaveTranscript deterministically. Ordering is established by channels, never
// by sleeps: the first writer signals that it has read "no prior transcript"
// and then blocks, so the second writer provably starts inside that window.
// With the note-row lock the second writer cannot even reach its own read
// until the first commits; without it, the second sails past and the first
// then collides with the unique note_id index instead of failing cleanly.
func TestConcurrentFirstWritersDoNotRaceTheUniqueIndex(t *testing.T) {
	// No t.Parallel: this installs a package-level hook.
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	noteID := seedNote(t, st)

	var mu sync.Mutex
	var calls int
	reached1 := make(chan struct{})
	reached2 := make(chan struct{})
	release1 := make(chan struct{})
	restore := store.SetTestHookAfterPriorTranscriptRead(func() {
		mu.Lock()
		calls++
		which := calls
		mu.Unlock()
		switch which {
		case 1:
			close(reached1)
			<-release1
		case 2:
			close(reached2)
		}
	})
	defer restore()

	tr := model.Transcript{
		NoteID:            noteID,
		TranscriberPlugin: "whisper",
		Segments:          []model.Segment{{StartMS: 0, EndMS: 10, Text: "one", Source: "mic"}},
	}
	results := make(chan error, 2)
	go func() { _, err := st.SaveTranscript(ctx, tr, 0); results <- err }()
	<-reached1 // writer 1 has read, and is holding the window open

	atLock := make(chan struct{})
	go func() {
		close(atLock) // writer 2 is scheduled and running
		_, err := st.SaveTranscript(ctx, tr, 0)
		results <- err
	}()
	<-atLock

	select {
	case <-reached2:
		// Mutated build: writer 2 reached its own read, so both reads precede
		// either insert — the interleaving the lock exists to prevent.
	case <-time.After(2 * time.Second):
		// Correct build: writer 2 is blocked on the note-row lock. atLock proves
		// it was scheduled, though not how far it got.
	}
	close(release1)

	var succeeded int
	var errs []error
	for i := 0; i < 2; i++ {
		if err := <-results; err == nil {
			succeeded++
		} else {
			errs = append(errs, err)
		}
	}
	if succeeded != 1 {
		t.Fatalf("%d writers succeeded, want exactly 1 (errors: %v)", succeeded, errs)
	}
	if !errors.Is(errs[0], store.ErrGenerationMismatch) {
		t.Fatalf("loser error = %v, want ErrGenerationMismatch (a unique-violation here means the note row was not locked)", errs[0])
	}
}

// TestConcurrentStreamCreatorsDoNotRaceTheUniqueIndex is
// TestConcurrentFirstWritersDoNotRaceTheUniqueIndex for CreateStreamTranscript,
// binding its note-row lock the same way.
func TestConcurrentStreamCreatorsDoNotRaceTheUniqueIndex(t *testing.T) {
	// No t.Parallel: this installs a package-level hook.
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	noteID := seedNote(t, st)

	var mu sync.Mutex
	var calls int
	reached1 := make(chan struct{})
	reached2 := make(chan struct{})
	release1 := make(chan struct{})
	restore := store.SetTestHookAfterPriorTranscriptRead(func() {
		mu.Lock()
		calls++
		which := calls
		mu.Unlock()
		switch which {
		case 1:
			close(reached1)
			<-release1
		case 2:
			close(reached2)
		}
	})
	defer restore()

	results := make(chan error, 2)
	go func() {
		_, err := st.CreateStreamTranscript(ctx, noteID, "stream-a", "whisper-live", "tiny", 0)
		results <- err
	}()
	<-reached1 // writer 1 has read, and is holding the window open

	atLock := make(chan struct{})
	go func() {
		close(atLock) // writer 2 is scheduled and running
		_, err := st.CreateStreamTranscript(ctx, noteID, "stream-b", "whisper-live", "tiny", 0)
		results <- err
	}()
	<-atLock

	select {
	case <-reached2:
		// Mutated build: writer 2 reached its own read, so both reads precede
		// either insert — the interleaving the lock exists to prevent.
	case <-time.After(2 * time.Second):
		// Correct build: writer 2 is blocked on the note-row lock. atLock proves
		// it was scheduled, though not how far it got.
	}
	close(release1)

	var succeeded int
	var errs []error
	for i := 0; i < 2; i++ {
		if err := <-results; err == nil {
			succeeded++
		} else {
			errs = append(errs, err)
		}
	}
	if succeeded != 1 {
		t.Fatalf("%d writers succeeded, want exactly 1 (errors: %v)", succeeded, errs)
	}
	if !errors.Is(errs[0], store.ErrGenerationMismatch) {
		t.Fatalf("loser error = %v, want ErrGenerationMismatch (a unique-violation here means the note row was not locked)", errs[0])
	}
}

// TestAppendBeforeReplacementIsDeletedByIt covers ordering A of the two
// interleavings between a live append and a batch replacement: the append
// commits first and succeeds, then replacement removes it. The append does
// NOT fail — a row lock confers order, not priority. Ordering B — replacement
// first, then a late append — is TestAppendStreamSegmentRejectsSupersededStream.
func TestAppendBeforeReplacementIsDeletedByIt(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	noteID := seedNote(t, st)

	live, err := st.CreateStreamTranscript(ctx, noteID, "stream-a", "whisper-live", "tiny", 0)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := st.AppendStreamSegment(ctx, live.ID, "stream-a", model.Segment{
		StartMS: 0, EndMS: 500, Text: "live text", Source: "mic",
	}); err != nil {
		t.Fatalf("append: %v", err)
	}
	if _, err := st.SaveTranscript(ctx, model.Transcript{
		NoteID:            noteID,
		TranscriberPlugin: "whisper",
		Segments:          []model.Segment{{StartMS: 0, EndMS: 500, Text: "batch text", Source: "mic"}},
	}, live.Generation); err != nil {
		t.Fatalf("batch save: %v", err)
	}

	got, err := st.GetTranscript(ctx, noteID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got.Segments) != 1 || got.Segments[0].Text != "batch text" {
		t.Fatalf("segments = %+v, want exactly the batch segment", got.Segments)
	}
	if got.Segments[0].Provisional {
		t.Fatal("surviving segment should be the batch one, not provisional")
	}
}
