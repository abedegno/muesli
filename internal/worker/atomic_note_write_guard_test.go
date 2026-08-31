package worker_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/abedegno/muesli/internal/config"
	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/plugin"
	"github.com/abedegno/muesli/internal/plugintest"
	"github.com/abedegno/muesli/internal/storage"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
	"github.com/abedegno/muesli/internal/worker"
)

// replaceTranscript commits a fresh transcript for f's note at generation
// atGeneration+1, the way a concurrent transcribe job's own SaveTranscript
// would. Used inside a testHook to land a replacement squarely inside the
// window between runTranscribe's shared group check and one specific
// generation-guarded write.
func replaceTranscript(t *testing.T, f *resumeFixture, atGeneration int) {
	t.Helper()
	if _, err := f.st.SaveTranscript(context.Background(), model.Transcript{
		NoteID:            f.noteID,
		TranscriberPlugin: "whisper",
		Model:             "newer",
		Segments:          []model.Segment{{StartMS: 0, EndMS: 900, Text: "newer transcript", Source: "mic"}},
	}, atGeneration); err != nil {
		t.Fatalf("replacement writer failed: %v", err)
	}
}

// assertJobDoneCleanly reads back jobID and fails the test unless it settled
// as 'done' — a job racing a replacement transcript in the check-to-write gap
// is a clean no-op, not a failure, exactly like the pre-check gap the
// existing TestRunTranscribeSkipsNoteWorkWhenTranscriptReplacedAfterSave
// covers.
func assertJobDoneCleanly(t *testing.T, st *store.Store, jobID string) {
	t.Helper()
	gotJob, err := st.GetJob(context.Background(), jobID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if gotJob.Status != model.JobDone {
		t.Fatalf("job status = %q, want %q (racing the check-to-write gap is a no-op, not a failure)", gotJob.Status, model.JobDone)
	}
}

// assertReplacementIntact reads back the note's transcript and fails unless
// it is still the replacement committed by replaceTranscript — i.e. the
// guarded write under test did not clobber it.
func assertReplacementIntact(t *testing.T, st *store.Store, noteID string, wantGeneration int) {
	t.Helper()
	got, err := st.GetTranscript(context.Background(), noteID)
	if err != nil {
		t.Fatalf("GetTranscript: %v", err)
	}
	if got.Generation != wantGeneration || len(got.Segments) != 1 || got.Segments[0].Text != "newer transcript" {
		t.Fatalf("transcript = %+v, want the replacement at generation %d", got, wantGeneration)
	}
}

// TestRunTranscribeSkipsSetNoteHashesWhenTranscriptReplacedInCheckToWriteGap
// binds SetNoteHashesIfCurrent's own generation guard, not the shared group
// check: the replacement lands AFTER the group check has already passed,
// squarely in the window between it and this specific write (hashing the
// audio takes real time and sits between them).
func TestRunTranscribeSkipsSetNoteHashesWhenTranscriptReplacedInCheckToWriteGap(t *testing.T) {
	f := newResumeFixture(t, "keep", plainResumeSegments)

	restore := worker.SetTestHookBeforeSetNoteHashes(func() { replaceTranscript(t, f, 1) })
	defer restore()

	jobID := f.enqueue(t, 0)
	f.runOnce(t, jobID)

	assertJobDoneCleanly(t, f.st, jobID)

	if raw, normalized := noteHashes(t, f.st, f.noteID); raw != "" || normalized != "" {
		t.Fatalf("note hashes = (%q, %q), want both empty — SetNoteHashes ran on a superseded transcript", raw, normalized)
	}
	assertReplacementIntact(t, f.st, f.noteID, 2)
}

// TestRunTranscribeSkipsPartialTranscriptClearWhenReplacedInCheckToWriteGap
// binds the generation guard on the "clear partial flag" call site (the
// common, non-partial-result path).
func TestRunTranscribeSkipsPartialTranscriptClearWhenReplacedInCheckToWriteGap(t *testing.T) {
	ctx := context.Background()
	f := newResumeFixture(t, "keep", plainResumeSegments)

	if _, err := f.st.Pool().Exec(ctx, `UPDATE notes SET partial_transcript=TRUE WHERE id=$1`, f.noteID); err != nil {
		t.Fatalf("seed partial flag: %v", err)
	}

	restore := worker.SetTestHookBeforeSetNotePartialTranscript(func() { replaceTranscript(t, f, 1) })
	defer restore()

	jobID := f.enqueue(t, 0)
	f.runOnce(t, jobID)

	assertJobDoneCleanly(t, f.st, jobID)

	var partial bool
	if err := f.st.Pool().QueryRow(ctx, `SELECT partial_transcript FROM notes WHERE id=$1`, f.noteID).Scan(&partial); err != nil {
		t.Fatalf("read partial flag: %v", err)
	}
	if !partial {
		t.Fatal("partial_transcript was cleared — SetNotePartialTranscript ran on a superseded transcript")
	}
	assertReplacementIntact(t, f.st, f.noteID, 2)
}

// newPartialResumeFixture is newResumeFixture's shape, except the transcriber
// stub reports a partial result — the only way to exercise the OTHER
// SetNotePartialTranscript call site (the one that sets the flag TRUE ahead
// of a terminal, non-retryable failure).
func newPartialResumeFixture(t *testing.T) *resumeFixture {
	t.Helper()
	ctx := context.Background()
	st := store.New(testutil.NewPool(t))
	cr := testCrypto(t)
	_ = st.SeedBuiltInTemplates(ctx)

	root := t.TempDir()
	prov, err := storage.NewLocal(root, "http://example.test", "http://example.test", []byte("test-signing-key-0123456789"))
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/info", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(plugin.Info{Name: "stub-partial-transcriber", Version: "0", PluginAPI: 1, Kind: "transcriber"})
	})
	mux.HandleFunc("/transcribe", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(plugin.TranscribeResponse{
			Segments: plainResumeSegments, Language: "en", Model: "stub-partial", DurationMS: 1000,
			Partial: true, PartialReason: "upstream cut short",
		})
	})
	trSrv := httptest.NewServer(mux)
	t.Cleanup(trSrv.Close)

	ag := plugintest.NewAgent()
	t.Cleanup(ag.Close)

	tp, _ := st.CreatePlugin(ctx, cr, model.Plugin{Kind: model.PluginTranscriber, Name: "t", EndpointURL: trSrv.URL, Token: "x", Enabled: true, Config: json.RawMessage(`{}`)})
	ap, _ := st.CreatePlugin(ctx, cr, model.Plugin{Kind: model.PluginAgent, Name: "a", EndpointURL: ag.URL(), Token: "x", Enabled: true, Config: json.RawMessage(`{}`)})
	_ = st.SetDefaultPlugin(ctx, tp.ID)
	_ = st.SetDefaultPlugin(ctx, ap.ID)

	u, err := st.CreateUser(ctx, "partial@example.com", "h")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	n, err := st.CreateNote(ctx, u.ID, "Partial")
	if err != nil {
		t.Fatalf("create note: %v", err)
	}
	key := "notes/" + n.ID + "/audio/a.webm"
	writeStorageObject(t, root, key, []byte("audio-bytes-for-partial"))
	if err := st.SetNoteAudio(ctx, u.ID, n.ID, key); err != nil {
		t.Fatalf("set note audio: %v", err)
	}

	proc := worker.NewProcessor(st, cr, prov, config.Config{AudioRetention: "keep"}, nil)
	return &resumeFixture{proc: proc, st: st, prov: prov, ownerID: u.ID, noteID: n.ID, audioKey: key}
}

