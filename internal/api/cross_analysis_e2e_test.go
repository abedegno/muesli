package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/abedegno/muesli/internal/crypto"
	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/plugin"
)

// TestCrossAnalysisEndToEnd is Task 14's durable end-to-end coverage: two
// real completed notes/transcripts, a real cross template (created through
// the real HTTP API, not a store shortcut), a deterministic agent stub, run
// through the real create-and-send HTTP path, then reloaded via the real
// GET .../messages route, following citations back to both notes.
//
// This exercises the same HTTP/store/executor path Electron and hosted
// deployments both use (no local-only shortcut) -- see the accepted
// spec's "Hosted reads are owner-scoped..." paragraph -- but stops at the
// Go HTTP boundary: it does not drive a real Electron renderer or a real
// plugin subprocess (those live in the separate Playwright e2e suite under
// e2e/specs, which this PR does not extend -- see the PR description for
// that scope note).
func TestCrossAnalysisEndToEnd(t *testing.T) {
	t.Parallel()
	gen := &fakeChatGenerator{resp: plugin.GenerateResponse{
		Summary: plugin.SummaryPayload{Sections: []model.SummarySection{
			{Heading: "Decisions", ContentMarkdown: "Sprint decided to ship Friday [1]; retro flagged the slip [2]."},
		}},
		Model: "e2e-agent-model",
		Usage: &plugin.GenerateUsage{TokensUsed: 7},
	}}
	srv, st := newChatTestServer(t, gen)
	cr, _ := crypto.New("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	createDefaultAgentPlugin(t, st, cr, crossAdmissionConfig(nil))
	owner, hdr := newCrossTestOwner(t, srv, st, "cross-e2e@example.com")

	a := createCrossReadyNote(t, st, owner, "Sprint planning", "We agreed to ship on Friday.")
	b := createCrossReadyNote(t, st, owner, "Retro", "The team missed last sprint's deadline.")

	// Real cross template created through the real HTTP API (auto_run
	// explicitly false -- true is the endpoint's default and would be
	// correctly rejected, exercised separately below).
	tmplRec := doGuardJSON(t, srv, http.MethodPost, "/api/templates", map[string]any{
		"name":     "Cross recap e2e",
		"phase":    "cross",
		"sections": []map[string]string{{"heading": "Decisions", "instruction": "List decisions."}},
		"auto_run": false,
	}, hdr)
	if tmplRec.Code != http.StatusCreated {
		t.Fatalf("create template=%d body=%s", tmplRec.Code, tmplRec.Body)
	}
	var tmpl struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(tmplRec.Body.Bytes(), &tmpl)

	// Negative case: a cross+auto_run template is rejected by the real HTTP
	// create endpoint too (not only the store layer).
	rejectRec := doGuardJSON(t, srv, http.MethodPost, "/api/templates", map[string]any{
		"name":     "Cross auto-run e2e",
		"phase":    "cross",
		"sections": []map[string]string{{"heading": "H", "instruction": "I"}},
		"auto_run": true,
	}, hdr)
	if rejectRec.Code != http.StatusBadRequest {
		t.Fatalf("cross+auto_run create=%d, want 400; body=%s", rejectRec.Code, rejectRec.Body)
	}

	// Run the cross-meeting analysis via create-and-send.
	runRec := doGuardJSON(t, srv, http.MethodPost, "/api/conversations", map[string]any{
		"title":   "",
		"content": "Compare decisions",
		"cross_analysis": map[string]any{
			"template_id": tmpl.ID,
			"note_ids":    []string{a.ID, b.ID},
		},
	}, hdr)
	if runRec.Code != http.StatusCreated {
		t.Fatalf("run cross-analysis=%d body=%s", runRec.Code, runRec.Body)
	}
	var created struct {
		ID      string        `json:"id"`
		Message model.Message `json:"message"`
	}
	if err := json.Unmarshal(runRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Distinct documents: the captured plugin request carries BOTH notes as
	// separate documents, never a concatenated single transcript.
	if len(gen.lastReq.Documents) != 2 {
		t.Fatalf("expected 2 distinct documents in the plugin request, got %d: %+v", len(gen.lastReq.Documents), gen.lastReq.Documents)
	}
	gotNoteIDs := map[string]bool{gen.lastReq.Documents[0].NoteID: true, gen.lastReq.Documents[1].NoteID: true}
	if !gotNoteIDs[a.ID] || !gotNoteIDs[b.ID] {
		t.Fatalf("expected documents for both notes, got %+v", gen.lastReq.Documents)
	}
	if gen.lastReq.Transcript != nil {
		t.Fatalf("expected the legacy transcript field to stay nil for a documents-based call, got %+v", gen.lastReq.Transcript)
	}

	if len(created.Message.Sources) == 0 {
		t.Fatal("expected sources on the created assistant message")
	}

	// Reload via the real GET .../messages route ("reload Chat").
	getRec := doGuardJSON(t, srv, http.MethodGet, "/api/conversations/"+created.ID+"/messages", nil, hdr)
	if getRec.Code != http.StatusOK {
		t.Fatalf("list messages=%d body=%s", getRec.Code, getRec.Body)
	}
	var reloaded []model.Message
	if err := json.Unmarshal(getRec.Body.Bytes(), &reloaded); err != nil {
		t.Fatalf("unmarshal reloaded messages: %v", err)
	}
	if len(reloaded) != 2 {
		t.Fatalf("expected 2 reloaded messages, got %d", len(reloaded))
	}

	// Follow citations to BOTH notes: every source's note_id resolves to
	// one of the two selected notes.
	var assistant model.Message
	for _, m := range reloaded {
		if m.Role == "assistant" {
			assistant = m
		}
	}
	if len(assistant.Sources) == 0 {
		t.Fatal("expected sources on the reloaded assistant message")
	}
	seenNoteIDs := map[string]bool{}
	for _, src := range assistant.Sources {
		if src.NoteID == nil {
			t.Fatalf("expected every citation to have a live note_id, got nil: %+v", src)
		}
		seenNoteIDs[*src.NoteID] = true
	}
	if !seenNoteIDs[a.ID] || !seenNoteIDs[b.ID] {
		t.Fatalf("expected citations navigable to both notes, got note ids %v", seenNoteIDs)
	}
}

// TestCrossAnalysisEndToEndGenerationChangeBeforeInvocation404sTo412 is the
// negative case the accepted plan's Task 14 calls out explicitly: change a
// generation right before invocation and observe the guard fail (red),
// exercised end to end through the real HTTP routes for both preflight and
// invocation (via the two in-package calls preflightCrossDocuments /
// invokeCrossAnalysis, since HTTP alone cannot isolate the window -- see
// TestCrossAnalysisGenerationMismatch412's doc comment for why).
func TestCrossAnalysisEndToEndGenerationChangeBeforeInvocation412(t *testing.T) {
	t.Parallel()
	gen := fakeCrossAgent(t)
	srv, st := newChatTestServer(t, gen)
	cr, _ := crypto.New("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	createDefaultAgentPlugin(t, st, cr, crossAdmissionConfig(nil))
	owner, _ := newCrossTestOwner(t, srv, st, "cross-e2e-generation@example.com")
	ctx := context.Background()

	a := createCrossReadyNote(t, st, owner, "A", "a text")
	b := createCrossReadyNote(t, st, owner, "B", "b text")
	tmpl, err := st.CreateTemplate(ctx, owner, "Cross e2e gen", "cross",
		[]model.TemplateSection{{Heading: "H", Instruction: "I"}}, false, "", "", nil)
	if err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}
	conv, err := st.CreateConversation(ctx, owner, nil, "", nil)
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}

	req := crossAnalysisRequest{TemplateID: tmpl.ID, NoteIDs: []string{a.ID, b.ID}}
	pre, err := srv.preflightCrossDocuments(ctx, owner, conv, req, "focus")
	if err != nil {
		t.Fatalf("preflightCrossDocuments: %v", err)
	}

	// Break the guard: replace B's transcript right before invocation.
	if _, err := st.SaveTranscript(ctx, model.Transcript{
		NoteID: b.ID, TranscriberPlugin: "test", Model: "m",
		Segments: []model.Segment{{StartMS: 0, EndMS: 1000, Text: "b text v2", Source: "mic"}},
	}, 1); err != nil {
		t.Fatalf("re-save transcript: %v", err)
	}

	// Observe it failing (red).
	if _, err := srv.invokeCrossAnalysis(ctx, owner, conv, pre, "focus"); err == nil {
		t.Fatal("expected the generation guard to fail after a mid-flight transcript change")
	}
	if gen.lastReq.Documents != nil {
		t.Fatal("expected zero generator calls when the guard fails")
	}

	// Restore: a FRESH preflight against the new generation succeeds.
	prefreshed, err := srv.preflightCrossDocuments(ctx, owner, conv, req, "focus")
	if err != nil {
		t.Fatalf("preflightCrossDocuments after refresh: %v", err)
	}
	if _, err := srv.invokeCrossAnalysis(ctx, owner, conv, prefreshed, "focus"); err != nil {
		t.Fatalf("invokeCrossAnalysis after refresh: %v", err)
	}
	if gen.lastReq.Documents == nil {
		t.Fatal("expected the generator to be called once the guard was satisfied")
	}
}
