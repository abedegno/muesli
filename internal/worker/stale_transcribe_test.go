package worker_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
// (matching the generation it observed), but by the time it tries to save,
// another writer has published a newer transcript. This simulates that other
// writer directly via SQL rather than through SaveTranscript, standing in for
// Task 4's CreateStreamTranscript, which is documented to leave the claim
// column alone (unlike SaveTranscript's unconditional clear) — the one
// property this test needs and Task 4 hasn't landed yet to provide.
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
		// Simulate a concurrent writer publishing a newer transcript generation
		// without touching notes.transcribing_job_id.
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

	// Establish generation 1 and park the note at "ready".
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

	// Correct at enqueue time (generation is 1); by the time this job reaches
	// SaveTranscript, the /transcribe handler above has already bumped it to 2.
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

	// The stale job's own segments must never have been published.
	got, err := st.GetTranscript(ctx, noteID)
	if err != nil {
		t.Fatalf("GetTranscript: %v", err)
	}
	if len(got.Segments) != 1 || got.Segments[0].Text != "hi" {
		t.Fatalf("transcript changed by the stale job: %+v", got.Segments)
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
	// it's mid-flight, the same way
	// TestRunTranscribeStaleJobDetectedLateRestoresDisplacedStatus does, so
	// this attempt's own SaveTranscript hits a late mismatch and must
	// release — the observable moment H1 is about.
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
