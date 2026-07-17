package store_test

import (
	"context"
	"fmt"
	"testing"

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
	saved, err := st.SaveTranscript(ctx, tr)
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
		saved, err := st.SaveTranscript(ctx, tr)
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
		saved, err := st.SaveTranscript(ctx, tr)
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
		_, err := st.SaveTranscript(ctx, tr)
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
		_, err := st.SaveTranscript(ctx, tr)
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
		_, err := st.SaveTranscript(ctx, tr)
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
		saved, err := st.SaveTranscript(ctx, tr)
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
		saved, err := st.SaveTranscript(ctx, tr)
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
		saved, err := st.SaveTranscript(ctx, tr)
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
		saved, err := st.SaveTranscript(ctx, tr)
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
	_, err := st.SaveTranscript(ctx, tr)
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
	saved, err := st.SaveTranscript(ctx, tr)
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
			if _, err := st.SaveTranscript(ctx, tr); err != nil {
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
			if _, err := st.SaveTranscript(ctx, tr); err != nil {
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