// TestRunTranscribeSkipsPartialTranscriptSetWhenReplacedInCheckToWriteGap
// binds the generation guard on the "mark partial" call site. Per the
// contract this write shares with the other six (see abortStaleNoteWrite): a
// store.ErrGenerationMismatch here is treated exactly like the shared group
// check's own stale path — release the claim, log, and stop — NOT like the
// job's own "partial transcript" terminal failure. That matters because a
// replacement transcript already exists and owns the note; this job's own
// partial result is about a transcript that is no longer current and must
// not fail the note out from under the replacement.
func TestRunTranscribeSkipsPartialTranscriptSetWhenReplacedInCheckToWriteGap(t *testing.T) {
	ctx := context.Background()
	f := newPartialResumeFixture(t)

	restore := worker.SetTestHookBeforeSetNotePartialTranscript(func() { replaceTranscript(t, f, 1) })
	defer restore()

	jobID := f.enqueue(t, 0)
	f.runOnce(t, jobID)

	// The generation-guard abort wins over the job's own "partial transcript"
	// terminal failure: the job settles as a clean no-op, not a failure.
	assertJobDoneCleanly(t, f.st, jobID)

	var partial bool
	if err := f.st.Pool().QueryRow(ctx, `SELECT partial_transcript FROM notes WHERE id=$1`, f.noteID).Scan(&partial); err != nil {
		t.Fatalf("read partial flag: %v", err)
	}
	if partial {
		t.Fatal("partial_transcript was set TRUE — SetNotePartialTranscript ran on a superseded transcript")
	}
	assertReplacementIntact(t, f.st, f.noteID, 2)
}

