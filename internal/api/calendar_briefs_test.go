package api_test

// This file is DB-backed and CI-only: testutil.NewPool skips when
// TEST_DATABASE_URL is unset.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/api"
	"github.com/abedegno/muesli/internal/calendar"
	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
)

// calendarBriefsTestBase is a fixed, deterministic instant used instead of
// the wall clock in this file (see scripts/check-test-determinism.sh).
var calendarBriefsTestBase = testutil.NewFakeClock(time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)).Now()

// calendarEventsResponseItem captures exactly the public shape of one
// calendar event's JSON, so a test can assert the brief's key allowlist
// precisely: no job payloads, hashes, generations, owner ids, credentials,
// or plugin errors.
type calendarEventsResponseItem struct {
	ID     string           `json:"id"`
	Briefs []map[string]any `json:"briefs"`
}

func seedBriefEventForOwner(t *testing.T, st *store.Store, ownerID string, startsIn time.Duration) string {
	t.Helper()
	ctx := context.Background()
	src, err := st.CreateSource(ctx, ownerID, "ics", "Cal", "sealed")
	if err != nil {
		t.Fatalf("create source: %v", err)
	}
	starts := calendarBriefsTestBase.Add(startsIn)
	if err := st.UpsertEvents(ctx, ownerID, src.ID, []calendar.NormalizedEvent{
		{ExternalID: "ext-1", Title: "Planning", StartsAt: starts, EndsAt: starts.Add(time.Hour)},
	}); err != nil {
		t.Fatalf("upsert event: %v", err)
	}
	evs, err := st.ListEvents(ctx, ownerID, starts.Add(-time.Minute), starts.Add(time.Minute))
	if err != nil || len(evs) != 1 {
		t.Fatalf("list event: %+v %v", evs, err)
	}
	return evs[0].ID
}

func getCalendarEvents(t *testing.T, srv *api.Server, hdr map[string]string) []calendarEventsResponseItem {
	t.Helper()
	from := calendarBriefsTestBase.Add(-time.Hour).Format(time.RFC3339)
	to := calendarBriefsTestBase.Add(8 * 24 * time.Hour).Format(time.RFC3339)
	path := "/api/calendar/events?from=" + url.QueryEscape(from) + "&to=" + url.QueryEscape(to)
	rec := doJSON(t, srv, http.MethodGet, path, nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("list calendar events status %d body %s", rec.Code, rec.Body)
	}
	var items []calendarEventsResponseItem
	if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
		t.Fatalf("decode calendar events: %v", err)
	}
	return items
}

func findCalendarEvent(t *testing.T, items []calendarEventsResponseItem, eventID string) calendarEventsResponseItem {
	t.Helper()
	for _, it := range items {
		if it.ID == eventID {
			return it
		}
	}
	t.Fatalf("event %s not found in response: %+v", eventID, items)
	return calendarEventsResponseItem{}
}

