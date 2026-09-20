package execution

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/plugin"
)

// fakeGenerator captures the last request it received and returns a
// scripted response (or error).
type fakeGenerator struct {
	lastReq plugin.GenerateRequest
	calls   int
	resp    plugin.GenerateResponse
	err     error
}

func (f *fakeGenerator) Generate(_ context.Context, req plugin.GenerateRequest) (plugin.GenerateResponse, error) {
	f.calls++
	f.lastReq = req
	return f.resp, f.err
}

func twoSectionTemplate() model.Template {
	return model.Template{Sections: []model.TemplateSection{
		{Heading: "Decisions", Instruction: "List decisions."},
		{Heading: "Risks", Instruction: "List unresolved risks."},
	}}
}

func prepared2Docs(t *testing.T) PreparedExecution {
	t.Helper()
	prepared, err := PrepareDocuments(twoDocInput())
	if err != nil {
		t.Fatalf("PrepareDocuments: %v", err)
	}
	return prepared
}

// TestRunOneDocumentCompatibility proves Run works with a single document
// exactly like the existing one-note path: one call, request carries exactly
// one plugin.Document.
func TestRunOneDocumentCompatibility(t *testing.T) {
	gen := &fakeGenerator{resp: plugin.GenerateResponse{
		Summary: plugin.SummaryPayload{Sections: []model.SummarySection{{Heading: "Overview", ContentMarkdown: "done [1]"}}},
		Model:   "m1",
	}}
	input := ExecutionInput{
		Template:  model.Template{Sections: []model.TemplateSection{{Heading: "Overview", Instruction: "Summarize."}}},
		Documents: []Document{{NoteID: "note-1", Segments: []model.Segment{{StartMS: 0, Text: "hi"}}}},
	}
	prepared, err := PrepareDocuments(input)
	if err != nil {
		t.Fatalf("PrepareDocuments: %v", err)
	}
	res, err := NewExecutor(gen).Run(context.Background(), prepared)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if gen.calls != 1 {
		t.Fatalf("expected exactly 1 generator call, got %d", gen.calls)
	}
	if len(gen.lastReq.Documents) != 1 {
		t.Fatalf("expected exactly 1 document in request, got %d", len(gen.lastReq.Documents))
	}
	if len(res.Sections) != 1 || res.Sections[0].Heading != "Overview" {
		t.Fatalf("unexpected result sections: %+v", res.Sections)
	}
}

// TestRunForwardsOverridesAndBoundaries proves SystemPrompt/Model/Temperature
// overrides and every document's boundary metadata reach the plugin request
// unchanged.
func TestRunForwardsOverridesAndBoundaries(t *testing.T) {
	gen := &fakeGenerator{resp: plugin.GenerateResponse{
		Summary: plugin.SummaryPayload{Sections: []model.SummarySection{
			{Heading: "Decisions", ContentMarkdown: "d [1]"},
			{Heading: "Risks", ContentMarkdown: "r [3]"},
		}},
		Model: "override-model",
		Usage: &plugin.GenerateUsage{TokensUsed: 42, MaxTokens: 1000},
	}}
	input := twoDocInput()
	temp := 0.4
	input.Template = twoSectionTemplate()
	input.SystemPrompt = "custom system prompt"
	input.Model = "override-model"
	input.Temperature = &temp
	input.Config = []byte(`{"k":"v"}`)

	prepared, err := PrepareDocuments(input)
	if err != nil {
		t.Fatalf("PrepareDocuments: %v", err)
	}
	res, err := NewExecutor(gen).Run(context.Background(), prepared)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if gen.lastReq.SystemPrompt != "custom system prompt" || gen.lastReq.Model != "override-model" || gen.lastReq.Temperature == nil || *gen.lastReq.Temperature != 0.4 {
		t.Fatalf("overrides not forwarded: %+v", gen.lastReq)
	}
	if len(gen.lastReq.Documents) != 2 || gen.lastReq.Documents[0].NoteID != "note-1" || gen.lastReq.Documents[1].NoteID != "note-2" {
		t.Fatalf("document boundaries not preserved: %+v", gen.lastReq.Documents)
	}
	if res.TokensUsed == nil || *res.TokensUsed != 42 {
		t.Fatalf("expected tokens used 42, got %+v", res.TokensUsed)
	}
	if len(res.Sources) != 3 {
		t.Fatalf("expected 3 sources carried through, got %d", len(res.Sources))
	}
}

