package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/abedegno/muesli/internal/chat"
	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/plugin"
	"github.com/abedegno/muesli/internal/store"
	"github.com/go-chi/chi/v5"
)

// chatTopK bounds how many cross-note sources are retrieved for a global
// (not note-scoped) conversation turn. Fixed and small so the prompt sent to
// the agent plugin stays a reasonable size; see internal/chat.Retriever.TopK.
const chatTopK = 5

// errNoDefaultAgentPlugin mirrors internal/worker/pipeline.go's runSummarize:
// no plugin is registered as the enabled default for kind "agent". Mapped to
// a 500 by the HTTP handlers (an operator configuration problem, not a bad
// request), same as the summarize pipeline's handling.
var errNoDefaultAgentPlugin = errors.New("no default agent plugin configured")

// errChatSendInFlight is returned by sendChatMessage when a send for the
// same conversation id is already in progress (chatSendGuard). Mapped to a
// 409 by the HTTP handlers.
var errChatSendInFlight = errors.New("message send already in progress")

// ChatGenerator is the seam the chat-send handlers use to call the
// configured default agent plugin's /generate endpoint. It is satisfied by
// *plugin.Client (the production implementation) and by test fakes, mirroring
// the Deps.PluginHealthProber seam in admin_health.go: a nil
// Deps.ChatGenerator falls back to a real plugin.New(...) client built from
// the owner's default agent plugin, so Go tests can mock the plugin call
// without a real HTTP server while still exercising real conversation/message
// persistence against a test database.
type ChatGenerator interface {
	Generate(ctx context.Context, req plugin.GenerateRequest) (plugin.GenerateResponse, error)
}

// chatGenerator returns the injected generator, defaulting to a real
// plugin.Client built from the resolved default agent plugin's credentials.
func (s *Server) chatGenerator(plug model.Plugin) ChatGenerator {
	if s.deps.ChatGenerator != nil {
		return s.deps.ChatGenerator
	}
	return plugin.New(plug.EndpointURL, plug.Token)
}

// chatRetriever builds a chat.Retriever from the server's store/embedder/config.
func (s *Server) chatRetriever() *chat.Retriever {
	return chat.NewRetriever(s.deps.Store, s.deps.Embedder, s.deps.Config)
}

// sendMessageRequest is the body of POST /api/conversations/{id}/messages.
// ModelOverride here is a PER-REQUEST override for this one turn only,
// distinct from (and taking precedence over) the conversation's own stored
// model_override -- see resolveModelOverride.
type sendMessageRequest struct {
	Content       string  `json:"content"`
	ModelOverride *string `json:"model_override"`
}

// chatSendResponse is the body returned by a successful send: the persisted
// assistant message plus the sources it cited this turn.
type chatSendResponse struct {
	Message model.Message `json:"message"`
	Sources []chat.Source `json:"sources"`
}

