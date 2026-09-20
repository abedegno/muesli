package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/abedegno/muesli/internal/crypto"
	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/plugin"
	"github.com/abedegno/muesli/internal/store"
)

// crossAdmissionConfig returns a valid agent plugin config JSON object
// (issue #765's admission fields plus the ordinary model field), letting
// callers override individual keys.
func crossAdmissionConfig(overrides map[string]any) map[string]any {
	cfg := map[string]any{
		"model":                   "default-model",
		"context_tokens":          8192,
		"output_reserve_tokens":   4096,
		"provider_framing_tokens": 0,
		"byte_fallback_tokenizer": true,
	}
	for k, v := range overrides {
		cfg[k] = v
	}
	return cfg
}

// createCrossReadyNote creates a note with a transcript and marks it ready
// (model.NoteReady, not partial) -- the minimum eligibility bar for cross-
// meeting analysis.
func createCrossReadyNote(t *testing.T, st *store.Store, ownerID, title, text string) model.Note {
	t.Helper()
	ctx := context.Background()
	n, err := st.CreateNote(ctx, ownerID, title)
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}
	if _, err := st.SaveTranscript(ctx, model.Transcript{
		NoteID: n.ID, TranscriberPlugin: "test", Model: "m",
		Segments: []model.Segment{{StartMS: 0, EndMS: 1000, Text: text, Source: "mic"}},
	}, 0); err != nil {
		t.Fatalf("SaveTranscript: %v", err)
	}
	if err := st.SetNoteStatus(ctx, n.ID, model.NoteReady); err != nil {
		t.Fatalf("SetNoteStatus: %v", err)
	}
	return n
}

// crossOwnerID resolves the owner id behind hdr's bearer token by hitting a
// cheap authenticated route and reading back the created user id indirectly
// via the store: simplest is to create notes directly against ownerID
// returned from setupGuardTestUser's underlying CreateUser call, so tests
// call st.CreateUser themselves instead of going through /api/setup.
func newCrossTestOwner(t *testing.T, srv *Server, st *store.Store, email string) (ownerID string, hdr map[string]string) {
	t.Helper()
	hdr = setupGuardTestUser(t, srv, email)
	u, err := st.GetUserByEmail(context.Background(), email)
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}
	return u.ID, hdr
}

func fakeCrossAgent(t *testing.T) *fakeChatGenerator {
	t.Helper()
	return &fakeChatGenerator{resp: plugin.GenerateResponse{
		Summary: plugin.SummaryPayload{Sections: []model.SummarySection{
			{Heading: "Decisions", ContentMarkdown: "Ship Friday [1]."},
			{Heading: "Risks", ContentMarkdown: "Deadline slipped [2]."},
		}},
		Model: "cross-agent-model",
		Usage: &plugin.GenerateUsage{TokensUsed: 10},
	}}
}

