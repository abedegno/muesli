package worker_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

// staleTranscribeFixture seeds a user, a note with audio, and registers the
// given transcriber stub (plus a healthy agent) as the defaults. It does NOT
// enqueue a job — callers enqueue their own transcribe job with a specific
// expected_generation so they control exactly what "stale" means.
func staleTranscribeFixture(t *testing.T, transcriberURL string) (*worker.Processor, *store.Store, string, string) {
	t.Helper()
	ctx := context.Background()
	st := store.New(testutil.NewPool(t))
	cr := testCrypto(t)
	_ = st.SeedBuiltInTemplates(ctx)

	prov, err := storage.NewLocal(t.TempDir(), "http://example.test", "http://example.test", []byte("test-signing-key-0123456789"))
	if err != nil {
		t.Fatal(err)
	}

	ag := plugintest.NewAgent()
	t.Cleanup(ag.Close)

	tp, _ := st.CreatePlugin(ctx, cr, model.Plugin{Kind: model.PluginTranscriber, Name: "t", EndpointURL: transcriberURL, Token: "x", Enabled: true, Config: json.RawMessage(`{}`)})
	ap, _ := st.CreatePlugin(ctx, cr, model.Plugin{Kind: model.PluginAgent, Name: "a", EndpointURL: ag.URL(), Token: "x", Enabled: true, Config: json.RawMessage(`{}`)})
	_ = st.SetDefaultPlugin(ctx, tp.ID)
	_ = st.SetDefaultPlugin(ctx, ap.ID)

	u, _ := st.CreateUser(ctx, "o@example.com", "h")
	n, _ := st.CreateNote(ctx, u.ID, "M")
	key := "notes/" + n.ID + "/audio/a.webm"
	_, _ = prov.PresignUpload(key, time.Minute)
	_ = st.SetNoteAudio(ctx, u.ID, n.ID, key)

	cfg := config.Config{AudioRetention: "keep"}
	proc := worker.NewProcessor(st, cr, prov, cfg, nil)
	return proc, st, n.ID, key
}

// noteClaimState reads status and transcribing_job_id directly (not exposed
// by GetNoteByID).
func noteClaimState(t *testing.T, st *store.Store, noteID string) (status string, claimedBy *string) {
	t.Helper()
	if err := st.Pool().QueryRow(context.Background(),
		`SELECT status, transcribing_job_id FROM notes WHERE id=$1`, noteID).Scan(&status, &claimedBy); err != nil {
		t.Fatalf("read note claim state: %v", err)
	}
	return status, claimedBy
}

// noteHashes reads audio_hash and normalized_audio_hash directly (not
// exposed by GetNoteByID). Empty string means NULL.
func noteHashes(t *testing.T, st *store.Store, noteID string) (rawHash, normalizedHash string) {
	t.Helper()
	var raw, normalized *string
	if err := st.Pool().QueryRow(context.Background(),
		`SELECT audio_hash, normalized_audio_hash FROM notes WHERE id=$1`, noteID).Scan(&raw, &normalized); err != nil {
		t.Fatalf("read note hashes: %v", err)
	}
	if raw != nil {
		rawHash = *raw
	}
	if normalized != nil {
		normalizedHash = *normalized
	}
	return rawHash, normalizedHash
}

