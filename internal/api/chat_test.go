package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/abedegno/muesli/internal/crypto"
	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/plugin"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
)

// ---------------------------------------------------------------------------
// CHT04: chat send endpoint (send message, model resolution, in-flight
// guard). These tests live in package api (not api_test) so they can reach
// the unexported chatSendGuard field directly for the guard test, mirroring
// notes_summarize_guard_test.go's convention. newGuardTestServer/doGuardJSON/
// setupGuardTestUser are defined there and reused here.
//
// The agent plugin call itself is always mocked via Deps.ChatGenerator so
// none of these tests make a real HTTP call to a plugin; only the store (a
// real, migrated test-schema Postgres via testutil.NewPool, which SKIPS
// gracefully without TEST_DATABASE_URL) is real.
// ---------------------------------------------------------------------------

// fakeChatGenerator is a test double for ChatGenerator that returns a fixed
// response and records the last request it received, so tests can assert on
// what Config (and therefore model precedence) was sent.
type fakeChatGenerator struct {
	resp    plugin.GenerateResponse
	err     error
	lastReq plugin.GenerateRequest
}

func (f *fakeChatGenerator) Generate(_ context.Context, req plugin.GenerateRequest) (plugin.GenerateResponse, error) {
	f.lastReq = req
	return f.resp, f.err
}

// newChatTestServer builds a Server wired with a real (test-schema) store
// and crypto, plus the given fake generator as Deps.ChatGenerator, so
// DefaultPlugin lookups (needed to resolve plug.Config) are real while the
// actual /generate call is mocked.
func newChatTestServer(t *testing.T, gen ChatGenerator) (*Server, *store.Store) {
	t.Helper()
	st := store.New(testutil.NewPool(t))
	cr, err := crypto.New("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	if err != nil {
		t.Fatal(err)
	}
	return NewServer(Deps{Store: st, Crypto: cr, ChatGenerator: gen}), st
}

// createDefaultAgentPlugin registers and marks default an "agent" plugin
// with the given config (as a JSON object), via the same store methods the
// real admin endpoints use.
func createDefaultAgentPlugin(t *testing.T, st *store.Store, cr *crypto.Crypto, config map[string]any) model.Plugin {
	t.Helper()
	cfgBytes, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	p, err := st.CreatePlugin(ctx, cr, model.Plugin{
		Kind:        model.PluginAgent,
		Name:        "test-agent",
		EndpointURL: "http://agent.invalid",
		Token:       "test-token",
		Config:      cfgBytes,
		Enabled:     true,
	})
	if err != nil {
		t.Fatalf("CreatePlugin: %v", err)
	}
	if err := st.SetDefaultPlugin(ctx, p.ID); err != nil {
		t.Fatalf("SetDefaultPlugin: %v", err)
	}
	return p
}

// createNoteWithTranscript creates a note owned by uid with a single
// transcript segment containing text, returning the note id.
func createNoteWithTranscript(t *testing.T, srv *Server, st *store.Store, hdr map[string]string, title, text string) string {
	t.Helper()
	rec := doGuardJSON(t, srv, http.MethodPost, "/api/notes", map[string]string{"title": title}, hdr)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create note=%d body=%s", rec.Code, rec.Body)
	}
	var note struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &note)
	if note.ID == "" {
		t.Fatalf("no note id; body %s", rec.Body)
	}
	ctx := context.Background()
	if _, err := st.SaveTranscript(ctx, model.Transcript{
		NoteID:            note.ID,
		TranscriberPlugin: "test",
		Model:             "m",
		Segments:          []model.Segment{{StartMS: 1000, EndMS: 2000, Text: text, Source: "mic"}},
	}); err != nil {
		t.Fatalf("SaveTranscript: %v", err)
	}
	return note.ID
}