// TestCalendarEventsBriefsAlwaysArrayAndKeyAllowlist covers empty, pending,
// ready, and failed states, plus the exact public key allowlist -- no
// hashes, generations, owner ids, agent_plugin, or event_id ever leave the
// server.
func TestCalendarEventsBriefsAlwaysArrayAndKeyAllowlist(t *testing.T) {
	t.Parallel()
	srv, st := newCalendarTestServer(t)
	hdr := calendarAuthHeader(t, srv, "brief-owner@example.com")

	ctx := context.Background()
	// calendarAuthHeader already created the user via /api/setup; look it up
	// so this test can drive the store directly.
	owner, err := st.GetUserByEmail(ctx, "brief-owner@example.com")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}

	emptyEventID := seedBriefEventForOwner(t, st, owner.ID, 3*time.Hour)

	pendingEventID := seedBriefEventForOwner(t, st, owner.ID, 4*time.Hour)
	pendingTmpl, err := st.CreateTemplate(ctx, owner.ID, "Pending template", "pre",
		[]model.TemplateSection{{Heading: "Context", Instruction: "Summarize."}}, true, "", "", nil)
	if err != nil {
		t.Fatalf("create pending template: %v", err)
	}
	hp := "hp"
	if _, err := st.ReconcileEventBriefPair(ctx, pendingEventID, pendingTmpl.ID, pendingTmpl.Name, &hp); err != nil {
		t.Fatalf("reconcile pending: %v", err)
	}

	readyEventID := seedBriefEventForOwner(t, st, owner.ID, 5*time.Hour)
	readyTmpl, err := st.CreateTemplate(ctx, owner.ID, "Ready template", "pre",
		[]model.TemplateSection{{Heading: "Context", Instruction: "Summarize."}}, true, "", "", nil)
	if err != nil {
		t.Fatalf("create ready template: %v", err)
	}
	hr := "hr"
	if _, err := st.ReconcileEventBriefPair(ctx, readyEventID, readyTmpl.ID, readyTmpl.Name, &hr); err != nil {
		t.Fatalf("reconcile ready: %v", err)
	}
	readyBriefs, err := st.EventBriefsForEvents(ctx, owner.ID, []string{readyEventID})
	if err != nil || len(readyBriefs[readyEventID]) != 1 {
		t.Fatalf("ready briefs: %+v %v", readyBriefs, err)
	}
	if _, err := st.PublishEventBrief(ctx, readyBriefs[readyEventID][0].ID, 1, "ollama-agent", "llama3.2:3b",
		[]model.SummarySection{{Heading: "Context", ContentMarkdown: "Ready content."}}); err != nil {
		t.Fatalf("publish ready: %v", err)
	}

	failedEventID := seedBriefEventForOwner(t, st, owner.ID, 6*time.Hour)
	failedTmpl, err := st.CreateTemplate(ctx, owner.ID, "Failed template", "pre",
		[]model.TemplateSection{{Heading: "Context", Instruction: "Summarize."}}, true, "", "", nil)
	if err != nil {
		t.Fatalf("create failed template: %v", err)
	}
	hf := "hf"
	if _, err := st.ReconcileEventBriefPair(ctx, failedEventID, failedTmpl.ID, failedTmpl.Name, &hf); err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	failedBriefs, err := st.EventBriefsForEvents(ctx, owner.ID, []string{failedEventID})
	if err != nil || len(failedBriefs[failedEventID]) != 1 {
		t.Fatalf("failed briefs: %+v %v", failedBriefs, err)
	}
	if _, err := st.FailEventBriefIfCurrent(ctx, failedBriefs[failedEventID][0].ID, 1); err != nil {
		t.Fatalf("fail: %v", err)
	}

	items := getCalendarEvents(t, srv, hdr)

	empty := findCalendarEvent(t, items, emptyEventID)
	if empty.Briefs == nil || len(empty.Briefs) != 0 {
		t.Fatalf("expected briefs=[] for an event with no templates, got %+v", empty.Briefs)
	}

	pending := findCalendarEvent(t, items, pendingEventID)
	if len(pending.Briefs) != 1 {
		t.Fatalf("expected one pending brief, got %+v", pending.Briefs)
	}
	assertBriefKeyAllowlist(t, pending.Briefs[0])
	if pending.Briefs[0]["status"] != model.BriefPending {
		t.Fatalf("pending brief status = %v", pending.Briefs[0]["status"])
	}
	if sections, ok := pending.Briefs[0]["sections"].([]any); !ok || len(sections) != 0 {
		t.Fatalf("pending brief sections must be empty, got %v", pending.Briefs[0]["sections"])
	}

	ready := findCalendarEvent(t, items, readyEventID)
	if len(ready.Briefs) != 1 {
		t.Fatalf("expected one ready brief, got %+v", ready.Briefs)
	}
	assertBriefKeyAllowlist(t, ready.Briefs[0])
	if ready.Briefs[0]["status"] != model.BriefReady {
		t.Fatalf("ready brief status = %v", ready.Briefs[0]["status"])
	}
	if sections, ok := ready.Briefs[0]["sections"].([]any); !ok || len(sections) != 1 {
		t.Fatalf("ready brief must carry its generated sections, got %v", ready.Briefs[0]["sections"])
	}
	if ready.Briefs[0]["model"] != "llama3.2:3b" {
		t.Fatalf("ready brief model = %v", ready.Briefs[0]["model"])
	}

	failed := findCalendarEvent(t, items, failedEventID)
	if len(failed.Briefs) != 1 {
		t.Fatalf("expected one failed brief, got %+v", failed.Briefs)
	}
	assertBriefKeyAllowlist(t, failed.Briefs[0])
	if failed.Briefs[0]["status"] != model.BriefFailed {
		t.Fatalf("failed brief status = %v", failed.Briefs[0]["status"])
	}
	if sections, ok := failed.Briefs[0]["sections"].([]any); !ok || len(sections) != 0 {
		t.Fatalf("failed brief sections must be empty, got %v", failed.Briefs[0]["sections"])
	}
}

func assertBriefKeyAllowlist(t *testing.T, brief map[string]any) {
	t.Helper()
	allowed := map[string]bool{
		"id": true, "template_id": true, "template_name": true,
		"status": true, "sections": true, "model": true, "updated_at": true,
	}
	for key := range brief {
		if !allowed[key] {
			t.Fatalf("brief exposed a disallowed key %q: %+v", key, brief)
		}
	}
	for key := range allowed {
		if _, ok := brief[key]; !ok {
			t.Fatalf("brief missing required key %q: %+v", key, brief)
		}
	}
}