// TestRunTranscribeStaleJobDetectedEarlyTouchesNothing verifies that a
// transcribe job carrying an expected_generation the note has since moved
// past is discarded before it claims the note: no status change, no claim, no
// summarize fan-out, and the job itself completes (it is a no-op, not a
// failure).
//
// The final-state assertions alone (job done, note untouched, transcript
// unchanged) would ALL still pass even with the early check deleted: the late
// check inside SaveTranscript would reject the same job, and its claim/release
// would put the note back exactly where it started. That would only prove
// "touched then correctly reverted", not "touched nothing" — the thing the
// early check exists to guarantee (skip the network round-trip and the
// claim/release pair entirely). LastBody() is nil only if /transcribe was
// never called, which is the one assertion the late-check path cannot also
// satisfy (it always calls the plugin before SaveTranscript can reject it) —
// see the mutation check recorded in task-3-report.md.
func TestRunTranscribeStaleJobDetectedEarlyTouchesNothing(t *testing.T) {
	tr := plugintest.NewTranscriber()
	t.Cleanup(tr.Close)
	proc, st, noteID, key := staleTranscribeFixture(t, tr.URL())
	ctx := context.Background()

	// Establish generation 1 and park the note at "ready", the way a finished
	// note sits between transcriptions.
	if _, err := st.SaveTranscript(ctx, model.Transcript{
		NoteID:            noteID,
		TranscriberPlugin: "whisper",
		Segments:          []model.Segment{{StartMS: 0, EndMS: 10, Text: "hi", Source: "mic"}},
	}, 0); err != nil {
		t.Fatalf("seed transcript: %v", err)
	}
	if err := st.SetNoteStatus(ctx, noteID, model.NoteReady); err != nil {
		t.Fatalf("set status: %v", err)
	}

	// This job was enqueued back when the note had no transcript at all
	// (expected_generation 0) — now stale against generation 1.
	jobID, err := st.EnqueueJob(ctx, noteID, model.JobTranscribe,
		json.RawMessage(`{"audio_key":"`+key+`","expected_generation":0}`))
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	job, ok, err := st.ClaimJob(ctx, 30*time.Second)
	if err != nil || !ok {
		t.Fatalf("claim job: ok=%v err=%v", ok, err)
	}
	proc.Process(ctx, job)

	gotJob, err := st.GetJob(ctx, jobID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if gotJob.Status != model.JobDone {
		t.Fatalf("job status = %q, want %q (a stale job is a successful no-op)", gotJob.Status, model.JobDone)
	}

	status, claimedBy := noteClaimState(t, st, noteID)
	if status != model.NoteReady {
		t.Fatalf("note status = %q, want %q (untouched)", status, model.NoteReady)
	}
	if claimedBy != nil {
		t.Fatalf("transcribing_job_id = %v, want nil (never claimed)", claimedBy)
	}

	got, err := st.GetTranscript(ctx, noteID)
	if err != nil {
		t.Fatalf("GetTranscript: %v", err)
	}
	if len(got.Segments) != 1 || got.Segments[0].Text != "hi" {
		t.Fatalf("transcript changed: %+v", got.Segments)
	}

	// The decisive assertion: the transcribe plugin's HTTP endpoint was never
	// hit at all. LastBody() is nil only if /transcribe was never called — the
	// late (post-plugin-call) mismatch path always calls it before rejecting,
	// so this is what actually distinguishes "discarded before claim" from
	// "claimed, called the plugin, then discarded and reverted".
	if body := tr.LastBody(); body != nil {
		t.Fatalf("transcribe plugin was called (body=%s); a stale job detected early must never reach it", body)
	}

	if _, ok, _ := st.ClaimJob(ctx, 30*time.Second); ok {
		t.Fatal("expected no further claimable jobs (no summarize fan-out from a discarded stale job)")
	}
}

// TestRunTranscribeStaleJobDetectedLateRestoresDisplacedStatus covers the
// TOCTOU window the early check cannot close: the job's own claim succeeds
// (matching the generation it observed), but by the time it tries to save, a
// live stream has published a newer transcript. The competing writer is the
// real CreateStreamTranscript, which is what makes this test bind the
// deliberate asymmetry it depends on — that a stream start leaves
// notes.transcribing_job_id alone, unlike SaveTranscript's unconditional
// clear. Make the two symmetric and this test fails: the losing batch job's
// release matches nothing and the note stays stuck at "transcribing".
func TestRunTranscribeStaleJobDetectedLateRestoresDisplacedStatus(t *testing.T) {
	ctx := context.Background()

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/info", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(plugin.Info{Name: "stub-late-transcriber", Version: "0", PluginAPI: 1, Kind: "transcriber"})
	})
	trSrv := httptest.NewServer(mux)
	t.Cleanup(trSrv.Close)

	proc, st, noteID, key := staleTranscribeFixture(t, trSrv.URL)

	// The handler needs the store, so wire it up now that staleTranscribeFixture
	// has built one; register it after the fact by replacing the mux handler.
	mux.HandleFunc("/transcribe", func(w http.ResponseWriter, r *http.Request) {
		// A live stream starts while this batch job is mid-flight, superseding
		// the note's stream-owned transcript (generation 1 -> 2) and leaving
		// notes.transcribing_job_id alone.
		if _, err := st.CreateStreamTranscript(context.Background(), noteID, "stream-late", "streaming", "", 1); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(plugin.TranscribeResponse{
			Segments: []model.Segment{{StartMS: 0, EndMS: 1000, Text: "late result", Source: "mic"}},
			Model:    "stub-late",
		})
	})

	// Establish generation 1 as a STREAM-owned transcript and park the note at
	// "ready". Stream-owned, because a stream start supersedes a stream-owned
	// transcript and is refused on a batch-owned one.
	live, err := st.CreateStreamTranscript(ctx, noteID, "stream-early", "streaming", "", 0)
	if err != nil {
		t.Fatalf("seed stream transcript: %v", err)
	}
	if err := st.AppendStreamSegment(ctx, live.ID, "stream-early", model.Segment{
		StartMS: 0, EndMS: 10, Text: "hi", Source: "mic",
	}); err != nil {
		t.Fatalf("seed live segment: %v", err)
	}
	if err := st.SetNoteStatus(ctx, noteID, model.NoteReady); err != nil {
		t.Fatalf("set status: %v", err)
	}

	// Correct at enqueue time (generation is 1); by the time this job reaches
	// SaveTranscript, the /transcribe handler above has already moved it to 2.
	jobID, err := st.EnqueueJob(ctx, noteID, model.JobTranscribe,
		json.RawMessage(`{"audio_key":"`+key+`","expected_generation":1}`))
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	job, ok, err := st.ClaimJob(ctx, 30*time.Second)
	if err != nil || !ok {
		t.Fatalf("claim job: ok=%v err=%v", ok, err)
	}
	proc.Process(ctx, job)

	gotJob, err := st.GetJob(ctx, jobID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if gotJob.Status != model.JobDone {
		t.Fatalf("job status = %q, want %q (a stale job is a successful no-op, never failed)", gotJob.Status, model.JobDone)
	}

	status, claimedBy := noteClaimState(t, st, noteID)
	if status != model.NoteReady {
		t.Fatalf("note status = %q, want %q (restored — neither stuck at transcribing nor failed)", status, model.NoteReady)
	}
	if claimedBy != nil {
		t.Fatalf("transcribing_job_id = %v, want nil (released)", claimedBy)
	}

	// The winner is the live stream's transcript, and the stale batch job's own
	// segments were never published.
	got, err := st.GetTranscript(ctx, noteID)
	if err != nil {
		t.Fatalf("GetTranscript: %v", err)
	}
	if got.Generation != 2 || got.StreamID == nil || *got.StreamID != "stream-late" {
		t.Fatalf("transcript = generation %d stream %v, want generation 2 owned by stream-late", got.Generation, got.StreamID)
	}
	if len(got.Segments) != 0 {
		t.Fatalf("stale job published segments into the live transcript: %+v", got.Segments)
	}

	// No summarize jobs fanned out from the discarded stale job.
	if _, ok, _ := st.ClaimJob(ctx, 30*time.Second); ok {
		t.Fatal("expected no further claimable jobs")
	}
}

