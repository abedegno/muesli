package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
)

// TestDeleteNoteSummariesIfCurrentLocksAgainstConcurrentSaveTranscript is the
// specific coverage the round-2 review found missing: it proves
// DeleteNoteSummariesIfCurrent's notes-row lock actually EXCLUDES a
// concurrent SaveTranscript, not merely that a sequential
// replace-then-call ends up consistent. Ordering is established by channels,
// never sleeps, mirroring TestConcurrentFirstWritersDoNotRaceTheUniqueIndex
// in transcripts_test.go: DeleteNoteSummariesIfCurrent is parked, still
// holding the notes-row lock, right after its own generation check passes;
// SaveTranscript is then launched and proven NOT to complete while that lock
// is held (a bounded time.After select — a soft mutation-detection signal,
// not proof on its own, exactly as documented at the precedent test); only
// once the lock is released does SaveTranscript unblock and succeed.
func TestDeleteNoteSummariesIfCurrentLocksAgainstConcurrentSaveTranscript(t *testing.T) {
	// No t.Parallel: this installs a package-level hook.
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	if err := st.SeedBuiltInTemplates(ctx); err != nil {
		t.Fatalf("seed templates: %v", err)
	}
	ownerID, noteID := seedNoteWithOwner(t, st)

	saved, err := st.SaveTranscript(ctx, model.Transcript{
		NoteID:            noteID,
		TranscriberPlugin: "whisper",
		Segments:          []model.Segment{{StartMS: 0, EndMS: 10, Text: "one", Source: "mic"}},
	}, 0)
	if err != nil {
		t.Fatalf("seed transcript: %v", err)
	}

	templates, err := st.TemplatesForSummary(ctx, ownerID)
	if err != nil || len(templates) == 0 {
		t.Fatalf("templates for summary: %v (%d)", err, len(templates))
	}
	summaryID, err := st.CreatePendingSummary(ctx, noteID, templates[0].ID)
	if err != nil {
		t.Fatalf("seed summary: %v", err)
	}

	reachedLock := make(chan struct{})
	releaseLock := make(chan struct{})
	restore := store.SetTestHookAfterDeleteNoteSummariesGenerationCheck(func() {
		close(reachedLock)
		<-releaseLock
	})
	defer restore()

	deleteResult := make(chan error, 1)
	go func() {
		deleteResult <- st.DeleteNoteSummariesIfCurrent(ctx, ownerID, noteID, saved.Generation)
	}()
	<-reachedLock // the delete tx holds the notes-row lock; its own generation check has already passed

	saveResult := make(chan error, 1)
	go func() {
		_, err := st.SaveTranscript(ctx, model.Transcript{
			NoteID:            noteID,
			TranscriberPlugin: "whisper",
			Segments:          []model.Segment{{StartMS: 0, EndMS: 20, Text: "two", Source: "mic"}},
		}, saved.Generation)
		saveResult <- err
	}()

	select {
	case err := <-saveResult:
		t.Fatalf("SaveTranscript completed while DeleteNoteSummariesIfCurrent still held the notes-row lock (err=%v) — the row was not locked", err)
	case <-time.After(300 * time.Millisecond):
		// Expected: SaveTranscript's own first statement blocks on the same
		// notes-row lock DeleteNoteSummariesIfCurrent is holding.
	}

	close(releaseLock)

	if err := <-deleteResult; err != nil {
		t.Fatalf("DeleteNoteSummariesIfCurrent: %v", err)
	}
	if err := <-saveResult; err != nil {
		t.Fatalf("SaveTranscript after lock release: %v", err)
	}

	// Summaries at the old generation were deleted.
	summaries, err := st.GetSummaries(ctx, noteID)
	if err != nil {
		t.Fatalf("GetSummaries: %v", err)
	}
	for _, sm := range summaries {
		if sm.ID == summaryID {
			t.Fatalf("summary %s still present after DeleteNoteSummariesIfCurrent committed", summaryID)
		}
	}

	// The transcript is now at the new generation, published only after the
	// lock was released.
	got, err := st.GetTranscript(ctx, noteID)
	if err != nil {
		t.Fatalf("GetTranscript: %v", err)
	}
	if got.Generation != saved.Generation+1 {
		t.Fatalf("generation = %d, want %d", got.Generation, saved.Generation+1)
	}
	if len(got.Segments) != 1 || got.Segments[0].Text != "two" {
		t.Fatalf("transcript segments = %+v, want the replacement", got.Segments)
	}
}

