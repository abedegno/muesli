package store_test

// This file is DB-backed and CI-only: testutil.NewPool skips when
// TEST_DATABASE_URL is unset.

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
)

func newLiveTestOwner(t *testing.T, st *store.Store) string {
	t.Helper()
	u, err := st.CreateUser(context.Background(), fmt.Sprintf("live-owner-%d@example.com", seedUserCounter.Add(1)), "h")
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	return u.ID
}

func newLiveTestTemplate(t *testing.T, st *store.Store, ownerID, name, phase string, autoRun bool) model.Template {
	t.Helper()
	sections := []model.TemplateSection{{Heading: "Notes", Instruction: "Summarize."}}
	tm, err := st.CreateTemplate(context.Background(), ownerID, name, phase, sections, autoRun, "", "", nil)
	if err != nil {
		t.Fatalf("create template %s: %v", name, err)
	}
	return tm
}

func newLiveTestStream(t *testing.T, st *store.Store, ownerID string) (noteID, transcriptID, streamID string) {
	t.Helper()
	ctx := context.Background()
	n, err := st.CreateNote(ctx, ownerID, "Live note")
	if err != nil {
		t.Fatalf("create note: %v", err)
	}
	streamID = "stream-" + n.ID
	tr, err := st.CreateStreamTranscript(ctx, n.ID, streamID, "whisper-live", "tiny", 0)
	if err != nil {
		t.Fatalf("create stream transcript: %v", err)
	}
	return n.ID, tr.ID, streamID
}

func appendFinalSegment(t *testing.T, st *store.Store, transcriptID, streamID, text string) {
	t.Helper()
	err := st.AppendStreamSegment(context.Background(), transcriptID, streamID, model.Segment{
		StartMS: 0, EndMS: 1000, Text: text, Source: "streaming",
	})
	if err != nil {
		t.Fatalf("append segment: %v", err)
	}
}

func countLiveJobsForOutput(t *testing.T, st *store.Store, outputID string) int {
	t.Helper()
	var n int
	if err := st.Pool().QueryRow(context.Background(),
		`SELECT count(*) FROM jobs WHERE live_output_id=$1`, outputID).Scan(&n); err != nil {
		t.Fatalf("count jobs: %v", err)
	}
	return n
}

func countActiveLiveJobsForOutput(t *testing.T, st *store.Store, outputID string) int {
	t.Helper()
	var n int
	if err := st.Pool().QueryRow(context.Background(),
		`SELECT count(*) FROM jobs WHERE live_output_id=$1 AND status IN ('pending','running')`, outputID).Scan(&n); err != nil {
		t.Fatalf("count active jobs: %v", err)
	}
	return n
}

// TestCreateTemplate_LiveCapRejectsNinth proves the ninth during+auto_run
// template is rejected with the existing validation-error shape, while
// disabled and other-phase templates never consume the cap.
func TestCreateTemplate_LiveCapRejectsNinth(t *testing.T) {
	st := store.New(testutil.NewPool(t))
	owner := newLiveTestOwner(t, st)

	// Disabled and other-phase templates do not count toward the cap.
	newLiveTestTemplate(t, st, owner, "disabled-during", "during", false)
	newLiveTestTemplate(t, st, owner, "after-autorun", "after", true)

	for i := 0; i < store.MaxLiveTemplates; i++ {
		newLiveTestTemplate(t, st, owner, fmt.Sprintf("live-%d", i), "during", true)
	}

	sections := []model.TemplateSection{{Heading: "Notes", Instruction: "Summarize."}}
	_, err := st.CreateTemplate(context.Background(), owner, "live-ninth", "during", sections, true, "", "", nil)
	if err == nil {
		t.Fatal("expected ninth live template to be rejected")
	}
	var ve store.ValidationError
	if !errorsAs(err, &ve) {
		t.Fatalf("expected ValidationError, got %T: %v", err, err)
	}
}

// errorsAs is a tiny local errors.As wrapper to avoid importing "errors" just
// for this one assertion pattern across the file.
func errorsAs(err error, target *store.ValidationError) bool {
	ve, ok := err.(store.ValidationError)
	if ok {
		*target = ve
	}
	return ok
}