// TestCrossAnalysisSendToExistingHappyPath exercises the send-to-existing
// route end to end: two eligible notes, a cross template, success response
// shape, source metadata, and title generation on the first exchange.
func TestCrossAnalysisSendToExistingHappyPath(t *testing.T) {
	t.Parallel()
	titleGen := &fakeChatGenerator{resp: plugin.GenerateResponse{
		Summary: plugin.SummaryPayload{Sections: []model.SummarySection{{Heading: "Title", ContentMarkdown: "Cross meeting recap"}}},
		Model:   "cross-agent-model",
	}}
	gen := fakeCrossAgent(t)
	multi := &multiGenerator{byCallCount: []ChatGenerator{gen, titleGen}}
	srv, st := newChatTestServer(t, multi)
	cr, _ := crypto.New("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	createDefaultAgentPlugin(t, st, cr, crossAdmissionConfig(nil))
	owner, hdr := newCrossTestOwner(t, srv, st, "cross-happy@example.com")

	a := createCrossReadyNote(t, st, owner, "Sprint planning", "We ship Friday.")
	b := createCrossReadyNote(t, st, owner, "Retro", "We missed the deadline.")
	tmpl, err := st.CreateTemplate(context.Background(), owner, "Cross recap", "cross",
		[]model.TemplateSection{{Heading: "Decisions", Instruction: "List decisions."}, {Heading: "Risks", Instruction: "List risks."}},
		false, "", "", nil)
	if err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}

	convRec := doGuardJSON(t, srv, http.MethodPost, "/api/conversations", map[string]any{"title": "", "content": ""}, hdr)
	if convRec.Code != http.StatusCreated {
		t.Fatalf("create conversation=%d body=%s", convRec.Code, convRec.Body)
	}
	var conv struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(convRec.Body.Bytes(), &conv)

	rec := doGuardJSON(t, srv, http.MethodPost, "/api/conversations/"+conv.ID+"/messages", map[string]any{
		"content": "Compare decisions and risks",
		"cross_analysis": map[string]any{
			"template_id": tmpl.ID,
			"note_ids":    []string{a.ID, b.ID},
		},
	}, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("send cross-analysis=%d body=%s", rec.Code, rec.Body)
	}
	var resp struct {
		Message model.Message `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Message.Role != "assistant" {
		t.Fatalf("expected assistant message, got %+v", resp.Message)
	}
	if resp.Message.Content == "" || resp.Message.Content[:2] != "##" {
		t.Fatalf("expected markdown section headings, got %q", resp.Message.Content)
	}
	if len(resp.Message.Sources) == 0 {
		t.Fatalf("expected sources on the response message, got none")
	}

	msgs, err := st.ListMessages(context.Background(), owner, conv.ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 persisted messages, got %d", len(msgs))
	}
	if msgs[0].Content == "" {
		t.Fatal("expected a non-empty persisted user message")
	}

	afterConv, err := st.GetConversation(context.Background(), owner, conv.ID)
	if err != nil {
		t.Fatalf("GetConversation: %v", err)
	}
	if afterConv.Title == "" {
		t.Fatal("expected a title generated after the first exchange")
	}
}

// multiGenerator returns each entry in byCallCount, in order, one per call
// -- used to script a distinct response for the main run vs the subsequent
// title-generation call, mirroring the real handler's two sequential plugin
// calls.
type multiGenerator struct {
	byCallCount []ChatGenerator
	n           int
}

func (m *multiGenerator) Generate(ctx context.Context, req plugin.GenerateRequest) (plugin.GenerateResponse, error) {
	g := m.byCallCount[m.n]
	if m.n < len(m.byCallCount)-1 {
		m.n++
	}
	return g.Generate(ctx, req)
}

// TestCrossAnalysisValidationErrors table-tests the 400/404/409/413/422
// statuses for send-to-existing.
func TestCrossAnalysisValidationErrors(t *testing.T) {
	t.Parallel()
	gen := fakeCrossAgent(t)
	srv, st := newChatTestServer(t, gen)
	cr, _ := crypto.New("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	createDefaultAgentPlugin(t, st, cr, crossAdmissionConfig(nil))
	owner, hdr := newCrossTestOwner(t, srv, st, "cross-validation@example.com")

	a := createCrossReadyNote(t, st, owner, "A", "a text")
	b := createCrossReadyNote(t, st, owner, "B", "b text")
	other := addUserAPI(t, st, "cross-other-owner@example.com")
	theirs := createCrossReadyNote(t, st, other, "Theirs", "theirs text")

	notReady, err := st.CreateNote(context.Background(), owner, "Not ready")
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}

	crossTmpl, err := st.CreateTemplate(context.Background(), owner, "Cross A", "cross",
		[]model.TemplateSection{{Heading: "H", Instruction: "I"}}, false, "", "", nil)
	if err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}
	afterTmpl, err := st.CreateTemplate(context.Background(), owner, "After A", "after",
		[]model.TemplateSection{{Heading: "H", Instruction: "I"}}, false, "", "", nil)
	if err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}

	// Note-scoped conversation: cross-analysis requires global.
	scopedConvRec := doGuardJSON(t, srv, http.MethodPost, "/api/conversations", map[string]any{"title": "", "note_id": a.ID}, hdr)
	var scopedConv struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(scopedConvRec.Body.Bytes(), &scopedConv)

	globalConvRec := doGuardJSON(t, srv, http.MethodPost, "/api/conversations", map[string]any{"title": ""}, hdr)
	var globalConv struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(globalConvRec.Body.Bytes(), &globalConv)

	send := func(convID string, body map[string]any) *httptest.ResponseRecorder {
		return doGuardJSON(t, srv, http.MethodPost, "/api/conversations/"+convID+"/messages", body, hdr)
	}

	cases := []struct {
		name       string
		convID     string
		body       map[string]any
		wantStatus int
	}{
		{
			name:   "note-scoped conversation",
			convID: scopedConv.ID,
			body: map[string]any{"cross_analysis": map[string]any{
				"template_id": crossTmpl.ID, "note_ids": []string{a.ID, b.ID},
			}},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:   "fewer than two notes",
			convID: globalConv.ID,
			body: map[string]any{"cross_analysis": map[string]any{
				"template_id": crossTmpl.ID, "note_ids": []string{a.ID},
			}},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:   "duplicate notes",
			convID: globalConv.ID,
			body: map[string]any{"cross_analysis": map[string]any{
				"template_id": crossTmpl.ID, "note_ids": []string{a.ID, a.ID},
			}},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:   "wrong template phase",
			convID: globalConv.ID,
			body: map[string]any{"cross_analysis": map[string]any{
				"template_id": afterTmpl.ID, "note_ids": []string{a.ID, b.ID},
			}},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:   "note not ready",
			convID: globalConv.ID,
			body: map[string]any{"cross_analysis": map[string]any{
				"template_id": crossTmpl.ID, "note_ids": []string{a.ID, notReady.ID},
			}},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:   "template does not exist",
			convID: globalConv.ID,
			body: map[string]any{"cross_analysis": map[string]any{
				"template_id": "00000000-0000-0000-0000-000000000000", "note_ids": []string{a.ID, b.ID},
			}},
			wantStatus: http.StatusNotFound,
		},
		{
			name:   "note belongs to another owner",
			convID: globalConv.ID,
			body: map[string]any{"cross_analysis": map[string]any{
				"template_id": crossTmpl.ID, "note_ids": []string{a.ID, theirs.ID},
			}},
			wantStatus: http.StatusNotFound,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			rec := send(tc.convID, tc.body)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tc.wantStatus, rec.Body)
			}
		})
	}
}

// addUserAPI creates an additional user via the store directly (package api
// tests don't share internal/store's test-only addUser helper).
func addUserAPI(t *testing.T, st *store.Store, email string) string {
	t.Helper()
	u, err := st.CreateUser(context.Background(), email, "h")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	return u.ID
}

// TestCrossAnalysisCreateAndSendPreflightFailureCreatesNothing proves a
// preflight (pre-creation) failure -- here, fewer than two notes -- leaves
// no conversation behind at all.
func TestCrossAnalysisCreateAndSendPreflightFailureCreatesNothing(t *testing.T) {
	t.Parallel()
	gen := fakeCrossAgent(t)
	srv, st := newChatTestServer(t, gen)
	cr, _ := crypto.New("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	createDefaultAgentPlugin(t, st, cr, crossAdmissionConfig(nil))
	owner, hdr := newCrossTestOwner(t, srv, st, "cross-preflight-nothing@example.com")

	a := createCrossReadyNote(t, st, owner, "A", "a text")
	tmpl, err := st.CreateTemplate(context.Background(), owner, "Cross", "cross",
		[]model.TemplateSection{{Heading: "H", Instruction: "I"}}, false, "", "", nil)
	if err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}

	before, err := st.ListConversations(context.Background(), owner, nil)
	if err != nil {
		t.Fatalf("ListConversations before: %v", err)
	}

	rec := doGuardJSON(t, srv, http.MethodPost, "/api/conversations", map[string]any{
		"title": "",
		"cross_analysis": map[string]any{
			"template_id": tmpl.ID, "note_ids": []string{a.ID}, // only one note
		},
	}, hdr)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body)
	}

	after, err := st.ListConversations(context.Background(), owner, nil)
	if err != nil {
		t.Fatalf("ListConversations after: %v", err)
	}
	if len(after) != len(before) {
		t.Fatalf("expected no conversation created on preflight failure: before=%d after=%d", len(before), len(after))
	}
}

// TestCrossAnalysisCreateAndSendSuccess proves the happy create-and-send
// path: empty focus succeeds, the conversation is created once, and the
// response carries the assistant message.
func TestCrossAnalysisCreateAndSendSuccess(t *testing.T) {
	t.Parallel()
	gen := fakeCrossAgent(t)
	srv, st := newChatTestServer(t, gen)
	cr, _ := crypto.New("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	createDefaultAgentPlugin(t, st, cr, crossAdmissionConfig(nil))
	owner, hdr := newCrossTestOwner(t, srv, st, "cross-create-success@example.com")

	a := createCrossReadyNote(t, st, owner, "A", "a text")
	b := createCrossReadyNote(t, st, owner, "B", "b text")
	tmpl, err := st.CreateTemplate(context.Background(), owner, "Cross", "cross",
		[]model.TemplateSection{{Heading: "Decisions", Instruction: "I"}, {Heading: "Risks", Instruction: "I"}}, false, "", "", nil)
	if err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}

	rec := doGuardJSON(t, srv, http.MethodPost, "/api/conversations", map[string]any{
		"title":   "",
		"content": "", // empty focus must succeed
		"cross_analysis": map[string]any{
			"template_id": tmpl.ID, "note_ids": []string{a.ID, b.ID},
		},
	}, hdr)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body)
	}
	var resp struct {
		ID      string        `json:"id"`
		Message model.Message `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.ID == "" || resp.Message.ID == "" {
		t.Fatalf("expected a created conversation and message, got %+v", resp)
	}

	msgs, err := st.ListMessages(context.Background(), owner, resp.ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 persisted messages, got %d", len(msgs))
	}
}

// TestCrossAnalysisMissingAgentPostCreation422 proves ruling 2: with no
// default agent configured, create-and-send still creates exactly one
// conversation, which stays empty and titleless, and returns 422.
func TestCrossAnalysisMissingAgentPostCreation422(t *testing.T) {
	t.Parallel()
	gen := fakeCrossAgent(t)
	srv, st := newChatTestServer(t, gen) // no default agent plugin registered
	owner, hdr := newCrossTestOwner(t, srv, st, "cross-missing-agent@example.com")

	a := createCrossReadyNote(t, st, owner, "A", "a text")
	b := createCrossReadyNote(t, st, owner, "B", "b text")
	tmpl, err := st.CreateTemplate(context.Background(), owner, "Cross", "cross",
		[]model.TemplateSection{{Heading: "H", Instruction: "I"}}, false, "", "", nil)
	if err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}

	rec := doGuardJSON(t, srv, http.MethodPost, "/api/conversations", map[string]any{
		"title": "",
		"cross_analysis": map[string]any{
			"template_id": tmpl.ID, "note_ids": []string{a.ID, b.ID},
		},
	}, hdr)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body)
	}

	convs, err := st.ListConversations(context.Background(), owner, nil)
	if err != nil {
		t.Fatalf("ListConversations: %v", err)
	}
	if len(convs) != 1 {
		t.Fatalf("expected exactly one created conversation, got %d", len(convs))
	}
	if convs[0].Title != "" {
		t.Fatalf("expected the conversation to stay titleless, got %q", convs[0].Title)
	}
	msgs, err := st.ListMessages(context.Background(), owner, convs[0].ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected the conversation to stay empty, got %d messages", len(msgs))
	}
}