func TestEnqueueSummarizeJobs(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	if err := st.SeedBuiltInTemplates(ctx); err != nil {
		t.Fatalf("seed: %v", err)
	}
	noteID := seedNote(t, st)
	ownerID, err := st.NoteOwnerID(ctx, noteID)
	if err != nil {
		t.Fatalf("owner: %v", err)
	}

	if err := st.EnqueueSummarizeJobs(ctx, ownerID, noteID); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	// One pending summary per built-in template.
	tmpls, err := st.BuiltInTemplates(ctx)
	if err != nil {
		t.Fatalf("BuiltInTemplates: %v", err)
	}
	sums, err := st.GetSummaries(ctx, noteID)
	if err != nil {
		t.Fatalf("GetSummaries: %v", err)
	}
	if len(sums) != len(tmpls) {
		t.Fatalf("summaries = %d, want %d built-ins", len(sums), len(tmpls))
	}
	for _, s := range sums {
		if s.Status != model.SummaryPending {
			t.Fatalf("summary status = %q, want pending", s.Status)
		}
	}

	// Note advanced to 'summarizing'.
	n, err := st.GetNoteByID(ctx, noteID)
	if err != nil {
		t.Fatalf("GetNoteByID: %v", err)
	}
	if n.Status != model.NoteSummarizing {
		t.Fatalf("note status = %q, want summarizing", n.Status)
	}

	// One summarize job per template was enqueued.
	jobs := 0
	for i := 0; i < len(tmpls)+1; i++ {
		job, ok, err := st.ClaimJob(ctx, 30*time.Second)
		if err != nil {
			t.Fatalf("ClaimJob: %v", err)
		}
		if !ok {
			break
		}
		if job.Type != model.JobSummarize {
			t.Fatalf("job type = %q, want summarize", job.Type)
		}
		jobs++
	}
	if jobs != len(tmpls) {
		t.Fatalf("enqueued jobs = %d, want %d", jobs, len(tmpls))
	}
}

func TestEnqueueSummarizeJobsSkipsManualOnlyTemplates(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	if err := st.SeedBuiltInTemplates(ctx); err != nil {
		t.Fatalf("seed: %v", err)
	}
	noteID := seedNote(t, st)
	ownerID, err := st.NoteOwnerID(ctx, noteID)
	if err != nil {
		t.Fatalf("owner: %v", err)
	}

	autoRunTmpl, err := st.CreateTemplate(ctx, ownerID, "Auto run", "after", []model.TemplateSection{{Heading: "A", Instruction: "A"}}, true, "", "", nil)
	if err != nil {
		t.Fatalf("create auto-run: %v", err)
	}
	manualOnlyTmpl, err := st.CreateTemplate(ctx, ownerID, "Manual only", "after", []model.TemplateSection{{Heading: "M", Instruction: "M"}}, false, "", "", nil)
	if err != nil {
		t.Fatalf("create manual-only: %v", err)
	}

	forSummary, err := st.TemplatesForSummary(ctx, ownerID)
	if err != nil {
		t.Fatalf("TemplatesForSummary: %v", err)
	}
	var sawAutoRun, sawManualOnly bool
	for _, tmpl := range forSummary {
		if tmpl.ID == autoRunTmpl.ID {
			sawAutoRun = true
		}
		if tmpl.ID == manualOnlyTmpl.ID {
			sawManualOnly = true
		}
		if !tmpl.AutoRun {
			t.Fatalf("TemplatesForSummary included non-auto-run template: %+v", tmpl)
		}
	}
	if !sawAutoRun {
		t.Fatalf("TemplatesForSummary missing auto-run template: %+v", forSummary)
	}
	if sawManualOnly {
		t.Fatalf("TemplatesForSummary included manual-only template: %+v", forSummary)
	}

	if err := st.EnqueueSummarizeJobs(ctx, ownerID, noteID); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	sums, err := st.GetSummaries(ctx, noteID)
	if err != nil {
		t.Fatalf("GetSummaries: %v", err)
	}
	if len(sums) != len(forSummary) {
		t.Fatalf("summaries = %d, want %d auto-run templates", len(sums), len(forSummary))
	}
	for _, s := range sums {
		if s.TemplateID == manualOnlyTmpl.ID {
			t.Fatalf("manual-only template was enqueued: %+v", s)
		}
	}
}

func TestEnqueueSummarizeJobsNoTemplatesSetsReady(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	// No built-ins seeded and the owner has no templates.
	noteID := seedNote(t, st)
	ownerID, err := st.NoteOwnerID(ctx, noteID)
	if err != nil {
		t.Fatalf("owner: %v", err)
	}

	if err := st.EnqueueSummarizeJobs(ctx, ownerID, noteID); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	n, _ := st.GetNoteByID(ctx, noteID)
	if n.Status != model.NoteReady {
		t.Fatalf("note status = %q, want ready (nothing to summarize)", n.Status)
	}
	sums, _ := st.GetSummaries(ctx, noteID)
	if len(sums) != 0 {
		t.Fatalf("summaries = %d, want 0", len(sums))
	}
}