// TestChatSendHappyPath sends a message in a note-scoped conversation, with
// the plugin mocked to reply with a [1] citation, and asserts the persisted
// assistant message and returned sources.
func TestChatSendHappyPath(t *testing.T) {
	t.Parallel()
	gen := &fakeChatGenerator{resp: plugin.GenerateResponse{
		Summary: plugin.SummaryPayload{Sections: []model.SummarySection{{
			Heading:         "Answer",
			ContentMarkdown: "The meeting starts at 9am [1].",
		}}},
		Model: "agent-model-v1",
		Usage: &plugin.GenerateUsage{TokensUsed: 42, MaxTokens: 8192},
	}}
	srv, st := newChatTestServer(t, gen)
	cr, _ := crypto.New("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	createDefaultAgentPlugin(t, st, cr, map[string]any{"model": "default-model"})
	hdr := setupGuardTestUser(t, srv, "chat-happy@example.com")

	noteID := createNoteWithTranscript(t, srv, st, hdr, "Standup", "The meeting starts at 9am sharp.")

	convRec := doGuardJSON(t, srv, http.MethodPost, "/api/conversations",
		map[string]any{"title": "About standup", "note_id": noteID}, hdr)
	if convRec.Code != http.StatusCreated {
		t.Fatalf("create conversation=%d body=%s", convRec.Code, convRec.Body)
	}
	var conv struct{ ID string }
	_ = json.Unmarshal(convRec.Body.Bytes(), &conv)

	rec := doGuardJSON(t, srv, http.MethodPost, "/api/conversations/"+conv.ID+"/messages",
		map[string]any{"content": "When does the meeting start?"}, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("send message=%d body=%s", rec.Code, rec.Body)
	}

	var body struct {
		Message model.Message `json:"message"`
		Sources []struct {
			N            int    `json:"n"`
			NoteID       string `json:"note_id"`
			SegmentIndex int    `json:"segment_index"`
			Snippet      string `json:"snippet"`
		} `json:"sources"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, rec.Body)
	}
	if body.Message.Role != "assistant" {
		t.Fatalf("role=%q, want assistant", body.Message.Role)
	}
	if body.Message.Content != "The meeting starts at 9am [1]." {
		t.Fatalf("content=%q", body.Message.Content)
	}
	if body.Message.Model != "agent-model-v1" {
		t.Fatalf("model=%q, want agent-model-v1", body.Message.Model)
	}
	if body.Message.TokensUsed == nil || *body.Message.TokensUsed != 42 {
		t.Fatalf("tokens_used=%v, want 42", body.Message.TokensUsed)
	}
	if len(body.Sources) != 1 {
		t.Fatalf("sources len=%d, want 1; body=%s", len(body.Sources), rec.Body)
	}
	if body.Sources[0].N != 1 || body.Sources[0].NoteID != noteID {
		t.Fatalf("source[0]=%+v, want n=1 note_id=%s", body.Sources[0], noteID)
	}

	// Persisted: two messages (user then assistant), in chronological order.
	msgs, err := st.ListMessages(context.Background(), mustUserID(t, srv, hdr), conv.ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("persisted messages len=%d, want 2", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[0].Content != "When does the meeting start?" {
		t.Fatalf("first message=%+v", msgs[0])
	}
	if msgs[1].Role != "assistant" || msgs[1].Model != "agent-model-v1" {
		t.Fatalf("second message=%+v", msgs[1])
	}
}

// mustUserID extracts the owner id from a bearer token header by round-tripping
// through GetConversation-adjacent store lookups isn't available directly, so
// instead we look the user up via the token itself.
func mustUserID(t *testing.T, srv *Server, hdr map[string]string) string {
	t.Helper()
	// The auth header is "Bearer <raw token>"; validate via a lightweight
	// authenticated call and read back the owner id from a conversation list
	// (owner-scoped) is unnecessary here -- ListConversations doesn't expose
	// it either. Simplest: create a throwaway conversation and read its
	// OwnerID field directly from the store's response.
	rec := doGuardJSON(t, srv, http.MethodPost, "/api/conversations", map[string]any{"title": "whoami"}, hdr)
	var c model.Conversation
	_ = json.Unmarshal(rec.Body.Bytes(), &c)
	return c.OwnerID
}

// TestChatModelResolution covers the three-way precedence: request
// model_override > conversation model_override > plugin default, asserting
// on the "model" key inside the Config the fake generator actually received.
func TestChatModelResolution(t *testing.T) {
	t.Parallel()

	newConvo := func(t *testing.T, srv *Server, hdr map[string]string, modelOverride *string) string {
		t.Helper()
		body := map[string]any{"title": "General"}
		if modelOverride != nil {
			body["model_override"] = *modelOverride
		}
		rec := doGuardJSON(t, srv, http.MethodPost, "/api/conversations", body, hdr)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create conversation=%d body=%s", rec.Code, rec.Body)
		}
		var c struct{ ID string }
		_ = json.Unmarshal(rec.Body.Bytes(), &c)
		return c.ID
	}

	extractModel := func(t *testing.T, cfg json.RawMessage) string {
		t.Helper()
		var m map[string]any
		if err := json.Unmarshal(cfg, &m); err != nil {
			t.Fatalf("unmarshal config %s: %v", cfg, err)
		}
		v, _ := m["model"].(string)
		return v
	}

	t.Run("request_override_wins", func(t *testing.T) {
		t.Parallel()
		gen := &fakeChatGenerator{resp: fixedReply()}
		srv, st := newChatTestServer(t, gen)
		cr, _ := crypto.New("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
		createDefaultAgentPlugin(t, st, cr, map[string]any{"model": "default-model"})
		hdr := setupGuardTestUser(t, srv, "chat-model-req@example.com")

		convOverride := "conv-model"
		convID := newConvo(t, srv, hdr, &convOverride)

		reqOverride := "request-model"
		rec := doGuardJSON(t, srv, http.MethodPost, "/api/conversations/"+convID+"/messages",
			map[string]any{"content": "hi", "model_override": reqOverride}, hdr)
		if rec.Code != http.StatusOK {
			t.Fatalf("send=%d body=%s", rec.Code, rec.Body)
		}
		if got := extractModel(t, gen.lastReq.Config); got != "request-model" {
			t.Fatalf("resolved model=%q, want request-model", got)
		}
	})

	t.Run("conversation_override_wins_over_default", func(t *testing.T) {
		t.Parallel()
		gen := &fakeChatGenerator{resp: fixedReply()}
		srv, st := newChatTestServer(t, gen)
		cr, _ := crypto.New("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
		createDefaultAgentPlugin(t, st, cr, map[string]any{"model": "default-model"})
		hdr := setupGuardTestUser(t, srv, "chat-model-conv@example.com")

		convOverride := "conv-model"
		convID := newConvo(t, srv, hdr, &convOverride)

		rec := doGuardJSON(t, srv, http.MethodPost, "/api/conversations/"+convID+"/messages",
			map[string]any{"content": "hi"}, hdr)
		if rec.Code != http.StatusOK {
			t.Fatalf("send=%d body=%s", rec.Code, rec.Body)
		}
		if got := extractModel(t, gen.lastReq.Config); got != "conv-model" {
			t.Fatalf("resolved model=%q, want conv-model", got)
		}
	})

	t.Run("default_when_no_override", func(t *testing.T) {
		t.Parallel()
		gen := &fakeChatGenerator{resp: fixedReply()}
		srv, st := newChatTestServer(t, gen)
		cr, _ := crypto.New("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
		createDefaultAgentPlugin(t, st, cr, map[string]any{"model": "default-model"})
		hdr := setupGuardTestUser(t, srv, "chat-model-default@example.com")

		convID := newConvo(t, srv, hdr, nil)

		rec := doGuardJSON(t, srv, http.MethodPost, "/api/conversations/"+convID+"/messages",
			map[string]any{"content": "hi"}, hdr)
		if rec.Code != http.StatusOK {
			t.Fatalf("send=%d body=%s", rec.Code, rec.Body)
		}
		if got := extractModel(t, gen.lastReq.Config); got != "default-model" {
			t.Fatalf("resolved model=%q, want default-model", got)
		}
	})
}

func fixedReply() plugin.GenerateResponse {
	return plugin.GenerateResponse{
		Summary: plugin.SummaryPayload{Sections: []model.SummarySection{{
			Heading:         "Answer",
			ContentMarkdown: "ok",
		}}},
		Model: "agent-model-v1",
	}
}

// TestChatSendGuardRejectsConcurrentCall simulates a first request already
// holding the in-flight guard for a conversation id, and asserts a
// concurrent/subsequent send for the SAME conversation id is rejected with
// 409 and never touches the store (no message persisted).
func TestChatSendGuardRejectsConcurrentCall(t *testing.T) {
	t.Parallel()
	gen := &fakeChatGenerator{resp: fixedReply()}
	srv, st := newChatTestServer(t, gen)
	cr, _ := crypto.New("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	createDefaultAgentPlugin(t, st, cr, map[string]any{"model": "default-model"})
	hdr := setupGuardTestUser(t, srv, "chat-guard-concurrent@example.com")

	convRec := doGuardJSON(t, srv, http.MethodPost, "/api/conversations", map[string]any{"title": "General"}, hdr)
	var conv struct{ ID string }
	_ = json.Unmarshal(convRec.Body.Bytes(), &conv)

	if !srv.chatSendGuard.tryAcquire(conv.ID) {
		t.Fatalf("expected to acquire guard for a fresh conversation id")
	}
	defer srv.chatSendGuard.release(conv.ID)

	rec := doGuardJSON(t, srv, http.MethodPost, "/api/conversations/"+conv.ID+"/messages",
		map[string]any{"content": "hi"}, hdr)
	if rec.Code != http.StatusConflict {
		t.Fatalf("in-flight send status=%d, want 409; body=%s", rec.Code, rec.Body)
	}

	msgs, err := st.ListMessages(context.Background(), mustUserID(t, srv, hdr), conv.ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("store was touched by rejected request: messages=%v", msgs)
	}
}

// TestChatSendGuardReleasedAfterSuccess asserts the guard slot is freed once
// a send completes successfully, so an immediately following send for the
// same conversation id is not rejected by the guard.
func TestChatSendGuardReleasedAfterSuccess(t *testing.T) {
	t.Parallel()
	gen := &fakeChatGenerator{resp: fixedReply()}
	srv, st := newChatTestServer(t, gen)
	cr, _ := crypto.New("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	createDefaultAgentPlugin(t, st, cr, map[string]any{"model": "default-model"})
	hdr := setupGuardTestUser(t, srv, "chat-guard-success@example.com")

	convRec := doGuardJSON(t, srv, http.MethodPost, "/api/conversations", map[string]any{"title": "General"}, hdr)
	var conv struct{ ID string }
	_ = json.Unmarshal(convRec.Body.Bytes(), &conv)

	rec := doGuardJSON(t, srv, http.MethodPost, "/api/conversations/"+conv.ID+"/messages",
		map[string]any{"content": "first"}, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("first send status=%d, want 200; body=%s", rec.Code, rec.Body)
	}

	rec = doGuardJSON(t, srv, http.MethodPost, "/api/conversations/"+conv.ID+"/messages",
		map[string]any{"content": "second"}, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("second send status=%d, want 200 (guard should have released); body=%s", rec.Code, rec.Body)
	}
}

// TestChatSendGuardReleasedAfterFailure asserts the guard slot is freed even
// when the handler exits via a non-happy-path error (no default agent
// plugin configured -> 500), so an immediately following call reaches that
// same failure path again rather than the guard's 409.
func TestChatSendGuardReleasedAfterFailure(t *testing.T) {
	t.Parallel()
	// No default agent plugin registered at all -> every send 500s.
	srv, _ := newChatTestServer(t, &fakeChatGenerator{resp: fixedReply()})
	hdr := setupGuardTestUser(t, srv, "chat-guard-failure@example.com")

	convRec := doGuardJSON(t, srv, http.MethodPost, "/api/conversations", map[string]any{"title": "General"}, hdr)
	var conv struct{ ID string }
	_ = json.Unmarshal(convRec.Body.Bytes(), &conv)

	rec := doGuardJSON(t, srv, http.MethodPost, "/api/conversations/"+conv.ID+"/messages",
		map[string]any{"content": "first"}, hdr)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("first send status=%d, want 500 (no default plugin); body=%s", rec.Code, rec.Body)
	}

	rec = doGuardJSON(t, srv, http.MethodPost, "/api/conversations/"+conv.ID+"/messages",
		map[string]any{"content": "second"}, hdr)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("second send status=%d, want 500 again (guard should have released, not 409); body=%s", rec.Code, rec.Body)
	}
}

// TestChatCreateAndSend covers the create-and-send variant: POST
// /api/conversations with a "content" field creates the conversation and
// immediately sends that first message in one call.
func TestChatCreateAndSend(t *testing.T) {
	t.Parallel()
	gen := &fakeChatGenerator{resp: fixedReply()}
	srv, st := newChatTestServer(t, gen)
	cr, _ := crypto.New("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	createDefaultAgentPlugin(t, st, cr, map[string]any{"model": "default-model"})
	hdr := setupGuardTestUser(t, srv, "chat-create-and-send@example.com")

	rec := doGuardJSON(t, srv, http.MethodPost, "/api/conversations",
		map[string]any{"title": "General", "content": "hello there"}, hdr)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create-and-send status=%d, want 201; body=%s", rec.Code, rec.Body)
	}
	var body struct {
		ID      string        `json:"id"`
		Message model.Message `json:"message"`
		Sources []any         `json:"sources"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, rec.Body)
	}
	if body.ID == "" {
		t.Fatalf("expected conversation id in response; body=%s", rec.Body)
	}
	if body.Message.Role != "assistant" || body.Message.Content != "ok" {
		t.Fatalf("message=%+v, want assistant/ok", body.Message)
	}

	msgs, err := st.ListMessages(context.Background(), mustUserID(t, srv, hdr), body.ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("persisted messages len=%d, want 2 (user+assistant)", len(msgs))
	}
}