// TestUpdateTemplate_LiveCapRejectsNinth mirrors the create case for update.
func TestUpdateTemplate_LiveCapRejectsNinth(t *testing.T) {
	st := store.New(testutil.NewPool(t))
	owner := newLiveTestOwner(t, st)

	for i := 0; i < store.MaxLiveTemplates; i++ {
		newLiveTestTemplate(t, st, owner, fmt.Sprintf("live-%d", i), "during", true)
	}
	extra := newLiveTestTemplate(t, st, owner, "extra", "after", true)

	sections := []model.TemplateSection{{Heading: "Notes", Instruction: "Summarize."}}
	err := st.UpdateTemplate(context.Background(), owner, extra.ID, extra.Name, "during", sections, true, "", "", nil)
	if err == nil {
		t.Fatal("expected ninth-via-update to be rejected")
	}
	var ve store.ValidationError
	if !errorsAs(err, &ve) {
		t.Fatalf("expected ValidationError, got %T: %v", err, err)
	}
}

// TestReconcileStreamDemand_AdvancesAndCoalesces proves a finalized commit
// advances demand and creates one job, and that a second rapid commit
// coalesces onto the still-active job rather than creating a second one.
func TestReconcileStreamDemand_AdvancesAndCoalesces(t *testing.T) {
	st := store.New(testutil.NewPool(t))
	owner := newLiveTestOwner(t, st)
	tmpl := newLiveTestTemplate(t, st, owner, "live-during", "during", true)
	noteID, transcriptID, streamID := newLiveTestStream(t, st, owner)

	appendFinalSegment(t, st, transcriptID, streamID, "hello")

	rows, err := st.ListVisibleLiveTemplateOutputs(context.Background(), owner, noteID)
	if err != nil {
		t.Fatalf("list outputs: %v", err)
	}
	if len(rows) != 1 || rows[0].TemplateID != tmpl.ID {
		t.Fatalf("expected one live output row for the template, got %+v", rows)
	}
	if rows[0].DesiredRevision != 1 {
		t.Fatalf("desired_revision = %d, want 1", rows[0].DesiredRevision)
	}
	if countActiveLiveJobsForOutput(t, st, rows[0].ID) != 1 {
		t.Fatalf("expected exactly one active job after first segment")
	}

	appendFinalSegment(t, st, transcriptID, streamID, "world")

	rows2, err := st.ListVisibleLiveTemplateOutputs(context.Background(), owner, noteID)
	if err != nil {
		t.Fatalf("list outputs: %v", err)
	}
	if len(rows2) != 1 {
		t.Fatalf("expected still one output row, got %d", len(rows2))
	}
	if rows2[0].DesiredRevision != 2 {
		t.Fatalf("desired_revision = %d, want 2 after second segment", rows2[0].DesiredRevision)
	}
	if countActiveLiveJobsForOutput(t, st, rows2[0].ID) != 1 {
		t.Fatalf("expected demand growth to coalesce onto the single active job, not create a second")
	}
}