// TestCrossAnalysisGenerationMismatch412 calls preflightCrossDocuments and
// invokeCrossAnalysis directly (both unexported, reachable from this
// in-package test file) so a transcript replacement can be injected exactly
// between them -- the window HTTP alone cannot isolate synchronously.
// Proves 412, zero generator calls, and nothing persisted.
func TestCrossAnalysisGenerationMismatch412(t *testing.T) {
	t.Parallel()
	gen := fakeCrossAgent(t)
	srv, st := newChatTestServer(t, gen)
	cr, _ := crypto.New("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	createDefaultAgentPlugin(t, st, cr, crossAdmissionConfig(nil))
	owner, _ := newCrossTestOwner(t, srv, st, "cross-generation-mismatch@example.com")
	ctx := context.Background()

	a := createCrossReadyNote(t, st, owner, "A", "a text")
	b := createCrossReadyNote(t, st, owner, "B", "b text")
	tmpl, err := st.CreateTemplate(ctx, owner, "Cross", "cross",
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

	// Replace note A's transcript BETWEEN preflight and invocation, bumping
	// its generation from 1 to 2.
	if _, err := st.SaveTranscript(ctx, model.Transcript{
		NoteID: a.ID, TranscriberPlugin: "test", Model: "m",
		Segments: []model.Segment{{StartMS: 0, EndMS: 1000, Text: "a text v2", Source: "mic"}},
	}, 1); err != nil {
		t.Fatalf("re-save transcript: %v", err)
	}

	_, err = srv.invokeCrossAnalysis(ctx, owner, conv, pre, "focus")
	if err != store.ErrGenerationMismatch {
		t.Fatalf("invokeCrossAnalysis: got %v, want store.ErrGenerationMismatch", err)
	}
	if gen.lastReq.Documents != nil {
		t.Fatalf("expected zero generator calls on a generation mismatch, but a request was captured: %+v", gen.lastReq)
	}

	msgs, err := st.ListMessages(ctx, owner, conv.ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected no messages persisted after a 412, got %d", len(msgs))
	}
}

// TestCrossAnalysisBudget413 configures a byte ceiling far too small for the
// two selected notes' rendered corpus and proves 413, with zero generator
// calls and nothing created (via create-and-send).
func TestCrossAnalysisBudget413(t *testing.T) {
	t.Parallel()
	gen := fakeCrossAgent(t)
	srv, st := newChatTestServer(t, gen)
	cr, _ := crypto.New("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	createDefaultAgentPlugin(t, st, cr, crossAdmissionConfig(map[string]any{
		"context_tokens":        10,
		"output_reserve_tokens": 0,
	}))
	owner, hdr := newCrossTestOwner(t, srv, st, "cross-budget@example.com")

	a := createCrossReadyNote(t, st, owner, "A", "a long enough text to exceed a ten byte budget")
	b := createCrossReadyNote(t, st, owner, "B", "b long enough text to exceed a ten byte budget")
	tmpl, err := st.CreateTemplate(context.Background(), owner, "Cross", "cross",
		[]model.TemplateSection{{Heading: "H", Instruction: "I"}}, false, "", "", nil)
	if err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}

	before, err := st.ListConversations(context.Background(), owner, nil)
	if err != nil {
		t.Fatalf("ListConversations before: %v", err)
	}
	rec := doGuardJSON(t, srv, http.MethodPost, "/api/conversations", map[string]any{
		"title": "",
		"cross_analysis": map[string]any{
			"template_id": tmpl.ID, "note_ids": []string{a.ID, b.ID},
		},
	}, hdr)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413; body=%s", rec.Code, rec.Body)
	}
	if gen.lastReq.Documents != nil {
		t.Fatalf("expected zero generator calls on a budget rejection, got a captured request")
	}
	after, err := st.ListConversations(context.Background(), owner, nil)
	if err != nil {
		t.Fatalf("ListConversations after: %v", err)
	}
	if len(after) != len(before) {
		t.Fatalf("expected no conversation created on a 413 preflight failure: before=%d after=%d", len(before), len(after))
	}
}

