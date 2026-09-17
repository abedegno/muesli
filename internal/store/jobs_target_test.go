package store_test

// This file is DB-backed and CI-only: testutil.NewPool skips when
// TEST_DATABASE_URL is unset.

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
)

// seedJobTargetEvent creates a fresh owner and one calendar event (reusing
// the note_event_test.go seedCalendarEvent helper), returning the store,
// owner id, and event id -- enough to satisfy jobs.calendar_event_id's FK.
func seedJobTargetEvent(t *testing.T) (*store.Store, string, string) {
	t.Helper()
	ctx := context.Background()
	st := store.New(testutil.NewPool(t))
	email := fmt.Sprintf("jobtarget-%d@example.com", seedUserCounter.Add(1))
	u, err := st.CreateUser(ctx, email, "h")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	eventID := seedCalendarEvent(t, st, u.ID)
	return st, u.ID, eventID
}

func TestEnqueueJobRejectsMismatchedTarget(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := store.New(testutil.NewPool(t))

	if _, err := st.EnqueueJob(ctx, store.JobEnqueue{Type: model.JobTranscribe}); err == nil {
		t.Fatal("expected error enqueuing a transcribe job with no target")
	}
	if _, err := st.EnqueueJob(ctx, store.JobEnqueue{Type: model.JobPreGenerate}); err == nil {
		t.Fatal("expected error enqueuing a pre_generate job with no event target")
	}
	noteID := seedNote(t, st)
	if _, err := st.EnqueueJob(ctx, store.JobEnqueue{
		NoteID: noteID, CalendarEventID: "11111111-1111-1111-1111-111111111111", Type: model.JobTranscribe,
	}); err == nil {
		t.Fatal("expected error enqueuing a job with both targets set")
	}
	if _, err := st.EnqueueJob(ctx, store.JobEnqueue{NoteID: noteID, Type: model.JobPreGenerate}); err == nil {
		t.Fatal("expected error enqueuing a pre_generate job targeting a note")
	}
}

func TestEnqueueJobPreGenerateRequiresBrief(t *testing.T) {
	t.Parallel()
	st, _, eventID := seedJobTargetEvent(t)
	ctx := context.Background()

	if _, err := st.EnqueueJob(ctx, store.JobEnqueue{
		CalendarEventID: eventID, Type: model.JobPreGenerate,
	}); err == nil {
		t.Fatal("expected error enqueuing a pre_generate job with no brief id/generation")
	}
}

func TestEnqueueAndClaimPreGenerateJob(t *testing.T) {
	t.Parallel()
	st, _, eventID := seedJobTargetEvent(t)
	ctx := context.Background()

	briefID := "22222222-2222-2222-2222-222222222222"
	payload, _ := json.Marshal(map[string]any{"brief_id": briefID, "template_id": "t1", "generation": 1})
	jobID, err := st.EnqueueJob(ctx, store.JobEnqueue{
		CalendarEventID: eventID, BriefID: briefID, BriefGeneration: 1,
		Type: model.JobPreGenerate, Payload: payload,
	})
	if err != nil {
		t.Fatalf("enqueue pre_generate: %v", err)
	}

	job, ok, err := st.ClaimJob(ctx, 30*time.Second)
	if err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	if job.ID != jobID {
		t.Fatalf("claimed wrong job: %+v", job)
	}
	if job.NoteID != "" {
		t.Fatalf("pre_generate job should have no note target, got %q", job.NoteID)
	}
	if job.CalendarEventID != eventID || job.BriefID != briefID || job.BriefGeneration != 1 {
		t.Fatalf("claimed job target mismatch: %+v", job)
	}
	kind, err := job.TargetKind()
	if err != nil || kind != model.JobTargetCalendarEvent {
		t.Fatalf("TargetKind() = %v, %v, want calendar_event", kind, err)
	}

	got, err := st.GetJob(ctx, jobID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if got.CalendarEventID != eventID || got.BriefID != briefID || got.BriefGeneration != 1 {
		t.Fatalf("GetJob target mismatch: %+v", got)
	}
}

// TestEnqueueJobPreGenerateDuplicateGenerationRejected proves the partial
// unique index (brief_id, brief_generation) WHERE status IN
// ('pending','running') is the race guard against two reconcilers enqueuing
// the same generation twice.
func TestEnqueueJobPreGenerateDuplicateGenerationRejected(t *testing.T) {
	t.Parallel()
	st, _, eventID := seedJobTargetEvent(t)
	ctx := context.Background()

	briefID := "33333333-3333-3333-3333-333333333333"
	spec := store.JobEnqueue{CalendarEventID: eventID, BriefID: briefID, BriefGeneration: 1, Type: model.JobPreGenerate}
	if _, err := st.EnqueueJob(ctx, spec); err != nil {
		t.Fatalf("first enqueue: %v", err)
	}
	if _, err := st.EnqueueJob(ctx, spec); err == nil {
		t.Fatal("expected the second enqueue for the same brief+generation to fail the unique index")
	}
}

func TestListJobsIncludesCalendarEventJobs(t *testing.T) {
	t.Parallel()
	st, _, eventID := seedJobTargetEvent(t)
	ctx := context.Background()

	briefID := "44444444-4444-4444-4444-444444444444"
	jobID, err := st.EnqueueJob(ctx, store.JobEnqueue{
		CalendarEventID: eventID, BriefID: briefID, BriefGeneration: 1, Type: model.JobPreGenerate,
	})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	jobs, err := st.ListJobs(ctx, "")
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	var found bool
	for _, j := range jobs {
		if j.ID == jobID {
			found = true
			if j.NoteID != "" {
				t.Fatalf("event job must not carry a fake note id, got %q", j.NoteID)
			}
			if j.CalendarEventID != eventID {
				t.Fatalf("event job target mismatch: %+v", j)
			}
		}
	}
	if !found {
		t.Fatal("global job listing did not include the calendar event job")
	}
}
