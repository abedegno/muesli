// Package execution is the sole template-running boundary for both of this
// app's template-execution callers: the general, multi-document
// cross-meeting analysis introduced by issue #765 (an ordered collection of
// documents, one per selected meeting, rendered into a citation-numbered
// prompt corpus, admitted against a byte-conservative context budget, and
// executed through a single agent-plugin call whose response is strictly
// validated section-by-section against the requesting template), and the
// ordinary, pre-#765 single-note after-summary path
// (internal/worker/pipeline.go's runSummarize), which goes through this same
// boundary but with genuinely different wire-request and validation
// semantics -- see ExecutionInput.Mode / ModeSingleNoteSummary.
//
// This package does not itself talk to a real plugin over HTTP -- callers
// supply a Generator (satisfied by *plugin.Client in production) so tests can
// inject deterministic fakes. It also performs no store/DB access: callers
// (internal/api's cross-analysis handlers, internal/worker's summarize job)
// are responsible for loading documents, verifying transcript generations
// immediately before Run when applicable, and persisting the result.
package execution

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/plugin"
)

// Document is one ordered input document (one selected meeting) for a
// generalized template execution. Existing one-note callers supply a single-
// element collection; documents are never concatenated into a single
// transcript (see the accepted spec's "Architecture and data flow").
type Document struct {
	NoteID string
	Title  string
	// OccurredAt is an RFC3339 timestamp (typically the note's started_at,
	// falling back to created_at); empty when unknown.
	OccurredAt string
	// TranscriptGeneration is the generation this document's Segments were
	// captured at -- callers must re-verify it immediately before Run (see
	// VerifyCrossAnalysisGenerations in internal/store).
	TranscriptGeneration int
	Segments             []model.Segment
	// NotesMarkdown is this document's own note body, included only when the
	// caller already has it (never fetched by this package).
	NotesMarkdown string
}

// Mode selects which prompt-rendering and response-validation semantics
// PrepareDocuments/Executor.Run apply to a run. internal/execution is the
// sole template-running boundary for BOTH the general cross-meeting-
// analysis path and the ordinary, pre-issue-#765 single-note summary path
// (internal/worker/pipeline.go's runSummarize) -- but those two callers
// need genuinely different wire requests and validation strictness, so
// Mode is an explicit, minimal switch inside this shared boundary rather
// than either caller bypassing it.
type Mode int

const (
	// ModeCrossAnalysis is the zero value and the general, citation-driven
	// cross-meeting-analysis behavior introduced by issue #765: the corpus
	// gets the multi-meeting system directive, meeting delimiters, numbered
	// citations, and focus/citation framing (see prompt.go's buildCorpus),
	// the wire request always carries Documents, and Run strictly validates
	// the plugin's response sections against the requested template
	// (validateSections). Being the zero value means every pre-existing
	// caller of ExecutionInput (internal/api/cross_analysis.go) needs no
	// change.
	ModeCrossAnalysis Mode = iota
	// ModeSingleNoteSummary is the ordinary, pre-#765 after-summary path.
	// The wire request and the plugin's response handling stay
	// byte-for-byte/behaviorally identical to the direct plugin.Client.Generate
	// call this mode replaced: no multi-meeting framing at all (the corpus is
	// exactly the single document's own NotesMarkdown), the request forwards
	// the document's Segments via GenerateRequest.Transcript (never
	// Documents), and the plugin's returned sections are trusted as-is with
	// NO section validation -- exactly like the pre-refactor worker code,
	// which persisted whatever the plugin returned. PrepareDocuments requires
	// exactly one document in this mode.
	ModeSingleNoteSummary
)

