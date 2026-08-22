package store_test

import (
	"context"
	"testing"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
	"github.com/google/uuid"
)

// getNoteStatusAndClaim reads status and transcribing_job_id directly, since
// GetNoteByID does not expose the claim column.
func getNoteStatusAndClaim(t *testing.T, st *store.Store, noteID string) (status string, jobID *string) {
	t.Helper()
	if err := st.Pool().QueryRow(context.Background(),
		`SELECT status, transcribing_job_id FROM notes WHERE id=$1`, noteID).Scan(&status, &jobID); err != nil {
		t.Fatalf("read note claim state: %v", err)
	}
	return status, jobID
}

// TestClaimNoteForTranscriptionSetsStateAndReturnsPrior verifies that
// ClaimNoteForTranscription atomically marks the note transcribing, records
// the claiming job, and returns whatever status it displaced.
func TestClaimNoteForTranscriptionSetsStateAndReturnsPrior(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	noteID := seedNote(t, st)
	jobID1 := uuid.NewString()

	prior, err := st.ClaimNoteForTranscription(ctx, noteID, jobID1)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if prior != model.NoteDraft {
		t.Fatalf("prior = %q, want %q (seedNote's initial status)", prior, model.NoteDraft)
	}

	status, claimedBy := getNoteStatusAndClaim(t, st, noteID)
	if status != model.NoteTranscribing {
		t.Fatalf("status = %q, want %q", status, model.NoteTranscribing)
	}
	if claimedBy == nil || *claimedBy != jobID1 {
		t.Fatalf("transcribing_job_id = %v, want %q", claimedBy, jobID1)
	}
}

// TestClaimNoteForTranscriptionReClaimBySameJobReturnsEmptyAndDoesNotOverwrite
// is the store-level regression test for H1: a second claim call by the SAME
// job id (simulating a retry after an earlier attempt claimed but failed
// before saving) must not report the note's now-current "transcribing" status
// as a fresh "prior" — that would let a caller mistake it for what the
// ORIGINAL claim displaced (e.g. "ready") and later restore the wrong value.
// It returns "" instead (no note status is ever the empty string), and the
// note's own status column is left untouched by the re-claim.
func TestClaimNoteForTranscriptionReClaimBySameJobReturnsEmptyAndDoesNotOverwrite(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	noteID := seedNote(t, st)
	if err := st.SetNoteStatus(ctx, noteID, model.NoteReady); err != nil {
		t.Fatalf("set status: %v", err)
	}
	jobID1 := uuid.NewString()

	first, err := st.ClaimNoteForTranscription(ctx, noteID, jobID1)
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if first != model.NoteReady {
		t.Fatalf("first claim prior = %q, want %q", first, model.NoteReady)
	}

	// Re-claim by the SAME job id (the retry path).
	second, err := st.ClaimNoteForTranscription(ctx, noteID, jobID1)
	if err != nil {
		t.Fatalf("re-claim: %v", err)
	}
	if second != "" {
		t.Fatalf("re-claim prior = %q, want \"\" (re-claim must not report a fresh prior)", second)
	}

	// The note's status must still read "transcribing" — the re-claim must not
	// have touched it (not reverted to "ready", not corrupted to anything else).
	status, claimedBy := getNoteStatusAndClaim(t, st, noteID)
	if status != model.NoteTranscribing {
		t.Fatalf("status after re-claim = %q, want %q", status, model.NoteTranscribing)
	}
	if claimedBy == nil || *claimedBy != jobID1 {
		t.Fatalf("transcribing_job_id after re-claim = %v, want %q", claimedBy, jobID1)
	}

	// A THIRD claim by a genuinely different job must still get a fresh prior
	// ("transcribing" — the value the two claims above legitimately produced),
	// proving the "" sentinel is specific to same-job re-claims, not a general
	// "never report a prior after the first claim" rule.
	otherJobID := uuid.NewString()
	third, err := st.ClaimNoteForTranscription(ctx, noteID, otherJobID)
	if err != nil {
		t.Fatalf("third claim (different job): %v", err)
	}
	if third != model.NoteTranscribing {
		t.Fatalf("third claim prior = %q, want %q", third, model.NoteTranscribing)
	}
}

// TestReleaseNoteTranscriptionClaimRestoresWhenTokenMatches verifies the
// straight-line case: nothing else has touched the note since the claim, so
// release restores exactly the status the claim displaced.
func TestReleaseNoteTranscriptionClaimRestoresWhenTokenMatches(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	noteID := seedNote(t, st)
	if err := st.SetNoteStatus(ctx, noteID, model.NoteReady); err != nil {
		t.Fatalf("set status: %v", err)
	}
	jobID1 := uuid.NewString()

	prior, err := st.ClaimNoteForTranscription(ctx, noteID, jobID1)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if prior != model.NoteReady {
		t.Fatalf("prior = %q, want %q", prior, model.NoteReady)
	}

	if err := st.ReleaseNoteTranscriptionClaim(ctx, noteID, jobID1, prior); err != nil {
		t.Fatalf("release: %v", err)
	}

	status, claimedBy := getNoteStatusAndClaim(t, st, noteID)
	if status != model.NoteReady {
		t.Fatalf("status after release = %q, want %q", status, model.NoteReady)
	}
	if claimedBy != nil {
		t.Fatalf("transcribing_job_id after release = %v, want nil", claimedBy)
	}
}

