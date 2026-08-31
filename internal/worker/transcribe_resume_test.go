package worker_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/config"
	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/plugin"
	"github.com/abedegno/muesli/internal/plugintest"
	"github.com/abedegno/muesli/internal/storage"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
	"github.com/abedegno/muesli/internal/worker"
)

type resumeFixture struct {
	proc            *worker.Processor
	st              *store.Store
	prov            storage.Provider
	ownerID         string
	noteID          string
	audioKey        string
	transcribeCalls *atomic.Int64
}

// newResumeFixture builds a transcribe pipeline whose transcriber stub counts
// its calls and whose audio object really exists on disk — the second matters
// because "the audio was not discarded" is only observable if there was
// something to discard.
func newResumeFixture(t *testing.T, retention string, segments []model.Segment) *resumeFixture {
	t.Helper()
	return newResumeFixtureWithStorageWrap(t, retention, segments, nil)
}

// newResumeFixtureWithStorageWrap is newResumeFixture with an optional hook
// to wrap the real local storage.Provider before it is handed to the
// Processor — e.g. to make Delete fail on demand without touching the
// underlying object, so a test can inject a transient storage error while
// still exercising a real filesystem-backed provider for everything else.
// wrap may be nil, in which case this is exactly newResumeFixture.
func newResumeFixtureWithStorageWrap(t *testing.T, retention string, segments []model.Segment, wrap func(storage.Provider) storage.Provider) *resumeFixture {
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
	var wrapped storage.Provider = prov
	if wrap != nil {
		wrapped = wrap(prov)
	}

	var calls atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/info", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(plugin.Info{Name: "stub-resume-transcriber", Version: "0", PluginAPI: 1, Kind: "transcriber"})
	})
	mux.HandleFunc("/transcribe", func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_ = json.NewEncoder(w).Encode(plugin.TranscribeResponse{
			Segments: segments, Language: "en", Model: "stub-resume", DurationMS: 3000,
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

	u, err := st.CreateUser(ctx, "resume@example.com", "h")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	n, err := st.CreateNote(ctx, u.ID, "Resume")
	if err != nil {
		t.Fatalf("create note: %v", err)
	}
	key := "notes/" + n.ID + "/audio/a.webm"
	writeStorageObject(t, root, key, []byte("audio-bytes-for-resume"))
	if err := st.SetNoteAudio(ctx, u.ID, n.ID, key); err != nil {
		t.Fatalf("set note audio: %v", err)
	}

	proc := worker.NewProcessor(st, cr, wrapped, config.Config{AudioRetention: retention}, nil)
	return &resumeFixture{proc: proc, st: st, prov: wrapped, ownerID: u.ID, noteID: n.ID, audioKey: key, transcribeCalls: &calls}
}

// writeStorageObject places a real object under the local provider's root, the
// way an upload would.
func writeStorageObject(t *testing.T, root, key string, data []byte) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir object dir: %v", err)
	}
	if err := os.WriteFile(full, data, 0o644); err != nil {
		t.Fatalf("write object: %v", err)
	}
}

// enqueueAndRun enqueues a fresh transcribe job and processes it once.
func (f *resumeFixture) enqueue(t *testing.T, expectedGeneration int) string {
	t.Helper()
	jobID, err := f.st.EnqueueJob(context.Background(), f.noteID, model.JobTranscribe,
		json.RawMessage(`{"audio_key":"`+f.audioKey+`","expected_generation":`+itoa(expectedGeneration)+`}`))
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	return jobID
}

func (f *resumeFixture) runOnce(t *testing.T, wantJobID string) {
	t.Helper()
	ctx := context.Background()
	job, ok, err := f.st.ClaimJob(ctx, 30*time.Second)
	if err != nil || !ok {
		t.Fatalf("claim job: ok=%v err=%v", ok, err)
	}
	if job.ID != wantJobID {
		t.Fatalf("claimed job %s, want %s", job.ID, wantJobID)
	}
	f.proc.Process(ctx, job)
}