// TestLiveTemplateCap_ConcurrentWritersSerialize proves the eight-template
// cap holds under concurrent writers (cross-review finding on PR #769).
// validateLiveTemplateCap is a count followed by a decision, so the store
// serializes it per owner with a transaction advisory lock. The interleaving
// is forced rather than hoped for: the test holds the owner's lock itself,
// starts two writers racing for the eighth slot, proves neither completes
// while the lock is held (a writer that finishes here is one that never
// took the lock), then releases it and proves exactly one succeeded, one hit
// the cap, and the database holds exactly eight.
func TestLiveTemplateCap_ConcurrentWritersSerialize(t *testing.T) {
	for _, tc := range []struct {
		name      string
		viaUpdate bool
	}{{"create", false}, {"update", true}} {
		t.Run(tc.name, func(t *testing.T) {
			st := store.New(testutil.NewPool(t))
			owner := newLiveTestOwner(t, st)
			ctx := context.Background()
			for i := 0; i < store.MaxLiveTemplates-1; i++ {
				newLiveTestTemplate(t, st, owner, fmt.Sprintf("live-%d", i), "during", true)
			}
			var candA, candB model.Template
			if tc.viaUpdate {
				candA = newLiveTestTemplate(t, st, owner, "cand-a", "after", true)
				candB = newLiveTestTemplate(t, st, owner, "cand-b", "after", true)
			}

			hold, err := st.Pool().Begin(ctx)
			if err != nil {
				t.Fatalf("begin holder: %v", err)
			}
			defer hold.Rollback(ctx)
			if _, err := hold.Exec(ctx,
				`SELECT pg_advisory_xact_lock(hashtext('live_template_cap'), hashtext($1))`, owner); err != nil {
				t.Fatalf("hold cap lock: %v", err)
			}

			sections := []model.TemplateSection{{Heading: "Notes", Instruction: "Summarize."}}
			write := func(name string, tm model.Template) error {
				if tc.viaUpdate {
					return st.UpdateTemplate(ctx, owner, tm.ID, tm.Name, "during", sections, true, "", "", nil)
				}
				_, err := st.CreateTemplate(ctx, owner, name, "during", sections, true, "", "", nil)
				return err
			}
			results := make(chan error, 2)
			go func() { results <- write("eighth-a", candA) }()
			go func() { results <- write("eighth-b", candB) }()

			select {
			case err := <-results:
				t.Fatalf("a writer completed (err=%v) while the owner's cap lock was held: cap validation is not serialized", err)
			case <-time.After(300 * time.Millisecond):
			}
			if err := hold.Rollback(ctx); err != nil {
				t.Fatalf("release cap lock: %v", err)
			}

			var succeeded, capped int
			for i := 0; i < 2; i++ {
				select {
				case err := <-results:
					var ve store.ValidationError
					switch {
					case err == nil:
						succeeded++
					case errorsAs(err, &ve):
						capped++
					default:
						t.Fatalf("unexpected writer error: %v", err)
					}
				case <-time.After(10 * time.Second):
					t.Fatal("a writer never completed after the lock was released")
				}
			}
			if succeeded != 1 || capped != 1 {
				t.Fatalf("after the race: %d succeeded, %d hit the cap; want exactly one of each", succeeded, capped)
			}
			var n int
			if err := st.Pool().QueryRow(ctx,
				`SELECT count(*) FROM templates WHERE (owner_id IS NULL OR owner_id=$1) AND phase='during' AND auto_run=TRUE`,
				owner).Scan(&n); err != nil {
				t.Fatalf("count eligible: %v", err)
			}
			if n != store.MaxLiveTemplates {
				t.Fatalf("eligible templates = %d, want exactly %d", n, store.MaxLiveTemplates)
			}
		})
	}
}

// TestScheduleLiveJob_EnforcesCadence proves the durable not-before lease
// excludes a claim before 15s and admits it after, using a fake clock passed
// directly to ReconcileStreamDemandTx (rather than AppendStreamSegment's
// production wall clock).
func TestScheduleLiveJob_EnforcesCadence(t *testing.T) {
	st := store.New(testutil.NewPool(t))
	owner := newLiveTestOwner(t, st)
	newLiveTestTemplate(t, st, owner, "live-during", "during", true)
	noteID, transcriptID, streamID := newLiveTestStream(t, st, owner)

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	ctx := context.Background()
	appendFinalSegment(t, st, transcriptID, streamID, "hello")

	rows, err := st.ListVisibleLiveTemplateOutputs(ctx, owner, noteID)
	if err != nil || len(rows) != 1 {
		t.Fatalf("list outputs: %v %+v", err, rows)
	}
	outputID := rows[0].ID

	// Simulate the first job having already started, then force a second
	// demand-advancing reconciliation shortly after (before the 15s cadence
	// elapses) directly against the fixed clock.
	// The first job must be settled, not merely detached: jobs_live_active_uniq
	// allows one pending/running live_generate job per output row, so leaving
	// it pending would make the second schedule a unique violation instead of
	// a cadence check.
	if _, err := st.Pool().Exec(ctx,
		`UPDATE jobs SET status=$2, finished_at=now()
		  WHERE id=(SELECT active_job_id FROM live_template_outputs WHERE id=$1)`,
		outputID, model.JobDone); err != nil {
		t.Fatalf("settle first job: %v", err)
	}
	if _, err := st.Pool().Exec(ctx,
		`UPDATE live_template_outputs SET active_job_id=NULL, last_started_at=$2 WHERE id=$1`,
		outputID, base); err != nil {
		t.Fatalf("simulate started job: %v", err)
	}

	tx, err := st.Pool().Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx) // a t.Fatalf below must not leave the pool's connection held
	if _, err := store.ReconcileStreamDemandTx(ctx, tx, owner, noteID, transcriptID, streamID, base.Add(5*time.Second)); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	var leaseNotBefore time.Time
	if err := st.Pool().QueryRow(ctx,
		`SELECT j.lease_expires_at FROM jobs j JOIN live_template_outputs o ON o.active_job_id=j.id WHERE o.id=$1`,
		outputID).Scan(&leaseNotBefore); err != nil {
		t.Fatalf("read lease: %v", err)
	}
	wantNotBefore := base.Add(15 * time.Second)
	if !leaseNotBefore.Equal(wantNotBefore) {
		t.Fatalf("lease not-before = %v, want %v (base+15s cadence)", leaseNotBefore, wantNotBefore)
	}
}

