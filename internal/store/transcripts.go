package store

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"

	"github.com/abedegno/muesli/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var deterministicChannelSpeakerRe = regexp.MustCompile(`^Speaker \d+$`)

// ErrStreamSuperseded is returned when a live write targets a transcript that
// has been sealed or replaced. It is expected during normal shutdown races: the
// write should be dropped, never retried.
var ErrStreamSuperseded = errors.New("stream superseded")

// ErrBatchTranscriptExists is returned by CreateStreamTranscript when the note
// already holds a batch-authored transcript (stream_id IS NULL). Spec §7 scopes
// supersession to a note that already has a STREAM-owned transcript, so this is
// a permanent refusal rather than a race: no expected generation makes it
// succeed and a caller must never retry it. It is deliberately distinct from
// ErrGenerationMismatch, which does mean "re-read the generation and retry".
var ErrBatchTranscriptExists = errors.New("note already has a batch transcript")

// testHookAfterPriorTranscriptRead runs between reading the prior transcript
// row and writing the replacement. It is nil in production and exists so tests
// can hold one writer in the window the note-row lock is there to close.
var testHookAfterPriorTranscriptRead func()

// isDeterministicChannelSpeaker stays coupled to pluginkit.channelSpeaker in
// internal/pluginkit/multitrack.go so both packages agree on channel labels.
func isDeterministicChannelSpeaker(speaker string) bool {
	return speaker == model.SpeakerYou || speaker == model.SpeakerThem || deterministicChannelSpeakerRe.MatchString(speaker)
}