// ExecutionInput is the canonical input to PrepareDocuments/Run.
type ExecutionInput struct {
	Template model.Template
	// Mode selects cross-meeting-analysis vs. single-note-summary semantics;
	// see Mode's doc comment. Defaults to ModeCrossAnalysis.
	Mode Mode
	// Documents are in caller-supplied (request) order; PrepareDocuments
	// preserves it exactly -- this package never reorders or samples.
	Documents []Document
	// RunInstruction is the optional, user-supplied run-specific focus/
	// question. Never mutates the template's own section instructions --
	// see prompt.go's buildCorpus. Unused in ModeSingleNoteSummary.
	RunInstruction string
	// Config/SystemPrompt/Model/Temperature are the resolved agent plugin
	// configuration and template overrides, forwarded to the plugin exactly
	// as the single-note (existing) path does.
	Config       []byte
	SystemPrompt string
	Model        string
	Temperature  *float64
}

// SourceRef is one prompt-numbered citation assigned while preparing the
// corpus: a stable integer N, resolvable back to the originating document's
// segment. Callers convert these into model.MessageSource for persistence.
type SourceRef struct {
	N                    int
	NoteID               string
	TranscriptGeneration int
	SegmentIndex         int
	// Timestamp is the segment's StartMS.
	Timestamp int
	Snippet   string
}

// SectionPrompt is the complete, self-contained rendered prompt for ONE
// template section -- the full corpus (documents, citations, focus) plus
// that section's own heading/instruction -- used only for admission's
// conservative per-section byte bound (see admission.go). It is never sent
// to the plugin as a single opaque blob: the actual invocation forwards the
// structured Documents plus the shared corpus text (PreparedExecution.Corpus)
// and the template's sections structurally, exactly like the existing
// single-note path forwards NotesMarkdown + Template.Sections together.
type SectionPrompt struct {
	Heading string
	Prompt  string
}

// PreparedExecution is PrepareDocuments' result: everything Run and Admit
// need, computed once so both operate over the identical, immutable
// snapshot.
type PreparedExecution struct {
	Input ExecutionInput
	// Corpus is the shared, section-independent rendered text (documents,
	// citation table, focus) sent as the plugin request's NotesMarkdown.
	Corpus string
	// SectionPrompts holds one conservative complete-prompt rendering per
	// template section, consumed only by Admit.
	SectionPrompts []SectionPrompt
	// Sources is the complete, globally-numbered citation set for this run,
	// in ascending N order.
	Sources []SourceRef
	// Generations captures each document's transcript generation at prepare
	// time, keyed by note ID, for the immediately-pre-invocation re-check.
	Generations map[string]int
}

// ErrEmptyDocuments is returned by PrepareDocuments when Input.Documents is
// empty -- a generalized execution always has at least one document.
var ErrEmptyDocuments = errors.New("execution: at least one document is required")

// ErrEmptyTemplate is returned by PrepareDocuments when the template has no
// sections.
var ErrEmptyTemplate = errors.New("execution: template has no sections")

// ErrSingleNoteModeRequiresOneDocument is returned by PrepareDocuments when
// Input.Mode is ModeSingleNoteSummary but Input.Documents does not contain
// exactly one document -- that mode has no multi-document meaning.
var ErrSingleNoteModeRequiresOneDocument = errors.New("execution: single-note summary mode requires exactly one document")