// TestRunTranscribeSkipsDeleteNoteSummariesWhenReplacedInCheckToWriteGap
// binds DeleteNoteSummariesIfCurrent's own generation guard.
func TestRunTranscribeSkipsDeleteNoteSummariesWhenReplacedInCheckToWriteGap(t *testing.T) {
	ctx := context.Background()
	f := newResumeFixture(t, "keep", plainResumeSegments)

	templates, err := f.st.TemplatesForSummary(ctx, f.ownerID)
	if err != nil || len(templates) == 0 {
		t.Fatalf("templates for summary: %v (%d)", err, len(templates))
	}
	summaryID, err := f.st.CreatePendingSummary(ctx, f.noteID, templates[0].ID)
	if err != nil {
		t.Fatalf("seed summary: %v", err)
	}

	restore := worker.SetTestHookBeforeDeleteNoteSummaries(func() { replaceTranscript(t, f, 1) })
	defer restore()

	jobID := f.enqueue(t, 0)
	f.runOnce(t, jobID)

	assertJobDoneCleanly(t, f.st, jobID)

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
		t.Fatalf("seeded summary %s was deleted — DeleteNoteSummaries ran on a superseded transcript", summaryID)
	}
	assertReplacementIntact(t, f.st, f.noteID, 2)
}

// TestRunTranscribeSkipsEnqueueSummarizeJobsWhenReplacedInCheckToWriteGap
// binds EnqueueSummarizeJobsIfCurrent's own generation guard — the multi-
// statement fan-out that takes its own notes-row-locked transaction rather
// than a single conditional UPDATE.
func TestRunTranscribeSkipsEnqueueSummarizeJobsWhenReplacedInCheckToWriteGap(t *testing.T) {
	ctx := context.Background()
	f := newResumeFixture(t, "keep", plainResumeSegments)

	restore := worker.SetTestHookBeforeEnqueueSummarizeJobs(func() { replaceTranscript(t, f, 1) })
	defer restore()

	jobID := f.enqueue(t, 0)
	f.runOnce(t, jobID)

	assertJobDoneCleanly(t, f.st, jobID)

	var summarizeJobs int
	if err := f.st.Pool().QueryRow(ctx,
		`SELECT count(*) FROM jobs WHERE note_id=$1 AND type=$2`, f.noteID, model.JobSummarize).Scan(&summarizeJobs); err != nil {
		t.Fatalf("count summarize jobs: %v", err)
	}
	if summarizeJobs != 0 {
		t.Fatalf("summarize jobs enqueued = %d, want 0 — EnqueueSummarizeJobs ran on a superseded transcript", summarizeJobs)
	}
	assertReplacementIntact(t, f.st, f.noteID, 2)
}

// TestRunTranscribeSkipsRetentionKeptWhenReplacedInCheckToWriteGap binds
// SetRetentionStateIfCurrent's own generation guard on the non-destructive
// "keep" branch of applyAudioRetention. (The "discard" branch's own
// check-to-write-gap coverage is
// TestRunTranscribeDoesNotDiscardAudioForASupersededTranscript in
// transcribe_resume_test.go, which predates this task and already targets
// this exact window for the irreversible path.)
func TestRunTranscribeSkipsRetentionKeptWhenReplacedInCheckToWriteGap(t *testing.T) {
	ctx := context.Background()
	f := newResumeFixture(t, "keep", plainResumeSegments)

	restore := worker.SetTestHookBeforeAudioRetention(func() { replaceTranscript(t, f, 1) })
	defer restore()

	jobID := f.enqueue(t, 0)
	f.runOnce(t, jobID)

	assertJobDoneCleanly(t, f.st, jobID)

	var retentionState string
	if err := f.st.Pool().QueryRow(ctx, `SELECT retention_state FROM notes WHERE id=$1`, f.noteID).Scan(&retentionState); err != nil {
		t.Fatalf("read retention state: %v", err)
	}
	if retentionState != "pending" {
		t.Fatalf("retention_state = %q, want the seeded default %q — SetRetentionState ran on a superseded transcript", retentionState, "pending")
	}
	assertReplacementIntact(t, f.st, f.noteID, 2)
}

