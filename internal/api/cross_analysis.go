package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/abedegno/muesli/internal/execution"
	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
	"github.com/google/uuid"
)

// crossAnalysisPromptVersion pins the canonical corpus/prompt-rendering
// algorithm version (internal/execution/prompt.go). It is folded into the
// admission digest (see crossAnalysisDigest) so a change to that rendering
// invalidates any preflight computed against an older version -- a stale
// digest can never silently reuse an outdated admission decision.
const crossAnalysisPromptVersion = "cross-v1"

// crossAnalysisRequest is the optional "cross_analysis" object accepted by
// both POST /api/conversations (create-and-send) and
// POST /api/conversations/{id}/messages (issue #765).
type crossAnalysisRequest struct {
	TemplateID string   `json:"template_id"`
	NoteIDs    []string `json:"note_ids"`
}

// Sentinel preflight errors mapped to specific statuses by
// writeCrossAnalysisPreflightError. Never expose transcript content,
// generated content, credentials, or raw plugin bodies in their text.
var (
	errCrossAnalysisMalformed  = errors.New("cross-analysis: template_id and at least two note_ids are required")
	errCrossAnalysisNoteScoped = errors.New("cross-analysis requires a global conversation")
	errCrossAnalysisWrongPhase = errors.New("cross-analysis template must have phase cross")
	// errCrossAnalysisPluginChanged is returned by invokeCrossAnalysis (never
	// preflight) when the plugin resolved immediately before invocation does
	// not match the one preflight admitted against -- see ruling 4. Always a
	// post-creation 500 with zero generator calls.
	errCrossAnalysisPluginChanged = errors.New("cross-analysis: agent plugin changed since preflight")
)

// crossAnalysisPreflight is preflightCrossDocuments' result: everything
// invokeCrossAnalysis needs, plus the plugin identity/digest to re-verify
// immediately before calling it.
type crossAnalysisPreflight struct {
	Prepared     execution.PreparedExecution
	Plugin       model.Plugin
	Digest       string
	TemplateName string
}

// crossAnalysisMaxNotes returns the effective note-count ceiling: the
// configured value if positive and at or below the shared maximum, else the
// shared default (model.CrossAnalysisMaxNotes). A configured ceiling BELOW
// the default is honored (used by tests to exercise 413 without 40 real
// notes); a configured value above it, zero, or negative falls back to the
// default rather than raising the shared ceiling.
func (s *Server) crossAnalysisMaxNotes() int {
	max := s.deps.Config.CrossAnalysisMaxNotes
	if max <= 0 || max > model.CrossAnalysisMaxNotes {
		return model.CrossAnalysisMaxNotes
	}
	return max
}

// validateCrossAnalysisIDs checks template_id and note_ids are syntactically
// well-formed UUIDs before ever touching the store -- a malformed id is a
// 400, never a 404 (404 is reserved for a well-formed but absent/invisible
// id).
func validateCrossAnalysisIDs(req crossAnalysisRequest) error {
	if strings.TrimSpace(req.TemplateID) == "" || len(req.NoteIDs) < model.CrossAnalysisMinNotes {
		return errCrossAnalysisMalformed
	}
	if _, err := uuid.Parse(req.TemplateID); err != nil {
		return errCrossAnalysisMalformed
	}
	for _, id := range req.NoteIDs {
		if _, err := uuid.Parse(id); err != nil {
			return errCrossAnalysisMalformed
		}
	}
	return nil
}

