package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/api"
	"github.com/abedegno/muesli/internal/calendar"
	"github.com/abedegno/muesli/internal/crypto"
	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
)

// adminPreJobTestBase is a fixed, deterministic instant used instead of the
// wall clock in this file (see scripts/check-test-determinism.sh).
var adminPreJobTestBase = testutil.NewFakeClock(time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)).Now()

// store.RetryPreBriefJob's "has the event started" check is real production
// code and genuinely reads the wall clock (there is no per-call clock
// argument in its accepted interface -- see the plan's "RetryPreBriefJob(jobID)"
// ruling). A literal fixed instant would silently go stale as real time
// passes it, so this file pins that one seam to the same fixed base every
// event/job in this file is seeded relative to. This runs once at package
// init, before any t.Parallel() test starts, so it is not a data race with
// this file's own parallel subtests; no other file in this package exercises
// RetryPreBriefJob's real-time path.
func init() {
	store.RetryPreBriefClock = func() time.Time { return adminPreJobTestBase }
}

// adminPreJobFixture seeds a store, an owner, a calendar source/event, a
// pre/auto-run template, and one claimed-then-failed pre_generate job ready
// for retry via the admin HTTP API.
type adminPreJobFixture struct {
	st      *store.Store
	srv     *api.Server
	hdr     map[string]string
	owner   string
	eventID string
	tmplID  string
	briefID string
	jobID   string
}

func newAdminPreJobFixture(t *testing.T, startsIn time.Duration) adminPreJobFixture {
	t.Helper()
	ctx := context.Background()
	st := store.New(testutil.NewPool(t))
	cr, err := crypto.New("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	if err != nil {
		t.Fatal(err)
	}
	srv := api.NewServer(api.Deps{Store: st, Crypto: cr})
	hdr := setupLoginHdr(t, srv, "admin-pre-login@example.com")

	u, err := st.CreateUser(ctx, "admin-pre-owner@example.com", "h")
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	src, err := st.CreateSource(ctx, u.ID, "ics", "Cal", "sealed")
	if err != nil {
		t.Fatalf("create source: %v", err)
	}
	starts := adminPreJobTestBase.Add(startsIn)
	if err := st.UpsertEvents(ctx, u.ID, src.ID, []calendar.NormalizedEvent{
		{ExternalID: "ext-1", Title: "Planning", StartsAt: starts, EndsAt: starts.Add(time.Hour)},
	}); err != nil {
		t.Fatalf("upsert event: %v", err)
	}
	evs, err := st.ListEvents(ctx, u.ID, starts.Add(-time.Minute), starts.Add(time.Minute))
	if err != nil || len(evs) != 1 {
		t.Fatalf("list event: %+v %v", evs, err)
	}
	eventID := evs[0].ID

	tmpl, err := st.CreateTemplate(ctx, u.ID, "Pre-read", "pre",
		[]model.TemplateSection{{Heading: "Context", Instruction: "Summarize."}}, true, "", "", nil)
	if err != nil {
		t.Fatalf("create template: %v", err)
	}

	h := "h1"
	if _, err := st.ReconcileEventBriefPair(ctx, eventID, tmpl.ID, tmpl.Name, &h); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	briefs, err := st.EventBriefsForEvents(ctx, u.ID, []string{eventID})
	if err != nil || len(briefs[eventID]) != 1 {
		t.Fatalf("event briefs: %+v %v", briefs, err)
	}
	briefID := briefs[eventID][0].ID

	job, ok, err := st.ClaimJob(ctx, time.Minute)
	if err != nil || !ok {
		t.Fatalf("claim job: ok=%v err=%v", ok, err)
	}
	if err := st.FailJob(ctx, job.ID, "injected", false); err != nil {
		t.Fatalf("fail job: %v", err)
	}

	return adminPreJobFixture{st: st, srv: srv, hdr: hdr, owner: u.ID, eventID: eventID, tmplID: tmpl.ID, briefID: briefID, jobID: job.ID}
}