// deleteFailingProvider wraps a real storage.Provider and makes every Delete
// call fail with err, while delegating everything else (Open, Verify,
// PresignDownload, ...) to the wrapped provider unchanged — so the object
// really exists on disk and a test can tell "delete was never applied" from
// "delete succeeded".
type deleteFailingProvider struct {
	storage.Provider
	err error
}

func (d *deleteFailingProvider) Delete(key string) error {
	return d.err
}

// TestRunTranscribeStorageDeleteFailureLeavesRetentionUntouchedAndRetranscribable
// binds requirement 3 of the atomic note-write guard fix (and the exact
// regression PR #743's independent review caught): storage.Delete failing
// must NOT leave the note's retention_state as "discarded". Current main's
// pre-existing behaviour is to write "discarded" only AFTER a successful
// delete, log-and-swallow a delete failure, and leave retention_state
// untouched so the note stays retranscribable
// (internal/api/notes.go:retranscribeConflictReason refuses retranscription
// only once retention_state=="discarded"). This test pins that ordering
// through the new SetRetentionStateDiscardedIfCurrent transaction.
func TestRunTranscribeStorageDeleteFailureLeavesRetentionUntouchedAndRetranscribable(t *testing.T) {
	ctx := context.Background()
	wantErr := errors.New("injected transient storage failure")
	f := newResumeFixtureWithStorageWrap(t, "discard", plainResumeSegments, func(p storage.Provider) storage.Provider {
		return &deleteFailingProvider{Provider: p, err: wantErr}
	})

	jobID := f.enqueue(t, 0)
	f.runOnce(t, jobID)

	// A delete failure is swallowed exactly like the pre-existing behaviour:
	// the job is NOT failed over what is very likely a transient condition.
	assertJobDoneCleanly(t, f.st, jobID)

	note, err := f.st.GetNoteByID(ctx, f.noteID)
	if err != nil {
		t.Fatalf("GetNoteByID: %v", err)
	}
	if note.RetentionState == "discarded" {
		t.Fatal("retention_state = \"discarded\" despite storage.Delete failing — the note is now permanently refused retranscription")
	}
	if note.AudioObjectKey == "" {
		t.Fatal("note lost its audio object key despite storage.Delete failing")
	}

	exists, _, err := f.prov.Verify(f.audioKey)
	if err != nil {
		t.Fatalf("Verify audio: %v", err)
	}
	if !exists {
		t.Fatal("audio object no longer exists despite the injected Delete call reporting failure without ever running the real delete")
	}

	// retention_state alone is not the full retranscribe-eligibility check.
	// retranscribeConflictReason (internal/api/notes.go) requires BOTH:
	//   - AudioObjectKey != "" && RetentionState != "discarded", and
	//   - Status is a terminal one (ready or failed) — mid-pipeline statuses
	//     (recording/uploaded/transcribing/summarizing) are rejected too.
	// It is unexported in package api, so it can't be called directly from
	// this package; replicate its exact two-clause predicate instead of
	// asserting only the retention_state/audio-key/object-existence clause in
	// isolation. Drive the note through the rest of the pipeline this same
	// job already enqueued (the summarize fan-out it ran before reaching
	// retention) so status reaches its real terminal value, the way it would
	// in production, rather than asserting against a status this test forced
	// by hand.
	drain(t, f.proc, f.st)

	note, err = f.st.GetNoteByID(ctx, f.noteID)
	if err != nil {
		t.Fatalf("GetNoteByID after drain: %v", err)
	}
	if note.Status != model.NoteReady && note.Status != model.NoteFailed {
		t.Fatalf("note status = %q once the pipeline finished, want %q or %q — retranscribeConflictReason would still refuse this note", note.Status, model.NoteReady, model.NoteFailed)
	}
	if note.AudioObjectKey == "" || note.RetentionState == "discarded" {
		t.Fatalf("retranscribeConflictReason's audio/retention clause would refuse this note: audio_object_key=%q retention_state=%q", note.AudioObjectKey, note.RetentionState)
	}
}