// TestRunTranscribeStaleJobLateMismatchDoesNotClobberNewerClaim verifies that
// when a genuinely newer transcribe job has claimed the note after the stale
// job did, the stale job's late-detected release does not overwrite that
// newer job's claim.
func TestRunTranscribeStaleJobLateMismatchDoesNotClobberNewerClaim(t *testing.T) {
	ctx := context.Background()

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/info", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(plugin.Info{Name: "stub-late-transcriber", Version: "0", PluginAPI: 1, Kind: "transcriber"})
	})
	trSrv := httptest.NewServer(mux)
	t.Cleanup(trSrv.Close)

	proc, st, noteID, key := staleTranscribeFixture(t, trSrv.URL)

	var winnerJobID string
	mux.HandleFunc("/transcribe", func(w http.ResponseWriter, r *http.Request) {
		// Simulate a second, genuinely newer transcribe job claiming the note
		// (and bumping the transcript generation) while this job is mid-flight.
		winnerJobID = "22222222-2222-2222-2222-222222222222"
		if _, err := st.ClaimNoteForTranscription(context.Background(), noteID, winnerJobID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if _, err := st.Pool().Exec(context.Background(),
			`UPDATE transcripts SET generation = generation + 1 WHERE note_id = $1`, noteID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(plugin.TranscribeResponse{
			Segments: []model.Segment{{StartMS: 0, EndMS: 1000, Text: "late result", Source: "mic"}},
			Model:    "stub-late",
		})
	})

	if _, err := st.SaveTranscript(ctx, model.Transcript{
		NoteID:            noteID,
		TranscriberPlugin: "whisper",
		Segments:          []model.Segment{{StartMS: 0, EndMS: 10, Text: "hi", Source: "mic"}},
	}, 0); err != nil {
		t.Fatalf("seed transcript: %v", err)
	}
	if err := st.SetNoteStatus(ctx, noteID, model.NoteReady); err != nil {
		t.Fatalf("set status: %v", err)
	}

	jobID, err := st.EnqueueJob(ctx, noteID, model.JobTranscribe,
		json.RawMessage(`{"audio_key":"`+key+`","expected_generation":1}`))
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	job, ok, err := st.ClaimJob(ctx, 30*time.Second)
	if err != nil || !ok {
		t.Fatalf("claim job: ok=%v err=%v", ok, err)
	}
	proc.Process(ctx, job)

	gotJob, err := st.GetJob(ctx, jobID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if gotJob.Status != model.JobDone {
		t.Fatalf("job status = %q, want %q", gotJob.Status, model.JobDone)
	}

	// The winner's claim (status=transcribing, token=winnerJobID) must survive
	// the stale job's release attempt untouched.
	status, claimedBy := noteClaimState(t, st, noteID)
	if status != model.NoteTranscribing {
		t.Fatalf("note status = %q, want %q (winner's claim must survive)", status, model.NoteTranscribing)
	}
	if claimedBy == nil || *claimedBy != winnerJobID {
		t.Fatalf("transcribing_job_id = %v, want %q (winner's claim must survive)", claimedBy, winnerJobID)
	}
}

// TestRunTranscribeRetriedClaimPreservesOriginalPriorStatus is the regression
// test for H1: a job's FIRST attempt claims the note (displacing "ready"),
// then fails before ever reaching SaveTranscript (a plain retryable plugin
// error) — leaving the claim in place, since nothing released it. Process
// reclaims and retries the SAME job. Without persisting what the ORIGINAL
// claim displaced, that retry's own re-claim would read the note's CURRENT
// status ("transcribing", set by the first attempt) as its "prior" — so if
// the retry then hits a late generation mismatch and releases, it would
// restore "transcribing" over the note's real prior status ("ready"),
// permanently losing it. Persisting the original prior status onto the job's
// own payload at first-claim time, and reusing it on the re-claim, is what
// keeps "ready" recoverable.
func TestRunTranscribeRetriedClaimPreservesOriginalPriorStatus(t *testing.T) {
	ctx := context.Background()

	var calls int
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/info", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(plugin.Info{Name: "stub-retry-transcriber", Version: "0", PluginAPI: 1, Kind: "transcriber"})
	})
	trSrv := httptest.NewServer(mux)
	t.Cleanup(trSrv.Close)

	proc, st, noteID, key := staleTranscribeFixture(t, trSrv.URL)

	// Registered now that st/noteID exist: attempt 1 fails before
	// SaveTranscript ever runs, leaving the claim it just made in place
	// (nothing releases it on a plain retryable error). Attempt 2 (the retry)
	// simulates a concurrent writer bumping the transcript generation while
	// it's mid-flight (raw SQL here, standing for any competing writer — the
	// live-stream path specifically is covered by
	// TestRunTranscribeStaleJobDetectedLateRestoresDisplacedStatus), so this
	// attempt's own SaveTranscript hits a late mismatch and must release — the
	// observable moment H1 is about.
	mux.HandleFunc("/transcribe", func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			http.Error(w, "injected failure", http.StatusInternalServerError)
			return
		}
		if _, err := st.Pool().Exec(context.Background(),
			`UPDATE transcripts SET generation = generation + 1 WHERE note_id = $1`, noteID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(plugin.TranscribeResponse{
			Segments: []model.Segment{{StartMS: 0, EndMS: 1000, Text: "retry result", Source: "mic"}},
			Model:    "stub-retry",
		})
	})

	// Establish generation 1 and park the note at "ready" — this is the status
	// the job's claim will displace, and the value that must survive both
	// attempts.
	if _, err := st.SaveTranscript(ctx, model.Transcript{
		NoteID:            noteID,
		TranscriberPlugin: "whisper",
		Segments:          []model.Segment{{StartMS: 0, EndMS: 10, Text: "hi", Source: "mic"}},
	}, 0); err != nil {
		t.Fatalf("seed transcript: %v", err)
	}
	if err := st.SetNoteStatus(ctx, noteID, model.NoteReady); err != nil {
		t.Fatalf("set status: %v", err)
	}

	jobID, err := st.EnqueueJob(ctx, noteID, model.JobTranscribe,
		json.RawMessage(`{"audio_key":"`+key+`","expected_generation":1}`))
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	// Attempt 1: claims (ready -> transcribing), plugin call fails retryably.
	job, ok, err := st.ClaimJob(ctx, 30*time.Second)
	if err != nil || !ok {
		t.Fatalf("claim job (attempt 1): ok=%v err=%v", ok, err)
	}
	proc.Process(ctx, job)

	gotJob, err := st.GetJob(ctx, jobID)
	if err != nil {
		t.Fatalf("GetJob after attempt 1: %v", err)
	}
	if gotJob.Status != model.JobPending {
		t.Fatalf("job status after attempt 1 = %q, want %q (retryable failure)", gotJob.Status, model.JobPending)
	}
	statusAfterAttempt1, claimedByAfterAttempt1 := noteClaimState(t, st, noteID)
	if statusAfterAttempt1 != model.NoteTranscribing {
		t.Fatalf("note status after attempt 1 = %q, want %q (claim still in place)", statusAfterAttempt1, model.NoteTranscribing)
	}
	if claimedByAfterAttempt1 == nil || *claimedByAfterAttempt1 != jobID {
		t.Fatalf("transcribing_job_id after attempt 1 = %v, want %q", claimedByAfterAttempt1, jobID)
	}

	// Clear the retry lease so ClaimJob can pick it straight back up, the way
	// the shared drain() helper does.
	if _, err := st.Pool().Exec(ctx, "UPDATE jobs SET lease_expires_at = NULL WHERE id=$1", jobID); err != nil {
		t.Fatalf("clear retry lease: %v", err)
	}

	// Attempt 2 (the retry, same job id): re-claims, then hits a late
	// generation mismatch and must release.
	job2, ok, err := st.ClaimJob(ctx, 30*time.Second)
	if err != nil || !ok {
		t.Fatalf("claim job (attempt 2): ok=%v err=%v", ok, err)
	}
	if job2.ID != jobID {
		t.Fatalf("attempt 2 claimed a different job: %s, want %s", job2.ID, jobID)
	}
	proc.Process(ctx, job2)

	gotJob2, err := st.GetJob(ctx, jobID)
	if err != nil {
		t.Fatalf("GetJob after attempt 2: %v", err)
	}
	if gotJob2.Status != model.JobDone {
		t.Fatalf("job status after attempt 2 = %q, want %q (stale-detected-late is a successful no-op)", gotJob2.Status, model.JobDone)
	}

	// The core assertion: the note must be back at "ready" — the status BEFORE
	// attempt 1's claim — not "transcribing" (what attempt 2's re-claim would
	// have wrongly captured as "prior" without the fix).
	finalStatus, finalClaimedBy := noteClaimState(t, st, noteID)
	if finalStatus != model.NoteReady {
		t.Fatalf("note status after retry's release = %q, want %q (original prior status lost)", finalStatus, model.NoteReady)
	}
	if finalClaimedBy != nil {
		t.Fatalf("transcribing_job_id after release = %v, want nil", finalClaimedBy)
	}
	if calls != 2 {
		t.Fatalf("transcribe plugin calls = %d, want 2 (one per attempt)", calls)
	}
}

