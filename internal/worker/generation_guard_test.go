package worker_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/config"
	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/storage"
	"github.com/abedegno/muesli/internal/worker"
)

// TestRunTranscribeGroupWriteGuardRejectsReplacementAfterGroupCheckPasses
// binds the narrower window issue #727 is actually about, as distinct from
// TestRunTranscribeSkipsNoteWorkWhenTranscriptReplacedAfterSave's window.
//
// That existing test moves the replacement BEFORE runTranscribe's group
// transcriptStillCurrent check ever runs, so the check itself catches it —
// which is exactly what a read-before-write check is supposed to do when the
// replacement lands early enough. It says nothing about what happens when the
// replacement instead lands AFTER that check has already reported "still
// current" and BEFORE the writes that follow it run: a read-before-write
// check, no matter how close it sits to the writes, can never see a
// replacement that commits after it already returned. Before this issue's
// fix, that gap was real: none of the eight note-level writes carried their
// own generation predicate, so a replacement landing here would have gone
// through unguarded.
//
// This test moves the replacement into exactly that gap (via
// testHookAfterGroupCheckBeforeNoteWrites, which fires after the group check
// has passed) and asserts none of the eight writes ran anyway — this time
// because each write's OWN guard (SetNoteHashesIfGeneration and friends)
// rejects it, not because the group check caught it early.
func TestRunTranscribeGroupWriteGuardRejectsReplacementAfterGroupCheckPasses(t *testing.T) {
	ctx := context.Background()
	f := newResumeFixture(t, "discard", plainResumeSegments)

	if _, err := f.st.Pool().Exec(ctx, `UPDATE notes SET partial_transcript=TRUE WHERE id=$1`, f.noteID); err != nil {
		t.Fatalf("seed partial flag: %v", err)
	}
	templates, err := f.st.TemplatesForSummary(ctx, f.ownerID)
	if err != nil || len(templates) == 0 {
		t.Fatalf("templates for summary: %v (%d)", err, len(templates))
	}
	summaryID, err := f.st.CreatePendingSummary(ctx, f.noteID, templates[0].ID)
	if err != nil {
		t.Fatalf("seed summary: %v", err)
	}

	var hookErr error
	restore := worker.SetTestHookAfterGroupCheckBeforeNoteWrites(func() {
		// The group check has already reported "still current" by the time
		// this fires. A replacement landing here is exactly the race a
		// preceding check can never close.
		if _, err := f.st.SaveTranscript(context.Background(), model.Transcript{
			NoteID:            f.noteID,
			TranscriberPlugin: "whisper",
			Model:             "newer",
			Segments:          []model.Segment{{StartMS: 0, EndMS: 900, Text: "newer transcript", Source: "mic"}},
		}, 1); err != nil {
			hookErr = err
		}
	})
	defer restore()

	jobID := f.enqueue(t, 0)
	f.runOnce(t, jobID)
	if hookErr != nil {
		t.Fatalf("replacement writer failed: %v", hookErr)
	}

	gotJob, err := f.st.GetJob(ctx, jobID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if gotJob.Status != model.JobDone {
		t.Fatalf("job status = %q, want %q (aborting cleanly is a no-op, not a failure)", gotJob.Status, model.JobDone)
	}

	if raw, normalized := noteHashes(t, f.st, f.noteID); raw != "" || normalized != "" {
		t.Fatalf("note hashes = (%q, %q), want both empty — SetNoteHashesIfGeneration ran on a superseded transcript", raw, normalized)
	}
	var partial bool
	var retentionState string
	if err := f.st.Pool().QueryRow(ctx,
		`SELECT partial_transcript, retention_state FROM notes WHERE id=$1`, f.noteID).Scan(&partial, &retentionState); err != nil {
		t.Fatalf("read note flags: %v", err)
	}
	if !partial {
		t.Fatal("partial_transcript was cleared — SetNotePartialTranscriptIfGeneration ran on a superseded transcript")
	}
	if retentionState != "pending" {
		t.Fatalf("retention_state = %q, want the seeded default %q — a retention write ran on a superseded transcript", retentionState, "pending")
	}
	exists, _, err := f.prov.Verify(f.audioKey)
	if err != nil {
		t.Fatalf("Verify audio: %v", err)
	}
	if !exists {
		t.Fatal("the audio object was deleted by a job whose transcript had already been replaced")
	}
	summaries, err := f.st.GetSummaries(ctx, f.noteID)
	if err != nil {
		t.Fatalf("GetSummaries: %v", err)
	}
	found := false
	for _, s := range summaries {
		if s.ID == summaryID {
			found = true
		}
	}
	if !found {
		t.Fatalf("seeded summary %s was deleted — DeleteNoteSummariesIfGeneration ran on a superseded transcript", summaryID)
	}
	var summarizeJobs int
	if err := f.st.Pool().QueryRow(ctx,
		`SELECT count(*) FROM jobs WHERE note_id=$1 AND type=$2`, f.noteID, model.JobSummarize).Scan(&summarizeJobs); err != nil {
		t.Fatalf("count summarize jobs: %v", err)
	}
	if summarizeJobs != 0 {
		t.Fatalf("summarize jobs enqueued = %d, want 0 — EnqueueSummarizeJobsIfGeneration ran on a superseded transcript", summarizeJobs)
	}

	got, err := f.st.GetTranscript(ctx, f.noteID)
	if err != nil {
		t.Fatalf("GetTranscript: %v", err)
	}
	if got.Generation != 2 || len(got.Segments) != 1 || got.Segments[0].Text != "newer transcript" {
		t.Fatalf("transcript = %+v, want the replacement at generation 2", got)
	}
}

// TestRunTranscribeAudioClaimGuardRejectsReplacementAfterRetentionCheckPasses
// is the audio-discard analogue of
// TestRunTranscribeGroupWriteGuardRejectsReplacementAfterGroupCheckPasses: it
// moves the replacement into the gap AFTER applyAudioRetention's own
// transcriptStillCurrent check has already passed and BEFORE
// ClaimAudioDiscard runs (via testHookAfterRetentionCheckBeforeClaim), so the
// only thing that can still reject it is ClaimAudioDiscard's own generation
// predicate — never storage.Delete, because ClaimAudioDiscard runs, and must
// fail, BEFORE storage.Delete is ever called.
//
// TestRunTranscribeDoesNotDiscardAudioForASupersededTranscript (in
// transcribe_resume_test.go) covers the wider, pre-existing window — before
// applyAudioRetention's own check runs at all — which that check alone is
// sufficient to close. This test is the one that would have failed before
// this issue's fix: with only the preceding check in place, a replacement
// landing here raced storage.Delete unguarded.
func TestRunTranscribeAudioClaimGuardRejectsReplacementAfterRetentionCheckPasses(t *testing.T) {
	ctx := context.Background()
	f := newResumeFixture(t, "discard", plainResumeSegments)

	var hookErr error
	restore := worker.SetTestHookAfterRetentionCheckBeforeClaim(func() {
		if _, err := f.st.SaveTranscript(context.Background(), model.Transcript{
			NoteID:            f.noteID,
			TranscriberPlugin: "whisper",
			Model:             "newer",
			Segments:          []model.Segment{{StartMS: 0, EndMS: 900, Text: "newer transcript", Source: "mic"}},
		}, 1); err != nil {
			hookErr = err
		}
	})
	defer restore()

	jobID := f.enqueue(t, 0)
	f.runOnce(t, jobID)
	if hookErr != nil {
		t.Fatalf("replacement writer failed: %v", hookErr)
	}

	exists, _, err := f.prov.Verify(f.audioKey)
	if err != nil {
		t.Fatalf("Verify audio: %v", err)
	}
	if !exists {
		t.Fatal("audio deleted on the authority of a transcript that had already been replaced — ClaimAudioDiscard's own guard should have rejected the claim before storage.Delete ever ran")
	}
	var retentionState string
	if err := f.st.Pool().QueryRow(ctx,
		`SELECT retention_state FROM notes WHERE id=$1`, f.noteID).Scan(&retentionState); err != nil {
		t.Fatalf("read retention state: %v", err)
	}
	if retentionState != "pending" {
		t.Fatalf("retention_state = %q, want the seeded default %q — ClaimAudioDiscard must not have written its interim state either", retentionState, "pending")
	}
}

// failingDeleteStorage wraps a real storage.Provider (a *storage.Local
// pointed at an on-disk root, the same backend newResumeFixture and
// pipelineFixtureWithStorage already use for every other worker test) and
// injects a Delete failure on demand, while every other method — including
// Verify, used below to confirm the audio object genuinely still exists —
// passes straight through to the real backend.
type failingDeleteStorage struct {
	storage.Provider
	deleteErr error
}

func (f *failingDeleteStorage) Delete(key string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	return f.Provider.Delete(key)
}

var _ storage.Provider = (*failingDeleteStorage)(nil)

// TestRunTranscribeAudioDeleteFailureLeavesNoteRetranscribable is the
// regression test for what PR #743 got wrong: it was rejected because
// reordering the discard branch to write retention_state="discarded" via a
// guarded write BEFORE calling storage.Delete meant a Delete failure left
// retention_state stuck at "discarded" with the audio object still present —
// and retranscribeConflictReason (internal/api/notes.go) treats
// retention_state=="discarded" as a PERMANENT refusal to retranscribe, with
// no path back.
//
// This injects a storage.Delete failure via failingDeleteStorage and asserts
// the exact condition retranscribeConflictReason checks: RetentionState !=
// "discarded" while the audio object is still verifiably present. It does not
// call the HTTP handler — the store-level facts that function keys off are
// asserted directly, per this issue's test-coverage requirement.
func TestRunTranscribeAudioDeleteFailureLeavesNoteRetranscribable(t *testing.T) {
	ctx := context.Background()
	f := newResumeFixture(t, "discard", plainResumeSegments)

	failing := &failingDeleteStorage{Provider: f.prov, deleteErr: errors.New("injected: storage backend unavailable")}
	proc := worker.NewProcessor(f.st, testCrypto(t), failing, config.Config{AudioRetention: "discard"}, nil)

	jobID := f.enqueue(t, 0)
	job, ok, err := f.st.ClaimJob(ctx, 30*time.Second)
	if err != nil || !ok {
		t.Fatalf("claim job: ok=%v err=%v", ok, err)
	}
	if job.ID != jobID {
		t.Fatalf("claimed job %s, want %s", job.ID, jobID)
	}
	proc.Process(ctx, job)

	gotJob, err := f.st.GetJob(ctx, jobID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if gotJob.Status != model.JobDone {
		t.Fatalf("job status = %q, want %q — a storage.Delete failure must not fail the transcribe job itself", gotJob.Status, model.JobDone)
	}

	note, err := f.st.GetNoteByID(ctx, f.noteID)
	if err != nil {
		t.Fatalf("GetNoteByID: %v", err)
	}
	// This is exactly what retranscribeConflictReason (internal/api/notes.go)
	// checks: note.AudioObjectKey == "" || note.RetentionState == "discarded".
	if note.RetentionState == "discarded" {
		t.Fatalf("retention_state = %q after a storage.Delete failure — the note is now permanently refused retranscription with its audio object still present (the PR #743 regression)", note.RetentionState)
	}
	if note.AudioObjectKey == "" {
		t.Fatal("note lost its audio object key, which would also wrongly block retranscription")
	}
	exists, _, err := f.prov.Verify(f.audioKey)
	if err != nil {
		t.Fatalf("Verify audio: %v", err)
	}
	if !exists {
		t.Fatal("audio object is gone even though storage.Delete reported an error — Delete's real success/failure and the recorded retention state have diverged")
	}
}
