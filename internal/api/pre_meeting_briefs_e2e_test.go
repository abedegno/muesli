package api_test

// This file is DB-backed and CI-only: testutil.NewPool skips when
// TEST_DATABASE_URL is unset.
//
// TestPreMeetingBriefEndToEnd exercises the real API, store, and worker
// pieces together -- calendar reconciliation, job execution against a
// deterministic fake agent, and the Coming Up calendar-events read path --
// and proves a brief appears for an upcoming event without ever creating a
// note, transcript, or summary (issue #763's core end-to-end promise).

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/api"
	"github.com/abedegno/muesli/internal/calendar"
	"github.com/abedegno/muesli/internal/config"
	"github.com/abedegno/muesli/internal/crypto"
	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/plugintest"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
	"github.com/abedegno/muesli/internal/worker"
)

func TestPreMeetingBriefEndToEnd(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := store.New(testutil.NewPool(t))
	cr, err := crypto.New("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	if err != nil {
		t.Fatalf("crypto.New: %v", err)
	}
	srv := api.NewServer(api.Deps{Store: st, Crypto: cr})

	hdr := calendarAuthHeader(t, srv, "e2e-brief-owner@example.com")
	owner, err := st.GetUserByEmail(ctx, "e2e-brief-owner@example.com")
	if err != nil {
		t.Fatalf("get owner: %v", err)
	}

	// A deterministic fake agent (the same stub used throughout the worker
	// test suite) stands in for a real LLM.
	agent := plugintest.NewAgent()
	defer agent.Close()
	if err := st.EnsureDefaultPlugin(ctx, cr, model.PluginAgent, "agent", agent.URL(), "tok", "{}"); err != nil {
		t.Fatalf("ensure default agent: %v", err)
	}

	// Create the pre/auto-run template through the real HTTP API, exactly as
	// a user would from template settings.
	createTemplateRec := doJSON(t, srv, http.MethodPost, "/api/templates", map[string]any{
		"name":     "Pre-read",
		"phase":    "pre",
		"auto_run": true,
		"sections": []map[string]string{{"heading": "Context", "instruction": "Summarize the agenda and attendees."}},
	}, hdr)
	if createTemplateRec.Code != http.StatusCreated {
		t.Fatalf("create template status %d body %s", createTemplateRec.Code, createTemplateRec.Body)
	}

	// Seed an upcoming calendar event (bypassing the upstream ICS/CalDAV/
	// OAuth fetch itself, which internal/worker/calendarsync_test.go already
	// covers end-to-end; this test's focus is everything AFTER a successful
	// fetch: reconciliation, execution, and the read path).
	src, err := st.CreateSource(ctx, owner.ID, "ics", "Team Calendar", "sealed-creds")
	if err != nil {
		t.Fatalf("create source: %v", err)
	}
	now := time.Now()
	starts := now.Add(2 * time.Hour)
	if err := st.UpsertEvents(ctx, owner.ID, src.ID, []calendar.NormalizedEvent{
		{
			ExternalID: "ext-1", Title: "Quarterly planning", StartsAt: starts, EndsAt: starts.Add(time.Hour),
			Description: "Review the roadmap", Location: "Room 2",
			Attendees: []model.Attendee{{Name: "Jane", Email: "jane@example.com", Response: "accepted"}},
		},
	}); err != nil {
		t.Fatalf("upsert event: %v", err)
	}
	evs, err := st.ListEvents(ctx, owner.ID, starts.Add(-time.Minute), starts.Add(time.Minute))
	if err != nil || len(evs) != 1 {
		t.Fatalf("list seeded event: %+v %v", evs, err)
	}
	eventID := evs[0].ID

	// Reconciliation: the real worker entry point a successful calendar sync
	// calls (see internal/worker/calendarsync.go).
	if err := worker.ReconcilePreBriefs(ctx, st, cr, owner.ID, src.ID, now); err != nil {
		t.Fatalf("reconcile pre briefs: %v", err)
	}

	// Before execution: Coming Up must show the brief as pending, with no
	// sections exposed yet.
	beforeItems := getCalendarEvents(t, srv, hdr)
	before := findCalendarEvent(t, beforeItems, eventID)
	if len(before.Briefs) != 1 || before.Briefs[0]["status"] != model.BriefPending {
		t.Fatalf("expected one pending brief before execution: %+v", before.Briefs)
	}

	// Execution: the real worker job-processing path, via the same Processor
	// production wires up (worker.NewPool), claiming and running exactly the
	// job reconciliation enqueued.
	proc := worker.NewProcessor(st, cr, nil, config.Config{}, nil)
	job, ok, err := st.ClaimJob(ctx, time.Minute)
	if err != nil || !ok {
		t.Fatalf("claim job: ok=%v err=%v", ok, err)
	}
	if job.Type != model.JobPreGenerate || job.CalendarEventID != eventID {
		t.Fatalf("unexpected claimed job: %+v", job)
	}
	proc.Process(ctx, job)

	// After execution: the brief is ready with real generated sections, on
	// the ordinary Coming Up fetch -- no standalone brief endpoint, no
	// polling, just the next GET.
	afterItems := getCalendarEvents(t, srv, hdr)
	after := findCalendarEvent(t, afterItems, eventID)
	if len(after.Briefs) != 1 {
		t.Fatalf("expected exactly one brief after execution: %+v", after.Briefs)
	}
	got := after.Briefs[0]
	if got["status"] != model.BriefReady {
		t.Fatalf("brief status = %v, want ready", got["status"])
	}
	sections, ok := got["sections"].([]any)
	if !ok || len(sections) != 1 {
		t.Fatalf("expected one generated section, got %v", got["sections"])
	}
	assertBriefKeyAllowlist(t, got)

	// The core promise: no note, transcript, or summary was ever created for
	// this event -- a brief is entirely independent of the note pipeline.
	notesRec := doJSON(t, srv, http.MethodGet, "/api/notes", nil, hdr)
	if notesRec.Code != http.StatusOK {
		t.Fatalf("list notes status %d body %s", notesRec.Code, notesRec.Body)
	}
	var notes []map[string]any
	if err := json.Unmarshal(notesRec.Body.Bytes(), &notes); err != nil {
		t.Fatalf("decode notes: %v", err)
	}
	if len(notes) != 0 {
		t.Fatalf("expected zero notes, a pre-meeting brief must never create one: %+v", notes)
	}

	jobsRec := doJSON(t, srv, http.MethodGet, "/api/admin/jobs?status=pending", nil, hdr)
	if jobsRec.Code != http.StatusOK {
		t.Fatalf("list jobs status %d body %s", jobsRec.Code, jobsRec.Body)
	}
	var pendingJobs []model.Job
	if err := json.Unmarshal(jobsRec.Body.Bytes(), &pendingJobs); err != nil {
		t.Fatalf("decode jobs: %v", err)
	}
	for _, j := range pendingJobs {
		if j.Type == model.JobTranscribe || j.Type == model.JobSummarize {
			t.Fatalf("a pre-meeting brief must never enqueue transcribe/summarize work: %+v", j)
		}
	}
}