// TestRunTranscribeStaleJobDetectedLateDoesNotOverwriteAudioHashes is the
// regression test for H2: hashNoteAudio/SetNoteHashes moved to after
// SaveTranscript's authoritative check, so a job detected stale late (after
// its own claim, once the generation has moved on mid-flight) must never
// reach the hashing step at all — the note's audio_hash/normalized_audio_hash
// must be exactly what they were before this job ran.
//
// This does NOT reuse staleTranscribeFixture's storage setup, because that
// fixture never writes real bytes at the note's audio path — hashNoteAudio
// always fails there (file doesn't exist) regardless of where the hash block
// sits in runTranscribe, which would make "hashes unchanged" trivially true
// for the wrong reason. Real bytes are written directly at the local storage
// path here so hashNoteAudio can actually succeed and produce a real,
// distinguishable hash if the code path is reached.
func TestRunTranscribeStaleJobDetectedLateDoesNotOverwriteAudioHashes(t *testing.T) {
	ctx := context.Background()

	root := t.TempDir()
	prov, err := storage.NewLocal(root, "http://example.test", "http://example.test", []byte("test-signing-key-0123456789"))
	if err != nil {
		t.Fatal(err)
	}

	st := store.New(testutil.NewPool(t))
	cr := testCrypto(t)
	_ = st.SeedBuiltInTemplates(ctx)

	ag := plugintest.NewAgent()
	t.Cleanup(ag.Close)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/info", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(plugin.Info{Name: "stub-hash-transcriber", Version: "0", PluginAPI: 1, Kind: "transcriber"})
	})
	trSrv := httptest.NewServer(mux)
	t.Cleanup(trSrv.Close)

	tp, _ := st.CreatePlugin(ctx, cr, model.Plugin{Kind: model.PluginTranscriber, Name: "t", EndpointURL: trSrv.URL, Token: "x", Enabled: true, Config: json.RawMessage(`{}`)})
	ap, _ := st.CreatePlugin(ctx, cr, model.Plugin{Kind: model.PluginAgent, Name: "a", EndpointURL: ag.URL(), Token: "x", Enabled: true, Config: json.RawMessage(`{}`)})
	_ = st.SetDefaultPlugin(ctx, tp.ID)
	_ = st.SetDefaultPlugin(ctx, ap.ID)

	u, _ := st.CreateUser(ctx, "o@example.com", "h")
	n, _ := st.CreateNote(ctx, u.ID, "M")
	noteID := n.ID
	key := "notes/" + noteID + "/audio/a.webm"
	_, _ = prov.PresignUpload(key, time.Minute)
	_ = st.SetNoteAudio(ctx, u.ID, noteID, key)

	// Write real bytes at the local storage path so hashNoteAudio has
	// something real to hash.
	audioPath := filepath.Join(root, filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(audioPath), 0o755); err != nil {
		t.Fatalf("mkdir audio dir: %v", err)
	}
	if err := os.WriteFile(audioPath, []byte("fake audio bytes for hashing"), 0o644); err != nil {
		t.Fatalf("write audio bytes: %v", err)
	}

	// Registered now that st/noteID exist: bumps the transcript generation
	// mid-flight so this job's own SaveTranscript hits a late mismatch, the
	// same technique as TestRunTranscribeStaleJobDetectedLateRestoresDisplacedStatus.
	mux.HandleFunc("/transcribe", func(w http.ResponseWriter, r *http.Request) {
		if _, err := st.Pool().Exec(context.Background(),
			`UPDATE transcripts SET generation = generation + 1 WHERE note_id = $1`, noteID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(plugin.TranscribeResponse{
			Segments: []model.Segment{{StartMS: 0, EndMS: 1000, Text: "late result", Source: "mic"}},
			Model:    "stub-late",
		})
	})

	// Establish generation 1, park the note at "ready", and seed KNOWN hash
	// values — the ones that must survive this job untouched.
	if _, err := st.SaveTranscript(ctx, model.Transcript{
		NoteID:            noteID,
		TranscriberPlugin: "whisper",
		Segments:          []model.Segment{{StartMS: 0, EndMS: 10, Text: "hi", Source: "mic"}},
	}, 0); err != nil {
		t.Fatalf("seed transcript: %v", err)
	}
	if err := st.SetNoteStatus(ctx, noteID, model.NoteReady); err != nil {
		t.Fatalf("set status: %v", err)
	}
	const seededRawHash = "seeded-raw-hash-must-survive"
	const seededNormalizedHash = "seeded-normalized-hash-must-survive"
	if err := st.SetNoteHashes(ctx, noteID, seededRawHash, seededNormalizedHash); err != nil {
		t.Fatalf("seed hashes: %v", err)
	}

	cfg := config.Config{AudioRetention: "keep"}
	proc := worker.NewProcessor(st, cr, prov, cfg, nil)

	// Correct at enqueue time (generation is 1); the /transcribe handler above
	// bumps it to 2 before this job's own SaveTranscript runs.
	jobID, err := st.EnqueueJob(ctx, noteID, model.JobTranscribe,
		json.RawMessage(`{"audio_key":"`+key+`","expected_generation":1}`))
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	job, ok, err := st.ClaimJob(ctx, 30*time.Second)
	if err != nil || !ok {
		t.Fatalf("claim job: ok=%v err=%v", ok, err)
	}
	proc.Process(ctx, job)

	gotJob, err := st.GetJob(ctx, jobID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if gotJob.Status != model.JobDone {
		t.Fatalf("job status = %q, want %q (stale detected late is a successful no-op)", gotJob.Status, model.JobDone)
	}

	rawHash, normalizedHash := noteHashes(t, st, noteID)
	if rawHash != seededRawHash {
		t.Fatalf("audio_hash = %q, want unchanged %q (a stale job must not touch note hashes)", rawHash, seededRawHash)
	}
	if normalizedHash != seededNormalizedHash {
		t.Fatalf("normalized_audio_hash = %q, want unchanged %q", normalizedHash, seededNormalizedHash)
	}
}

// TestRunTranscribeExpectedGenerationPersistFailureIsRetryable is the
// regression test for the untested half of H3: persistTranscribePayload's
// error must propagate out of runTranscribe (making the job retryable, not a
// silent success) rather than being logged and swallowed. UpdateJobPayload's
// own RowsAffected check (the other half of H3) is already covered directly
// by TestUpdateJobPayload in internal/store/jobs_test.go.
//
// The seam: deleting this job's own row mid-flight, from inside the
// transcribe plugin stub, so UpdateJobPayload's post-save persist call (for
// the advanced expected_generation) hits its ErrNotFound. That also deletes
// the only row a later assertion could read job status from, so this proves
// the propagation a different way: if persistTranscribePayload's error
// stopped runTranscribe (the fix), execution never reaches the summarize
// fan-out below it, so no summarize jobs exist for the note. If it were
// reverted to log-and-continue, execution would reach EnqueueSummarizeJobs
// and create one summarize job per built-in template.
func TestRunTranscribeExpectedGenerationPersistFailureIsRetryable(t *testing.T) {
	ctx := context.Background()

	var jobID string

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/info", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(plugin.Info{Name: "stub-persist-fail-transcriber", Version: "0", PluginAPI: 1, Kind: "transcriber"})
	})
	trSrv := httptest.NewServer(mux)
	t.Cleanup(trSrv.Close)

	proc, st, noteID, key := staleTranscribeFixture(t, trSrv.URL)

	mux.HandleFunc("/transcribe", func(w http.ResponseWriter, r *http.Request) {
		// Delete this job's own row before responding, so that when
		// runTranscribe reaches its post-save persistTranscribePayload call
		// (after this handler returns and SaveTranscript succeeds),
		// UpdateJobPayload's UPDATE ... WHERE id=$1 affects zero rows and
		// returns ErrNotFound.
		if _, err := st.Pool().Exec(context.Background(), `DELETE FROM jobs WHERE id=$1`, jobID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(plugin.TranscribeResponse{
			Segments: []model.Segment{{StartMS: 0, EndMS: 1000, Text: "result", Source: "mic"}},
			Model:    "stub",
		})
	})

	enqueuedID, err := st.EnqueueJob(ctx, noteID, model.JobTranscribe,
		json.RawMessage(`{"audio_key":"`+key+`","expected_generation":0}`))
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	jobID = enqueuedID

	job, ok, err := st.ClaimJob(ctx, 30*time.Second)
	if err != nil || !ok {
		t.Fatalf("claim job: ok=%v err=%v", ok, err)
	}
	proc.Process(ctx, job)

	// SaveTranscript doesn't depend on the jobs table, so it succeeded before
	// the persist failure — confirms this test is really exercising the
	// post-save persist call, not an earlier failure that never reached it.
	saved, err := st.GetTranscript(ctx, noteID)
	if err != nil {
		t.Fatalf("GetTranscript: %v", err)
	}
	if len(saved.Segments) != 1 || saved.Segments[0].Text != "result" {
		t.Fatalf("transcript = %+v, want the plugin's segment (SaveTranscript should have succeeded before the persist failure)", saved.Segments)
	}

	// The job row itself is gone (deleted mid-flight), so the only way to
	// observe whether runTranscribe actually stopped at the failed persist —
	// rather than logging it and continuing, as before this fix — is that the
	// fan-out past that point never ran.
	jobs, err := st.ListJobsByNoteID(ctx, noteID)
	if err != nil {
		t.Fatalf("ListJobsByNoteID: %v", err)
	}
	for _, j := range jobs {
		if j.Type == model.JobSummarize {
			t.Fatalf("summarize job %s exists; a failed expected-generation persist must stop runTranscribe before the fan-out, not silently continue", j.ID)
		}
	}
}

