package worker_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/actionitems"
	"github.com/abedegno/muesli/internal/config"
	"github.com/abedegno/muesli/internal/crypto"
	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/plugin"
	"github.com/abedegno/muesli/internal/plugintest"
	"github.com/abedegno/muesli/internal/storage"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
	"github.com/abedegno/muesli/internal/worker"
)

func testCrypto(t *testing.T) *crypto.Crypto {
	t.Helper()
	c, err := crypto.New("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// pipelineFixture sets up store, crypto, storage, stub plugins (as defaults),
// a seeded note with audio, and an enqueued transcribe job. Returns the worker
// Processor, the store, the note id, and the transcriber + agent stubs (so tests
// can inject failures).
func pipelineFixture(t *testing.T, retention string) (*worker.Processor, *store.Store, string, *plugintest.Stub, *plugintest.Stub) {
	return pipelineFixtureWithLanguageAndTranscriber(t, retention, "", plugintest.NewTranscriber())
}

// pipelineFixtureWith is like pipelineFixture but lets the caller supply the
// transcriber stub (e.g. an empty-transcript transcriber).
func pipelineFixtureWith(t *testing.T, retention string, tr *plugintest.Stub) (*worker.Processor, *store.Store, string, *plugintest.Stub, *plugintest.Stub) {
	return pipelineFixtureWithLanguageAndTranscriber(t, retention, "", tr)
}

// pipelineFixtureWithLanguage is like pipelineFixture but forwards a
// TranscribeLanguage into the worker config.
func pipelineFixtureWithLanguage(t *testing.T, retention, transcribeLanguage string) (*worker.Processor, *store.Store, string, *plugintest.Stub, *plugintest.Stub) {
	return pipelineFixtureWithLanguageAndTranscriber(t, retention, transcribeLanguage, plugintest.NewTranscriber())
}

func pipelineFixtureWithLanguageAndTranscriber(t *testing.T, retention, transcribeLanguage string, tr *plugintest.Stub) (*worker.Processor, *store.Store, string, *plugintest.Stub, *plugintest.Stub) {
	return pipelineFixtureWithDefaults(t, retention, transcribeLanguage, tr, false, true, true)
}

func pipelineFixtureWithStorage(t *testing.T, retention string, tr *plugintest.Stub) (*worker.Processor, *store.Store, string, *plugintest.Stub, *plugintest.Stub, storage.Provider) {
	t.Helper()
	ctx := context.Background()
	st := store.New(testutil.NewPool(t))
	cr := testCrypto(t)
	_ = st.SeedBuiltInTemplates(ctx)

	prov, err := storage.NewLocal(t.TempDir(), "http://example.test", "http://example.test", []byte("test-signing-key-0123456789"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(tr.Close)
	ag := plugintest.NewAgent()
	t.Cleanup(ag.Close)

	tp, _ := st.CreatePlugin(ctx, cr, model.Plugin{Kind: model.PluginTranscriber, Name: "t", EndpointURL: tr.URL(), Token: "x", Enabled: true, Config: json.RawMessage(`{}`)})
	ap, _ := st.CreatePlugin(ctx, cr, model.Plugin{Kind: model.PluginAgent, Name: "a", EndpointURL: ag.URL(), Token: "x", Enabled: true, Config: json.RawMessage(`{}`)})
	_ = st.SetDefaultPlugin(ctx, tp.ID)
	_ = st.SetDefaultPlugin(ctx, ap.ID)

	u, _ := st.CreateUser(ctx, "o@example.com", "h")
	n, _ := st.CreateNote(ctx, u.ID, "M")
	key := "notes/" + n.ID + "/audio/a.webm"
	grant, _ := prov.PresignUpload(key, time.Minute)
	_ = grant
	_ = st.SetNoteAudio(ctx, u.ID, n.ID, key)

	cfg := config.Config{AudioRetention: retention}
	proc := worker.NewProcessor(st, cr, prov, cfg, nil)

	jobID, _ := st.EnqueueJob(ctx, n.ID, model.JobTranscribe, json.RawMessage(`{"audio_key":"`+key+`"}`))
	_ = jobID
	return proc, st, n.ID, tr, ag, prov
}

func pipelineFixtureWithDefaults(t *testing.T, retention, transcribeLanguage string, tr *plugintest.Stub, embedded, setTranscriberDefault, setAgentDefault bool) (*worker.Processor, *store.Store, string, *plugintest.Stub, *plugintest.Stub) {
	t.Helper()
	ctx := context.Background()
	st := store.New(testutil.NewPool(t))
	cr := testCrypto(t)
	_ = st.SeedBuiltInTemplates(ctx)

	prov, err := storage.NewLocal(t.TempDir(), "http://example.test", "http://example.test", []byte("test-signing-key-0123456789"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(tr.Close)
	ag := plugintest.NewAgent()
	t.Cleanup(ag.Close)

	tp, _ := st.CreatePlugin(ctx, cr, model.Plugin{Kind: model.PluginTranscriber, Name: "t", EndpointURL: tr.URL(), Token: "x", Enabled: true, Config: json.RawMessage(`{}`)})
	ap, _ := st.CreatePlugin(ctx, cr, model.Plugin{Kind: model.PluginAgent, Name: "a", EndpointURL: ag.URL(), Token: "x", Enabled: true, Config: json.RawMessage(`{}`)})
	if setTranscriberDefault {
		_ = st.SetDefaultPlugin(ctx, tp.ID)
	}
	if setAgentDefault {
		_ = st.SetDefaultPlugin(ctx, ap.ID)
	}

	u, _ := st.CreateUser(ctx, "o@example.com", "h")
	n, _ := st.CreateNote(ctx, u.ID, "M")
	// Put an audio object in storage and attach it to the note.
	key := "notes/" + n.ID + "/audio/a.webm"
	grant, _ := prov.PresignUpload(key, time.Minute)
	_ = grant // upload via handler not needed; PresignDownload only signs a URL
	_ = st.SetNoteAudio(ctx, u.ID, n.ID, key)

	cfg := config.Config{AudioRetention: retention, TranscribeLanguage: transcribeLanguage, Embedded: embedded}
	proc := worker.NewProcessor(st, cr, prov, cfg, nil)

	jobID, _ := st.EnqueueJob(ctx, n.ID, model.JobTranscribe, json.RawMessage(`{"audio_key":"`+key+`"}`))
	_ = jobID
	return proc, st, n.ID, tr, ag
}

func installOneShotSummarizeFanoutFailure(t *testing.T, st *store.Store) {
	t.Helper()
	ctx := context.Background()
	_, err := st.Pool().Exec(ctx, `
CREATE TEMP SEQUENCE fail_once_summarize_enqueue_seq START 1;
CREATE OR REPLACE FUNCTION pg_temp.fail_once_summarize_enqueue() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
	IF NEW.type = 'summarize' AND nextval('pg_temp.fail_once_summarize_enqueue_seq'::regclass) = 1 THEN
		RAISE EXCEPTION 'injected summarize enqueue failure';
	END IF;
	RETURN NEW;
END;
$$;
CREATE TRIGGER fail_once_summarize_enqueue
BEFORE INSERT ON jobs
FOR EACH ROW
EXECUTE FUNCTION pg_temp.fail_once_summarize_enqueue();
`)
	if err != nil {
		t.Fatalf("install one-shot summarize enqueue failure: %v", err)
	}
	t.Cleanup(func() {
		_, _ = st.Pool().Exec(context.Background(), `
DROP TRIGGER IF EXISTS fail_once_summarize_enqueue ON jobs;
DROP FUNCTION IF EXISTS pg_temp.fail_once_summarize_enqueue();
DROP SEQUENCE IF EXISTS pg_temp.fail_once_summarize_enqueue_seq;
`)
	})
}

// seedWebhook inserts a webhook row directly via SQL (there is no
// registration-API store method yet — that's EXT01e, out of scope here — so
// tests write the row straight to the table per the migration 0016 schema).
func seedWebhook(t *testing.T, st *store.Store, ownerID, url string, enabled bool) string {
	t.Helper()
	var id string
	err := st.Pool().QueryRow(context.Background(),
		`INSERT INTO webhooks (owner_id, url, secret, enabled) VALUES ($1,$2,$3,$4) RETURNING id`,
		ownerID, url, "shh-secret", enabled).Scan(&id)
	if err != nil {
		t.Fatalf("seed webhook: %v", err)
	}
	return id
}

// drain claims and processes jobs until the queue is empty (bounded loop).
func drain(t *testing.T, proc *worker.Processor, st *store.Store) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < 50; i++ {
		job, ok, err := st.ClaimJob(ctx, 30*time.Second)
		if err != nil {
			t.Fatalf("claim: %v", err)
		}
		if !ok {
			return
		}
		proc.Process(ctx, job)
		if _, err := st.Pool().Exec(ctx, "UPDATE jobs SET lease_expires_at = NULL WHERE id=$1", job.ID); err != nil {
			t.Fatalf("clear retry lease: %v", err)
		}
	}
	t.Fatal("drain did not terminate")
}

func seedProvisionalTranscript(t *testing.T, st *store.Store, noteID string) {
	t.Helper()
	ctx := context.Background()
	live, err := st.CreateStreamTranscript(ctx, noteID, "test-stream", "live-stub", "stream", 0)
	if err != nil {
		t.Fatalf("CreateStreamTranscript: %v", err)
	}
	if err := st.AppendStreamSegment(ctx, live.ID, "test-stream", model.Segment{
		StartMS: 0,
		EndMS:   1000,
		Text:    "provisional segment",
		Source:  "mic",
	}); err != nil {
		t.Fatalf("AppendStreamSegment: %v", err)
	}
	if err := st.SetNotePartialTranscript(ctx, noteID, true); err != nil {
		t.Fatalf("SetNotePartialTranscript: %v", err)
	}

	// The pipeline fixture already enqueued a transcribe job before this
	// provisional segment existed, so its payload's expected_generation is
	// stale (baked in as 0). Patch it to the generation the provisional
	// segment just created, mirroring what a real caller would have observed
	// had it enqueued after the live session started (see setNoteAudioAndJob
	// in internal/api/stream_e2e_test.go for the production-shaped version of
	// this same fix).
	generation, err := st.CurrentTranscriptGeneration(ctx, noteID)
	if err != nil {
		t.Fatalf("CurrentTranscriptGeneration: %v", err)
	}
	if _, err := st.Pool().Exec(ctx,
		`UPDATE jobs SET payload = jsonb_set(payload, '{expected_generation}', to_jsonb($2::int))
		 WHERE note_id=$1 AND type='transcribe' AND status='pending'`,
		noteID, generation); err != nil {
		t.Fatalf("patch transcribe job expected_generation: %v", err)
	}
}

func mustBuiltInTemplates(t *testing.T, st *store.Store) []model.Template {
	t.Helper()
	tmpls, err := st.BuiltInTemplates(context.Background())
	if err != nil {
		t.Fatalf("BuiltInTemplates: %v", err)
	}
	return tmpls
}

func claimSummarizeJobs(t *testing.T, st *store.Store, want int) []model.Job {
	t.Helper()
	ctx := context.Background()
	jobs := make([]model.Job, 0, want)
	for len(jobs) < want {
		job, ok, err := st.ClaimJob(ctx, 30*time.Second)
		if err != nil {
			t.Fatalf("ClaimJob: %v", err)
		}
		if !ok {
			t.Fatalf("claimed %d summarize jobs, want %d", len(jobs), want)
		}
		if job.Type != model.JobSummarize {
			t.Fatalf("job type = %q, want summarize", job.Type)
		}
		jobs = append(jobs, job)
	}
	return jobs
}

func TestPipelineHappyPath(t *testing.T) {
	proc, st, noteID, _, _ := pipelineFixture(t, "keep")
	drain(t, proc, st)
	ctx := context.Background()
	tmpls := mustBuiltInTemplates(t, st)

	n, _ := st.GetNoteByID(ctx, noteID)
	if n.Status != model.NoteReady {
		t.Fatalf("note status = %q, want ready", n.Status)
	}
	if n.PartialTranscript {
		t.Fatal("partial_transcript should be false after successful batch transcription")
	}
	tr, err := st.GetTranscript(ctx, noteID)
	if err != nil || len(tr.Segments) != 2 {
		t.Fatalf("transcript: %v segs=%d", err, len(tr.Segments))
	}
	sums, _ := st.GetSummaries(ctx, noteID)
	if len(sums) != len(tmpls) {
		t.Fatalf("summaries = %d, want %d (one per built-in template)", len(sums), len(tmpls))
	}
	for _, s := range sums {
		if s.Status != model.SummaryReady || len(s.Sections) == 0 {
			t.Fatalf("summary not ready: %+v", s)
		}
		if s.Truncated {
			t.Fatalf("summary from stub-agent (properly punctuated) should not be flagged truncated: %+v", s)
		}
	}
	// Audio kept.
	if n.RetentionState == "discarded" {
		t.Fatal("retention=keep should not discard audio")
	}
}

func TestPipelineReplacesProvisionalTranscriptAndClearsFlag(t *testing.T) {
	proc, st, noteID, _, _ := pipelineFixture(t, "keep")
	seedProvisionalTranscript(t, st, noteID)
	drain(t, proc, st)
	ctx := context.Background()

	n, err := st.GetNoteByID(ctx, noteID)
	if err != nil {
		t.Fatalf("GetNoteByID: %v", err)
	}
	if n.PartialTranscript {
		t.Fatal("partial_transcript should be false after successful batch transcription")
	}

	tr, err := st.GetTranscript(ctx, noteID)
	if err != nil {
		t.Fatalf("GetTranscript: %v", err)
	}
	if len(tr.Segments) != 2 {
		t.Fatalf("segments = %d, want 2 batch segments", len(tr.Segments))
	}
	if got, want := tr.Segments[0].Text, "Welcome everyone."; got != want {
		t.Fatalf("segment[0].Text = %q, want %q", got, want)
	}
	if got, want := tr.Segments[1].Text, "Let's begin."; got != want {
		t.Fatalf("segment[1].Text = %q, want %q", got, want)
	}
}

func TestPipelineSkipsTrashedNote(t *testing.T) {
	proc, st, noteID, _, _ := pipelineFixture(t, "keep")
	ctx := context.Background()

	// Trash the note after the transcribe job was enqueued.
	owner, _ := st.GetNoteByID(ctx, noteID)
	if err := st.DeleteNote(ctx, owner.OwnerID, noteID); err != nil {
		t.Fatalf("soft delete: %v", err)
	}

	// Draining must complete the queued job(s) without acting on the trashed note.
	drain(t, proc, st)

	if _, err := st.GetTranscript(ctx, noteID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("trashed note should not have a transcript, got %v", err)
	}
	if sums, _ := st.GetSummaries(ctx, noteID); len(sums) != 0 {
		t.Fatalf("trashed note should have no summaries, got %d", len(sums))
	}
	// The note remains trashed (no status change to ready/transcribing).
	if trashed, _ := st.NoteIsTrashed(ctx, noteID); !trashed {
		t.Fatal("note should still be trashed after skipped processing")
	}
}

func TestPipelineRetentionDiscard(t *testing.T) {
	proc, st, noteID, _, _, prov := pipelineFixtureWithStorage(t, "discard", plugintest.NewTranscriber())
	drain(t, proc, st)
	ctx := context.Background()
	n, _ := st.GetNoteByID(ctx, noteID)
	if n.RetentionState != "discarded" {
		t.Fatalf("retention state = %q, want discarded", n.RetentionState)
	}
	exists, _, err := prov.Verify(n.AudioObjectKey)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if exists {
		t.Fatal("retention=discard should delete the stored audio object")
	}
}

func TestPipelineRetryKeepsAudioUntilSummarizeFanoutSucceeds(t *testing.T) {
	proc, st, noteID, _, _, prov := pipelineFixtureWithStorage(t, "discard", plugintest.NewTranscriber())
	installOneShotSummarizeFanoutFailure(t, st)

	drain(t, proc, st)

	ctx := context.Background()
	n, err := st.GetNoteByID(ctx, noteID)
	if err != nil {
		t.Fatalf("GetNoteByID: %v", err)
	}
	if n.Status != model.NoteReady {
		t.Fatalf("note status = %q, want ready", n.Status)
	}
	if n.RetentionState != "discarded" {
		t.Fatalf("retention state = %q, want discarded", n.RetentionState)
	}
	exists, _, err := prov.Verify(n.AudioObjectKey)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if exists {
		t.Fatal("audio should be deleted after the retried summarize fan-out succeeds")
	}
}

func TestPipelineLanguageHintForwarded(t *testing.T) {
	proc, st, _, tr, _ := pipelineFixtureWithLanguage(t, "keep", "fr")
	drain(t, proc, st)

	if got := string(tr.LastBody()); !strings.Contains(got, `"language_hint":"fr"`) {
		t.Fatalf("transcribe body missing language hint: %s", got)
	}
}

func TestPipelineLanguageHintOmittedWhenEmpty(t *testing.T) {
	proc, st, _, tr, _ := pipelineFixtureWithLanguage(t, "keep", "")
	drain(t, proc, st)

	got := string(tr.LastBody())
	if strings.Contains(got, "language_hint") {
		t.Fatalf("transcribe body unexpectedly contains language hint: %s", got)
	}
}

func TestPipelineDiarizationHeldBeforeSummaryFanout(t *testing.T) {
	proc, st, noteID, _, _ := pipelineFixtureWith(t, "keep", plugintest.NewDiarizationTranscriber())
	drain(t, proc, st)
	ctx := context.Background()

	n, err := st.GetNoteByID(ctx, noteID)
	if err != nil {
		t.Fatalf("GetNoteByID: %v", err)
	}
	if n.Status != model.NoteTranscribing {
		t.Fatalf("note status = %q, want transcribing", n.Status)
	}

	tr, err := st.GetTranscript(ctx, noteID)
	if err != nil {
		t.Fatalf("GetTranscript: %v", err)
	}
	if tr.ReviewState != model.ReviewStatePending {
		t.Fatalf("review_state = %q, want %q", tr.ReviewState, model.ReviewStatePending)
	}

	sums, err := st.GetSummaries(ctx, noteID)
	if err != nil {
		t.Fatalf("GetSummaries: %v", err)
	}
	if len(sums) != 0 {
		t.Fatalf("summaries = %d, want 0 while diarization review is pending", len(sums))
	}
}

func TestPipelineEmbeddedAutoCompletesDiarizationReview(t *testing.T) {
	proc, st, noteID, _, _ := pipelineFixtureWithDefaults(t, "keep", "", plugintest.NewDiarizationTranscriber(), true, true, true)
	drain(t, proc, st)
	ctx := context.Background()

	n, err := st.GetNoteByID(ctx, noteID)
	if err != nil {
		t.Fatalf("GetNoteByID: %v", err)
	}
	if n.Status != model.NoteReady {
		t.Fatalf("note status = %q, want ready", n.Status)
	}

	tr, err := st.GetTranscript(ctx, noteID)
	if err != nil {
		t.Fatalf("GetTranscript: %v", err)
	}
	if tr.ReviewState != model.ReviewStateCompleted {
		t.Fatalf("review_state = %q, want %q", tr.ReviewState, model.ReviewStateCompleted)
	}
}

func TestPipelineDiarizationReviewReleaseAllowsSummaryFanout(t *testing.T) {
	proc, st, noteID, _, _ := pipelineFixtureWith(t, "keep", plugintest.NewDiarizationTranscriber())
	drain(t, proc, st)
	ctx := context.Background()

	ownerID, err := st.NoteOwnerID(ctx, noteID)
	if err != nil {
		t.Fatalf("NoteOwnerID: %v", err)
	}
	if err := st.UpdateReviewState(ctx, ownerID, noteID, model.ReviewStateInReview); err != nil {
		t.Fatalf("UpdateReviewState(in_review): %v", err)
	}
	if err := st.UpdateReviewState(ctx, ownerID, noteID, model.ReviewStateCompleted); err != nil {
		t.Fatalf("UpdateReviewState(completed): %v", err)
	}
	if err := st.EnqueueSummarizeJobs(ctx, ownerID, noteID); err != nil {
		t.Fatalf("EnqueueSummarizeJobs: %v", err)
	}

	drain(t, proc, st)

	n, err := st.GetNoteByID(ctx, noteID)
	if err != nil {
		t.Fatalf("GetNoteByID: %v", err)
	}
	if n.Status != model.NoteReady {
		t.Fatalf("note status = %q, want ready", n.Status)
	}

	tr, err := st.GetTranscript(ctx, noteID)
	if err != nil {
		t.Fatalf("GetTranscript: %v", err)
	}
	if tr.ReviewState != model.ReviewStateCompleted {
		t.Fatalf("review_state = %q, want %q", tr.ReviewState, model.ReviewStateCompleted)
	}
}

func TestPipelinePartialSuccessSummaryFails(t *testing.T) {
	proc, st, noteID, _, ag := pipelineFixture(t, "keep")
	ctx := context.Background()
	tmpls := mustBuiltInTemplates(t, st)

	// Process the transcribe job first (succeeds, enqueues one summarize job per
	// built-in template).
	job, _, _ := st.ClaimJob(ctx, 30*time.Second)
	proc.Process(ctx, job)

	// Claim every summarize job so only the chosen one can be reclaimed during
	// the retry loop.
	jobs := claimSummarizeJobs(t, st, len(tmpls))
	first := jobs[0]
	second := jobs[1]
	parked := jobs[1:]
	var firstSummaryID, secondSummaryID string
	{
		var p struct {
			SummaryID string `json:"summary_id"`
		}
		_ = json.Unmarshal(first.Payload, &p)
		firstSummaryID = p.SummaryID
		_ = json.Unmarshal(second.Payload, &p)
		secondSummaryID = p.SummaryID
	}

	// The agent fails for ALL of the first job's attempts (5xx → retryable until
	// attempts are exhausted, then terminal). Process settles each attempt and,
	// only on the terminal attempt, marks the summary failed.
	ag.FailNext(100)
	// Attempt 1 was already consumed by the ClaimJob above (attempts=1). Process it,
	// then reclaim+process until the job is no longer reclaimable (terminal).
	cur := first
	for {
		proc.Process(ctx, cur)
		if _, err := st.Pool().Exec(ctx, "UPDATE jobs SET lease_expires_at = NULL WHERE id=$1", cur.ID); err != nil {
			t.Fatalf("clear retry lease: %v", err)
		}
		rc, ok, _ := st.ClaimJob(ctx, 30*time.Second)
		if !ok {
			break
		}
		if rc.ID != cur.ID {
			// All other summarize jobs are leased, so only the same job should be
			// reclaimable here.
			t.Fatalf("unexpected job reclaimed: %s (want %s)", rc.ID, cur.ID)
		}
		cur = rc
	}

	// Now let the remaining summarize jobs succeed against a healthy agent.
	ag.FailNext(0)
	for _, job := range parked {
		proc.Process(ctx, job)
	}

	// The failed panel is failed; the healthy panel is ready; the note is ready.
	n, _ := st.GetNoteByID(ctx, noteID)
	if n.Status != model.NoteReady {
		t.Fatalf("partial success: note status = %q, want ready", n.Status)
	}
	sums, _ := st.GetSummaries(ctx, noteID)
	byID := map[string]model.Summary{}
	for _, s := range sums {
		byID[s.ID] = s
	}
	if byID[firstSummaryID].Status != model.SummaryFailed {
		t.Fatalf("first summary should be failed, got %q", byID[firstSummaryID].Status)
	}
	if byID[secondSummaryID].Status != model.SummaryReady {
		t.Fatalf("second summary should be ready, got %q", byID[secondSummaryID].Status)
	}
}

func TestPipeline_PartialSummarizeFailure(t *testing.T) {
	proc, st, noteID, _, ag := pipelineFixture(t, "keep")
	ctx := context.Background()
	tmpls := mustBuiltInTemplates(t, st)

	// Step 1: claim and process the transcribe job — it succeeds and fans out
	// one summarize job per built-in template.
	job, _, _ := st.ClaimJob(ctx, 30*time.Second)
	proc.Process(ctx, job)

	// Step 2: claim every summarize job upfront so only the first one can be
	// reclaimed while the rest stay leased.
	jobs := claimSummarizeJobs(t, st, len(tmpls))
	first := jobs[0]
	parked := jobs[1:]

	// Step 3: drive the first summarize job to terminal failure.
	// MaxJobAttempts=3 — the first attempt was consumed by ClaimJob above;
	// loop: process → reclaim → repeat until the job is no longer reclaimable.
	ag.FailNext(100)
	cur := first
	for {
		proc.Process(ctx, cur)
		if _, err := st.Pool().Exec(ctx, "UPDATE jobs SET lease_expires_at = NULL WHERE id=$1", cur.ID); err != nil {
			t.Fatalf("clear retry lease: %v", err)
		}
		rc, ok, _ := st.ClaimJob(ctx, 30*time.Second)
		if !ok {
			break
		}
		if rc.ID != cur.ID {
			t.Fatalf("unexpected job reclaimed: %s (want %s)", rc.ID, cur.ID)
		}
		cur = rc
	}

	// Step 4: let the remaining summarize jobs succeed against a healthy agent.
	ag.FailNext(0)
	for _, job := range parked {
		proc.Process(ctx, job)
	}

	// Step 5: note must reach ready (FinalizeNote was NOT skipped).
	n, _ := st.GetNoteByID(ctx, noteID)
	if n.Status != model.NoteReady {
		t.Fatalf("partial-summarize-failure: note status = %q, want ready", n.Status)
	}

	// Step 6: verify the partial-failure path is live — both status flavours present.
	sums, _ := st.GetSummaries(ctx, noteID)
	var hasFailed, hasReady bool
	for _, s := range sums {
		if s.Status == model.SummaryFailed {
			hasFailed = true
		}
		if s.Status == model.SummaryReady {
			hasReady = true
		}
	}
	if !hasFailed {
		t.Fatal("expected at least one summary with status failed")
	}
	if !hasReady {
		t.Fatal("expected at least one summary with status ready")
	}
}

func TestRunSummarize_NoDefaultAgentIsNonRetryable(t *testing.T) {
	proc, st, noteID, _, _ := pipelineFixtureWithDefaults(t, "keep", "", plugintest.NewTranscriber(), false, true, false)
	ctx := context.Background()

	transcribeJob, ok, err := st.ClaimJob(ctx, 30*time.Second)
	if err != nil {
		t.Fatalf("ClaimJob transcribe: %v", err)
	}
	if !ok {
		t.Fatal("expected transcribe job to be claimable")
	}
	proc.Process(ctx, transcribeJob)

	summarizeJob, ok, err := st.ClaimJob(ctx, 30*time.Second)
	if err != nil {
		t.Fatalf("ClaimJob summarize: %v", err)
	}
	if !ok {
		t.Fatal("expected summarize job to be claimable")
	}
	if summarizeJob.Type != model.JobSummarize {
		t.Fatalf("job type = %q, want summarize", summarizeJob.Type)
	}
	var payload struct {
		SummaryID string `json:"summary_id"`
	}
	if err := json.Unmarshal(summarizeJob.Payload, &payload); err != nil {
		t.Fatalf("unmarshal summarize payload: %v", err)
	}
	if payload.SummaryID == "" {
		t.Fatal("summarize payload missing summary_id")
	}

	proc.Process(ctx, summarizeJob)

	gotJob, err := st.GetJob(ctx, summarizeJob.ID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if gotJob.Status != model.JobFailed {
		t.Fatalf("job status = %q, want failed", gotJob.Status)
	}
	if gotJob.Attempts != 1 {
		t.Fatalf("job attempts = %d, want 1", gotJob.Attempts)
	}

	sums, err := st.GetSummaries(ctx, noteID)
	if err != nil {
		t.Fatalf("GetSummaries: %v", err)
	}
	var found bool
	for _, s := range sums {
		if s.ID == payload.SummaryID {
			found = true
			if s.Status != model.SummaryFailed {
				t.Fatalf("summary status = %q, want failed", s.Status)
			}
		}
	}
	if !found {
		t.Fatalf("summary %s not found", payload.SummaryID)
	}
}

func TestPipelineEmptyTranscriptSummarizeSucceeds(t *testing.T) {
	// Silent/very short audio yields zero transcript segments. The summarize step
	// must still succeed (summarize from notes alone) — the worker/client must send
	// "transcript": [] so the agent does not reject the request with 422.
	proc, st, noteID, _, _ := pipelineFixtureWith(t, "keep", plugintest.NewEmptyTranscriber())
	drain(t, proc, st)
	ctx := context.Background()
	tmpls := mustBuiltInTemplates(t, st)

	tr, err := st.GetTranscript(ctx, noteID)
	if err != nil {
		t.Fatalf("transcript: %v", err)
	}
	if len(tr.Segments) != 0 {
		t.Fatalf("expected empty transcript, got %d segments", len(tr.Segments))
	}

	n, _ := st.GetNoteByID(ctx, noteID)
	if n.Status != model.NoteReady {
		t.Fatalf("note status = %q, want ready", n.Status)
	}
	sums, _ := st.GetSummaries(ctx, noteID)
	if len(sums) != len(tmpls) {
		t.Fatalf("summaries = %d, want %d", len(sums), len(tmpls))
	}
	for _, s := range sums {
		if s.Status != model.SummaryReady {
			t.Fatalf("summary not ready with empty transcript: %+v", s)
		}
	}
}

func TestSummarizeFansOutOwnerTemplates(t *testing.T) {
	// Arrange: build processor + store; seed built-ins; create a user + note;
	// create a custom template owned by that user.
	proc, st, noteID, _, _ := pipelineFixture(t, "keep")
	ctx := context.Background()
	tmpls := mustBuiltInTemplates(t, st)

	// Identify the note's owner so we can create the custom template under them.
	ownerID, err := st.NoteOwnerID(ctx, noteID)
	if err != nil {
		t.Fatalf("NoteOwnerID: %v", err)
	}

	custom, err := st.CreateTemplate(ctx, ownerID, "My Custom", "after", []model.TemplateSection{
		{Heading: "Recap", Instruction: "Summarise in one sentence."},
	}, true, "", "", nil)
	if err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}

	// Act: drive the full pipeline to completion.
	drain(t, proc, st)

	// Assert: note is ready.
	n, _ := st.GetNoteByID(ctx, noteID)
	if n.Status != model.NoteReady {
		t.Fatalf("note status = %q, want ready", n.Status)
	}

	// Assert: there is a summary for the custom template plus all built-ins.
	sums, err := st.GetSummaries(ctx, noteID)
	if err != nil {
		t.Fatalf("GetSummaries: %v", err)
	}
	if len(sums) != len(tmpls)+1 {
		t.Fatalf("summaries = %d, want %d (%d built-in + 1 custom)", len(sums), len(tmpls)+1, len(tmpls))
	}
	var foundCustom bool
	for _, s := range sums {
		if s.TemplateID == custom.ID {
			foundCustom = true
			if s.Status != model.SummaryReady {
				t.Fatalf("custom template summary not ready: status=%q", s.Status)
			}
		}
	}
	if !foundCustom {
		t.Fatalf("no summary found for custom template %s", custom.ID)
	}
}

func TestPipelineTranscribeTerminalFailureMarksNoteFailed(t *testing.T) {
	proc, st, noteID, tr, _ := pipelineFixture(t, "keep")
	ctx := context.Background()

	// Make every transcribe attempt 500 so the job exhausts its retries.
	tr.FailNext(100)
	for i := 0; i < 50; i++ {
		job, ok, _ := st.ClaimJob(ctx, 30*time.Second)
		if !ok {
			break
		}
		proc.Process(ctx, job)
		if _, err := st.Pool().Exec(ctx, "UPDATE jobs SET lease_expires_at = NULL WHERE id=$1", job.ID); err != nil {
			t.Fatalf("clear retry lease: %v", err)
		}
	}

	// Terminal transcribe failure leaves the note 'failed' — never 'ready', never
	// stuck in 'transcribing' — and no summarize jobs were fanned out.
	n, _ := st.GetNoteByID(ctx, noteID)
	if n.Status != model.NoteFailed {
		t.Fatalf("note status = %q, want failed", n.Status)
	}
}

func TestPipelineTranscribeFailureLeavesProvisionalTranscriptIntact(t *testing.T) {
	proc, st, noteID, tr, _ := pipelineFixture(t, "keep")
	seedProvisionalTranscript(t, st, noteID)
	tr.FailNext(999)

	drain(t, proc, st)
	ctx := context.Background()

	n, err := st.GetNoteByID(ctx, noteID)
	if err != nil {
		t.Fatalf("GetNoteByID: %v", err)
	}
	if n.Status != model.NoteFailed {
		t.Fatalf("note status = %q, want failed", n.Status)
	}
	if !n.PartialTranscript {
		t.Fatal("partial_transcript should remain true after terminal transcribe failure")
	}

	trx, err := st.GetTranscript(ctx, noteID)
	if err != nil {
		t.Fatalf("GetTranscript: %v", err)
	}
	if len(trx.Segments) != 1 {
		t.Fatalf("segments = %d, want 1 provisional segment", len(trx.Segments))
	}
	if got, want := trx.Segments[0].Text, "provisional segment"; got != want {
		t.Fatalf("segment[0].Text = %q, want %q", got, want)
	}
}

func TestRunTranscribe_NoDefaultTranscriberIsNonRetryable(t *testing.T) {
	proc, st, noteID, _, _ := pipelineFixtureWithDefaults(t, "keep", "", plugintest.NewTranscriber(), false, false, true)
	ctx := context.Background()

	job, ok, err := st.ClaimJob(ctx, 30*time.Second)
	if err != nil {
		t.Fatalf("ClaimJob: %v", err)
	}
	if !ok {
		t.Fatal("expected transcribe job to be claimable")
	}
	proc.Process(ctx, job)

	gotJob, err := st.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if gotJob.Status != model.JobFailed {
		t.Fatalf("job status = %q, want failed", gotJob.Status)
	}
	if gotJob.Attempts != 1 {
		t.Fatalf("job attempts = %d, want 1", gotJob.Attempts)
	}

	if _, ok, err := st.ClaimJob(ctx, 30*time.Second); err != nil {
		t.Fatalf("ClaimJob after failure: %v", err)
	} else if ok {
		t.Fatal("expected no further claimable jobs")
	}

	n, err := st.GetNoteByID(ctx, noteID)
	if err != nil {
		t.Fatalf("GetNoteByID: %v", err)
	}
	if n.Status != model.NoteFailed {
		t.Fatalf("note status = %q, want failed", n.Status)
	}
}

// TestPipelineJobFailureDoesNotLogSecret is a DB-free test that verifies the
// log statement in Process() cannot expose raw plugin response bodies.
//
// Process logs job failures via:
//
//	log.Printf("job %s (%s) failed (retryable=%v): %v", job.ID, job.Type, retryable, err)
//
// Using %v calls err.Error(). Since HTTPError.Error() truncates its Body field
// to 80 chars and never emits the raw value, secrets in plugin responses stay
// out of log output even if a misbehaving plugin echoes them back.
func TestPipelineJobFailureDoesNotLogSecret(t *testing.T) {
	secret := "supersecret_token_value"

	// Capture all log output.
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	// Reproduce the exact log call that Process() emits on job failure.
	// This is DB-free: we only exercise the logging pattern, not the store.
	//
	// The body has the secret starting beyond the 80-char truncation boundary so
	// that it must not appear in the log (HTTPError.Error() clips the body to 80
	// chars). The prefix is "error: plugin upstream auth: " (29 chars) + 52 "x"
	// chars = 81 chars before the secret.
	prefix := "error: plugin upstream auth: " + strings.Repeat("x", 52)
	err := &plugin.HTTPError{StatusCode: 500, Body: prefix + secret}
	log.Printf("job %s (%s) failed (retryable=%v): %v", "job-id-1", "transcribe", true, err)

	out := buf.String()
	if strings.Contains(out, secret) {
		t.Errorf("log output contains raw secret; got: %s", out)
	}
	if !strings.Contains(out, "500") {
		t.Errorf("log output should contain HTTP status code 500; got: %s", out)
	}
}

// TestPipelineDiarizationSegmentsPersisted verifies that when cfg.Diarization is
// true, speaker labels returned by the transcriber are stored and readable via
// GetTranscript.
func TestPipelineDiarizationSegmentsPersisted(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	st := store.New(testutil.NewPool(t))
	cr := testCrypto(t)
	_ = st.SeedBuiltInTemplates(ctx)

	prov, err := storage.NewLocal(t.TempDir(), "http://example.test", "http://example.test", []byte("test-signing-key-0123456789"))
	if err != nil {
		t.Fatal(err)
	}

	tr := plugintest.NewDiarizationTranscriber()
	t.Cleanup(tr.Close)
	ag := plugintest.NewAgent()
	t.Cleanup(ag.Close)

	tp, _ := st.CreatePlugin(ctx, cr, model.Plugin{Kind: model.PluginTranscriber, Name: "t", EndpointURL: tr.URL(), Token: "x", Enabled: true, Config: json.RawMessage(`{}`)})
	ap, _ := st.CreatePlugin(ctx, cr, model.Plugin{Kind: model.PluginAgent, Name: "a", EndpointURL: ag.URL(), Token: "x", Enabled: true, Config: json.RawMessage(`{}`)})
	_ = st.SetDefaultPlugin(ctx, tp.ID)
	_ = st.SetDefaultPlugin(ctx, ap.ID)

	u, _ := st.CreateUser(ctx, "o@example.com", "h")
	n, _ := st.CreateNote(ctx, u.ID, "M")
	key := "notes/" + n.ID + "/audio/a.webm"
	_, _ = prov.PresignUpload(key, time.Minute)
	_ = st.SetNoteAudio(ctx, u.ID, n.ID, key)

	cfg := config.Config{AudioRetention: "keep", Diarization: true}
	proc := worker.NewProcessor(st, cr, prov, cfg, nil)

	noteID := n.ID
	_, _ = st.EnqueueJob(ctx, noteID, model.JobTranscribe, json.RawMessage(`{"audio_key":"`+key+`"}`))

	drain(t, proc, st)

	tr2, err := st.GetTranscript(ctx, noteID)
	if err != nil {
		t.Fatalf("GetTranscript: %v", err)
	}
	if len(tr2.Segments) != 2 {
		t.Fatalf("segments = %d, want 2", len(tr2.Segments))
	}
	for i, seg := range tr2.Segments {
		if seg.Speaker == "" {
			t.Errorf("segment[%d].Speaker is empty, want non-empty speaker label", i)
		}
	}
}

// TestPipelineSummaryTruncatedFlagged covers SUM02: when the agent plugin
// returns a summary whose content does not end in terminal punctuation (and
// reports no usage), runSummarize must flag the resulting summary Truncated.
func TestPipelineSummaryTruncatedFlagged(t *testing.T) {
	ctx := context.Background()
	st := store.New(testutil.NewPool(t))
	cr := testCrypto(t)
	_ = st.SeedBuiltInTemplates(ctx)

	prov, err := storage.NewLocal(t.TempDir(), "http://example.test", "http://example.test", []byte("test-signing-key-0123456789"))
	if err != nil {
		t.Fatal(err)
	}

	tr := plugintest.NewTranscriber()
	t.Cleanup(tr.Close)
	ag := plugintest.NewTruncatedAgent()
	t.Cleanup(ag.Close)

	tp, _ := st.CreatePlugin(ctx, cr, model.Plugin{Kind: model.PluginTranscriber, Name: "t", EndpointURL: tr.URL(), Token: "x", Enabled: true, Config: json.RawMessage(`{}`)})
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
	_, _ = st.EnqueueJob(ctx, n.ID, model.JobTranscribe, json.RawMessage(`{"audio_key":"`+key+`"}`))

	drain(t, proc, st)

	sums, err := st.GetSummaries(ctx, n.ID)
	if err != nil || len(sums) == 0 {
		t.Fatalf("summaries: %v len=%d", err, len(sums))
	}
	for _, s := range sums {
		if s.Status != model.SummaryReady {
			t.Fatalf("summary not ready: %+v", s)
		}
		if !s.Truncated {
			t.Fatalf("summary from truncated-agent stub should be flagged Truncated: %+v", s)
		}
	}
}

// TestFinalizeNoteEnqueuesWebhookPerEnabledWebhook covers EXT01d requirement
// 5a: an owner with an enabled webhook gets exactly one pending
// webhook_deliveries row once their note reaches ready, and the payload
// carries the note id/title/summary/segments.
func TestFinalizeNoteEnqueuesWebhookPerEnabledWebhook(t *testing.T) {
	proc, st, noteID, _, _ := pipelineFixture(t, "keep")
	ctx := context.Background()

	owner, err := st.NoteOwnerID(ctx, noteID)
	if err != nil {
		t.Fatalf("NoteOwnerID: %v", err)
	}
	whID := seedWebhook(t, st, owner, "https://example.test/hook", true)
	// A disabled webhook for the same owner must NOT get a delivery.
	seedWebhook(t, st, owner, "https://example.test/hook-disabled", false)

	drain(t, proc, st)

	n, _ := st.GetNoteByID(ctx, noteID)
	if n.Status != model.NoteReady {
		t.Fatalf("note status = %q, want ready", n.Status)
	}

	deliveries, err := st.ListPendingDeliveries(ctx, 50)
	if err != nil {
		t.Fatalf("ListPendingDeliveries: %v", err)
	}
	if len(deliveries) != 1 {
		t.Fatalf("deliveries = %d, want 1 (only the enabled webhook)", len(deliveries))
	}
	d := deliveries[0]
	if d.WebhookID != whID {
		t.Fatalf("delivery webhook_id = %q, want %q", d.WebhookID, whID)
	}
	if d.Status != model.DeliveryPending {
		t.Fatalf("delivery status = %q, want pending", d.Status)
	}

	var payload struct {
		Event string `json:"event"`
		Note  struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"note"`
		Summary *struct {
			Sections []model.SummarySection `json:"sections"`
		} `json:"summary"`
		Segments []struct {
			StartMS int    `json:"start_ms"`
			EndMS   int    `json:"end_ms"`
			Text    string `json:"text"`
		} `json:"segments"`
	}
	if err := json.Unmarshal(d.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.Event != "note.completed" {
		t.Fatalf("event = %q, want note.completed", payload.Event)
	}
	if payload.Note.ID != noteID {
		t.Fatalf("note.id = %q, want %q", payload.Note.ID, noteID)
	}
	if payload.Note.Title != "M" {
		t.Fatalf("note.title = %q, want %q", payload.Note.Title, "M")
	}
	if payload.Summary == nil || len(payload.Summary.Sections) == 0 {
		t.Fatalf("expected non-empty summary sections in payload, got %+v", payload.Summary)
	}
	if len(payload.Segments) != 2 {
		t.Fatalf("segments = %d, want 2", len(payload.Segments))
	}
}

// TestFinalizeNoteNoWebhooksNoOp covers EXT01d requirement 5b: with zero
// webhooks configured for the owner, the note still transitions to ready
// (existing behaviour unaffected) but no delivery rows are created.
func TestFinalizeNoteNoWebhooksNoOp(t *testing.T) {
	proc, st, noteID, _, _ := pipelineFixture(t, "keep")
	ctx := context.Background()

	drain(t, proc, st)

	n, _ := st.GetNoteByID(ctx, noteID)
	if n.Status != model.NoteReady {
		t.Fatalf("note status = %q, want ready", n.Status)
	}
	deliveries, err := st.ListPendingDeliveries(ctx, 50)
	if err != nil {
		t.Fatalf("ListPendingDeliveries: %v", err)
	}
	if len(deliveries) != 0 {
		t.Fatalf("deliveries = %d, want 0 (no webhooks configured for owner)", len(deliveries))
	}
}

// TestFinalizeNoteDoubleCallCreatesWebhookOnce is the regression test for the
// MarkNoteReady TOCTOU fix (EXT01d requirement 5c): calling FinalizeNote a
// second time for a note that is already ready (simulating a duplicate/racing
// completion) must not create a second delivery.
func TestFinalizeNoteDoubleCallCreatesWebhookOnce(t *testing.T) {
	proc, st, noteID, _, _ := pipelineFixture(t, "keep")
	ctx := context.Background()

	owner, err := st.NoteOwnerID(ctx, noteID)
	if err != nil {
		t.Fatalf("NoteOwnerID: %v", err)
	}
	seedWebhook(t, st, owner, "https://example.test/hook", true)

	var calls int
	var got actionitems.Input
	proc.SetActionItemsExtractor(func(_ context.Context, input actionitems.Input) (actionitems.Result, error) {
		calls++
		got = input
		return actionitems.Result{ActionItems: []actionitems.ActionItem{{Text: "ship the doc", Owner: "Alice", DueHint: "Friday"}}}, nil
	})

	drain(t, proc, st) // the note's natural path to ready calls FinalizeNote once

	n, _ := st.GetNoteByID(ctx, noteID)
	if n.Status != model.NoteReady {
		t.Fatalf("note status = %q, want ready", n.Status)
	}
	if calls != 1 {
		t.Fatalf("action items extraction calls = %d, want 1", calls)
	}
	if len(got.Transcript) == 0 || len(got.Summary) == 0 {
		t.Fatalf("action items input incomplete: %+v", got)
	}

	// Simulate a duplicate/racing FinalizeNote invocation on the already-ready
	// note. Before the MarkNoteReady fix this would re-run the unconditional
	// SetNoteStatus + unconditional enqueue and create a second delivery.
	proc.FinalizeNote(ctx, noteID)

	deliveries, err := st.ListPendingDeliveries(ctx, 50)
	if err != nil {
		t.Fatalf("ListPendingDeliveries: %v", err)
	}
	if len(deliveries) != 1 {
		t.Fatalf("deliveries = %d, want exactly 1 despite duplicate FinalizeNote call", len(deliveries))
	}
	if calls != 1 {
		t.Fatalf("action items extraction calls = %d, want exactly 1 despite duplicate FinalizeNote call", calls)
	}
}

func TestFinalizeNotePersistsActionItems(t *testing.T) {
	proc, st, noteID, _, _ := pipelineFixture(t, "keep")
	ctx := context.Background()

	var calls int
	proc.SetActionItemsExtractor(func(_ context.Context, input actionitems.Input) (actionitems.Result, error) {
		calls++
		if len(input.Transcript) == 0 || len(input.Summary) == 0 {
			t.Fatalf("action items input incomplete: %+v", input)
		}
		return actionitems.Result{
			ActionItems: []actionitems.ActionItem{{Text: "ship the doc", Owner: "Alice", DueHint: "Friday"}},
			Decisions:   []actionitems.Decision{{Text: "keep the weekly cadence"}},
		}, nil
	})

	drain(t, proc, st)

	ownerID, err := st.NoteOwnerID(ctx, noteID)
	if err != nil {
		t.Fatalf("NoteOwnerID: %v", err)
	}
	items, decisions, err := st.ListForNote(ctx, ownerID, noteID)
	if err != nil {
		t.Fatalf("ListForNote: %v", err)
	}
	if calls != 1 {
		t.Fatalf("action items extraction calls = %d, want 1", calls)
	}
	if len(items) != 1 {
		t.Fatalf("items len=%d want 1", len(items))
	}
	if items[0].Text != "ship the doc" || items[0].DueHint != "Friday" || items[0].Status != model.ActionItemOpen {
		t.Fatalf("unexpected persisted action item: %+v", items[0])
	}
	if items[0].OwnerPersonID != nil {
		t.Fatalf("owner_person_id should be nil, got %+v", items[0].OwnerPersonID)
	}
	if len(decisions) != 1 || decisions[0].Text != "keep the weekly cadence" {
		t.Fatalf("unexpected persisted decisions: %+v", decisions)
	}
	ownerItems, err := st.ListForOwner(ctx, ownerID, model.ActionItemOpen)
	if err != nil {
		t.Fatalf("ListForOwner: %v", err)
	}
	if len(ownerItems) != 1 || ownerItems[0].ID != items[0].ID {
		t.Fatalf("owner items=%+v want one persisted item", ownerItems)
	}
}

// TestFinalizeNoteWebhookSummaryPresentWhenZeroReadySummaries is a regression
// test (cross-review blocking finding on PR #201): a note can legitimately
// reach ready with ZERO ready summaries via the partial-success path (all
// summarize jobs terminally failed — failed summarize jobs do not block
// readiness). The note.completed webhook payload's "summary" key must still
// be present in that case, with an empty (never null, never missing)
// "sections" array — not omitted entirely.
func TestFinalizeNoteWebhookSummaryPresentWhenZeroReadySummaries(t *testing.T) {
	proc, st, noteID, _, ag := pipelineFixture(t, "keep")
	ctx := context.Background()

	owner, err := st.NoteOwnerID(ctx, noteID)
	if err != nil {
		t.Fatalf("NoteOwnerID: %v", err)
	}
	seedWebhook(t, st, owner, "https://example.test/hook", true)

	var calls int
	proc.SetActionItemsExtractor(func(_ context.Context, input actionitems.Input) (actionitems.Result, error) {
		calls++
		return actionitems.Result{}, nil
	})

	// Process the transcribe job: succeeds, fans out 2 summarize jobs (one per
	// built-in template).
	job, _, _ := st.ClaimJob(ctx, 30*time.Second)
	proc.Process(ctx, job)

	// Fail the agent for every remaining attempt so BOTH summarize jobs exhaust
	// MaxJobAttempts and terminally fail. The note still reaches ready (partial
	// success), but with zero ready summaries.
	ag.FailNext(1000)
	for i := 0; i < 50; i++ {
		j, ok, _ := st.ClaimJob(ctx, 30*time.Second)
		if !ok {
			break
		}
		proc.Process(ctx, j)
		if _, err := st.Pool().Exec(ctx, "UPDATE jobs SET lease_expires_at = NULL WHERE id=$1", j.ID); err != nil {
			t.Fatalf("clear retry lease: %v", err)
		}
	}

	n, _ := st.GetNoteByID(ctx, noteID)
	if n.Status != model.NoteReady {
		t.Fatalf("note status = %q, want ready (partial success with zero ready summaries)", n.Status)
	}
	sums, _ := st.GetSummaries(ctx, noteID)
	for _, s := range sums {
		if s.Status == model.SummaryReady {
			t.Fatalf("expected zero ready summaries for this test, found one: %+v", s)
		}
	}

	deliveries, err := st.ListPendingDeliveries(ctx, 50)
	if err != nil {
		t.Fatalf("ListPendingDeliveries: %v", err)
	}
	if len(deliveries) != 1 {
		t.Fatalf("deliveries = %d, want 1", len(deliveries))
	}
	raw := deliveries[0].Payload

	if !strings.Contains(string(raw), `"summary"`) {
		t.Fatalf("payload missing the summary key entirely: %s", raw)
	}

	var payload struct {
		Summary struct {
			Sections []model.SummarySection `json:"sections"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	// A nil Sections after unmarshal would mean "summary" (or "sections"
	// within it) was missing/null in the JSON rather than an explicit [].
	if payload.Summary.Sections == nil {
		t.Fatal("summary.sections should be an empty array ([]), not missing/null")
	}
	if len(payload.Summary.Sections) != 0 {
		t.Fatalf("summary.sections = %d, want 0", len(payload.Summary.Sections))
	}
	if calls != 0 {
		t.Fatalf("action items extraction calls = %d, want 0 when there is no ready summary", calls)
	}
}