// preflightCrossDocuments validates the conversation, template visibility
// and phase, and the note snapshot, then prepares and admits the documents
// against the resolved default agent's declared admission config -- all
// read-only, no persistence, and (for create-and-send) called BEFORE any
// conversation exists (see the accepted spec's architecture step 1-4).
//
// Exact store.ErrNotFound from the default-agent lookup is returned as
// errNoDefaultAgentPlugin so create-and-send can defer its 422 until after
// creation (ruling 2); every other lookup, decrypt, JSON, or config error is
// returned as-is for the caller to map to a sanitized 500.
func (s *Server) preflightCrossDocuments(ctx context.Context, ownerID string, conv model.Conversation, req crossAnalysisRequest, focus string) (crossAnalysisPreflight, error) {
	if conv.NoteID != nil {
		return crossAnalysisPreflight{}, errCrossAnalysisNoteScoped
	}
	if err := validateCrossAnalysisIDs(req); err != nil {
		return crossAnalysisPreflight{}, err
	}

	tmpl, err := s.deps.Store.GetTemplate(ctx, ownerID, req.TemplateID)
	if err != nil {
		return crossAnalysisPreflight{}, err // store.ErrNotFound (404) or a real 500
	}
	if tmpl.Phase != "cross" {
		return crossAnalysisPreflight{}, errCrossAnalysisWrongPhase
	}

	snap, err := s.deps.Store.LoadCrossAnalysisSnapshot(ctx, ownerID, req.NoteIDs, s.crossAnalysisMaxNotes())
	if err != nil {
		return crossAnalysisPreflight{}, err
	}

	plug, err := s.deps.Store.DefaultPlugin(ctx, s.deps.Crypto, model.PluginAgent)
	if errors.Is(err, store.ErrNotFound) {
		return crossAnalysisPreflight{}, errNoDefaultAgentPlugin
	} else if err != nil {
		return crossAnalysisPreflight{}, err
	}

	prepared, err := prepareCrossAnalysisExecution(tmpl, snap, plug, focus)
	if err != nil {
		return crossAnalysisPreflight{}, err
	}

	admissionCfg, err := execution.ParseAdmissionConfig(plug.Config)
	if err != nil {
		return crossAnalysisPreflight{}, err
	}
	if err := execution.Admit(prepared, admissionCfg); err != nil {
		return crossAnalysisPreflight{}, err // ErrContextBudget (413) or ErrInvalidAdmissionConfig (500)
	}

	return crossAnalysisPreflight{
		Prepared:     prepared,
		Plugin:       plug,
		Digest:       crossAnalysisDigest(plug.ID, admissionCfg),
		TemplateName: tmpl.Name,
	}, nil
}

// prepareCrossAnalysisExecution converts a loaded store snapshot into an
// execution.ExecutionInput and calls execution.PrepareDocuments.
func prepareCrossAnalysisExecution(tmpl model.Template, snap store.CrossAnalysisSnapshot, plug model.Plugin, focus string) (execution.PreparedExecution, error) {
	documents := make([]execution.Document, 0, len(snap.Documents))
	for _, d := range snap.Documents {
		occurred := ""
		if d.OccurredAt != nil {
			occurred = d.OccurredAt.UTC().Format(time.RFC3339)
		}
		documents = append(documents, execution.Document{
			NoteID:               d.NoteID,
			Title:                d.Title,
			OccurredAt:           occurred,
			TranscriptGeneration: d.TranscriptGeneration,
			Segments:             d.Segments,
		})
	}
	return execution.PrepareDocuments(execution.ExecutionInput{
		Template:       tmpl,
		Documents:      documents,
		RunInstruction: strings.TrimSpace(focus),
		Config:         plug.Config,
		SystemPrompt:   tmpl.SystemPrompt,
		Model:          tmpl.Model,
		Temperature:    tmpl.Temperature,
	})
}

// crossAnalysisDigest is the canonical digest of every admission-relevant
// field preflight recorded: the resolved plugin's ID plus its declared
// context/output/framing/capability values and the prompt-rendering
// version. invokeCrossAnalysis recomputes it immediately before invocation
// and requires exact equality -- any change (a different default plugin, an
// edited config, a bumped prompt version) fails closed with a 500 rather
// than invoking against an unvalidated admission decision (ruling 4).
func crossAnalysisDigest(pluginID string, cfg execution.AdmissionConfig) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s|%s|%d|%d|%d|%t",
		crossAnalysisPromptVersion, pluginID, cfg.ContextTokens, cfg.OutputReserveTokens, cfg.ProviderFramingTokens, cfg.ByteFallbackTokenizer)
	return hex.EncodeToString(h.Sum(nil))
}

// invokeCrossAnalysis re-verifies every captured transcript generation and
// the admitted plugin's identity/digest immediately before calling Run (so a
// generation change or a changed/newly-appearing agent is caught with ZERO
// generator calls), executes the run, converts its result into one assistant
// markdown message plus its complete source set, atomically persists the
// turn, and best-effort generates a title. It never re-runs preflight's
// validation (conversation/template/note eligibility) -- pre is trusted as
// already validated.
func (s *Server) invokeCrossAnalysis(ctx context.Context, ownerID string, conv model.Conversation, pre crossAnalysisPreflight, focus string) (model.Message, error) {
	if err := s.deps.Store.VerifyCrossAnalysisGenerations(ctx, pre.Prepared.Generations); err != nil {
		return model.Message{}, err // store.ErrGenerationMismatch -> 412
	}

	plug, err := s.deps.Store.DefaultPlugin(ctx, s.deps.Crypto, model.PluginAgent)
	if errors.Is(err, store.ErrNotFound) {
		return model.Message{}, errNoDefaultAgentPlugin // 422
	} else if err != nil {
		return model.Message{}, err
	}
	admissionCfg, err := execution.ParseAdmissionConfig(plug.Config)
	if err != nil {
		return model.Message{}, err
	}
	if plug.ID != pre.Plugin.ID || crossAnalysisDigest(plug.ID, admissionCfg) != pre.Digest {
		return model.Message{}, errCrossAnalysisPluginChanged
	}

	result, err := execution.NewExecutor(s.chatGenerator(plug)).Run(ctx, pre.Prepared)
	if err != nil {
		return model.Message{}, err
	}

	userContent := buildCrossAnalysisUserContent(pre.TemplateName, len(pre.Prepared.Input.Documents), focus)
	assistantContent := renderCrossAnalysisMarkdown(result.Sections)
	sources := toMessageSources(result.Sources)

	history, err := s.deps.Store.ListMessages(ctx, ownerID, conv.ID)
	if err != nil {
		return model.Message{}, err
	}
	isFirstExchange := len(history) == 0

	_, assistantMsg, err := s.deps.Store.AppendCrossAnalysisTurn(ctx, conv.ID, userContent, assistantContent, result.Model, result.TokensUsed, sources)
	if err != nil {
		return model.Message{}, err
	}

	if isFirstExchange && conv.Title == "" {
		if cfg, cfgErr := resolveModelConfig(plug.Config, resolveModelOverride(nil, conv.ModelOverride)); cfgErr == nil {
			s.generateConversationTitle(ctx, conv, userContent, assistantContent, plug, cfg)
		}
	}

	return assistantMsg, nil
}

