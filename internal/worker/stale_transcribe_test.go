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