// PrepareDocuments renders the canonical prompt corpus, assigns stable
// citation numbers to every segment of every document (in document, then
// segment, order -- deduplicated by note-ID+segment-index, though within one
// call that key is already unique per document), and records each
// document's transcript generation. It performs no I/O.
//
// In ModeSingleNoteSummary, the corpus is exactly the single document's own
// NotesMarkdown -- no multi-meeting directive, meeting delimiters, citation
// numbering, or focus/citation framing -- and SectionPrompts is left empty
// (Admit, the byte-budget gate, is never called for this mode).
func PrepareDocuments(input ExecutionInput) (PreparedExecution, error) {
	if len(input.Documents) == 0 {
		return PreparedExecution{}, ErrEmptyDocuments
	}
	if len(input.Template.Sections) == 0 {
		return PreparedExecution{}, ErrEmptyTemplate
	}
	if input.Mode == ModeSingleNoteSummary && len(input.Documents) != 1 {
		return PreparedExecution{}, ErrSingleNoteModeRequiresOneDocument
	}

	sources := make([]SourceRef, 0)
	seen := make(map[string]int) // dedup key (note_id + "\x00" + segment_index) -> N
	generations := make(map[string]int, len(input.Documents))
	for _, doc := range input.Documents {
		generations[doc.NoteID] = doc.TranscriptGeneration
		for segIdx, seg := range doc.Segments {
			key := fmt.Sprintf("%s\x00%d", doc.NoteID, segIdx)
			if _, dup := seen[key]; dup {
				continue
			}
			n := len(sources) + 1
			seen[key] = n
			sources = append(sources, SourceRef{
				N:                    n,
				NoteID:               doc.NoteID,
				TranscriptGeneration: doc.TranscriptGeneration,
				SegmentIndex:         segIdx,
				Timestamp:            seg.StartMS,
				Snippet:              seg.Text,
			})
		}
	}

	var corpus string
	var sectionPrompts []SectionPrompt
	if input.Mode == ModeSingleNoteSummary {
		corpus = input.Documents[0].NotesMarkdown
	} else {
		corpus = buildCorpus(input, sources)
		sectionPrompts = make([]SectionPrompt, 0, len(input.Template.Sections))
		for _, sec := range input.Template.Sections {
			sectionPrompts = append(sectionPrompts, SectionPrompt{
				Heading: sec.Heading,
				Prompt:  buildSectionPrompt(corpus, sec),
			})
		}
	}

	return PreparedExecution{
		Input:          input,
		Corpus:         corpus,
		SectionPrompts: sectionPrompts,
		Sources:        sources,
		Generations:    generations,
	}, nil
}

// Generator is the seam Run uses to call the agent plugin's /generate
// endpoint. Satisfied by *plugin.Client (production) and by test fakes; the
// same shape as internal/api's ChatGenerator interface.
type Generator interface {
	Generate(ctx context.Context, req plugin.GenerateRequest) (plugin.GenerateResponse, error)
}

// Result is Run's successful output: the generated sections in template
// order, the citation set they may reference, and plugin-reported metadata.
type Result struct {
	Sections   []model.SummarySection
	Sources    []SourceRef
	Model      string
	TokensUsed *int
	// Usage is the plugin's raw token-usage report, when it sent one -- nil
	// otherwise. TokensUsed above is the same value in the shape cross-analysis
	// persistence needs; Usage is preserved in full so callers with their own
	// heuristics over both TokensUsed and MaxTokens (e.g. internal/worker's
	// DetectTruncation) do not need a second plugin call to get it.
	Usage *plugin.GenerateUsage
}

// ErrSectionMismatch is returned by Run when the plugin's response sections
// do not exactly match the requested template sections: missing, extra,
// renamed, reordered, or duplicated.
var ErrSectionMismatch = errors.New("execution: response sections do not match the requested template")

// ErrEmptyOutput is returned by Run when a section's generated content is
// blank.
var ErrEmptyOutput = errors.New("execution: agent returned empty content for a section")