func TestDeleteNoteSummaries(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	if err := st.SeedBuiltInTemplates(ctx); err != nil {
		t.Fatalf("seed: %v", err)
	}
	tmpls, err := st.BuiltInTemplates(ctx)
	if err != nil || len(tmpls) == 0 {
		t.Fatalf("templates: %v", err)
	}
	noteID := seedNote(t, st)
	ownerID, err := st.NoteOwnerID(ctx, noteID)
	if err != nil {
		t.Fatalf("owner: %v", err)
	}
	if _, err := st.CreatePendingSummary(ctx, noteID, tmpls[0].ID); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := st.CreatePendingSummary(ctx, noteID, tmpls[1].ID); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Cross-owner delete (wrong owner) leaves them untouched.
	if err := st.DeleteNoteSummaries(ctx, "00000000-0000-0000-0000-000000000000", noteID); err != nil {
		t.Fatalf("cross-owner delete: %v", err)
	}
	if sums, _ := st.GetSummaries(ctx, noteID); len(sums) != 2 {
		t.Fatalf("after cross-owner delete summaries = %d, want 2 (untouched)", len(sums))
	}

	// Correct-owner delete removes them.
	if err := st.DeleteNoteSummaries(ctx, ownerID, noteID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if sums, _ := st.GetSummaries(ctx, noteID); len(sums) != 0 {
		t.Fatalf("after delete summaries = %d, want 0", len(sums))
	}
}

func TestSummaryLifecycle(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	noteID := seedNote(t, st)
	if err := st.SeedBuiltInTemplates(ctx); err != nil {
		t.Fatalf("seed: %v", err)
	}
	tmpls, err := st.BuiltInTemplates(ctx)
	if err != nil || len(tmpls) < 2 {
		t.Fatalf("templates: %v len=%d", err, len(tmpls))
	}

	// Create a pending summary for the first template.
	sumID, err := st.CreatePendingSummary(ctx, noteID, tmpls[0].ID)
	if err != nil {
		t.Fatalf("create pending: %v", err)
	}

	// A just-created summary reads "pending" (not "failed") so /full can tell
	// "not started yet" apart from "genuinely failed".
	pendingList, err := st.GetSummaries(ctx, noteID)
	if err != nil || len(pendingList) != 1 || pendingList[0].Status != model.SummaryPending {
		t.Fatalf("freshly-created summary should be pending, got %+v err=%v", pendingList, err)
	}

	// Mark it ready with sections.
	err = st.CompleteSummary(ctx, sumID, "ollama", "llama3", []model.SummarySection{
		{Heading: "Overview", ContentMarkdown: "It was a meeting.", Refs: []int{0}},
	}, false)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}

	// Create + fail a second summary.
	sum2, _ := st.CreatePendingSummary(ctx, noteID, tmpls[1].ID)
	if err := st.FailSummary(ctx, sum2); err != nil {
		t.Fatalf("fail: %v", err)
	}

	got, err := st.GetSummaries(ctx, noteID)
	if err != nil || len(got) != 2 {
		t.Fatalf("summaries len=%d err=%v", len(got), err)
	}
	byStatus := map[string]model.Summary{}
	for _, s := range got {
		byStatus[s.Status] = s
	}
	if byStatus[model.SummaryReady].Sections[0].Heading != "Overview" {
		t.Fatalf("ready summary wrong: %+v", byStatus[model.SummaryReady])
	}
	if byStatus[model.SummaryReady].TemplateName == "" {
		t.Fatal("summary should carry its template name")
	}
	if _, ok := byStatus[model.SummaryFailed]; !ok {
		t.Fatal("expected a failed summary")
	}
}

// TestCompleteSummaryTruncatedRoundTrip verifies the truncated flag is a
// distinct signal from status: a summary completed with truncated=true reads
// back Truncated:true via GetSummaries, and one completed with
// truncated=false reads back Truncated:false.
func TestCompleteSummaryTruncatedRoundTrip(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	ctx := context.Background()
	noteID := seedNote(t, st)
	if err := st.SeedBuiltInTemplates(ctx); err != nil {
		t.Fatalf("seed: %v", err)
	}
	tmpls, err := st.BuiltInTemplates(ctx)
	if err != nil || len(tmpls) < 2 {
		t.Fatalf("templates: %v len=%d", err, len(tmpls))
	}

	truncSumID, err := st.CreatePendingSummary(ctx, noteID, tmpls[0].ID)
	if err != nil {
		t.Fatalf("create pending (truncated): %v", err)
	}
	if err := st.CompleteSummary(ctx, truncSumID, "ollama", "llama3", []model.SummarySection{
		{Heading: "Overview", ContentMarkdown: "It was cut off mid"},
	}, true); err != nil {
		t.Fatalf("complete (truncated=true): %v", err)
	}

	okSumID, err := st.CreatePendingSummary(ctx, noteID, tmpls[1].ID)
	if err != nil {
		t.Fatalf("create pending (not truncated): %v", err)
	}
	if err := st.CompleteSummary(ctx, okSumID, "ollama", "llama3", []model.SummarySection{
		{Heading: "Overview", ContentMarkdown: "It concluded properly."},
	}, false); err != nil {
		t.Fatalf("complete (truncated=false): %v", err)
	}

	got, err := st.GetSummaries(ctx, noteID)
	if err != nil || len(got) != 2 {
		t.Fatalf("summaries len=%d err=%v", len(got), err)
	}
	byID := map[string]model.Summary{}
	for _, s := range got {
		byID[s.ID] = s
	}
	if !byID[truncSumID].Truncated {
		t.Fatalf("expected summary %s to round-trip Truncated=true, got %+v", truncSumID, byID[truncSumID])
	}
	if byID[okSumID].Truncated {
		t.Fatalf("expected summary %s to round-trip Truncated=false, got %+v", okSumID, byID[okSumID])
	}
}