func TestRunRejectsMismatchedSections(t *testing.T) {
	cases := []struct {
		name string
		got  []model.SummarySection
	}{
		{"missing", []model.SummarySection{{Heading: "Decisions", ContentMarkdown: "d"}}},
		{"extra", []model.SummarySection{
			{Heading: "Decisions", ContentMarkdown: "d"}, {Heading: "Risks", ContentMarkdown: "r"}, {Heading: "Extra", ContentMarkdown: "e"},
		}},
		{"renamed", []model.SummarySection{{Heading: "Decisions!", ContentMarkdown: "d"}, {Heading: "Risks", ContentMarkdown: "r"}}},
		{"reordered", []model.SummarySection{{Heading: "Risks", ContentMarkdown: "r"}, {Heading: "Decisions", ContentMarkdown: "d"}}},
		{"duplicate", []model.SummarySection{{Heading: "Decisions", ContentMarkdown: "d"}, {Heading: "Decisions", ContentMarkdown: "d2"}}},
		{"empty content", []model.SummarySection{{Heading: "Decisions", ContentMarkdown: "  "}, {Heading: "Risks", ContentMarkdown: "r"}}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			gen := &fakeGenerator{resp: plugin.GenerateResponse{Summary: plugin.SummaryPayload{Sections: tc.got}, Model: "m"}}
			prepared := prepared2Docs(t)
			prepared.Input.Template = twoSectionTemplate()
			_, err := NewExecutor(gen).Run(context.Background(), prepared)
			if err == nil {
				t.Fatal("expected an error, got nil")
			}
			if !errors.Is(err, ErrSectionMismatch) && !errors.Is(err, ErrEmptyOutput) {
				t.Fatalf("expected ErrSectionMismatch or ErrEmptyOutput, got %v", err)
			}
		})
	}
}

func TestRunPropagatesGeneratorError(t *testing.T) {
	gen := &fakeGenerator{err: errors.New("boom")}
	prepared := prepared2Docs(t)
	prepared.Input.Template = twoSectionTemplate()
	if _, err := NewExecutor(gen).Run(context.Background(), prepared); err == nil {
		t.Fatal("expected an error, got nil")
	}
}

// TestPrepareDocumentsSingleNoteModeNoCrossMeetingFraming proves
// ModeSingleNoteSummary's corpus is exactly the single document's own
// NotesMarkdown -- no multi-meeting directive, meeting delimiters, citation
// numbering, or focus/citation framing -- unlike the default
// ModeCrossAnalysis corpus (see TestPrepareDocumentsOneDocumentCompatibility,
// which proves the OPPOSITE for the same one-document shape under the
// default mode).
func TestPrepareDocumentsSingleNoteModeNoCrossMeetingFraming(t *testing.T) {
	const rawNotes = "- discussed Q3 roadmap\n- alice: ship by Friday"
	input := ExecutionInput{
		Template: model.Template{Sections: []model.TemplateSection{{Heading: "Overview", Instruction: "Summarize."}}},
		Mode:     ModeSingleNoteSummary,
		Documents: []Document{{
			NoteID:        "note-1",
			Segments:      []model.Segment{{StartMS: 0, EndMS: 100, Text: "hello", Speaker: "Alice"}},
			NotesMarkdown: rawNotes,
		}},
	}
	prepared, err := PrepareDocuments(input)
	if err != nil {
		t.Fatalf("PrepareDocuments: %v", err)
	}
	if prepared.Corpus != rawNotes {
		t.Fatalf("corpus = %q, want exactly the document's own NotesMarkdown %q", prepared.Corpus, rawNotes)
	}
	for _, forbidden := range []string{
		"MEETING", "analyzing multiple distinct meetings", "CITATIONS:", "FOCUS:", SystemDirective,
	} {
		if strings.Contains(prepared.Corpus, forbidden) {
			t.Fatalf("corpus unexpectedly contains cross-meeting-only text %q:\n%s", forbidden, prepared.Corpus)
		}
	}
	if len(prepared.SectionPrompts) != 0 {
		t.Fatalf("expected no section prompts computed for ModeSingleNoteSummary (Admit is never called for it), got %+v", prepared.SectionPrompts)
	}
}

// TestPrepareDocumentsSingleNoteModeRequiresExactlyOneDocument proves
// ModeSingleNoteSummary rejects anything but exactly one document.
func TestPrepareDocumentsSingleNoteModeRequiresExactlyOneDocument(t *testing.T) {
	input := twoDocInput()
	input.Mode = ModeSingleNoteSummary
	if _, err := PrepareDocuments(input); !errors.Is(err, ErrSingleNoteModeRequiresOneDocument) {
		t.Fatalf("got %v, want ErrSingleNoteModeRequiresOneDocument", err)
	}
}