// Run executes every template section from prepared through gen in a
// single plugin call, forwarding SystemPrompt/Model/Temperature unchanged.
//
// In ModeCrossAnalysis (the general, multi-document path), the request
// carries Documents plus the shared corpus (as NotesMarkdown) plus every
// template section structurally, and Run fails the whole run on malformed
// output (section count/heading/order mismatch) or any empty section --
// there is no partial success.
//
// In ModeSingleNoteSummary, the request instead carries the single
// document's Segments via Transcript and its own NotesMarkdown verbatim
// (never Documents, never the cross-meeting corpus), matching the direct
// plugin.Client.Generate call this mode replaced byte-for-byte; the
// plugin's returned sections are trusted as-is with no validation, exactly
// like the pre-refactor worker code.
//
// Either mode fails the whole run on a timeout/cancellation (gen returns an
// error).
func (e *Executor) Run(ctx context.Context, prepared PreparedExecution) (Result, error) {
	wantSections := make([]model.TemplateSection, len(prepared.Input.Template.Sections))
	copy(wantSections, prepared.Input.Template.Sections)

	var req plugin.GenerateRequest
	if prepared.Input.Mode == ModeSingleNoteSummary {
		// PrepareDocuments already enforced exactly one document for this mode.
		doc := prepared.Input.Documents[0]
		req = plugin.GenerateRequest{
			Transcript:    doc.Segments,
			NotesMarkdown: prepared.Corpus, // == doc.NotesMarkdown; see PrepareDocuments.
			Template:      plugin.TemplatePayload{Sections: wantSections},
			Config:        prepared.Input.Config,
			SystemPrompt:  prepared.Input.SystemPrompt,
			Model:         prepared.Input.Model,
			Temperature:   prepared.Input.Temperature,
		}
	} else {
		docs := make([]plugin.Document, 0, len(prepared.Input.Documents))
		for _, d := range prepared.Input.Documents {
			docs = append(docs, plugin.Document{
				NoteID:               d.NoteID,
				Title:                d.Title,
				OccurredAt:           d.OccurredAt,
				TranscriptGeneration: d.TranscriptGeneration,
				Segments:             d.Segments,
				NotesMarkdown:        d.NotesMarkdown,
			})
		}
		req = plugin.GenerateRequest{
			Documents:     docs,
			NotesMarkdown: prepared.Corpus,
			Template:      plugin.TemplatePayload{Sections: wantSections},
			Config:        prepared.Input.Config,
			SystemPrompt:  prepared.Input.SystemPrompt,
			Model:         prepared.Input.Model,
			Temperature:   prepared.Input.Temperature,
		}
	}

	resp, err := e.Generator.Generate(ctx, req)
	if err != nil {
		return Result{}, fmt.Errorf("execution: generate: %w", err)
	}

	sections := resp.Summary.Sections
	if prepared.Input.Mode != ModeSingleNoteSummary {
		sections, err = validateSections(wantSections, resp.Summary.Sections)
		if err != nil {
			return Result{}, err
		}
	}

	var tokensUsed *int
	if resp.Usage != nil {
		t := resp.Usage.TokensUsed
		tokensUsed = &t
	}
	return Result{Sections: sections, Sources: prepared.Sources, Model: resp.Model, TokensUsed: tokensUsed, Usage: resp.Usage}, nil
}

// validateSections proves got is exactly want, positionally: same length, no
// duplicate/renamed/reordered headings, no empty content. Missing sections
// surface as a length mismatch; extra sections too.
func validateSections(want []model.TemplateSection, got []model.SummarySection) ([]model.SummarySection, error) {
	if len(got) != len(want) {
		return nil, fmt.Errorf("%w: got %d sections, want %d", ErrSectionMismatch, len(got), len(want))
	}
	seen := make(map[string]struct{}, len(got))
	for i, sec := range got {
		if sec.Heading != want[i].Heading {
			return nil, fmt.Errorf("%w: section %d heading %q, want %q (renamed or reordered)", ErrSectionMismatch, i, sec.Heading, want[i].Heading)
		}
		if _, dup := seen[sec.Heading]; dup {
			return nil, fmt.Errorf("%w: duplicate section heading %q", ErrSectionMismatch, sec.Heading)
		}
		seen[sec.Heading] = struct{}{}
		if strings.TrimSpace(sec.ContentMarkdown) == "" {
			return nil, fmt.Errorf("%w: section %q", ErrEmptyOutput, sec.Heading)
		}
	}
	return got, nil
}

// Executor runs a PreparedExecution through a Generator.
type Executor struct {
	Generator Generator
}

// NewExecutor builds an Executor over gen.
func NewExecutor(gen Generator) *Executor {
	return &Executor{Generator: gen}
}
