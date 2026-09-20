// Package execution is the generalized template-running boundary introduced
// alongside issue #765's cross-meeting analysis: an ordered collection of
// documents (one per selected meeting) is rendered into a citation-numbered
// prompt corpus, admitted against a byte-conservative context budget, and
// executed through a single agent-plugin call whose response is validated
// section-by-section against the requesting template.
//
// This package does not itself talk to a real plugin over HTTP -- callers
// supply a Generator (satisfied by *plugin.Client in production) so tests can
// inject deterministic fakes. It also performs no store/DB access: callers
// (internal/api's cross-analysis handlers) are responsible for loading
// documents, verifying transcript generations immediately before Run, and
// persisting the result.
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

// ExecutionInput is the canonical input to PrepareDocuments/Run.
type ExecutionInput struct {
	Template model.Template
	// Documents are in caller-supplied (request) order; PrepareDocuments
	// preserves it exactly -- this package never reorders or samples.
	Documents []Document
	// RunInstruction is the optional, user-supplied run-specific focus/
	// question. Never mutates the template's own section instructions --
	// see prompt.go's buildCorpus.
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

// PrepareDocuments renders the canonical prompt corpus, assigns stable
// citation numbers to every segment of every document (in document, then
// segment, order -- deduplicated by note-ID+segment-index, though within one
// call that key is already unique per document), and records each
// document's transcript generation. It performs no I/O.
func PrepareDocuments(input ExecutionInput) (PreparedExecution, error) {
	if len(input.Documents) == 0 {
		return PreparedExecution{}, ErrEmptyDocuments
	}
	if len(input.Template.Sections) == 0 {
		return PreparedExecution{}, ErrEmptyTemplate
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

	corpus := buildCorpus(input, sources)
	sectionPrompts := make([]SectionPrompt, 0, len(input.Template.Sections))
	for _, sec := range input.Template.Sections {
		sectionPrompts = append(sectionPrompts, SectionPrompt{
			Heading: sec.Heading,
			Prompt:  buildSectionPrompt(corpus, sec),
		})
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

// Run executes every template section from prepared through gen in a single
// plugin call, exactly like the single-note (existing) path: Documents plus
// the shared corpus (as NotesMarkdown) plus every template section
// structurally, forwarding SystemPrompt/Model/Temperature unchanged. It
// fails the whole run on a timeout/cancellation (gen returns an error),
// malformed output (section count/heading/order mismatch), or any empty
// section -- there is no partial success.
func (e *Executor) Run(ctx context.Context, prepared PreparedExecution) (Result, error) {
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

	wantSections := make([]model.TemplateSection, len(prepared.Input.Template.Sections))
	copy(wantSections, prepared.Input.Template.Sections)

	req := plugin.GenerateRequest{
		Documents:     docs,
		NotesMarkdown: prepared.Corpus,
		Template:      plugin.TemplatePayload{Sections: wantSections},
		Config:        prepared.Input.Config,
		SystemPrompt:  prepared.Input.SystemPrompt,
		Model:         prepared.Input.Model,
		Temperature:   prepared.Input.Temperature,
	}

	resp, err := e.Generator.Generate(ctx, req)
	if err != nil {
		return Result{}, fmt.Errorf("execution: generate: %w", err)
	}

	sections, err := validateSections(wantSections, resp.Summary.Sections)
	if err != nil {
		return Result{}, err
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