// buildCrossAnalysisUserContent is the persisted, system-authored user turn:
// the supplied focus (if any) plus a concise template-and-count statement.
// It NEVER embeds transcript text.
func buildCrossAnalysisUserContent(templateName string, noteCount int, focus string) string {
	statement := fmt.Sprintf("Cross-meeting analysis using template %q across %d meetings.", templateName, noteCount)
	focus = strings.TrimSpace(focus)
	if focus == "" {
		return statement
	}
	return focus + "\n\n" + statement
}

// renderCrossAnalysisMarkdown converts every returned template section into
// one assistant markdown message, preserving section order and headings.
func renderCrossAnalysisMarkdown(sections []model.SummarySection) string {
	parts := make([]string, 0, len(sections))
	for _, sec := range sections {
		parts = append(parts, fmt.Sprintf("## %s\n\n%s", sec.Heading, sec.ContentMarkdown))
	}
	return strings.Join(parts, "\n\n")
}

// toMessageSources converts EVERY assigned citation (not only cited ones --
// see the accepted plan's ruling 5) into its persisted wire shape.
func toMessageSources(refs []execution.SourceRef) []model.MessageSource {
	out := make([]model.MessageSource, 0, len(refs))
	for _, ref := range refs {
		noteID := ref.NoteID
		out = append(out, model.MessageSource{
			N:                    ref.N,
			NoteID:               &noteID,
			TranscriptGeneration: ref.TranscriptGeneration,
			SegmentIndex:         ref.SegmentIndex,
			Timestamp:            ref.Timestamp,
			Snippet:              ref.Snippet,
		})
	}
	return out
}

