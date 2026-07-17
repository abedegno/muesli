package store

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/abedegno/muesli/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// SaveTranscript writes a transcript and its segments in one transaction,
// replacing any prior transcript for the note (idempotent re-runs). The
// transcripts(note_id) unique index (migration 0003) guarantees at most one
// transcript row per note even under concurrent/retried transcribe jobs.
//
// review_state is set automatically:
//   - Acoustic diarization guesses speaker labels ("SPEAKER_00", ...) and needs
//     confirming before summarizing → "pending".
//   - Channel-based multitrack ("You"/"Them") is deterministic, and no-speaker
//     transcripts have nothing to review → "completed".
func (s *Store) SaveTranscript(ctx context.Context, tr model.Transcript) (model.Transcript, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.Transcript{}, err
	}
	defer tx.Rollback(ctx)

	// Idempotency: drop any existing transcript for this note (cascades segments).
	// The unique index on note_id keeps this to a single row.
	if _, err := tx.Exec(ctx, `DELETE FROM transcripts WHERE note_id=$1`, tr.NoteID); err != nil {
		return model.Transcript{}, err
	}

	// Determine review_state. Acoustic diarization guesses speaker labels
	// (e.g. "SPEAKER_00") that a user confirms before summarizing -> "pending".
	// Channel-based multitrack attribution (the deterministic "You"/"Them"
	// role labels) is ground truth and needs no review -> stays "completed".
	reviewState := model.ReviewStateCompleted
	for _, seg := range tr.Segments {
		if seg.Speaker != "" && seg.Speaker != model.SpeakerYou && seg.Speaker != model.SpeakerThem {
			reviewState = model.ReviewStatePending
			break
		}
	}
	tr.ReviewState = reviewState

	tr.ID = uuid.NewString()
	_, err = tx.Exec(ctx,
		`INSERT INTO transcripts (id, note_id, transcriber_plugin, model, review_state)
		 VALUES ($1,$2,$3,$4,$5)`,
		tr.ID, tr.NoteID, tr.TranscriberPlugin, tr.Model, tr.ReviewState)
	if err != nil {
		return model.Transcript{}, err
	}
	for i := range tr.Segments {
		seg := &tr.Segments[i]
		seg.ID = uuid.NewString()
		var speaker *string
		if seg.Speaker != "" {
			speaker = &seg.Speaker
		}
		var wordsJSON []byte
		if len(seg.Words) > 0 {
			wordsJSON, err = json.Marshal(seg.Words)
			if err != nil {
				return model.Transcript{}, err
			}
		}
		_, err = tx.Exec(ctx,
			`INSERT INTO transcript_segments (id, transcript_id, start_ms, end_ms, text, source, speaker, words, confidence)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
			seg.ID, tr.ID, seg.StartMS, seg.EndMS, seg.Text, seg.Source, speaker, wordsJSON, seg.Confidence)
		if err != nil {
			return model.Transcript{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return model.Transcript{}, err
	}
	return tr, nil
}

// AppendProvisionalTranscriptSegment ensures a transcript row exists for the
// note and appends one provisional segment. It is used by the live streaming
// transcription path; the batch SaveTranscript path still replaces the whole
// transcript atomically later.
func (s *Store) AppendProvisionalTranscriptSegment(ctx context.Context, noteID string, tr model.Transcript, seg model.Segment) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Create the transcript row on first live segment, but never overwrite an
	// existing row for the note.
	_, err = tx.Exec(ctx,
		`INSERT INTO transcripts (id, note_id, transcriber_plugin, model, review_state)
		 VALUES ($1,$2,$3,$4,$5)
		 ON CONFLICT (note_id) DO NOTHING`,
		uuid.NewString(), noteID, tr.TranscriberPlugin, tr.Model, model.ReviewStateCompleted)
	if err != nil {
		return err
	}

	var transcriptID string
	if err := tx.QueryRow(ctx, `SELECT id FROM transcripts WHERE note_id=$1`, noteID).Scan(&transcriptID); err != nil {
		return err
	}

	segID := uuid.NewString()
	var speaker *string
	if seg.Speaker != "" {
		speaker = &seg.Speaker
	}
	var wordsJSON []byte
	if len(seg.Words) > 0 {
		wordsJSON, err = json.Marshal(seg.Words)
		if err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx,
		`INSERT INTO transcript_segments (id, transcript_id, start_ms, end_ms, text, source, speaker, words, confidence, provisional)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		segID, transcriptID, seg.StartMS, seg.EndMS, seg.Text, seg.Source, speaker, wordsJSON, seg.Confidence, true)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// GetTranscript returns a note's transcript with ordered segments.
func (s *Store) GetTranscript(ctx context.Context, noteID string) (model.Transcript, error) {
	var tr model.Transcript
	err := s.pool.QueryRow(ctx,
		`SELECT id, note_id, transcriber_plugin, model, review_state FROM transcripts WHERE note_id=$1`, noteID).
		Scan(&tr.ID, &tr.NoteID, &tr.TranscriberPlugin, &tr.Model, &tr.ReviewState)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.Transcript{}, ErrNotFound
	}
	if err != nil {
		return model.Transcript{}, err
	}
	rows, err := s.pool.Query(ctx,
		`SELECT id, start_ms, end_ms, text, source, COALESCE(speaker,''), words, confidence
		 FROM transcript_segments WHERE transcript_id=$1 ORDER BY start_ms`, tr.ID)
	if err != nil {
		return model.Transcript{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var seg model.Segment
		var wordsJSON []byte
		var confidence *float64
		if err := rows.Scan(&seg.ID, &seg.StartMS, &seg.EndMS, &seg.Text, &seg.Source, &seg.Speaker, &wordsJSON, &confidence); err != nil {
			return model.Transcript{}, err
		}
		seg.Confidence = confidence
		if len(wordsJSON) > 0 {
			if err := json.Unmarshal(wordsJSON, &seg.Words); err != nil {
				return model.Transcript{}, err
			}
		}
		tr.Segments = append(tr.Segments, seg)
	}
	return tr, rows.Err()
}

// GetDiarizationReview returns the diarization review payload for a note's
// transcript. Segments (turns) are sorted ascending by confidence (NULLs last)
// then by start_ms. Returns ErrNotFound if the transcript does not exist or
// does not belong to the owner.
func (s *Store) GetDiarizationReview(ctx context.Context, ownerID, noteID string) (*model.DiarizationReview, error) {
	var transcriptID string
	var reviewState string
	err := s.pool.QueryRow(ctx,
		`SELECT t.id, t.review_state
		 FROM transcripts t
		 JOIN notes n ON n.id = t.note_id
		 WHERE t.note_id = $1 AND n.owner_id = $2`,
		noteID, ownerID).
		Scan(&transcriptID, &reviewState)
	if errors.Is(err, pgx.ErrNoRows) || isUUIDSyntaxError(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	rows, err := s.pool.Query(ctx,
		`SELECT id, start_ms, end_ms, text, source, COALESCE(speaker,''), words, confidence
		 FROM transcript_segments
		 WHERE transcript_id = $1
		 ORDER BY confidence ASC NULLS LAST, start_ms ASC`,
		transcriptID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var turns []model.Segment
	for rows.Next() {
		var seg model.Segment
		var wordsJSON []byte
		var confidence *float64
		if err := rows.Scan(&seg.ID, &seg.StartMS, &seg.EndMS, &seg.Text, &seg.Source, &seg.Speaker, &wordsJSON, &confidence); err != nil {
			return nil, err
		}
		seg.Confidence = confidence
		if len(wordsJSON) > 0 {
			if err := json.Unmarshal(wordsJSON, &seg.Words); err != nil {
				return nil, err
			}
		}
		turns = append(turns, seg)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return &model.DiarizationReview{
		NoteID:      noteID,
		ReviewState: reviewState,
		Turns:       turns,
	}, nil
}

// ConfirmSegmentSpeaker sets the speaker label on a segment, enforcing that
// the segment belongs to the transcript of a note owned by ownerID. Returns
// ErrNotFound if the segment doesn't exist or ownership check fails.
func (s *Store) ConfirmSegmentSpeaker(ctx context.Context, ownerID, noteID, segmentID, speaker string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE transcript_segments SET speaker = $1
		 WHERE id = $2
		   AND transcript_id = (
		     SELECT t.id FROM transcripts t
		     JOIN notes n ON n.id = t.note_id
		     WHERE t.note_id = $3 AND n.owner_id = $4
		   )`,
		speaker, segmentID, noteID, ownerID)
	if isUUIDSyntaxError(err) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateReviewState transitions a transcript's review_state. Legal transitions:
//
//	pending    → in_review
//	in_review  → completed
//	in_review  → pending
//	completed  → in_review
//
// Any other transition (including same→same) returns ErrInvalidTransition.
// Returns ErrNotFound if the transcript doesn't exist or isn't owned by ownerID.
func (s *Store) UpdateReviewState(ctx context.Context, ownerID, noteID, newState string) error {
	// Fetch current state with ownership check.
	var currentState string
	err := s.pool.QueryRow(ctx,
		`SELECT t.review_state
		 FROM transcripts t
		 JOIN notes n ON n.id = t.note_id
		 WHERE t.note_id = $1 AND n.owner_id = $2`,
		noteID, ownerID).
		Scan(&currentState)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}

	// Validate transition.
	if !isLegalTransition(currentState, newState) {
		return ErrInvalidTransition
	}

	_, err = s.pool.Exec(ctx,
		`UPDATE transcripts SET review_state = $1
		 WHERE note_id = $2`,
		newState, noteID)
	return err
}

// SetReviewState updates a transcript's review_state without ownership or
// transition checks. It is reserved for trusted in-process pipeline code.
func (s *Store) SetReviewState(ctx context.Context, noteID, state string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE transcripts SET review_state = $1
		 WHERE note_id = $2`,
		state, noteID)
	return err
}

// isLegalTransition reports whether transitioning from `from` to `to` is
// permitted by the diarization review lifecycle.
func isLegalTransition(from, to string) bool {
	switch from {
	case model.ReviewStatePending:
		return to == model.ReviewStateInReview
	case model.ReviewStateInReview:
		return to == model.ReviewStateCompleted || to == model.ReviewStatePending
	case model.ReviewStateCompleted:
		return to == model.ReviewStateInReview
	}
	return false
}

// isUUIDSyntaxError reports whether err is a Postgres 22P02 "invalid input
// syntax for type uuid" error. This can happen when a caller passes a
// non-UUID string (e.g. in tests) where the column expects a UUID; we treat
// it as ErrNotFound rather than a 500.
func isUUIDSyntaxError(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "22P02"
}