// TestCrossAnalysisConfiguredCeilingBelowDefault413 proves a configured
// ceiling below the shared default (40) is honored, rejecting a note count
// the default would have allowed.
func TestCrossAnalysisConfiguredCeilingBelowDefault413(t *testing.T) {
	t.Parallel()
	gen := fakeCrossAgent(t)
	srv, st := newChatTestServer(t, gen)
	srv.deps.Config.CrossAnalysisMaxNotes = 2
	cr, _ := crypto.New("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	createDefaultAgentPlugin(t, st, cr, crossAdmissionConfig(nil))
	owner, hdr := newCrossTestOwner(t, srv, st, "cross-configured-ceiling@example.com")

	a := createCrossReadyNote(t, st, owner, "A", "a")
	b := createCrossReadyNote(t, st, owner, "B", "b")
	c := createCrossReadyNote(t, st, owner, "C", "c")
	tmpl, err := st.CreateTemplate(context.Background(), owner, "Cross", "cross",
		[]model.TemplateSection{{Heading: "H", Instruction: "I"}}, false, "", "", nil)
	if err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}

	rec := doGuardJSON(t, srv, http.MethodPost, "/api/conversations", map[string]any{
		"title": "",
		"cross_analysis": map[string]any{
			"template_id": tmpl.ID, "note_ids": []string{a.ID, b.ID, c.ID},
		},
	}, hdr)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413; body=%s", rec.Code, rec.Body)
	}
}