// TestRunTranscribeRetryReleasesClaimLeftByFailedAttemptOnEarlyMismatch is the
// regression test for the gap between the early and late mismatch handling:
// attempt 1 claims the note (persisting ClaimedPriorStatus onto the job
// payload), then fails retryably before ever reaching SaveTranscript —
// leaving transcribing_job_id set, since a plain retryable failure releases
// nothing. A live stream then supersedes the transcript via
// CreateStreamTranscript, which deliberately preserves that claim (see
// internal/store/transcripts.go — clearing it there would strand a legitimate
// batch job). The retry (same job) now observes a generation that has already
// moved on before it ever re-claims, so it hits the EARLY mismatch branch —
// not the late one covered by
// TestRunTranscribeStaleJobDetectedLateRestoresDisplacedStatus, which only
// fires after a fresh claim and a plugin round-trip. Without releasing there
// too, the job completes successfully and nothing is ever left to clear the
// claim: the note is stuck at "transcribing" permanently.
func TestRunTranscribeRetryReleasesClaimLeftByFailedAttemptOnEarlyMismatch(t *testing.T) {
	ctx := context.Background()

	var calls int
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/info", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(plugin.Info{Name: "stub-early-after-fail-transcriber", Version: "0", PluginAPI: 1, Kind: "transcriber"})
	})
	trSrv := httptest.NewServer(mux)
	t.Cleanup(trSrv.Close)

	proc, st, noteID, key := staleTranscribeFixture(t, trSrv.URL)

	// Attempt 1 fails retryably before SaveTranscript ever runs. Attempt 2 must
	// never reach this handler at all — it is caught by the early mismatch
	// check before the plugin is ever called again.
	mux.HandleFunc("/transcribe", func(w http.ResponseWriter, r *http.Request) {
		calls++
		http.Error(w, "injected failure", http.StatusInternalServerError)
	})

	// Establish generation 1 as a STREAM-owned transcript — CreateStreamTranscript
	// refuses to supersede a batch-owned one with ErrBatchTranscriptExists — and
	// park the note at "ready", the status this job's claim will displace and
	// the value that must be restored.
	live, err := st.CreateStreamTranscript(ctx, noteID, "stream-early", "streaming", "", 0)
	if err != nil {
		t.Fatalf("seed stream transcript: %v", err)
	}
	if err := st.AppendStreamSegment(ctx, live.ID, "stream-early", model.Segment{
		StartMS: 0, EndMS: 10, Text: "hi", Source: "mic",
	}); err != nil {
		t.Fatalf("seed live segment: %v", err)
	}
	if err := st.SetNoteStatus(ctx, noteID, model.NoteReady); err != nil {
		t.Fatalf("set status: %v", err)
	}

	jobID, err := st.EnqueueJob(ctx, noteID, model.JobTranscribe,
		json.RawMessage(`{"audio_key":"`+key+`","expected_generation":1}`))
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	// Attempt 1: claims (ready -> transcribing, persisting ClaimedPriorStatus
	// "ready" onto the job payload), then the plugin call fails retryably.
	job, ok, err := st.ClaimJob(ctx, 30*time.Second)
	if err != nil || !ok {
		t.Fatalf("claim job (attempt 1): ok=%v err=%v", ok, err)
	}
	proc.Process(ctx, job)

	gotJob, err := st.GetJob(ctx, jobID)
	if err != nil {
		t.Fatalf("GetJob after attempt 1: %v", err)
	}
	if gotJob.Status != model.JobPending {
		t.Fatalf("job status after attempt 1 = %q, want %q (retryable failure)", gotJob.Status, model.JobPending)
	}
	statusAfterAttempt1, claimedByAfterAttempt1 := noteClaimState(t, st, noteID)
	if statusAfterAttempt1 != model.NoteTranscribing {
		t.Fatalf("note status after attempt 1 = %q, want %q (claim still in place)", statusAfterAttempt1, model.NoteTranscribing)
	}
	if claimedByAfterAttempt1 == nil || *claimedByAfterAttempt1 != jobID {
		t.Fatalf("transcribing_job_id after attempt 1 = %v, want %q", claimedByAfterAttempt1, jobID)
	}

	// A live stream starts, superseding the stream-owned transcript this job's
	// claim was made against (generation 1 -> 2). This is the real
	// CreateStreamTranscript, which deliberately leaves transcribing_job_id
	// alone — see internal/store/transcripts.go.
	if _, err := st.CreateStreamTranscript(ctx, noteID, "stream-supersede", "streaming", "", 1); err != nil {
		t.Fatalf("supersede stream transcript: %v", err)
	}

	// Clear the retry lease so ClaimJob can pick attempt 1's job straight back
	// up, the way the shared drain() helper does.
	if _, err := st.Pool().Exec(ctx, "UPDATE jobs SET lease_expires_at = NULL WHERE id=$1", jobID); err != nil {
		t.Fatalf("clear retry lease: %v", err)
	}

	// Attempt 2 (the retry, same job id): the note's transcript generation has
	// already moved on (1 -> 2) before this attempt ever re-claims, so it hits
	// the EARLY mismatch branch — not the late one, which only fires after a
	// fresh claim and a plugin round-trip.
	job2, ok, err := st.ClaimJob(ctx, 30*time.Second)
	if err != nil || !ok {
		t.Fatalf("claim job (attempt 2): ok=%v err=%v", ok, err)
	}
	if job2.ID != jobID {
		t.Fatalf("attempt 2 claimed a different job: %s, want %s", job2.ID, jobID)
	}
	proc.Process(ctx, job2)

	gotJob2, err := st.GetJob(ctx, jobID)
	if err != nil {
		t.Fatalf("GetJob after attempt 2: %v", err)
	}
	if gotJob2.Status != model.JobDone {
		t.Fatalf("job status after attempt 2 = %q, want %q (stale-detected-early is a successful no-op)", gotJob2.Status, model.JobDone)
	}

	// The core assertion: the note must be back at "ready" with its claim
	// cleared, not stuck at "transcribing" forever — no future attempt of this
	// job will ever run again to clear it.
	finalStatus, finalClaimedBy := noteClaimState(t, st, noteID)
	if finalStatus != model.NoteReady {
		t.Fatalf("note status after attempt 2 = %q, want %q (claim left by the failed attempt 1 was never released)", finalStatus, model.NoteReady)
	}
	if finalClaimedBy != nil {
		t.Fatalf("transcribing_job_id after attempt 2 = %v, want nil (released)", finalClaimedBy)
	}

	// The decisive assertion distinguishing "caught early" from "caught late":
	// attempt 2 must never reach the plugin at all.
	if calls != 1 {
		t.Fatalf("transcribe plugin calls = %d, want 1 (attempt 2 must be caught by the early mismatch, never reaching the plugin)", calls)
	}
}