// clearRetryLease makes a job with a pending retry immediately claimable again.
func (f *resumeFixture) clearRetryLease(t *testing.T, jobID string) {
	t.Helper()
	if _, err := f.st.Pool().Exec(context.Background(),
		"UPDATE jobs SET lease_expires_at = NULL WHERE id=$1", jobID); err != nil {
		t.Fatalf("clear retry lease: %v", err)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

var plainResumeSegments = []model.Segment{
	{StartMS: 0, EndMS: 1500, Text: "Welcome everyone.", Source: "mic"},
	{StartMS: 1500, EndMS: 3000, Text: "Let's begin.", Source: "system"},
}

// TestRunTranscribeRetryAfterPublicationResumesWithoutRepublishing binds the
// durable publication checkpoint. The first attempt publishes and then fails
// retryably in the summarize fan-out; the retry must resume AFTER publication —
// doing the remaining downstream work without transcribing or saving again.
//
// Advancing the payload's expected generation to the saved one (the shape this
// replaced) passes none of this: the retry calls the plugin a second time and
// replaces its own transcript, so both the call count and the transcript
// identity change.
func TestRunTranscribeRetryAfterPublicationResumesWithoutRepublishing(t *testing.T) {
	ctx := context.Background()
	f := newResumeFixture(t, "keep", plainResumeSegments)
	installOneShotSummarizeFanoutFailure(t, f.st)

	jobID := f.enqueue(t, 0)
	f.runOnce(t, jobID)

	afterFirst, err := f.st.GetJob(ctx, jobID)
	if err != nil {
		t.Fatalf("GetJob after attempt 1: %v", err)
	}
	if afterFirst.Status != model.JobPending {
		t.Fatalf("job status after attempt 1 = %q, want %q (retryable fan-out failure)", afterFirst.Status, model.JobPending)
	}
	published, err := f.st.GetTranscript(ctx, f.noteID)
	if err != nil {
		t.Fatalf("GetTranscript after attempt 1: %v", err)
	}
	if published.Generation != 1 {
		t.Fatalf("generation after attempt 1 = %d, want 1", published.Generation)
	}

	f.clearRetryLease(t, jobID)
	f.runOnce(t, jobID)

	if calls := f.transcribeCalls.Load(); calls != 1 {
		t.Fatalf("transcribe plugin called %d times, want 1 — the retry re-transcribed instead of resuming", calls)
	}
	got, err := f.st.GetTranscript(ctx, f.noteID)
	if err != nil {
		t.Fatalf("GetTranscript after retry: %v", err)
	}
	if got.ID != published.ID {
		t.Fatalf("transcript id = %q after the retry, want the published %q — the retry replaced its own transcript", got.ID, published.ID)
	}
	if got.Generation != 1 {
		t.Fatalf("generation = %d after the retry, want 1 (no second publication)", got.Generation)
	}

	afterRetry, err := f.st.GetJob(ctx, jobID)
	if err != nil {
		t.Fatalf("GetJob after retry: %v", err)
	}
	if afterRetry.Status != model.JobDone {
		t.Fatalf("job status after retry = %q, want %q", afterRetry.Status, model.JobDone)
	}
	// The downstream work the first attempt never finished is done by the retry.
	if _, ok, _ := f.st.ClaimJob(ctx, 30*time.Second); !ok {
		t.Fatal("no summarize job enqueued by the retry — the resume skipped the downstream work as well")
	}
}

// TestRunTranscribeRetryPreservesReviewMadeAfterPublication is the reason the
// checkpoint replaced the payload advance rather than sitting alongside it.
// Review mutations do NOT increment the generation, so a diarization review a
// user makes between the failure and the retry sits at exactly the generation
// an advanced payload would match — and SaveTranscript deletes before it
// inserts. Stuck is recoverable; erased review work is not.
func TestRunTranscribeRetryPreservesReviewMadeAfterPublication(t *testing.T) {
	ctx := context.Background()
	f := newResumeFixture(t, "keep", plainResumeSegments)
	installOneShotSummarizeFanoutFailure(t, f.st)

	jobID := f.enqueue(t, 0)
	f.runOnce(t, jobID)

	published, err := f.st.GetTranscript(ctx, f.noteID)
	if err != nil {
		t.Fatalf("GetTranscript after attempt 1: %v", err)
	}
	if len(published.Segments) == 0 {
		t.Fatal("no segments published")
	}
	reviewedSegmentID := published.Segments[0].ID

	// The user reviews the published transcript while the job sits in retry.
	// Neither mutation moves the generation — that is the whole point.
	if err := f.st.UpdateReviewState(ctx, f.ownerID, f.noteID, model.ReviewStateInReview, published.Generation); err != nil {
		t.Fatalf("user opens the review: %v", err)
	}
	if err := f.st.ConfirmSegmentSpeaker(ctx, f.ownerID, f.noteID, reviewedSegmentID, "Alice", published.Generation); err != nil {
		t.Fatalf("user confirms a speaker: %v", err)
	}

	f.clearRetryLease(t, jobID)
	f.runOnce(t, jobID)

	got, err := f.st.GetTranscript(ctx, f.noteID)
	if err != nil {
		t.Fatalf("GetTranscript after retry: %v", err)
	}
	if got.ID != published.ID || got.Generation != published.Generation {
		t.Fatalf("transcript = id %q generation %d after the retry, want id %q generation %d — the retry destroyed the reviewed transcript",
			got.ID, got.Generation, published.ID, published.Generation)
	}
	if got.ReviewState != model.ReviewStateInReview {
		t.Fatalf("review_state = %q after the retry, want %q (the user's transition survives)", got.ReviewState, model.ReviewStateInReview)
	}
	var confirmed string
	for _, seg := range got.Segments {
		if seg.ID == reviewedSegmentID {
			confirmed = seg.Speaker
		}
	}
	if confirmed != "Alice" {
		t.Fatalf("reviewed segment speaker = %q, want %q — the user's diarization review was erased by the retry", confirmed, "Alice")
	}
}

// TestRunTranscribeSkipsNoteWorkWhenTranscriptReplacedAfterSave binds the
// generation re-check that guards the eight note-level mutations following the
// save: SetNoteHashes, SetNotePartialTranscript (twice), SetRetentionState
// (three times, one of which DISCARDS AUDIO), DeleteNoteSummaries and
// EnqueueSummarizeJobs. Only SetReviewState ever carried a generation.
//
// The test pauses immediately after publication, replaces the transcript from
// another writer, and then asserts that not one of the eight ran. Each assertion
// is against pre-seeded state that differs from what the mutation would write,
// so "not called" is distinguishable from "called with the default".
func TestRunTranscribeSkipsNoteWorkWhenTranscriptReplacedAfterSave(t *testing.T) {
	ctx := context.Background()
	f := newResumeFixture(t, "discard", plainResumeSegments)

	// Pre-seed each observable so its untouched value is distinctive.
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
	restore := worker.SetTestHookAfterTranscriptPublished(func() {
		// Another writer publishes over this job's transcript in the window
		// between its save and its note-level work.
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

	// 1. SetNoteHashes
	if raw, normalized := noteHashes(t, f.st, f.noteID); raw != "" || normalized != "" {
		t.Fatalf("note hashes = (%q, %q), want both empty — SetNoteHashes ran on a superseded transcript", raw, normalized)
	}
	// 2. SetNotePartialTranscript
	var partial bool
	var retentionState string
	if err := f.st.Pool().QueryRow(ctx,
		`SELECT partial_transcript, retention_state FROM notes WHERE id=$1`, f.noteID).Scan(&partial, &retentionState); err != nil {
		t.Fatalf("read note flags: %v", err)
	}
	if !partial {
		t.Fatal("partial_transcript was cleared — SetNotePartialTranscript ran on a superseded transcript")
	}
	// 3. SetRetentionState
	if retentionState != "pending" {
		t.Fatalf("retention_state = %q, want the seeded default %q — SetRetentionState ran on a superseded transcript", retentionState, "pending")
	}
	// 4. The audio itself
	exists, _, err := f.prov.Verify(f.audioKey)
	if err != nil {
		t.Fatalf("Verify audio: %v", err)
	}
	if !exists {
		t.Fatal("the audio object was deleted by a job whose transcript had already been replaced")
	}
	// 5. DeleteNoteSummaries
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
	// 6. EnqueueSummarizeJobs
	var summarizeJobs int
	if err := f.st.Pool().QueryRow(ctx,
		`SELECT count(*) FROM jobs WHERE note_id=$1 AND type=$2`, f.noteID, model.JobSummarize).Scan(&summarizeJobs); err != nil {
		t.Fatalf("count summarize jobs: %v", err)
	}
	if summarizeJobs != 0 {
		t.Fatalf("summarize jobs enqueued = %d, want 0 — EnqueueSummarizeJobs ran on a superseded transcript", summarizeJobs)
	}
	// The replacement is untouched.
	got, err := f.st.GetTranscript(ctx, f.noteID)
	if err != nil {
		t.Fatalf("GetTranscript: %v", err)
	}
	if got.Generation != 2 || len(got.Segments) != 1 || got.Segments[0].Text != "newer transcript" {
		t.Fatalf("transcript = %+v, want the replacement at generation 2", got)
	}
}

// TestRunTranscribeDoesNotDiscardAudioForASupersededTranscript binds the SECOND
// generation re-check — the one inside applyAudioRetention. The group check
// above cannot bind it: with that check in place the job aborts long before
// retention, so removing the retention check alone leaves
// TestRunTranscribeSkipsNoteWorkWhenTranscriptReplacedAfterSave green.
//
// Deleting audio is the only irreversible thing runTranscribe does, and the
// step furthest from the group check — a long-running audio hash sits between
// them. This moves the transcript inside exactly that window and asserts the
// audio survives.
func TestRunTranscribeDoesNotDiscardAudioForASupersededTranscript(t *testing.T) {
	ctx := context.Background()
	f := newResumeFixture(t, "discard", plainResumeSegments)

	var hookErr error
	restore := worker.SetTestHookBeforeAudioRetention(func() {
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
		t.Fatal("audio deleted on the authority of a transcript that had already been replaced")
	}
	var retentionState string
	if err := f.st.Pool().QueryRow(ctx,
		`SELECT retention_state FROM notes WHERE id=$1`, f.noteID).Scan(&retentionState); err != nil {
		t.Fatalf("read retention state: %v", err)
	}
	if retentionState != "pending" {
		t.Fatalf("retention_state = %q, want the seeded default %q", retentionState, "pending")
	}
}
