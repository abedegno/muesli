package worker_test

import (
	"context"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/plugintest"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/worker"
)

// claimAndProcessOne claims the single job the pipeline fixture already
// enqueued and runs it once. Every test in this file deliberately aborts
// runTranscribe at (or before) the very first note-level write once a
// replacement lands, so exactly one job is ever produced — no drain loop
// needed.
func claimAndProcessOne(t *testing.T, proc *worker.Processor, st *store.Store) model.Job {
	t.Helper()
	ctx := context.Background()
	job, ok, err := st.ClaimJob(ctx, 30*time.Second)
	if err != nil || !ok {
		t.Fatalf("claim job: ok=%v err=%v", ok, err)
	}
	proc.Process(ctx, job)
	return job
}

// replaceTranscriptAtGeneration1 installs a package-level test hook that,
// when fired, publishes a fresh transcript for noteID at expectedGeneration
// 1 (the generation every fixture in this file publishes as its own first
// transcript) — simulating a second, newer transcribe job winning the race
// in the gap between the shared group re-check and one specific note-level
// write. Returns the restore func.
func replaceTranscriptAtGeneration1(t *testing.T, st *store.Store, noteID string, install func(func()) func()) {
	t.Helper()
	var hookErr error
	restore := install(func() {
		if _, err := st.SaveTranscript(context.Background(), model.Transcript{
			NoteID:            noteID,
			TranscriberPlugin: "whisper",
			Model:             "newer",
			Segments:          []model.Segment{{StartMS: 0, EndMS: 900, Text: "newer transcript", Source: "mic"}},
		}, 1); err != nil {
			hookErr = err
		}
	})
	t.Cleanup(func() {
		restore()
		if hookErr != nil {
			t.Fatalf("replacement writer failed: %v", hookErr)
		}
	})
}

// assertJobDoneNotFailed is the common outcome every gap test in this file
// shares: the job that lost the race to a replacement must settle 'done', not
// 'failed' — aborting cleanly on a stale generation is a no-op, not an error.
func assertJobDoneNotFailed(t *testing.T, st *store.Store, jobID string) {
	t.Helper()
	got, err := st.GetJob(context.Background(), jobID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if got.Status != model.JobDone {
		t.Fatalf("job status = %q, want %q (aborting on a stale write is a no-op, not a failure)", got.Status, model.JobDone)
	}
}

// assertReplacementIntact confirms the transcript that superseded this job's
// own publication was never touched by the aborted write.
func assertReplacementIntact(t *testing.T, st *store.Store, noteID string) {
	t.Helper()
	got, err := st.GetTranscript(context.Background(), noteID)
	if err != nil {
		t.Fatalf("GetTranscript: %v", err)
	}
	if got.Generation != 2 || len(got.Segments) != 1 || got.Segments[0].Text != "newer transcript" {
		t.Fatalf("transcript = %+v, want the replacement at generation 2", got)
	}
}

// TestSetNoteHashesGapAbortsOnReplacement binds SetNoteHashesIfCurrent's own
// generation predicate: a replacement landing between the shared group
// re-check and this specific write must stop the write from applying, not
// just the write that follows it.
//
// Unlike the other gap tests in this file, this one needs a REAL audio object
// on disk: SetNoteHashesIfCurrent is only reached when hashNoteAudio actually
// succeeds (a rawHash of "" — the case with pipelineFixtureWith's registered-
// but-never-written key — skips the call, and the hook, entirely). It uses
// newResumeFixture, which writes real bytes via writeStorageObject, the same
// fixture transcribe_resume_test.go's audio-survives assertions depend on.
func TestSetNoteHashesGapAbortsOnReplacement(t *testing.T) {
	f := newResumeFixture(t, "keep", plainResumeSegments)
	replaceTranscriptAtGeneration1(t, f.st, f.noteID, worker.SetTestHookBeforeSetNoteHashes)

	jobID := f.enqueue(t, 0)
	f.runOnce(t, jobID)

	assertJobDoneNotFailed(t, f.st, jobID)
	if raw, normalized := noteHashes(t, f.st, f.noteID); raw != "" || normalized != "" {
		t.Fatalf("note hashes = (%q, %q), want both empty — SetNoteHashesIfCurrent ran on a superseded transcript", raw, normalized)
	}
	assertReplacementIntact(t, f.st, f.noteID)
}

// TestSetNotePartialTranscriptClearGapAbortsOnReplacement binds the "clear"
// call site (the success path, run after a non-partial result).
func TestSetNotePartialTranscriptClearGapAbortsOnReplacement(t *testing.T) {
	ctx := context.Background()
	proc, st, noteID, _, _ := pipelineFixtureWith(t, "keep", plugintest.NewTranscriber())

	// Pre-seed so "the write did not apply" is distinguishable from "the
	// write applied its default value".
	if err := st.SetNotePartialTranscript(ctx, noteID, true); err != nil {
		t.Fatalf("seed partial flag: %v", err)
	}

	replaceTranscriptAtGeneration1(t, st, noteID, worker.SetTestHookBeforeSetNotePartialTranscript)

	job := claimAndProcessOne(t, proc, st)

	assertJobDoneNotFailed(t, st, job.ID)
	got, err := st.GetNoteByID(ctx, noteID)
	if err != nil {
		t.Fatalf("GetNoteByID: %v", err)
	}
	if !got.PartialTranscript {
		t.Fatal("partial_transcript was cleared — SetNotePartialTranscriptIfCurrent ran on a superseded transcript")
	}
	assertReplacementIntact(t, st, noteID)
}

// TestSetNotePartialTranscriptMarkGapAbortsOnReplacement binds the "mark
// partial" call site, reached only when the plugin itself returns a partial
// result. A replacement landing here must abort cleanly (job done) rather
// than fail the job over a partial result that no longer belongs to the
// note's current transcript.
func TestSetNotePartialTranscriptMarkGapAbortsOnReplacement(t *testing.T) {
	ctx := context.Background()
	proc, st, noteID, _, _ := pipelineFixtureWith(t, "keep", plugintest.NewPartialTranscriber("gpu oom"))

	replaceTranscriptAtGeneration1(t, st, noteID, worker.SetTestHookBeforeSetNotePartialTranscript)

	job := claimAndProcessOne(t, proc, st)

	assertJobDoneNotFailed(t, st, job.ID)
	got, err := st.GetNoteByID(ctx, noteID)
	if err != nil {
		t.Fatalf("GetNoteByID: %v", err)
	}
	if got.PartialTranscript {
		t.Fatal("partial_transcript was set — SetNotePartialTranscriptIfCurrent ran on a superseded transcript")
	}
	if got.Status == model.NoteFailed {
		t.Fatal("note failed — a stale partial write must abort cleanly, not fail the job")
	}
	assertReplacementIntact(t, st, noteID)
}

// TestDeleteNoteSummariesGapAbortsOnReplacement binds
// DeleteNoteSummariesIfCurrent's own generation predicate.
func TestDeleteNoteSummariesGapAbortsOnReplacement(t *testing.T) {
	ctx := context.Background()
	proc, st, noteID, _, _ := pipelineFixtureWith(t, "keep", plugintest.NewTranscriber())
	ownerID, err := st.NoteOwnerID(ctx, noteID)
	if err != nil {
		t.Fatalf("NoteOwnerID: %v", err)
	}
	templates, err := st.TemplatesForSummary(ctx, ownerID)
	if err != nil || len(templates) == 0 {
		t.Fatalf("templates for summary: %v (%d)", err, len(templates))
	}
	summaryID, err := st.CreatePendingSummary(ctx, noteID, templates[0].ID)
	if err != nil {
		t.Fatalf("seed summary: %v", err)
	}

	replaceTranscriptAtGeneration1(t, st, noteID, worker.SetTestHookBeforeDeleteNoteSummaries)

	job := claimAndProcessOne(t, proc, st)

	assertJobDoneNotFailed(t, st, job.ID)
	summaries, err := st.GetSummaries(ctx, noteID)
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
		t.Fatalf("seeded summary %s was deleted — DeleteNoteSummariesIfCurrent ran on a superseded transcript", summaryID)
	}
	assertReplacementIntact(t, st, noteID)
}