// handleSendMessage sends a new user message in an EXISTING conversation and
// returns the assistant's reply. See sendChatMessage for the shared
// retrieval/prompt/plugin-call/persist logic (also used by
// handleCreateConversation's create-and-send variant).
func (s *Server) handleSendMessage(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	id := chi.URLParam(r, "id")
	if !validID(w, r, id) {
		return
	}
	var req sendMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}

	conv, err := s.deps.Store.GetConversation(r.Context(), uid, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		log.Printf("handleSendMessage: get conversation: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	msg, sources, err := s.sendChatMessage(r.Context(), uid, conv, req.Content, req.ModelOverride)
	switch {
	case errors.Is(err, errChatSendInFlight):
		writeError(w, http.StatusConflict, "message send already in progress")
		return
	case errors.Is(err, errNoDefaultAgentPlugin):
		log.Printf("handleSendMessage: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	case err != nil:
		log.Printf("handleSendMessage: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, chatSendResponse{Message: msg, Sources: sources})
}

// sendChatMessage is the shared "send" logic used by both
// handleSendMessage (an existing conversation) and handleCreateConversation's
// create-and-send variant (a brand-new conversation): in-flight guard,
// retrieval, prompt assembly, the plugin call, model resolution, and
// persisting both the user and assistant messages.
//
// requestOverride is the PER-REQUEST model override (sendMessageRequest's
// model_override); pass nil when the caller has no such per-request value
// (e.g. create-and-send, where conv.ModelOverride -- set from the same
// request's model_override at creation time -- already carries it).
//
// Prior history for the prompt is read BEFORE the new user message is
// persisted, so the new message is never duplicated in both history and the
// trailing user turn.
func (s *Server) sendChatMessage(ctx context.Context, ownerID string, conv model.Conversation, content string, requestOverride *string) (model.Message, []chat.Source, error) {
	if !s.chatSendGuard.tryAcquire(conv.ID) {
		return model.Message{}, nil, errChatSendInFlight
	}
	defer s.chatSendGuard.release(conv.ID)

	sources, err := s.chatSources(ctx, ownerID, conv, content)
	if err != nil {
		return model.Message{}, nil, err
	}

	history, err := s.deps.Store.ListMessages(ctx, ownerID, conv.ID)
	if err != nil {
		return model.Message{}, nil, err
	}
	// Capture whether this is the first exchange BEFORE using history.
	isFirstExchange := len(history) == 0
	prompt := chat.BuildPrompt(sources, history, content)

	plug, err := s.deps.Store.DefaultPlugin(ctx, s.deps.Crypto, model.PluginAgent)
	if errors.Is(err, store.ErrNotFound) {
		return model.Message{}, nil, errNoDefaultAgentPlugin
	} else if err != nil {
		return model.Message{}, nil, err
	}

	cfg, err := resolveModelConfig(plug.Config, resolveModelOverride(requestOverride, conv.ModelOverride))
	if err != nil {
		return model.Message{}, nil, err
	}

	resp, err := s.chatGenerator(plug).Generate(ctx, plugin.GenerateRequest{
		Transcript:    []model.Segment{},
		NotesMarkdown: renderPromptText(prompt),
		Template: plugin.TemplatePayload{Sections: []model.TemplateSection{{
			Heading:     "Answer",
			Instruction: "Answer the user's latest message using only the notes above; cite sources inline like [1], [2] where relevant.",
		}}},
		Config: cfg,
	})
	if err != nil {
		return model.Message{}, nil, err
	}
	if len(resp.Summary.Sections) == 0 {
		return model.Message{}, nil, errors.New("plugin returned no answer sections")
	}
	replyText := resp.Summary.Sections[0].ContentMarkdown

	if _, err := s.deps.Store.AppendMessage(ctx, conv.ID, "user", content, "", nil); err != nil {
		return model.Message{}, nil, err
	}

	var tokensUsed *int
	if resp.Usage != nil {
		t := resp.Usage.TokensUsed
		tokensUsed = &t
	}
	assistantMsg, err := s.deps.Store.AppendMessage(ctx, conv.ID, "assistant", replyText, resp.Model, tokensUsed)
	if err != nil {
		return model.Message{}, nil, err
	}

	// Best-effort auto-generate a title after the first successful exchange,
	// if the conversation title is still empty. Any error is logged and
	// swallowed; the caller's response still succeeds.
	if isFirstExchange && conv.Title == "" {
		s.generateConversationTitle(ctx, conv, content, replyText, plug, cfg)
	}

	citedSources := chat.ParseCitations(replyText, sources)
	return assistantMsg, citedSources, nil
}

// chatSources resolves the retrieval sources for one turn: the note's own
// transcript (rendered as one TranscriptRef per segment, so each segment is
// individually numbered/citeable) when the conversation is note-scoped, or a
// cross-note top-k lexical+semantic search over the query text otherwise.
//
// A note-scoped conversation whose note has no transcript yet (or was
// removed after the conversation was created) degrades to an empty source
// list rather than failing the turn -- chat should still work even before a
// transcript exists, just without any citeable context.
func (s *Server) chatSources(ctx context.Context, ownerID string, conv model.Conversation, query string) ([]chat.TranscriptRef, error) {
	if conv.NoteID == nil {
		return s.chatRetriever().TopK(ctx, ownerID, query, chatTopK)
	}

	noteID := *conv.NoteID
	noteTitle := ""
	if note, err := s.deps.Store.GetNote(ctx, ownerID, noteID); err == nil {
		noteTitle = note.Title
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}

	nt, err := s.chatRetriever().NoteTranscript(ctx, ownerID, noteID)
	if errors.Is(err, store.ErrNotFound) {
		return []chat.TranscriptRef{}, nil
	}
	if err != nil {
		return nil, err
	}
	return segmentsToSources(noteID, noteTitle, nt.Segments), nil
}

// segmentsToSources turns a note's transcript segments into one
// chat.TranscriptRef per segment, in order, so RenderRetrievalBlock/BuildPrompt
// can number them [1..N] and ParseCitations can resolve markers back to a
// specific segment -- the simplest defensible adapter from
// Retriever.NoteTranscript's Segments to the numbered-source shape
// BuildPrompt/ParseCitations expect.
func segmentsToSources(noteID, noteTitle string, segments []model.Segment) []chat.TranscriptRef {
	refs := make([]chat.TranscriptRef, 0, len(segments))
	for i, seg := range segments {
		refs = append(refs, chat.TranscriptRef{
			NoteID:       noteID,
			NoteTitle:    noteTitle,
			SegmentID:    seg.ID,
			SegmentIndex: i,
			StartMS:      seg.StartMS,
			Snippet:      seg.Text,
		})
	}
	return refs
}

// resolveModelOverride implements the per-turn model precedence: (a) a
// non-empty per-request override, else (b) a non-nil/non-empty conversation
// model_override, else (c) "" (no override -- resolveModelConfig then passes
// the plugin's own Config through unmodified).
func resolveModelOverride(requestOverride, conversationOverride *string) string {
	if requestOverride != nil && strings.TrimSpace(*requestOverride) != "" {
		return *requestOverride
	}
	if conversationOverride != nil && strings.TrimSpace(*conversationOverride) != "" {
		return *conversationOverride
	}
	return ""
}

// resolveModelConfig returns a per-call copy of baseConfig with its "model"
// key set to override, WITHOUT mutating baseConfig (the stored plugin
// config). When override is "", baseConfig is returned unchanged so the
// plugin's own operator-configured default model applies.
func resolveModelConfig(baseConfig json.RawMessage, override string) (json.RawMessage, error) {
	if override == "" {
		return baseConfig, nil
	}
	m := map[string]any{}
	if len(baseConfig) > 0 {
		if err := json.Unmarshal(baseConfig, &m); err != nil {
			return nil, err
		}
	}
	m["model"] = override
	return json.Marshal(m)
}

// renderPromptText flattens the assembled chat.PromptMessage sequence into a
// single text block for plugin.GenerateRequest.NotesMarkdown, since the
// plugin contract has no chat/messages-shaped request -- each message
// becomes one "ROLE: content" line, in order, separated by blank lines.
func renderPromptText(prompt []chat.PromptMessage) string {
	parts := make([]string, 0, len(prompt))
	for _, m := range prompt {
		parts = append(parts, strings.ToUpper(m.Role)+": "+m.Content)
	}
	return strings.Join(parts, "\n\n")
}

// generateConversationTitle best-effort generates and persists a title for
// conv, based on the first user message (userContent) and assistant reply
// (assistantReply). It truncates both to ~500 chars, prompts the same agent
// plugin (via gen) to produce an 8-word title, sanitizes the result, and
// calls store.SetConversationTitleIfEmpty. Any error (plugin call, empty
// result, or store write) is logged and swallowed -- the caller's send/create
// response still succeeds regardless.
func (s *Server) generateConversationTitle(ctx context.Context, conv model.Conversation, userContent, assistantReply string, plug model.Plugin, cfg json.RawMessage) {
	const maxChars = 500
	truncUser := truncateString(userContent, maxChars)
	truncAssist := truncateString(assistantReply, maxChars)

	notes := "USER: " + truncUser + "\n\nASSISTANT: " + truncAssist

	resp, err := s.chatGenerator(plug).Generate(ctx, plugin.GenerateRequest{
		Transcript:    []model.Segment{},
		NotesMarkdown: notes,
		Template: plugin.TemplatePayload{Sections: []model.TemplateSection{{
			Heading:     "Title",
			Instruction: "Generate a concise title for this conversation. At most 8 words. Match the conversation's language. Respond with the title text only, no preamble, no quotes, no trailing punctuation.",
		}}},
		Config: cfg,
	})
	if err != nil {
		log.Printf("generateConversationTitle: plugin call failed: %v", err)
		return
	}
	if len(resp.Summary.Sections) == 0 {
		log.Printf("generateConversationTitle: plugin returned no sections")
		return
	}

	rawTitle := resp.Summary.Sections[0].ContentMarkdown
	title := sanitizeTitle(rawTitle)
	if title == "" {
		log.Printf("generateConversationTitle: sanitized title is empty (raw=%q)", rawTitle)
		return
	}

	updated, err := s.deps.Store.SetConversationTitleIfEmpty(ctx, conv.ID, title)
	if err != nil {
		log.Printf("generateConversationTitle: store update failed: %v", err)
		return
	}
	if !updated {
		log.Printf("generateConversationTitle: conversation %s already has a title, skipped", conv.ID)
	}
}

// truncateString truncates s to at most maxChars runes, appending "..." if
// truncated. Simple rune-based truncation (does not respect word boundaries).
func truncateString(s string, maxChars int) string {
	runes := []rune(s)
	if len(runes) <= maxChars {
		return s
	}
	return string(runes[:maxChars]) + "..."
}

// sanitizeTitle trims whitespace/newlines, strips wrapping quote characters
// (single or double), collapses internal whitespace runs to single spaces, and
// caps length at 200 chars. Returns "" if the result is empty.
func sanitizeTitle(raw string) string {
	// Trim leading/trailing whitespace
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}

	// Strip wrapping quotes (single or double)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			s = s[1 : len(s)-1]
			s = strings.TrimSpace(s)
		}
	}

	// Collapse internal whitespace (including newlines) to single spaces
	s = strings.Join(strings.Fields(s), " ")

	// Cap length at 200 chars
	const maxTitleLen = 200
	runes := []rune(s)
	if len(runes) > maxTitleLen {
		s = string(runes[:maxTitleLen])
	}

	return s
}