// TestReconcileOwnerEligibility_TemplateDeleted_RemovesIdleAndQueuedRows
// proves idle and queued rows disappear immediately on deletion, without any
// later transcript segment.
func TestReconcileOwnerEligibility_TemplateDeleted_RemovesIdleAndQueuedRows(t *testing.T) {
	st := store.New(testutil.NewPool(t))
	owner := newLiveTestOwner(t, st)
	tmpl := newLiveTestTemplate(t, st, owner, "live-during", "during", true)
	noteID, transcriptID, streamID := newLiveTestStream(t, st, owner)
	appendFinalSegment(t, st, transcriptID, streamID, "hello")

	rows, err := st.ListVisibleLiveTemplateOutputs(context.Background(), owner, noteID)
	if err != nil || len(rows) != 1 {
		t.Fatalf("list outputs: %v %+v", err, rows)
	}
	outputID := rows[0].ID

	if err := st.DeleteTemplate(context.Background(), owner, tmpl.ID); err != nil {
		t.Fatalf("delete template: %v", err)
	}

	var exists bool
	if err := st.Pool().QueryRow(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM live_template_outputs WHERE id=$1)`, outputID).Scan(&exists); err != nil {
		t.Fatalf("check row: %v", err)
	}
	if exists {
		t.Fatal("expected queued/idle live output row to be deleted immediately on template deletion")
	}
}

// TestReconcileOwnerEligibility_RunningRow_HiddenAndCancellationRequested
// proves a running row is hidden and cancellation-requested (not deleted)
// when its template becomes ineligible while still existing (auto_run
// switched off). Deletion is the one ineligibility that cannot leave a hidden
// row: live_template_outputs.template_id cascades on template delete, so the
// row and its job vanish before reconciliation runs and the completion fence
// then finds no job row (see the TemplateDeleted test above).
func TestReconcileOwnerEligibility_RunningRow_HiddenAndCancellationRequested(t *testing.T) {
	st := store.New(testutil.NewPool(t))
	owner := newLiveTestOwner(t, st)
	tmpl := newLiveTestTemplate(t, st, owner, "live-during", "during", true)
	noteID, transcriptID, streamID := newLiveTestStream(t, st, owner)
	appendFinalSegment(t, st, transcriptID, streamID, "hello")

	rows, err := st.ListVisibleLiveTemplateOutputs(context.Background(), owner, noteID)
	if err != nil || len(rows) != 1 {
		t.Fatalf("list outputs: %v %+v", err, rows)
	}
	outputID := rows[0].ID
	if _, err := st.Pool().Exec(context.Background(),
		`UPDATE live_template_outputs SET status='running' WHERE id=$1`, outputID); err != nil {
		t.Fatalf("simulate running: %v", err)
	}
	if _, err := st.Pool().Exec(context.Background(),
		`UPDATE jobs SET status='running' WHERE live_output_id=$1`, outputID); err != nil {
		t.Fatalf("simulate running job: %v", err)
	}

	sections := []model.TemplateSection{{Heading: "Notes", Instruction: "Summarize."}}
	if err := st.UpdateTemplate(context.Background(), owner, tmpl.ID, tmpl.Name, "during", sections, false, "", "", nil); err != nil {
		t.Fatalf("switch auto_run off: %v", err)
	}

	var clientVisible bool
	var cancelRequested bool
	if err := st.Pool().QueryRow(context.Background(),
		`SELECT client_visible, cancellation_requested_at IS NOT NULL FROM live_template_outputs WHERE id=$1`,
		outputID).Scan(&clientVisible, &cancelRequested); err != nil {
		t.Fatalf("read row: %v", err)
	}
	if clientVisible {
		t.Fatal("expected running row to become non-visible")
	}
	if !cancelRequested {
		t.Fatal("expected running row to be cancellation-requested")
	}
}

// TestReconcileOwnerEligibility_ZeroOutputActiveStream_DiscoversOwner proves
// a regression fixture: an owner with an active stream that has finalized
// speech but NO live_template_outputs rows yet is still discovered when a
// template becomes eligible, scheduling at the current revision without any
// later segment.
func TestReconcileOwnerEligibility_ZeroOutputActiveStream_DiscoversOwner(t *testing.T) {
	st := store.New(testutil.NewPool(t))
	owner := newLiveTestOwner(t, st)
	noteID, transcriptID, streamID := newLiveTestStream(t, st, owner)
	appendFinalSegment(t, st, transcriptID, streamID, "hello")
	appendFinalSegment(t, st, transcriptID, streamID, "world")

	// No live_template_outputs rows exist yet (no eligible template so far).
	rows, err := st.ListVisibleLiveTemplateOutputs(context.Background(), owner, noteID)
	if err != nil || len(rows) != 0 {
		t.Fatalf("expected zero output rows before any eligible template, got %v %+v", err, rows)
	}

	// Now make a template eligible via CreateTemplate; this alone must
	// discover the active stream and schedule at revision 2.
	newLiveTestTemplate(t, st, owner, "live-during", "during", true)

	rows2, err := st.ListVisibleLiveTemplateOutputs(context.Background(), owner, noteID)
	if err != nil {
		t.Fatalf("list outputs: %v", err)
	}
	if len(rows2) != 1 {
		t.Fatalf("expected the newly eligible template to be discovered and scheduled, got %+v", rows2)
	}
	if rows2[0].DesiredRevision != 2 {
		t.Fatalf("desired_revision = %d, want 2 (current revision, no later segment needed)", rows2[0].DesiredRevision)
	}
}

// TestSeededOverCap_SchedulesOnlyStableFirstEight seeds nine directly-created
// eligible templates (bypassing the create/update cap, mirroring
// pre-existing over-cap data) and proves only the stable first eight ever
// get a live output row.
func TestSeededOverCap_SchedulesOnlyStableFirstEight(t *testing.T) {
	st := store.New(testutil.NewPool(t))
	owner := newLiveTestOwner(t, st)

	// Seed nine eligible templates directly at the DB layer, bypassing
	// CreateTemplate's cap validation -- the same shape pre-existing
	// over-cap data would have.
	ctx := context.Background()
	var names []string
	for i := 0; i < store.MaxLiveTemplates+1; i++ {
		name := fmt.Sprintf("seed-%02d", i)
		names = append(names, name)
		sections := `[{"heading":"Notes","instruction":"Summarize."}]`
		if _, err := st.Pool().Exec(ctx,
			`INSERT INTO templates (id, owner_id, name, phase, sections, auto_run) VALUES (gen_random_uuid(),$1,$2,'during',$3::jsonb,TRUE)`,
			owner, name, sections); err != nil {
			t.Fatalf("seed template %d: %v", i, err)
		}
	}

	noteID, transcriptID, streamID := newLiveTestStream(t, st, owner)
	appendFinalSegment(t, st, transcriptID, streamID, "hello")

	rows, err := st.ListVisibleLiveTemplateOutputs(ctx, owner, noteID)
	if err != nil {
		t.Fatalf("list outputs: %v", err)
	}
	if len(rows) != store.MaxLiveTemplates {
		t.Fatalf("expected exactly %d scheduled outputs, got %d", store.MaxLiveTemplates, len(rows))
	}
	// The stable order is by lower(name); the ninth ("seed-08") sorts last
	// and must be the one omitted.
	for _, r := range rows {
		if r.TemplateName == "seed-08" {
			t.Fatalf("expected the alphabetically-last (ninth) template to be omitted, found it scheduled")
		}
	}
}