func TestAdminRetryPreGenerateJobEligibleReEnqueues(t *testing.T) {
	t.Parallel()
	f := newAdminPreJobFixture(t, 2*time.Hour)

	rec := doJSON(t, f.srv, http.MethodPost, "/api/admin/jobs/"+f.jobID+"/retry", nil, f.hdr)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("retry status = %d, body %s", rec.Code, rec.Body)
	}
	var body struct {
		Status string `json:"status"`
		JobID  string `json:"job_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Status != "queued" || body.JobID == "" || body.JobID == f.jobID {
		t.Fatalf("unexpected retry response: %+v", body)
	}

	got, err := f.st.GetJob(context.Background(), body.JobID)
	if err != nil {
		t.Fatalf("get new job: %v", err)
	}
	if got.CalendarEventID != f.eventID || got.BriefID != f.briefID || got.Type != model.JobPreGenerate {
		t.Fatalf("new job target mismatch: %+v", got)
	}
}

func TestAdminRetryPreGenerateJobMissingReturns404(t *testing.T) {
	t.Parallel()
	f := newAdminPreJobFixture(t, 2*time.Hour)
	_ = f

	rec := doJSON(t, f.srv, http.MethodPost, "/api/admin/jobs/00000000-0000-0000-0000-000000000000/retry", nil, f.hdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("retry status = %d, want 404, body %s", rec.Code, rec.Body)
	}
}

func TestAdminRetryPreGenerateJobStartedEventReturns409(t *testing.T) {
	t.Parallel()
	// The event already started (startsIn negative), so the pair is
	// ineligible by the time retry runs.
	f := newAdminPreJobFixture(t, -time.Hour)

	rec := doJSON(t, f.srv, http.MethodPost, "/api/admin/jobs/"+f.jobID+"/retry", nil, f.hdr)
	if rec.Code != http.StatusConflict {
		t.Fatalf("retry status = %d, want 409, body %s", rec.Code, rec.Body)
	}
}

func TestAdminRetryPreGenerateJobDeletedTemplateReturns404(t *testing.T) {
	t.Parallel()
	f := newAdminPreJobFixture(t, 2*time.Hour)

	if err := f.st.DeleteTemplate(context.Background(), f.owner, f.tmplID); err != nil {
		t.Fatalf("delete template: %v", err)
	}

	rec := doJSON(t, f.srv, http.MethodPost, "/api/admin/jobs/"+f.jobID+"/retry", nil, f.hdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("retry status = %d, want 404, body %s", rec.Code, rec.Body)
	}
}

// TestAdminRetryPreGenerateJobTwoOwnersNoCrossOwnerJoin proves retrying owner
// A's job, in a store shared with a second owner who has an
// identically-named "Pre-read" pre/auto-run template, resolves strictly
// through A's own event -- never B's template -- even though
// RetryPreBriefJob starts from the job id alone and derives owner
// internally.
func TestAdminRetryPreGenerateJobTwoOwnersNoCrossOwnerJoin(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := store.New(testutil.NewPool(t))
	cr, err := crypto.New("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	if err != nil {
		t.Fatal(err)
	}
	srv := api.NewServer(api.Deps{Store: st, Crypto: cr})
	hdr := setupLoginHdr(t, srv, "admin-two-owner-login@example.com")

	ownerA, err := st.CreateUser(ctx, "owner-a@example.com", "h")
	if err != nil {
		t.Fatal(err)
	}
	ownerB, err := st.CreateUser(ctx, "owner-b@example.com", "h")
	if err != nil {
		t.Fatal(err)
	}

	seedPair := func(owner string) (eventID, templateID, jobID string) {
		src, err := st.CreateSource(ctx, owner, "ics", "Cal", "sealed")
		if err != nil {
			t.Fatal(err)
		}
		starts := adminPreJobTestBase.Add(2 * time.Hour)
		if err := st.UpsertEvents(ctx, owner, src.ID, []calendar.NormalizedEvent{
			{ExternalID: "ext-1", Title: "Planning", StartsAt: starts, EndsAt: starts.Add(time.Hour)},
		}); err != nil {
			t.Fatal(err)
		}
		evs, err := st.ListEvents(ctx, owner, starts.Add(-time.Minute), starts.Add(time.Minute))
		if err != nil || len(evs) != 1 {
			t.Fatalf("list event: %+v %v", evs, err)
		}
		tmpl, err := st.CreateTemplate(ctx, owner, "Pre-read", "pre",
			[]model.TemplateSection{{Heading: "Context", Instruction: "Summarize."}}, true, "", "", nil)
		if err != nil {
			t.Fatal(err)
		}
		h := "h1"
		if _, err := st.ReconcileEventBriefPair(ctx, evs[0].ID, tmpl.ID, tmpl.Name, &h); err != nil {
			t.Fatal(err)
		}
		job, ok, err := st.ClaimJob(ctx, time.Minute)
		if err != nil || !ok {
			t.Fatalf("claim: ok=%v err=%v", ok, err)
		}
		if err := st.FailJob(ctx, job.ID, "injected", false); err != nil {
			t.Fatal(err)
		}
		return evs[0].ID, tmpl.ID, job.ID
	}

	eventA, tmplA, jobA := seedPair(ownerA.ID)
	eventB, tmplB, _ := seedPair(ownerB.ID)
	if tmplA == tmplB {
		t.Fatal("test setup bug: templates must have distinct ids")
	}

	rec := doJSON(t, srv, http.MethodPost, "/api/admin/jobs/"+jobA+"/retry", nil, hdr)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("retry status = %d, body %s", rec.Code, rec.Body)
	}
	var body struct {
		JobID string `json:"job_id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)

	newJob, err := st.GetJob(ctx, body.JobID)
	if err != nil {
		t.Fatalf("get new job: %v", err)
	}
	if newJob.CalendarEventID != eventA {
		t.Fatalf("retry resolved the wrong event: got %s, want %s", newJob.CalendarEventID, eventA)
	}
	var payload struct {
		TemplateID string `json:"template_id"`
	}
	if err := json.Unmarshal(newJob.Payload, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.TemplateID != tmplA {
		t.Fatalf("retry resolved owner B's template: got %s, want owner A's %s", payload.TemplateID, tmplA)
	}
	_ = eventB
}