// smartChatGenerator is a test double that can distinguish between "Answer"
// and "Title" generation requests based on the Template.Sections[0].Heading,
// returning different responses for each.
type smartChatGenerator struct {
	answerResp plugin.GenerateResponse
	titleResp  plugin.GenerateResponse
	err        error
	callCount  int
}

func (s *smartChatGenerator) Generate(_ context.Context, req plugin.GenerateRequest) (plugin.GenerateResponse, error) {
	s.callCount++
	if s.err != nil {
		return plugin.GenerateResponse{}, s.err
	}
	if len(req.Template.Sections) > 0 && req.Template.Sections[0].Heading == "Title" {
		return s.titleResp, nil
	}
	return s.answerResp, nil
}

// TestChatAutoTitleFirstExchange verifies that a title is auto-generated and
// set after the first successful message exchange in an empty-title conversation.
func TestChatAutoTitleFirstExchange(t *testing.T) {
	t.Parallel()
	gen := &smartChatGenerator{
		answerResp: plugin.GenerateResponse{
			Summary: plugin.SummaryPayload{Sections: []model.SummarySection{{
				Heading:         "Answer",
				ContentMarkdown: "The answer is 42.",
			}}},
			Model: "test-model",
		},
		titleResp: plugin.GenerateResponse{
			Summary: plugin.SummaryPayload{Sections: []model.SummarySection{{
				Heading:         "Title",
				ContentMarkdown: "Question about meaning",
			}}},
			Model: "test-model",
		},
	}
	srv, st := newChatTestServer(t, gen)
	cr, _ := crypto.New("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	createDefaultAgentPlugin(t, st, cr, map[string]any{"model": "default-model"})
	hdr := setupGuardTestUser(t, srv, "auto-title@example.com")

	// Create an empty-title conversation.
	convRec := doGuardJSON(t, srv, http.MethodPost, "/api/conversations",
		map[string]any{"title": ""}, hdr)
	if convRec.Code != http.StatusCreated {
		t.Fatalf("create conversation=%d body=%s", convRec.Code, convRec.Body)
	}
	var conv struct{ ID string }
	_ = json.Unmarshal(convRec.Body.Bytes(), &conv)

	// Send the first message.
	rec := doGuardJSON(t, srv, http.MethodPost, "/api/conversations/"+conv.ID+"/messages",
		map[string]any{"content": "What is the meaning of life?"}, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("send message=%d body=%s", rec.Code, rec.Body)
	}

	// Verify the title was set.
	gotConv, err := st.GetConversation(context.Background(), mustUserID(t, srv, hdr), conv.ID)
	if err != nil {
		t.Fatalf("GetConversation: %v", err)
	}
	if gotConv.Title != "Question about meaning" {
		t.Fatalf("expected title=%q, got %q", "Question about meaning", gotConv.Title)
	}

	// Verify both Answer and Title calls were made.
	if gen.callCount != 2 {
		t.Fatalf("expected 2 plugin calls (answer + title), got %d", gen.callCount)
	}
}

// TestChatAutoTitleFailureTolerated verifies that when title generation fails
// (plugin error or no sections), the send/create-and-send request still
// succeeds (200/201, message present), and the conversation's title remains
// empty afterward.
func TestChatAutoTitleFailureTolerated(t *testing.T) {
	t.Parallel()
	gen := &smartChatGenerator{
		answerResp: plugin.GenerateResponse{
			Summary: plugin.SummaryPayload{Sections: []model.SummarySection{{
				Heading:         "Answer",
				ContentMarkdown: "The answer is 42.",
			}}},
			Model: "test-model",
		},
		titleResp: plugin.GenerateResponse{
			Summary: plugin.SummaryPayload{Sections: []model.SummarySection{}}, // No sections!
			Model:   "test-model",
		},
	}
	srv, st := newChatTestServer(t, gen)
	cr, _ := crypto.New("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	createDefaultAgentPlugin(t, st, cr, map[string]any{"model": "default-model"})
	hdr := setupGuardTestUser(t, srv, "title-fail@example.com")

	// Create an empty-title conversation.
	convRec := doGuardJSON(t, srv, http.MethodPost, "/api/conversations",
		map[string]any{"title": ""}, hdr)
	var conv struct{ ID string }
	_ = json.Unmarshal(convRec.Body.Bytes(), &conv)

	// Send the first message. Even though title generation will fail (no
	// sections), the send should succeed.
	rec := doGuardJSON(t, srv, http.MethodPost, "/api/conversations/"+conv.ID+"/messages",
		map[string]any{"content": "What is the meaning of life?"}, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("send message=%d body=%s (should succeed despite title failure)", rec.Code, rec.Body)
	}

	// Verify the message was persisted.
	var body struct {
		Message model.Message `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body.Message.Role != "assistant" {
		t.Fatalf("message role=%q, want assistant", body.Message.Role)
	}

	// Verify the title remains empty.
	gotConv, err := st.GetConversation(context.Background(), mustUserID(t, srv, hdr), conv.ID)
	if err != nil {
		t.Fatalf("GetConversation: %v", err)
	}
	if gotConv.Title != "" {
		t.Fatalf("expected title to remain empty, got %q", gotConv.Title)
	}
}

// TestChatAutoTitleNeverOverwrites verifies that an existing (non-empty) title
// is NEVER overwritten, even on the first exchange.
func TestChatAutoTitleNeverOverwrites(t *testing.T) {
	t.Parallel()
	gen := &smartChatGenerator{
		answerResp: plugin.GenerateResponse{
			Summary: plugin.SummaryPayload{Sections: []model.SummarySection{{
				Heading:         "Answer",
				ContentMarkdown: "The answer is 42.",
			}}},
			Model: "test-model",
		},
		titleResp: plugin.GenerateResponse{
			Summary: plugin.SummaryPayload{Sections: []model.SummarySection{{
				Heading:         "Title",
				ContentMarkdown: "Generated Title",
			}}},
			Model: "test-model",
		},
	}
	srv, st := newChatTestServer(t, gen)
	cr, _ := crypto.New("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	createDefaultAgentPlugin(t, st, cr, map[string]any{"model": "default-model"})
	hdr := setupGuardTestUser(t, srv, "title-no-overwrite@example.com")

	// Create a conversation with an explicit non-empty title.
	convRec := doGuardJSON(t, srv, http.MethodPost, "/api/conversations",
		map[string]any{"title": "Preset Title"}, hdr)
	var conv struct{ ID string }
	_ = json.Unmarshal(convRec.Body.Bytes(), &conv)

	// Send the first message.
	rec := doGuardJSON(t, srv, http.MethodPost, "/api/conversations/"+conv.ID+"/messages",
		map[string]any{"content": "What is the meaning of life?"}, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("send message=%d body=%s", rec.Code, rec.Body)
	}

	// Verify the preset title was NOT overwritten.
	gotConv, err := st.GetConversation(context.Background(), mustUserID(t, srv, hdr), conv.ID)
	if err != nil {
		t.Fatalf("GetConversation: %v", err)
	}
	if gotConv.Title != "Preset Title" {
		t.Fatalf("expected title unchanged at %q, got %q", "Preset Title", gotConv.Title)
	}

	// Only the answer call should have been made (isFirstExchange=true but
	// conv.Title != ""), so no title call.
	if gen.callCount != 1 {
		t.Fatalf("expected 1 plugin call (answer only, no title), got %d", gen.callCount)
	}
}

// TestChatAutoTitleOnlyFirstExchange verifies that a second exchange does NOT
// re-trigger title generation.
func TestChatAutoTitleOnlyFirstExchange(t *testing.T) {
	t.Parallel()
	gen := &smartChatGenerator{
		answerResp: plugin.GenerateResponse{
			Summary: plugin.SummaryPayload{Sections: []model.SummarySection{{
				Heading:         "Answer",
				ContentMarkdown: "The answer is 42.",
			}}},
			Model: "test-model",
		},
		titleResp: plugin.GenerateResponse{
			Summary: plugin.SummaryPayload{Sections: []model.SummarySection{{
				Heading:         "Title",
				ContentMarkdown: "Auto Title",
			}}},
			Model: "test-model",
		},
	}
	srv, st := newChatTestServer(t, gen)
	cr, _ := crypto.New("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	createDefaultAgentPlugin(t, st, cr, map[string]any{"model": "default-model"})
	hdr := setupGuardTestUser(t, srv, "title-only-first@example.com")

	// Create an empty-title conversation.
	convRec := doGuardJSON(t, srv, http.MethodPost, "/api/conversations",
		map[string]any{"title": ""}, hdr)
	var conv struct{ ID string }
	_ = json.Unmarshal(convRec.Body.Bytes(), &conv)

	// Send the first message (should trigger title generation).
	rec1 := doGuardJSON(t, srv, http.MethodPost, "/api/conversations/"+conv.ID+"/messages",
		map[string]any{"content": "First message"}, hdr)
	if rec1.Code != http.StatusOK {
		t.Fatalf("first send=%d body=%s", rec1.Code, rec1.Body)
	}

	// callCount after first exchange: 1 answer + 1 title = 2.
	if gen.callCount != 2 {
		t.Fatalf("after first exchange: expected 2 calls, got %d", gen.callCount)
	}

	// Send a second message (should NOT trigger title generation).
	rec2 := doGuardJSON(t, srv, http.MethodPost, "/api/conversations/"+conv.ID+"/messages",
		map[string]any{"content": "Second message"}, hdr)
	if rec2.Code != http.StatusOK {
		t.Fatalf("second send=%d body=%s", rec2.Code, rec2.Body)
	}

	// callCount after second exchange: 2 + 1 answer (no title) = 3.
	if gen.callCount != 3 {
		t.Fatalf("after second exchange: expected 3 calls total, got %d", gen.callCount)
	}

	// Verify the title was set (and not changed).
	gotConv, err := st.GetConversation(context.Background(), mustUserID(t, srv, hdr), conv.ID)
	if err != nil {
		t.Fatalf("GetConversation: %v", err)
	}
	if gotConv.Title != "Auto Title" {
		t.Fatalf("expected title=%q, got %q", "Auto Title", gotConv.Title)
	}
}

// TestChatAutoTitleCreateAndSend verifies that the create-and-send variant
// (POST /api/conversations with content) also triggers title generation.
func TestChatAutoTitleCreateAndSend(t *testing.T) {
	t.Parallel()
	gen := &smartChatGenerator{
		answerResp: plugin.GenerateResponse{
			Summary: plugin.SummaryPayload{Sections: []model.SummarySection{{
				Heading:         "Answer",
				ContentMarkdown: "The answer is 42.",
			}}},
			Model: "test-model",
		},
		titleResp: plugin.GenerateResponse{
			Summary: plugin.SummaryPayload{Sections: []model.SummarySection{{
				Heading:         "Title",
				ContentMarkdown: "New Conversation Title",
			}}},
			Model: "test-model",
		},
	}
	srv, st := newChatTestServer(t, gen)
	cr, _ := crypto.New("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	createDefaultAgentPlugin(t, st, cr, map[string]any{"model": "default-model"})
	hdr := setupGuardTestUser(t, srv, "title-create-send@example.com")

	// Create-and-send with an empty title.
	rec := doGuardJSON(t, srv, http.MethodPost, "/api/conversations",
		map[string]any{"title": "", "content": "Hello there"}, hdr)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create-and-send=%d body=%s", rec.Code, rec.Body)
	}

	var body struct {
		ID      string        `json:"id"`
		Message model.Message `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Verify the title was set.
	gotConv, err := st.GetConversation(context.Background(), mustUserID(t, srv, hdr), body.ID)
	if err != nil {
		t.Fatalf("GetConversation: %v", err)
	}
	if gotConv.Title != "New Conversation Title" {
		t.Fatalf("expected title=%q, got %q", "New Conversation Title", gotConv.Title)
	}

	// Verify both answer and title calls were made.
	if gen.callCount != 2 {
		t.Fatalf("expected 2 plugin calls, got %d", gen.callCount)
	}
}
