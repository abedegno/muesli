package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// lockNoteAndCheckGeneration locks noteID's row and reports ErrGenerationMismatch
// if the note's transcript is not at expectedGeneration. It exists for guarded
// writes that can't express their predicate as a single UPDATE ... WHERE (a
// write with zero affected rows can be legitimate on its own, e.g. deleting a
// note's summaries when it has none, so RowsAffected()==0 can't double as the
// mismatch signal the way it does for SetNoteHashesIfGeneration and friends).
//
// Locking the note row is what makes this safe against SaveTranscript /
// CreateStreamTranscript racing in: every transcripts.generation-changing
// writer takes the note row's lock before it touches transcripts (see the
// comments on SaveTranscript and CreateStreamTranscript), so a caller that
// also locks the note row first and keeps its own generation-dependent writes
// in the SAME transaction cannot be interleaved with a replacement — either
// this call blocks until the replacement commits (and then correctly observes
// the new generation), or it runs first and the replacement waits behind it.
//
// Callers MUST run this inside a transaction and perform their generation-
// dependent writes in that same transaction, immediately after — a separate
// transaction reopens exactly the window this is meant to close.
func lockNoteAndCheckGeneration(ctx context.Context, tx pgx.Tx, noteID string, expectedGeneration int) error {
	if _, err := tx.Exec(ctx, `SELECT id FROM notes WHERE id=$1 FOR UPDATE`, noteID); err != nil {
		return err
	}
	var generation int
	err := tx.QueryRow(ctx, `SELECT generation FROM transcripts WHERE note_id=$1`, noteID).Scan(&generation)
	if errors.Is(err, pgx.ErrNoRows) {
		generation = 0
	} else if err != nil {
		return err
	}
	if generation != expectedGeneration {
		return ErrGenerationMismatch
	}
	return nil
}