// TestCalendarEventsBriefsOwnerIsolation proves two owners' briefs never
// cross even when both have an eligible pair for an event of the same shape.
func TestCalendarEventsBriefsOwnerIsolation(t *testing.T) {
	t.Parallel()
	srv, st := newCalendarTestServer(t)
	ownerAHdr := calendarAuthHeader(t, srv, "brief-owner-a@example.com")
	ownerBHdr := calendarAuthHeaderForOtherUser(t, st, "brief-owner-b@example.com")

	ctx := context.Background()
	ownerA, err := st.GetUserByEmail(ctx, "brief-owner-a@example.com")
	if err != nil {
		t.Fatalf("get owner A: %v", err)
	}
	ownerB, err := st.GetUserByEmail(ctx, "brief-owner-b@example.com")
	if err != nil {
		t.Fatalf("get owner B: %v", err)
	}

	eventA := seedBriefEventForOwner(t, st, ownerA.ID, 3*time.Hour)
	tmplA, err := st.CreateTemplate(ctx, ownerA.ID, "Pre-read", "pre",
		[]model.TemplateSection{{Heading: "Context", Instruction: "Summarize."}}, true, "", "", nil)
	if err != nil {
		t.Fatalf("create template A: %v", err)
	}
	hA := "ha"
	if _, err := st.ReconcileEventBriefPair(ctx, eventA, tmplA.ID, tmplA.Name, &hA); err != nil {
		t.Fatalf("reconcile A: %v", err)
	}

	eventB := seedBriefEventForOwner(t, st, ownerB.ID, 3*time.Hour)
	tmplB, err := st.CreateTemplate(ctx, ownerB.ID, "Pre-read", "pre",
		[]model.TemplateSection{{Heading: "Context", Instruction: "Summarize."}}, true, "", "", nil)
	if err != nil {
		t.Fatalf("create template B: %v", err)
	}
	hB := "hb"
	if _, err := st.ReconcileEventBriefPair(ctx, eventB, tmplB.ID, tmplB.Name, &hB); err != nil {
		t.Fatalf("reconcile B: %v", err)
	}

	itemsA := getCalendarEvents(t, srv, ownerAHdr)
	for _, it := range itemsA {
		if it.ID == eventB {
			t.Fatalf("owner A's response leaked owner B's event: %+v", it)
		}
	}
	gotA := findCalendarEvent(t, itemsA, eventA)
	if len(gotA.Briefs) != 1 || gotA.Briefs[0]["template_id"] != tmplA.ID {
		t.Fatalf("owner A did not see its own brief: %+v", gotA.Briefs)
	}

	itemsB := getCalendarEvents(t, srv, ownerBHdr)
	for _, it := range itemsB {
		if it.ID == eventA {
			t.Fatalf("owner B's response leaked owner A's event: %+v", it)
		}
	}
	gotB := findCalendarEvent(t, itemsB, eventB)
	if len(gotB.Briefs) != 1 || gotB.Briefs[0]["template_id"] != tmplB.ID {
		t.Fatalf("owner B did not see its own brief: %+v", gotB.Briefs)
	}
}

// TestCalendarEventsBriefsRemovedTemplateAndPrunedEvent proves a brief for a
// deleted template is absent, and pruning an event cascades away its brief.
func TestCalendarEventsBriefsRemovedTemplateAndPrunedEvent(t *testing.T) {
	t.Parallel()
	srv, st := newCalendarTestServer(t)
	hdr := calendarAuthHeader(t, srv, "brief-prune-owner@example.com")
	ctx := context.Background()
	owner, err := st.GetUserByEmail(ctx, "brief-prune-owner@example.com")
	if err != nil {
		t.Fatalf("get owner: %v", err)
	}

	eventID := seedBriefEventForOwner(t, st, owner.ID, 3*time.Hour)
	tmpl, err := st.CreateTemplate(ctx, owner.ID, "Pre-read", "pre",
		[]model.TemplateSection{{Heading: "Context", Instruction: "Summarize."}}, true, "", "", nil)
	if err != nil {
		t.Fatalf("create template: %v", err)
	}
	h := "h1"
	if _, err := st.ReconcileEventBriefPair(ctx, eventID, tmpl.ID, tmpl.Name, &h); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	items := getCalendarEvents(t, srv, hdr)
	before := findCalendarEvent(t, items, eventID)
	if len(before.Briefs) != 1 {
		t.Fatalf("expected one brief before deletion, got %+v", before.Briefs)
	}

	if err := st.DeleteTemplate(ctx, owner.ID, tmpl.ID); err != nil {
		t.Fatalf("delete template: %v", err)
	}
	items = getCalendarEvents(t, srv, hdr)
	afterTemplateDelete := findCalendarEvent(t, items, eventID)
	if len(afterTemplateDelete.Briefs) != 0 {
		t.Fatalf("expected no briefs once the template is deleted, got %+v", afterTemplateDelete.Briefs)
	}

	// Prune the event entirely (source no longer reports it upstream) --
	// cascades any remaining briefs (there are none left here, but the event
	// itself must also disappear from the response).
	src, err := st.ListSources(ctx, owner.ID)
	if err != nil || len(src) == 0 {
		t.Fatalf("list sources: %+v %v", src, err)
	}
	if err := st.PruneEvents(ctx, src[0].ID, nil); err != nil {
		t.Fatalf("prune events: %v", err)
	}
	items = getCalendarEvents(t, srv, hdr)
	for _, it := range items {
		if it.ID == eventID {
			t.Fatalf("expected the pruned event absent from the response: %+v", it)
		}
	}
}