// TestEnqueueSummarizeJobsGapAbortsOnReplacement binds
// EnqueueSummarizeJobsIfCurrent's own generation predicate. By the time this
// hook fires, DeleteNoteSummariesIfCurrent has already run (still legitimately
// at generation 1), so this isolates the SECOND write's own atomicity.
func TestEnqueueSummarizeJobsGapAbortsOnReplacement(t *testing.T) {
	ctx := context.Background()
	proc, st, noteID, _, _ := pipelineFixtureWith(t, "keep", plugintest.NewTranscriber())

	replaceTranscriptAtGeneration1(t, st, noteID, worker.SetTestHookBeforeEnqueueSummarizeJobs)

	job := claimAndProcessOne(t, proc, st)

	assertJobDoneNotFailed(t, st, job.ID)
	var summarizeJobs int
	if err := st.Pool().QueryRow(ctx,
		`SELECT count(*) FROM jobs WHERE note_id=$1 AND type=$2`, noteID, model.JobSummarize).Scan(&summarizeJobs); err != nil {
		t.Fatalf("count summarize jobs: %v", err)
	}
	if summarizeJobs != 0 {
		t.Fatalf("summarize jobs enqueued = %d, want 0 — EnqueueSummarizeJobsIfCurrent ran on a superseded transcript", summarizeJobs)
	}
	assertReplacementIntact(t, st, noteID)
}

// TestRetentionKeptGapAbortsOnReplacement binds SetRetentionStateIfCurrent's
// own generation predicate for the non-destructive "kept" path, using the
// same testHookBeforeAudioRetention entry point the existing discard-path
// test (TestRunTranscribeDoesNotDiscardAudioForASupersededTranscript) uses.
func TestRetentionKeptGapAbortsOnReplacement(t *testing.T) {
	ctx := context.Background()
	proc, st, noteID, _, _ := pipelineFixtureWith(t, "keep", plugintest.NewTranscriber())

	if _, err := st.Pool().Exec(ctx, `UPDATE notes SET retention_state='pending' WHERE id=$1`, noteID); err != nil {
		t.Fatalf("seed retention state: %v", err)
	}

	replaceTranscriptAtGeneration1(t, st, noteID, worker.SetTestHookBeforeAudioRetention)

	job := claimAndProcessOne(t, proc, st)

	assertJobDoneNotFailed(t, st, job.ID)
	var retentionState string
	if err := st.Pool().QueryRow(ctx,
		`SELECT retention_state FROM notes WHERE id=$1`, noteID).Scan(&retentionState); err != nil {
		t.Fatalf("read retention state: %v", err)
	}
	if retentionState != "pending" {
		t.Fatalf("retention_state = %q, want the seeded default %q — SetRetentionStateIfCurrent ran on a superseded transcript", retentionState, "pending")
	}
	assertReplacementIntact(t, st, noteID)
}