// writeCrossAnalysisPreflightError maps a preflightCrossDocuments error to
// its HTTP status per the accepted spec's exhaustive table. Never used for
// create-and-send's deferred errNoDefaultAgentPlugin case -- callers handle
// that themselves before reaching here.
func writeCrossAnalysisPreflightError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errCrossAnalysisMalformed),
		errors.Is(err, errCrossAnalysisNoteScoped),
		errors.Is(err, errCrossAnalysisWrongPhase),
		errors.Is(err, store.ErrCrossAnalysisDuplicateNotes),
		errors.Is(err, store.ErrCrossAnalysisTooFewNotes),
		errors.Is(err, store.ErrCrossAnalysisNoteNotEligible):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not found")
	case errors.Is(err, store.ErrCrossAnalysisTooManyNotes):
		writeError(w, http.StatusRequestEntityTooLarge, "too many notes selected")
	case errors.Is(err, execution.ErrContextBudget):
		writeError(w, http.StatusRequestEntityTooLarge, "selected meetings exceed the model's context budget")
	case errors.Is(err, errNoDefaultAgentPlugin):
		writeError(w, http.StatusUnprocessableEntity, "no default agent configured")
	default:
		log.Printf("cross-analysis preflight: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}

// writeCrossAnalysisPostCreationError maps an invokeCrossAnalysis error
// (raised strictly after preflight already succeeded) to its HTTP status.
func writeCrossAnalysisPostCreationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrGenerationMismatch):
		writeError(w, http.StatusPreconditionFailed, "selected meetings changed, please retry")
	case errors.Is(err, errNoDefaultAgentPlugin):
		writeError(w, http.StatusUnprocessableEntity, "no default agent configured")
	default:
		log.Printf("cross-analysis invoke: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}

// handleCrossAnalysisSend serves the cross_analysis branch of
// POST /api/conversations/{id}/messages: acquires the conversation's send
// guard BEFORE preflight (ruling: existing-send acquires its guard before
// preflight), then preflights and invokes. Always releases the guard.
func (s *Server) handleCrossAnalysisSend(w http.ResponseWriter, ctx context.Context, ownerID string, conv model.Conversation, req crossAnalysisRequest, focus string) {
	if !s.chatSendGuard.tryAcquire(conv.ID) {
		writeError(w, http.StatusConflict, "message send already in progress")
		return
	}
	defer s.chatSendGuard.release(conv.ID)

	pre, err := s.preflightCrossDocuments(ctx, ownerID, conv, req, focus)
	if err != nil {
		writeCrossAnalysisPreflightError(w, err)
		return
	}
	msg, err := s.invokeCrossAnalysis(ctx, ownerID, conv, pre, focus)
	if err != nil {
		writeCrossAnalysisPostCreationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, chatSendResponse{Message: msg})
}

// handleCreateAndSendCrossAnalysis serves the cross_analysis branch of
// POST /api/conversations: preflight runs BEFORE any conversation exists, so
// a 400/404/413 (or a pre-creation 500) creates nothing. Only once preflight
// succeeds -- OR fails with exactly errNoDefaultAgentPlugin, deferred per
// ruling 2 -- does the handler create the conversation once and acquire its
// guard.
func (s *Server) handleCreateAndSendCrossAnalysis(w http.ResponseWriter, ctx context.Context, ownerID string, req conversationRequest) {
	if req.NoteID != nil {
		writeCrossAnalysisPreflightError(w, errCrossAnalysisNoteScoped)
		return
	}
	focus := strings.TrimSpace(req.Content)
	synthetic := model.Conversation{NoteID: nil}

	pre, err := s.preflightCrossDocuments(ctx, ownerID, synthetic, *req.CrossAnalysis, focus)
	if err != nil && !errors.Is(err, errNoDefaultAgentPlugin) {
		// Pre-creation failure (400/404/413/500): no conversation is created.
		writeCrossAnalysisPreflightError(w, err)
		return
	}

	conv, cerr := s.deps.Store.CreateConversation(ctx, ownerID, req.NoteID, req.Title, req.ModelOverride)
	if errors.Is(cerr, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if cerr != nil {
		log.Printf("handleCreateAndSendCrossAnalysis: create conversation: %v", cerr)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if err != nil {
		// errNoDefaultAgentPlugin, deferred: the conversation now exists but
		// remains empty and titleless. Re-resolve the default agent
		// immediately before responding, to guard against the race where an
		// agent plugin becomes the default in the window between preflight's
		// lookup and here: that agent was never admitted by preflight (never
		// ran through prepareCrossAnalysisExecution / admission budget
		// checks), so reporting "no default agent configured" would be wrong.
		s.resolveDeferredNoDefaultAgentPlugin(ctx, w)
		return
	}

	if !s.chatSendGuard.tryAcquire(conv.ID) {
		writeError(w, http.StatusConflict, "message send already in progress")
		return
	}
	defer s.chatSendGuard.release(conv.ID)

	msg, err := s.invokeCrossAnalysis(ctx, ownerID, conv, pre, focus)
	if err != nil {
		writeCrossAnalysisPostCreationError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, conversationWithMessage{Conversation: conv, Message: msg})
}

// resolveDeferredNoDefaultAgentPlugin re-resolves the default agent plugin
// immediately before create-and-send responds to a deferred
// errNoDefaultAgentPlugin (ruling 2), and writes the correct response for
// the race between preflight's DefaultPlugin lookup and this point:
//
//   - still absent (store.ErrNotFound): the original 422 "no default agent
//     configured" stands, unchanged.
//   - present now, or the re-check itself errors for any other reason: that
//     agent (or condition) was never validated/admitted by preflight (it
//     never ran through prepareCrossAnalysisExecution / admission budget
//     checks), so the handler fails closed with a sanitized 500, mirroring
//     writeCrossAnalysisPreflightError's default case and the cerr handling
//     above in handleCreateAndSendCrossAnalysis.
func (s *Server) resolveDeferredNoDefaultAgentPlugin(ctx context.Context, w http.ResponseWriter) {
	_, err := s.deps.Store.DefaultPlugin(ctx, s.deps.Crypto, model.PluginAgent)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusUnprocessableEntity, "no default agent configured")
		return
	}
	if err != nil {
		log.Printf("handleCreateAndSendCrossAnalysis: default agent re-check: %v", err)
	} else {
		log.Printf("handleCreateAndSendCrossAnalysis: default agent became available after preflight deferred its error; failing closed since it was never admitted")
	}
	writeError(w, http.StatusInternalServerError, "internal error")
}