// TestReleaseNoteTranscriptionClaimNoopWhenTokenClearedByWinner verifies that
// a winning SaveTranscript (which unconditionally clears transcribing_job_id
// on commit) leaves a stale job's later release with nothing to match. This
// is the "stale claim exists when the winner saves" ordering: the release is
// a harmless no-op, and the note is correctly left at whatever the winner's
// own claim/save produced (never forced back to what the stale job displaced).
func TestReleaseNoteTranscriptionClaimNoopWhenTokenClearedByWinner(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	noteID := seedNote(t, st)
	if err := st.SetNoteStatus(ctx, noteID, model.NoteReady); err != nil {
		t.Fatalf("set status: %v", err)
	}
	staleJobID := uuid.NewString()

	// The stale job claims first.
	prior, err := st.ClaimNoteForTranscription(ctx, noteID, staleJobID)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}

	// The winner saves a transcript. SaveTranscript's UPDATE notes SET
	// transcribing_job_id=NULL commits as part of a successful save, clearing
	// the stale job's token regardless of who set it.
	if _, err := st.SaveTranscript(ctx, model.Transcript{
		NoteID:            noteID,
		TranscriberPlugin: "whisper",
		Segments:          []model.Segment{{StartMS: 0, EndMS: 10, Text: "hi", Source: "mic"}},
	}, 0); err != nil {
		t.Fatalf("winner save: %v", err)
	}

	// The stale job's release now matches nothing (transcribing_job_id is NULL,
	// not the stale job's id).
	if err := st.ReleaseNoteTranscriptionClaim(ctx, noteID, staleJobID, prior); err != nil {
		t.Fatalf("release: %v", err)
	}

	status, claimedBy := getNoteStatusAndClaim(t, st, noteID)
	// SaveTranscript never touches status, so it is left at whatever the claim
	// set it to (transcribing) — correct, since the winner may still be
	// awaiting review/summarization. The no-op release must not force it back
	// to NoteReady (the status the stale job displaced).
	if status != model.NoteTranscribing {
		t.Fatalf("status = %q, want %q (winner's save must not be clobbered by a no-op release)", status, model.NoteTranscribing)
	}
	if claimedBy != nil {
		t.Fatalf("transcribing_job_id = %v, want nil (cleared by the winner's save)", claimedBy)
	}
}

// TestReleaseNoteTranscriptionClaimNoopWhenAnotherJobClaimed verifies that if
// a second job claims the note after the stale job did, the stale job's later
// release does not clobber the second job's claim.
func TestReleaseNoteTranscriptionClaimNoopWhenAnotherJobClaimed(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	noteID := seedNote(t, st)
	if err := st.SetNoteStatus(ctx, noteID, model.NoteReady); err != nil {
		t.Fatalf("set status: %v", err)
	}
	staleJobID := uuid.NewString()
	winnerJobID := uuid.NewString()

	prior, err := st.ClaimNoteForTranscription(ctx, noteID, staleJobID)
	if err != nil {
		t.Fatalf("claim (stale): %v", err)
	}
	if _, err := st.ClaimNoteForTranscription(ctx, noteID, winnerJobID); err != nil {
		t.Fatalf("claim (winner): %v", err)
	}

	if err := st.ReleaseNoteTranscriptionClaim(ctx, noteID, staleJobID, prior); err != nil {
		t.Fatalf("release: %v", err)
	}

	status, claimedBy := getNoteStatusAndClaim(t, st, noteID)
	if status != model.NoteTranscribing {
		t.Fatalf("status = %q, want %q (winner's claim must survive)", status, model.NoteTranscribing)
	}
	if claimedBy == nil || *claimedBy != winnerJobID {
		t.Fatalf("transcribing_job_id = %v, want %q", claimedBy, winnerJobID)
	}
}

// TestSaveTranscriptMismatchLeavesTranscribingClaimIntact verifies a detail
// the late-recovery path depends on: SaveTranscript clears transcribing_job_id
// as its first statement, but that statement is inside the same transaction as
// the generation check, so a rejected (mismatched) save rolls the clear back
// too. A caller's own claim must survive its own failed save so its later
// ReleaseNoteTranscriptionClaim call can still match and restore.
func TestSaveTranscriptMismatchLeavesTranscribingClaimIntact(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	noteID := seedNote(t, st)
	jobID1 := uuid.NewString()

	// Establish generation 1 up front, then claim as jobID1 would.
	if _, err := st.SaveTranscript(ctx, model.Transcript{
		NoteID:            noteID,
		TranscriberPlugin: "whisper",
		Segments:          []model.Segment{{StartMS: 0, EndMS: 10, Text: "one", Source: "mic"}},
	}, 0); err != nil {
		t.Fatalf("seed generation 1: %v", err)
	}
	if _, err := st.ClaimNoteForTranscription(ctx, noteID, jobID1); err != nil {
		t.Fatalf("claim: %v", err)
	}

	// A save carrying a stale expected generation (0, when the note is really
	// at generation 1) must be rejected...
	_, err := st.SaveTranscript(ctx, model.Transcript{
		NoteID:            noteID,
		TranscriberPlugin: "whisper",
		Segments:          []model.Segment{{StartMS: 0, EndMS: 10, Text: "two", Source: "mic"}},
	}, 0)
	if err == nil {
		t.Fatal("expected ErrGenerationMismatch, got nil")
	}

	// ...and the claim jobID1 made before that failed save must still be there,
	// untouched by the rolled-back transaction.
	status, claimedBy := getNoteStatusAndClaim(t, st, noteID)
	if status != model.NoteTranscribing {
		t.Fatalf("status = %q, want %q (claim must survive the rejected save)", status, model.NoteTranscribing)
	}
	if claimedBy == nil || *claimedBy != jobID1 {
		t.Fatalf("transcribing_job_id = %v, want %q (rejected save must not clear it)", claimedBy, jobID1)
	}
}
