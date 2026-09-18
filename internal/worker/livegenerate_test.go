package worker

// This file is DB-backed and CI-only: testutil.NewPool skips when
// TEST_DATABASE_URL is unset. package worker (not worker_test) because it
// exercises runLiveGenerate and liveJobClock directly.

import (
	"context"
	"encoding/base64"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/config"
	"github.com/abedegno/muesli/internal/crypto"
	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/plugintest"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
)

type liveJobFixture struct {
	t     *testing.T
	proc  *Processor
	st    *store.Store
	owner string
	agent *plugintest.Stub
	now   time.Time
}

func newLiveJobFixture(t *testing.T) *liveJobFixture {
	t.Helper()
	ctx := context.Background()
	st := store.New(testutil.NewPool(t))
	cr, err := crypto.New(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	u, err := st.CreateUser(ctx, "livejob-owner@example.com", "h")
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	agent := plugintest.NewAgent()
	t.Cleanup(agent.Close)

	now := time.Date(2026, 9, 20, 8, 0, 0, 0, time.UTC)
	originalClock := liveJobClock
	liveJobClock = func() time.Time { return now }
	t.Cleanup(func() { liveJobClock = originalClock })

	proc := NewProcessor(st, cr, nil, config.Config{}, nil)
	f := &liveJobFixture{t: t, proc: proc, st: st, owner: u.ID, agent: agent, now: now}
	if err := st.EnsureDefaultPlugin(ctx, cr, model.PluginAgent, "agent", agent.URL(), "tok", "{}"); err != nil {
		t.Fatalf("ensure default agent: %v", err)
	}
	return f
}

func (f *liveJobFixture) seedTemplate(t *testing.T, name string) model.Template {
	t.Helper()
	sections := []model.TemplateSection{{Heading: "Live", Instruction: "Summarize so far."}}
	tm, err := f.st.CreateTemplate(context.Background(), f.owner, name, "during", sections, true, "", "", nil)
	if err != nil {
		t.Fatalf("create template: %v", err)
	}
	return tm
}

func (f *liveJobFixture) seedStream(t *testing.T) (noteID, transcriptID, streamID string) {
	t.Helper()
	ctx := context.Background()
	n, err := f.st.CreateNote(ctx, f.owner, "Live note")
	if err != nil {
		t.Fatalf("create note: %v", err)
	}
	streamID = "stream-" + n.ID
	tr, err := f.st.CreateStreamTranscript(ctx, n.ID, streamID, "whisper-live", "tiny", 0)
	if err != nil {
		t.Fatalf("create stream transcript: %v", err)
	}
	return n.ID, tr.ID, streamID
}

func (f *liveJobFixture) appendFinal(t *testing.T, transcriptID, streamID, text string) {
	t.Helper()
	if err := f.st.AppendStreamSegment(context.Background(), transcriptID, streamID, model.Segment{
		StartMS: 0, EndMS: 1000, Text: text, Source: "streaming",
	}); err != nil {
		t.Fatalf("append segment: %v", err)
	}
}

func (f *liveJobFixture) claimLiveJob(t *testing.T) model.Job {
	t.Helper()
	job, ok, err := f.st.ClaimJob(context.Background(), time.Minute)
	if err != nil || !ok {
		t.Fatalf("claim job: ok=%v err=%v", ok, err)
	}
	if job.Type != model.JobLiveGenerate {
		t.Fatalf("claimed job type = %q, want live_generate", job.Type)
	}
	return job
}

// TestRunLiveGenerate_Success proves a valid claim executes against exactly
// the finalized prefix and publishes ready with rendered_revision advanced
// to the fixed target.
func TestRunLiveGenerate_Success(t *testing.T) {
	f := newLiveJobFixture(t)
	f.seedTemplate(t, "live-during")
	noteID, transcriptID, streamID := f.seedStream(t)
	f.appendFinal(t, transcriptID, streamID, "hello")

	job := f.claimLiveJob(t)
	retryable, err := f.proc.runLiveGenerate(context.Background(), job)
	if err != nil {
		t.Fatalf("runLiveGenerate: retryable=%v err=%v", retryable, err)
	}

	rows, err := f.st.ListVisibleLiveTemplateOutputs(context.Background(), f.owner, noteID)
	if err != nil || len(rows) != 1 {
		t.Fatalf("list outputs: %v %+v", err, rows)
	}
	if rows[0].Status != model.LiveOutputReady {
		t.Fatalf("status = %q, want ready", rows[0].Status)
	}
	if rows[0].RenderedRevision != 1 {
		t.Fatalf("rendered_revision = %d, want 1", rows[0].RenderedRevision)
	}
	if len(rows[0].Sections) == 0 {
		t.Fatal("expected published sections")
	}
	if rows[0].ActiveJobID != "" {
		t.Fatalf("expected no follow-up job when desired==target, got active_job_id=%q", rows[0].ActiveJobID)
	}
}

// TestRunLiveGenerate_GrowthDuringExecutionCreatesOneFollowUp proves growth
// committed after target_revision was captured creates exactly one
// cadence-limited follow-up job.
func TestRunLiveGenerate_GrowthDuringExecutionCreatesOneFollowUp(t *testing.T) {
	f := newLiveJobFixture(t)
	f.seedTemplate(t, "live-during")
	noteID, transcriptID, streamID := f.seedStream(t)
	f.appendFinal(t, transcriptID, streamID, "hello")

	job := f.claimLiveJob(t)

	// Capture target_revision (=1) exactly as the worker's own live claim
	// does, then commit growth AFTER it. runLiveGenerate's re-claim must keep
	// that fixed target (COALESCE), render revision 1 only, and schedule the
	// single follow-up for revision 2.
	claim, err := f.st.ClaimLiveGenerateJobTx(context.Background(), job, f.now)
	if err != nil || !claim.Valid || claim.TargetRevision != 1 {
		t.Fatalf("claim: err=%v valid=%v target=%d, want a valid claim at target 1", err, claim.Valid, claim.TargetRevision)
	}
	f.appendFinal(t, transcriptID, streamID, "world")

	if _, err := f.proc.runLiveGenerate(context.Background(), job); err != nil {
		t.Fatalf("runLiveGenerate: %v", err)
	}

	rows, err := f.st.ListVisibleLiveTemplateOutputs(context.Background(), f.owner, noteID)
	if err != nil || len(rows) != 1 {
		t.Fatalf("list outputs: %v %+v", err, rows)
	}
	if rows[0].RenderedRevision != 1 {
		t.Fatalf("rendered_revision = %d, want 1 (fixed target)", rows[0].RenderedRevision)
	}
	if rows[0].DesiredRevision != 2 {
		t.Fatalf("desired_revision = %d, want 2", rows[0].DesiredRevision)
	}
	if rows[0].ActiveJobID == "" {
		t.Fatal("expected exactly one follow-up job scheduled")
	}
}

// TestRunLiveGenerate_IneligibleBeforeClaim proves an invalid claim (template
// deleted before the worker's own claim check) is a clean no-op: no plugin
// call, no failure, row removed.
func TestRunLiveGenerate_IneligibleBeforeClaim(t *testing.T) {
	f := newLiveJobFixture(t)
	tmpl := f.seedTemplate(t, "live-during")
	noteID, transcriptID, streamID := f.seedStream(t)
	f.appendFinal(t, transcriptID, streamID, "hello")

	job := f.claimLiveJob(t)

	if err := f.st.DeleteTemplate(context.Background(), f.owner, tmpl.ID); err != nil {
		t.Fatalf("delete template: %v", err)
	}

	retryable, err := f.proc.runLiveGenerate(context.Background(), job)
	if err != nil || retryable {
		t.Fatalf("expected clean no-op, got retryable=%v err=%v", retryable, err)
	}

	rows, err := f.st.ListVisibleLiveTemplateOutputs(context.Background(), f.owner, noteID)
	if err != nil || len(rows) != 0 {
		t.Fatalf("expected no visible rows after ineligible claim, got %v %+v", err, rows)
	}
}

// TestRunLiveGenerate_IneligibleDuringExecution_CompletionFenceDiscards
// proves that eligibility changing after claim but before the plugin call
// returns discards the result at completion: no publish, row removed.
func TestRunLiveGenerate_IneligibleDuringExecution_CompletionFenceDiscards(t *testing.T) {
	f := newLiveJobFixture(t)
	tmpl := f.seedTemplate(t, "live-during")
	noteID, transcriptID, streamID := f.seedStream(t)
	f.appendFinal(t, transcriptID, streamID, "hello")

	job := f.claimLiveJob(t)

	claim, err := f.st.ClaimLiveGenerateJobTx(context.Background(), job, f.now)
	if err != nil || !claim.Valid {
		t.Fatalf("claim: %v %+v", err, claim)
	}

	// Eligibility changes AFTER claim, before completion.
	if err := f.st.DeleteTemplate(context.Background(), f.owner, tmpl.ID); err != nil {
		t.Fatalf("delete template: %v", err)
	}

	published, err := f.st.CompleteLiveGenerateSuccessTx(context.Background(), job.ID, claim.TargetRevision, "stub", "stub-model",
		[]model.SummarySection{{Heading: "Live", ContentMarkdown: "x"}}, f.now)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if published {
		t.Fatal("expected completion fence to discard the result, not publish it")
	}

	rows, err := f.st.ListVisibleLiveTemplateOutputs(context.Background(), f.owner, noteID)
	if err != nil || len(rows) != 0 {
		t.Fatalf("expected no visible rows after fenced completion, got %v %+v", err, rows)
	}
}

// TestRunLiveGenerate_TerminalFailure_NoGrowthCreatesNoSuccessor proves a
// persistent terminal error with no later transcript growth creates no
// follow-up job, and classifies the safe error code.
func TestRunLiveGenerate_TerminalFailure_NoGrowthCreatesNoSuccessor(t *testing.T) {
	f := newLiveJobFixture(t)
	f.seedTemplate(t, "live-during")
	noteID, transcriptID, streamID := f.seedStream(t)
	f.appendFinal(t, transcriptID, streamID, "hello")

	job := f.claimLiveJob(t)
	f.agent.FailNext(1)
	retryable, err := f.proc.runLiveGenerate(context.Background(), job)
	if err == nil {
		t.Fatal("expected an error from the injected provider failure")
	}
	if !retryable {
		t.Fatalf("expected a 500 to classify as retryable, got retryable=%v", retryable)
	}
	// Simulate attempts exhausted (as Process would after MaxJobAttempts).
	job.Attempts = store.MaxJobAttempts
	f.proc.handleLiveGenerateTerminalFailure(context.Background(), job, err)

	rows, err2 := f.st.ListVisibleLiveTemplateOutputs(context.Background(), f.owner, noteID)
	if err2 != nil || len(rows) != 1 {
		t.Fatalf("list outputs: %v %+v", err2, rows)
	}
	if rows[0].Status != model.LiveOutputFailed {
		t.Fatalf("status = %q, want failed", rows[0].Status)
	}
	if rows[0].ErrorCode != model.LiveErrorProviderFailed {
		t.Fatalf("error_code = %q, want %q", rows[0].ErrorCode, model.LiveErrorProviderFailed)
	}
	if rows[0].ActiveJobID != "" {
		t.Fatalf("expected no successor job without later growth, got active_job_id=%q", rows[0].ActiveJobID)
	}
}

// TestRunLiveGenerate_TerminalFailure_WithGrowthCreatesOneSuccessor proves
// growth committed before terminal-failure completion creates exactly one
// follow-up.
func TestRunLiveGenerate_TerminalFailure_WithGrowthCreatesOneSuccessor(t *testing.T) {
	f := newLiveJobFixture(t)
	f.seedTemplate(t, "live-during")
	noteID, transcriptID, streamID := f.seedStream(t)
	f.appendFinal(t, transcriptID, streamID, "hello")

	job := f.claimLiveJob(t)
	f.agent.FailNext(1)
	_, err := f.proc.runLiveGenerate(context.Background(), job)
	if err == nil {
		t.Fatal("expected an error from the injected provider failure")
	}

	f.appendFinal(t, transcriptID, streamID, "world")

	job.Attempts = store.MaxJobAttempts
	f.proc.handleLiveGenerateTerminalFailure(context.Background(), job, err)

	rows, err2 := f.st.ListVisibleLiveTemplateOutputs(context.Background(), f.owner, noteID)
	if err2 != nil || len(rows) != 1 {
		t.Fatalf("list outputs: %v %+v", err2, rows)
	}
	if rows[0].ActiveJobID == "" {
		t.Fatal("expected exactly one follow-up job after growth before terminal completion")
	}
}