// SaveTranscript writes a transcript and its segments in one transaction,
// replacing any prior transcript for the note (idempotent re-runs). The
// transcripts(note_id) unique index (migration 0003) guarantees at most one
// transcript row per note even under concurrent/retried transcribe jobs.
//
// expectedGeneration must match the note's current transcript generation (0
// meaning "no transcript yet") or the call fails with ErrGenerationMismatch
// and nothing is written. This is what stops a stale transcribe job — one
// enqueued against an older generation — from overwriting a newer
// retranscription that has since completed.
//
// review_state is set automatically:
//   - Acoustic diarization guesses speaker labels ("SPEAKER_00", ...) and needs
//     confirming before summarizing → "pending".
//   - Channel-based multitrack ("You"/"Them") is deterministic, and no-speaker
//     transcripts have nothing to review → "completed".
func (s *Store) SaveTranscript(ctx context.Context, tr model.Transcript, expectedGeneration int) (model.Transcript, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.Transcript{}, err
	}
	defer tx.Rollback(ctx)

	// Serialise every writer for this note on a row that always exists, and
	// clear any outstanding transcribe claim in the same statement. Two things
	// are happening here and both are load-bearing:
	//
	//   - UPDATE takes the note's row lock. A FOR UPDATE on transcripts cannot
	//     lock a row that is not there yet, so two concurrent first-writers
	//     would both see no transcript and then race the unique note_id index.
	//   - Clearing transcribing_job_id means a losing job's release can no
	//     longer match its own token, so it cannot undo this save's outcome.
	//     Nothing else clears it: runTranscribe has no status advance after a
	//     successful save, and may leave the note transcribing while awaiting
	//     review or summarization.
	if _, err := tx.Exec(ctx,
		`UPDATE notes SET transcribing_job_id=NULL WHERE id=$1`, tr.NoteID); err != nil {
		return model.Transcript{}, err
	}

	nextGeneration := 1
	var priorID string
	var priorGeneration int
	err = tx.QueryRow(ctx,
		`SELECT id, generation FROM transcripts WHERE note_id=$1`,
		tr.NoteID).Scan(&priorID, &priorGeneration)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		nextGeneration = 1
	case err != nil:
		return model.Transcript{}, err
	default:
		nextGeneration = priorGeneration + 1
	}

	if testHookAfterPriorTranscriptRead != nil {
		testHookAfterPriorTranscriptRead()
	}

	// In the pgx.ErrNoRows branch priorGeneration is 0, so this one comparison
	// also covers "expected none, found none". Checked before the seal write
	// below so a stale caller's mismatch is caught without touching anything;
	// the transaction rolls back on return either way.
	if priorGeneration != expectedGeneration {
		return model.Transcript{}, ErrGenerationMismatch
	}

	if priorID != "" {
		// Seal as part of the locked replacement protocol. This does not publish
		// a visible sealed state — the UPDATE and DELETE commit together, so no
		// other transaction ever observes a committed sealed version of this
		// row. It protects rows that remain present, and documents intent.
		if _, err := tx.Exec(ctx, `UPDATE transcripts SET sealed=TRUE WHERE id=$1`, priorID); err != nil {
			return model.Transcript{}, err
		}
	}

	if _, err := tx.Exec(ctx, `DELETE FROM transcripts WHERE note_id=$1`, tr.NoteID); err != nil {
		return model.Transcript{}, err
	}

	// Determine review_state. Acoustic diarization guesses speaker labels
	// (e.g. "SPEAKER_00") that a user confirms before summarizing -> "pending".
	// Channel-based multitrack attribution (the deterministic "You"/"Them"
	// role labels) is ground truth and needs no review -> stays "completed".
	reviewState := model.ReviewStateCompleted
	for _, seg := range tr.Segments {
		if seg.Speaker != "" && !isDeterministicChannelSpeaker(seg.Speaker) {
			reviewState = model.ReviewStatePending
			break
		}
	}
	tr.ReviewState = reviewState

	tr.ID = uuid.NewString()
	if err := tx.QueryRow(ctx,
		`INSERT INTO transcripts (id, note_id, transcriber_plugin, model, review_state, generation)
		 VALUES ($1,$2,$3,$4,$5,$6)
		 RETURNING generation`,
		tr.ID, tr.NoteID, tr.TranscriberPlugin, tr.Model, tr.ReviewState, nextGeneration).Scan(&tr.Generation); err != nil {
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

// CurrentTranscriptGeneration returns the note's current transcript generation,
// or 0 when the note has no transcript. Callers pass the result as an expected
// generation, so 0 correctly means "expect none". Reads the generation
// directly rather than through GetTranscript, which does not select it.
func (s *Store) CurrentTranscriptGeneration(ctx context.Context, noteID string) (int, error) {
	var generation int
	err := s.pool.QueryRow(ctx, `SELECT generation FROM transcripts WHERE note_id=$1`, noteID).Scan(&generation)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return generation, nil
}

// CreateStreamTranscript creates the transcript a live stream owns. It runs
// once the plugin session is open and before any audio is accepted, because a
// gap can occur before the first segment and needs somewhere to live.
//
// It supersedes a STREAM-owned transcript — sealing it and publishing a fresh
// row at generation+1, rather than reusing the old row, so a re-recorded note
// never mixes two streams' segments under one identity. It refuses a
// BATCH-owned one with ErrBatchTranscriptExists: spec §7 scopes supersession to
// "a new start for a note that already has a stream-owned transcript", and a
// batch transcript can be a note's only surviving record of what was said
// (retranscribe is refused once retention_state is "discarded"), so replacing
// it here would be an unrecoverable loss.
func (s *Store) CreateStreamTranscript(ctx context.Context, noteID, streamID, plugin, modelName string, expectedGeneration int) (model.Transcript, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.Transcript{}, err
	}
	defer tx.Rollback(ctx)

	// Lock the note row, but deliberately do NOT clear transcribing_job_id here.
	// SaveTranscript clears it because a completed transcript is authoritative
	// over any claim it displaced. A stream start is not: if a legitimate batch
	// job holds the claim, clearing it would strand that job — its release would
	// match nothing on the generation mismatch that follows, and the note would
	// stay stuck at 'transcribing' forever. Leaving the claim intact lets the
	// losing job restore the status it displaced.
	if _, err := tx.Exec(ctx, `SELECT id FROM notes WHERE id=$1 FOR UPDATE`, noteID); err != nil {
		return model.Transcript{}, err
	}

	var priorID string
	var priorGeneration int
	var priorStreamID *string
	err = tx.QueryRow(ctx, `SELECT id, generation, stream_id FROM transcripts WHERE note_id=$1`, noteID).
		Scan(&priorID, &priorGeneration, &priorStreamID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return model.Transcript{}, err
	}

	if testHookAfterPriorTranscriptRead != nil {
		testHookAfterPriorTranscriptRead()
	}

	// Ownership is checked before the generation, because it is the permanent
	// condition of the two: reporting a generation mismatch for a batch-owned
	// transcript would invite a caller to re-read and retry a call that can
	// never succeed.
	if priorID != "" && priorStreamID == nil {
		return model.Transcript{}, ErrBatchTranscriptExists
	}
	if priorGeneration != expectedGeneration {
		return model.Transcript{}, ErrGenerationMismatch
	}
	if priorID != "" {
		if _, err := tx.Exec(ctx, `UPDATE transcripts SET sealed=TRUE WHERE id=$1`, priorID); err != nil {
			return model.Transcript{}, err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM transcripts WHERE note_id=$1`, noteID); err != nil {
			return model.Transcript{}, err
		}
	}

	tr := model.Transcript{
		ID:                uuid.NewString(),
		NoteID:            noteID,
		StreamID:          &streamID,
		TranscriberPlugin: plugin,
		Model:             modelName,
		ReviewState:       model.ReviewStateCompleted,
		Generation:        priorGeneration + 1,
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO transcripts (id, note_id, transcriber_plugin, model, review_state, stream_id, generation)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		tr.ID, tr.NoteID, tr.TranscriberPlugin, tr.Model, tr.ReviewState, streamID, tr.Generation); err != nil {
		return model.Transcript{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return model.Transcript{}, err
	}
	return tr, nil
}

// AppendStreamSegment appends one provisional segment to the transcript this
// stream owns. The SELECT ... FOR UPDATE is the guard: it takes the same row
// lock replacement takes, so this either observes a live parent it now holds,
// or finds none and reports ErrStreamSuperseded. Without the lock the insert
// could instead fail with a foreign-key violation whose timing depends on the
// scheduler.
func (s *Store) AppendStreamSegment(ctx context.Context, transcriptID, streamID string, seg model.Segment) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var found string
	err = tx.QueryRow(ctx,
		`SELECT id FROM transcripts
		  WHERE id=$1 AND stream_id=$2 AND sealed=FALSE
		  FOR UPDATE`,
		transcriptID, streamID).Scan(&found)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrStreamSuperseded
	} else if err != nil {
		return err
	}

	var speaker *string
	if seg.Speaker != "" {
		speaker = &seg.Speaker
	}
	var wordsJSON []byte
	if len(seg.Words) > 0 {
		if wordsJSON, err = json.Marshal(seg.Words); err != nil {
			return err
		}
	}
	var boundary *string
	if seg.Boundary != "" {
		boundary = &seg.Boundary
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO transcript_segments
		   (id, transcript_id, start_ms, end_ms, text, source, speaker, words, confidence, provisional, boundary)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,TRUE,$10)`,
		uuid.NewString(), transcriptID, seg.StartMS, seg.EndMS, seg.Text, seg.Source,
		speaker, wordsJSON, seg.Confidence, boundary); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// AppendTranscriptGap records an interval of audio that never reached a
// transcriber. Conditional on the stream still owning an unsealed transcript,
// so a superseded stream cannot annotate the transcript that replaced it. A nil
// DroppedSamples stores an open-ended interval.
func (s *Store) AppendTranscriptGap(ctx context.Context, transcriptID, streamID string, g model.TranscriptGap) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var found string
	err = tx.QueryRow(ctx,
		`SELECT id FROM transcripts
		  WHERE id=$1 AND stream_id=$2 AND sealed=FALSE
		  FOR UPDATE`,
		transcriptID, streamID).Scan(&found)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrStreamSuperseded
	} else if err != nil {
		return err
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO transcript_gaps (id, transcript_id, stream_id, start_sample, dropped_samples, origin)
		 VALUES ($1,$2,$3,$4,$5,$6)`,
		uuid.NewString(), transcriptID, streamID, g.StartSample, g.DroppedSamples, g.Origin); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// GetTranscript returns a note's transcript with ordered segments, plus the
// stream/continuity metadata (StreamID, Sealed, Generation, Gaps) every
// consumer in spec §9 reads through this one call.
func (s *Store) GetTranscript(ctx context.Context, noteID string) (model.Transcript, error) {
	var tr model.Transcript
	err := s.pool.QueryRow(ctx,
		`SELECT id, note_id, transcriber_plugin, model, review_state, stream_id, sealed, generation
		 FROM transcripts WHERE note_id=$1`, noteID).
		Scan(&tr.ID, &tr.NoteID, &tr.TranscriberPlugin, &tr.Model, &tr.ReviewState, &tr.StreamID, &tr.Sealed, &tr.Generation)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.Transcript{}, ErrNotFound
	}
	if err != nil {
		return model.Transcript{}, err
	}
	rows, err := s.pool.Query(ctx,
		`SELECT id, start_ms, end_ms, text, source, COALESCE(speaker,''), words, confidence,
		        provisional, COALESCE(boundary,'')
		 FROM transcript_segments WHERE transcript_id=$1 ORDER BY start_ms`, tr.ID)
	if err != nil {
		return model.Transcript{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var seg model.Segment
		var wordsJSON []byte
		var confidence *float64
		if err := rows.Scan(&seg.ID, &seg.StartMS, &seg.EndMS, &seg.Text, &seg.Source, &seg.Speaker, &wordsJSON, &confidence,
			&seg.Provisional, &seg.Boundary); err != nil {
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
	if err := rows.Err(); err != nil {
		return model.Transcript{}, err
	}

	gapRows, err := s.pool.Query(ctx,
		`SELECT id, stream_id, start_sample, dropped_samples, origin
		   FROM transcript_gaps WHERE transcript_id=$1 ORDER BY start_sample`, tr.ID)
	if err != nil {
		return model.Transcript{}, err
	}
	defer gapRows.Close()
	for gapRows.Next() {
		var g model.TranscriptGap
		// dropped_samples is nullable; &g.DroppedSamples is a **int64, which pgx
		// scans as NULL -> nil.
		if err := gapRows.Scan(&g.ID, &g.StreamID, &g.StartSample, &g.DroppedSamples, &g.Origin); err != nil {
			return model.Transcript{}, err
		}
		g.TranscriptID = tr.ID
		tr.Gaps = append(tr.Gaps, g)
	}
	if err := gapRows.Err(); err != nil {
		return model.Transcript{}, err
	}

	return tr, nil
}

// GetDiarizationReview returns the diarization review payload for a note's
// transcript. Segments (turns) are sorted ascending by confidence (NULLs last)
// then by start_ms. Returns ErrNotFound if the transcript does not exist or
// does not belong to the owner.
func (s *Store) GetDiarizationReview(ctx context.Context, ownerID, noteID string) (*model.DiarizationReview, error) {
	var transcriptID string
	var reviewState string
	var generation int
	err := s.pool.QueryRow(ctx,
		`SELECT t.id, t.review_state, t.generation
		 FROM transcripts t
		 JOIN notes n ON n.id = t.note_id
		 WHERE t.note_id = $1 AND n.owner_id = $2`,
		noteID, ownerID).
		Scan(&transcriptID, &reviewState, &generation)
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
		Generation:  generation,
	}, nil
}

// ConfirmSegmentSpeaker sets the speaker label on a segment, enforcing that
// the segment belongs to the transcript of a note owned by ownerID and that
// the transcript is still at expectedGeneration — the generation the caller
// last rendered. Ownership and existence are checked first and always report
// ErrNotFound, so a wrong owner never surfaces as a version conflict; only a
// transcript that exists, is owned, and sits at the wrong generation returns
// ErrGenerationMismatch.
func (s *Store) ConfirmSegmentSpeaker(ctx context.Context, ownerID, noteID, segmentID, speaker string, expectedGeneration int) error {
	var transcriptID string
	var generation int
	err := s.pool.QueryRow(ctx,
		`SELECT t.id, t.generation FROM transcripts t
		 JOIN notes n ON n.id = t.note_id
		 WHERE t.note_id = $1 AND n.owner_id = $2`,
		noteID, ownerID).Scan(&transcriptID, &generation)
	if errors.Is(err, pgx.ErrNoRows) || isUUIDSyntaxError(err) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if generation != expectedGeneration {
		return ErrGenerationMismatch
	}

	tag, err := s.pool.Exec(ctx,
		`UPDATE transcript_segments SET speaker = $1
		 WHERE id = $2 AND transcript_id = $3`,
		speaker, segmentID, transcriptID)
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
// Returns ErrNotFound if the transcript doesn't exist or isn't owned by
// ownerID — checked first, so a wrong owner never surfaces as a version
// conflict. expectedGeneration must match the transcript's current generation
// or the update is rejected with ErrGenerationMismatch: the transcript this
// caller was reviewing has since been replaced.
func (s *Store) UpdateReviewState(ctx context.Context, ownerID, noteID, newState string, expectedGeneration int) error {
	// Fetch current state + generation with ownership check.
	var currentState string
	var generation int
	err := s.pool.QueryRow(ctx,
		`SELECT t.review_state, t.generation
		 FROM transcripts t
		 JOIN notes n ON n.id = t.note_id
		 WHERE t.note_id = $1 AND n.owner_id = $2`,
		noteID, ownerID).
		Scan(&currentState, &generation)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}

	// Compare the generation BEFORE validating the transition, using the value
	// just read. currentState belongs to the REPLACEMENT, not to the transcript
	// this caller was reviewing, so validating first reports a stale submission
	// as ErrInvalidTransition (422 "invalid state transition") whenever the
	// replacement's state happens to make the requested transition illegal —
	// telling a client its transition was wrong when the real answer is "the
	// transcript changed, refetch". Spec §7's race table requires a conflict.
	if generation != expectedGeneration {
		return ErrGenerationMismatch
	}

	// Validate transition.
	if !isLegalTransition(currentState, newState) {
		return ErrInvalidTransition
	}

	tag, err := s.pool.Exec(ctx,
		`UPDATE transcripts SET review_state = $1
		 WHERE note_id = $2 AND generation = $3`,
		newState, noteID, expectedGeneration)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrGenerationMismatch
	}
	return nil
}

// SetReviewState updates a transcript's review_state without ownership or
// transition checks. It is reserved for trusted in-process pipeline code.
// expectedGeneration must match the transcript's current generation or the
// update is rejected with ErrGenerationMismatch.
func (s *Store) SetReviewState(ctx context.Context, noteID, state string, expectedGeneration int) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE transcripts SET review_state=$1
		  WHERE note_id=$2 AND generation=$3`,
		state, noteID, expectedGeneration)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrGenerationMismatch
	}
	return nil
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
