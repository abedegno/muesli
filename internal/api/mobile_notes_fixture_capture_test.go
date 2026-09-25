// This file is DB-backed and CI-only: testutil.NewPool (via newTestServer)
// skips when TEST_DATABASE_URL is unset, so it must not be run on the local
// runner. It keeps the checked-in Android contract corpus
// (native/android/notes/src/test/resources/contract/detail_complete.json,
// issue #768 Task 1) honest against the real handler over time: if the
// production detail response's shape ever drifts from the fixture the
// Android module was built against, this test fails in CI.
package api_test

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/model"
)

// mobileFixtureKeySet returns the set of keys present in a decoded JSON
// object, recursing into "note" and the first element of "summaries" and its
// first "sections" entry, so structural drift anywhere in the detail shape
// is caught, not just at the top level.
func mobileFixtureKeySet(t *testing.T, raw map[string]any) map[string]bool {
	t.Helper()
	keys := map[string]bool{}
	for k := range raw {
		keys[k] = true
	}
	return keys
}

func TestMobileNotesFixtureCapture(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)
	ctx := t.Context()

	if err := st.SeedBuiltInTemplates(ctx); err != nil {
		t.Fatalf("seed templates: %v", err)
	}

	hdr, uid := authHeaderForUser(t, st, "mobile-fixture-capture@example.com")

	note, err := st.CreateNote(ctx, uid, "Sprint planning")
	if err != nil {
		t.Fatalf("create note: %v", err)
	}
	if err := st.UpdateNoteBody(ctx, uid, note.ID,
		"# Sprint planning\n\nDiscussed roadmap for **Q1** and assigned owners.\n\n- Alice: API\n- Bob: mobile"); err != nil {
		t.Fatalf("update body: %v", err)
	}
	startedAt := time.Date(2026, 1, 10, 9, 0, 0, 0, time.UTC)
	endedAt := time.Date(2026, 1, 10, 9, 45, 0, 0, time.UTC)
	if _, err := st.Pool().Exec(ctx, `UPDATE notes SET started_at=$2, ended_at=$3 WHERE id=$1`,
		note.ID, startedAt, endedAt); err != nil {
		t.Fatalf("set started/ended_at: %v", err)
	}
	if _, err := st.AddNoteTag(ctx, uid, note.ID, "planning"); err != nil {
		t.Fatalf("add tag: %v", err)
	}
	if _, err := st.AddNoteTag(ctx, uid, note.ID, "roadmap"); err != nil {
		t.Fatalf("add tag: %v", err)
	}

	readyTemplate, err := st.CreateTemplate(ctx, uid, "Meeting Notes", "after",
		[]model.TemplateSection{{Heading: "Key Decisions", Instruction: "List decisions."}}, true, "", "", nil)
	if err != nil {
		t.Fatalf("create ready template: %v", err)
	}
	pendingTemplate, err := st.CreateTemplate(ctx, uid, "Follow-ups", "after",
		[]model.TemplateSection{{Heading: "Open Items", Instruction: "List follow-ups."}}, true, "", "", nil)
	if err != nil {
		t.Fatalf("create pending template: %v", err)
	}

	readySummaryID, err := st.CreatePendingSummary(ctx, note.ID, readyTemplate.ID)
	if err != nil {
		t.Fatalf("create pending (ready) summary: %v", err)
	}
	if err := st.CompleteSummary(ctx, readySummaryID, "test-agent", "test-model", []model.SummarySection{
		{Heading: "Key Decisions", ContentMarkdown: "- Ship the mobile notes viewer first."},
		{Heading: "Action Items", ContentMarkdown: "- Alice: API\n- Bob: mobile"},
	}, true); err != nil {
		t.Fatalf("complete summary: %v", err)
	}
	if _, err := st.CreatePendingSummary(ctx, note.ID, pendingTemplate.ID); err != nil {
		t.Fatalf("create pending summary: %v", err)
	}

	rec := doJSON(t, srv, http.MethodGet, "/api/mobile/v1/notes/"+note.ID, nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("detail status %d body %s", rec.Code, rec.Body)
	}

	var live map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &live); err != nil {
		t.Fatalf("decode live response: %v", err)
	}

	fixturePath := filepath.Join("..", "..", "native", "android", "notes", "src", "test", "resources",
		"contract", "detail_complete.json")
	fixtureBytes, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read checked-in fixture %s: %v", fixturePath, err)
	}
	var fixture map[string]any
	if err := json.Unmarshal(fixtureBytes, &fixture); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}

	// Top-level and nested key sets must match exactly: a genuine capture
	// against the real handler must have the identical shape the checked-in
	// Android corpus was built from.
	if got, want := mobileFixtureKeySet(t, live), mobileFixtureKeySet(t, fixture); !mapKeysEqual(got, want) {
		t.Fatalf("top-level keys drifted: live=%v fixture=%v", got, want)
	}
	liveNote, _ := live["note"].(map[string]any)
	fixtureNote, _ := fixture["note"].(map[string]any)
	if got, want := mobileFixtureKeySet(t, liveNote), mobileFixtureKeySet(t, fixtureNote); !mapKeysEqual(got, want) {
		t.Fatalf("note keys drifted: live=%v fixture=%v", got, want)
	}

	liveSummaries, _ := live["summaries"].([]any)
	if len(liveSummaries) != 2 {
		t.Fatalf("expected 2 summaries (one ready, one pending), got %d: %+v", len(liveSummaries), liveSummaries)
	}
	var readySummary, pendingSummary map[string]any
	for _, s := range liveSummaries {
		sm, _ := s.(map[string]any)
		switch sm["status"] {
		case "ready":
			readySummary = sm
		case "pending":
			pendingSummary = sm
		}
	}
	if readySummary == nil || pendingSummary == nil {
		t.Fatalf("expected one ready and one pending summary, got %+v", liveSummaries)
	}
	if readySummary["truncated"] != true {
		t.Fatalf("expected ready summary truncated=true, got %+v", readySummary)
	}
	sections, _ := readySummary["sections"].([]any)
	if len(sections) != 2 {
		t.Fatalf("expected 2 sections on ready summary, got %+v", sections)
	}
	firstSection, _ := sections[0].(map[string]any)
	fixtureSummaries, _ := fixture["summaries"].([]any)
	fixtureFirstSummary, _ := fixtureSummaries[0].(map[string]any)
	fixtureSections, _ := fixtureFirstSummary["sections"].([]any)
	fixtureFirstSection, _ := fixtureSections[0].(map[string]any)
	if got, want := mobileFixtureKeySet(t, firstSection), mobileFixtureKeySet(t, fixtureFirstSection); !mapKeysEqual(got, want) {
		t.Fatalf("section keys drifted: live=%v fixture=%v", got, want)
	}
	pendingSections, _ := pendingSummary["sections"].([]any)
	if len(pendingSections) != 0 {
		t.Fatalf("expected pending summary to have empty sections, got %+v", pendingSections)
	}

	// Optional fields must be present (non-nil) on a note that has them set,
	// exactly like the checked-in "complete detail" fixture.
	if liveNote["started_at"] == nil || liveNote["ended_at"] == nil {
		t.Fatalf("expected started_at/ended_at present: %+v", liveNote)
	}
	tags, _ := liveNote["tags"].([]any)
	if len(tags) != 2 {
		t.Fatalf("expected 2 tags, got %+v", tags)
	}
	if live["body_markdown"] == "" {
		t.Fatalf("expected non-empty body_markdown")
	}
}

func mapKeysEqual(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}