// TestCrossAnalysisPluginChangedAfterPreflight500 proves ruling 4: if the
// default agent plugin changes between preflight and invocation, invoke
// fails closed with a generic error (mapped to 500) rather than running
// against an unvalidated admission decision.
func TestCrossAnalysisPluginChangedAfterPreflight500(t *testing.T) {
	t.Parallel()
	gen := fakeCrossAgent(t)
	srv, st := newChatTestServer(t, gen)
	cr, _ := crypto.New("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	createDefaultAgentPlugin(t, st, cr, crossAdmissionConfig(nil))
	owner, _ := newCrossTestOwner(t, srv, st, "cross-plugin-changed@example.com")
	ctx := context.Background()

	a := createCrossReadyNote(t, st, owner, "A", "a text")
	b := createCrossReadyNote(t, st, owner, "B", "b text")
	tmpl, err := st.CreateTemplate(ctx, owner, "Cross", "cross",
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

	// A newly-registered, now-default plugin changes plug.ID between
	// preflight and invocation.
	createDefaultAgentPlugin(t, st, cr, crossAdmissionConfig(map[string]any{"model": "a-different-model"}))

	if _, err := srv.invokeCrossAnalysis(ctx, owner, conv, pre, "focus"); err != errCrossAnalysisPluginChanged {
		t.Fatalf("invokeCrossAnalysis: got %v, want errCrossAnalysisPluginChanged", err)
	}
	if gen.lastReq.Documents != nil {
		t.Fatal("expected zero generator calls when the plugin changed")
	}
}

// TestCrossAnalysisSendInFlightConflict409 proves the existing-conversation
// send guard rejects a concurrent cross-analysis send exactly like ordinary
// chat.
func TestCrossAnalysisSendInFlightConflict409(t *testing.T) {
	t.Parallel()
	gen := fakeCrossAgent(t)
	srv, st := newChatTestServer(t, gen)
	cr, _ := crypto.New("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	createDefaultAgentPlugin(t, st, cr, crossAdmissionConfig(nil))
	owner, hdr := newCrossTestOwner(t, srv, st, "cross-inflight@example.com")

	a := createCrossReadyNote(t, st, owner, "A", "a text")
	b := createCrossReadyNote(t, st, owner, "B", "b text")
	tmpl, err := st.CreateTemplate(context.Background(), owner, "Cross", "cross",
		[]model.TemplateSection{{Heading: "H", Instruction: "I"}}, false, "", "", nil)
	if err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}
	conv, err := st.CreateConversation(context.Background(), owner, nil, "", nil)
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}

	if !srv.chatSendGuard.tryAcquire(conv.ID) {
		t.Fatal("expected to acquire the guard directly")
	}
	defer srv.chatSendGuard.release(conv.ID)

	rec := doGuardJSON(t, srv, http.MethodPost, "/api/conversations/"+conv.ID+"/messages", map[string]any{
		"content": "focus",
		"cross_analysis": map[string]any{
			"template_id": tmpl.ID, "note_ids": []string{a.ID, b.ID},
		},
	}, hdr)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", rec.Code, rec.Body)
	}
}
