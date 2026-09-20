package execution

import (
	"context"
	"errors"
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