// TestRunSingleNoteModeSendsTranscriptNotDocuments proves the wire request
// Run sends for ModeSingleNoteSummary matches the pre-#765 direct
// plugin.Client.Generate call byte-for-byte in shape: Transcript carries the
// document's segments, NotesMarkdown carries the document's own raw note
// body verbatim (no cross-meeting framing), and Documents is never set.
func TestRunSingleNoteModeSendsTranscriptNotDocuments(t *testing.T) {
	const rawNotes = "- discussed Q3 roadmap"
	segs := []model.Segment{{StartMS: 0, EndMS: 100, Text: "hello", Speaker: "Alice"}}
	gen := &fakeGenerator{resp: plugin.GenerateResponse{
		Summary: plugin.SummaryPayload{Sections: []model.SummarySection{{Heading: "Overview", ContentMarkdown: "done"}}},
		Model:   "m1",
	}}
	input := ExecutionInput{
		Template:     model.Template{Sections: []model.TemplateSection{{Heading: "Overview", Instruction: "Summarize."}}},
		Mode:         ModeSingleNoteSummary,
		Documents:    []Document{{NoteID: "note-1", Segments: segs, NotesMarkdown: rawNotes}},
		SystemPrompt: "custom system prompt",
	}
	prepared, err := PrepareDocuments(input)
	if err != nil {
		t.Fatalf("PrepareDocuments: %v", err)
	}
	if _, err := NewExecutor(gen).Run(context.Background(), prepared); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if gen.lastReq.Documents != nil {
		t.Fatalf("expected Documents unset for ModeSingleNoteSummary, got %+v", gen.lastReq.Documents)
	}
	if len(gen.lastReq.Transcript) != 1 || gen.lastReq.Transcript[0].Text != "hello" {
		t.Fatalf("expected Transcript to carry the document's segments, got %+v", gen.lastReq.Transcript)
	}
	if gen.lastReq.NotesMarkdown != rawNotes {
		t.Fatalf("NotesMarkdown = %q, want the document's own raw note body %q", gen.lastReq.NotesMarkdown, rawNotes)
	}
	if gen.lastReq.SystemPrompt != "custom system prompt" {
		t.Fatalf("expected SystemPrompt forwarded, got %q", gen.lastReq.SystemPrompt)
	}
}

// TestRunSingleNoteModeTrustsPluginSectionsWithoutValidation proves
// ModeSingleNoteSummary does NOT apply ModeCrossAnalysis's strict
// section-count/heading/order/emptiness validation: whatever sections the
// plugin returns are trusted as-is and returned in Result.Sections, exactly
// like the pre-refactor worker code (which persisted whatever
// resp.Summary.Sections was, with no matching against the requested
// template). Contrast with TestRunRejectsMismatchedSections, which proves
// the OPPOSITE for the same mismatched shapes under the default mode.
func TestRunSingleNoteModeTrustsPluginSectionsWithoutValidation(t *testing.T) {
	mismatched := []model.SummarySection{
		{Heading: "Unexpected Heading", ContentMarkdown: "d"},
		{Heading: "Unexpected Heading", ContentMarkdown: ""}, // duplicate heading AND empty content
	}
	gen := &fakeGenerator{resp: plugin.GenerateResponse{Summary: plugin.SummaryPayload{Sections: mismatched}, Model: "m"}}
	input := ExecutionInput{
		Template:  model.Template{Sections: []model.TemplateSection{{Heading: "Overview", Instruction: "Summarize."}}},
		Mode:      ModeSingleNoteSummary,
		Documents: []Document{{NoteID: "note-1", Segments: []model.Segment{{StartMS: 0, Text: "hi"}}}},
	}
	prepared, err := PrepareDocuments(input)
	if err != nil {
		t.Fatalf("PrepareDocuments: %v", err)
	}
	res, err := NewExecutor(gen).Run(context.Background(), prepared)
	if err != nil {
		t.Fatalf("Run: expected no error (loose section handling), got %v", err)
	}
	if len(res.Sections) != len(mismatched) || res.Sections[0].Heading != "Unexpected Heading" {
		t.Fatalf("expected the plugin's sections passed through unvalidated, got %+v", res.Sections)
	}
}
