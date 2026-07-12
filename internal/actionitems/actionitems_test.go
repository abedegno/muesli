package actionitems

import (
	"context"
	"strings"
	"testing"

	"github.com/abedegno/muesli/internal/model"
)

type stubGenerator struct {
	prompt string
	raw    string
	err    error
}

func (s *stubGenerator) Generate(_ context.Context, prompt string) (string, error) {
	s.prompt = prompt
	return s.raw, s.err
}

func fixtureInput() Input {
	return Input{
		Transcript: []model.Segment{
			{Text: "We need to ship the doc by Friday."},
			{Text: "We also agreed to keep the weekly cadence."},
		},
		Summary: []model.SummarySection{
			{Heading: "Decisions", ContentMarkdown: "Ship the doc by Friday."},
		},
	}
}

func TestParseResponse(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want Result
	}{
		{
			name: "bullet list",
			raw: `
- [ ] Alice: ship the doc by Friday
- Decision: use the weekly cadence
`,
			want: Result{
				ActionItems: []ActionItem{{Text: "ship the doc", Owner: "Alice", DueHint: "Friday"}},
				Decisions:   []Decision{{Text: "use the weekly cadence"}},
			},
		},
		{
			name: "json",
			raw:  `{"action_items":[{"text":"ship the doc","owner":"Alice","due_hint":"Friday"}],"decisions":[{"text":"use the weekly cadence"}]}`,
			want: Result{
				ActionItems: []ActionItem{{Text: "ship the doc", Owner: "Alice", DueHint: "Friday"}},
				Decisions:   []Decision{{Text: "use the weekly cadence"}},
			},
		},
		{
			name: "empty",
			raw:  "No action items or decisions.",
			want: Result{
				ActionItems: []ActionItem{},
				Decisions:   []Decision{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseResponse(tt.raw)
			if len(got.ActionItems) != len(tt.want.ActionItems) {
				t.Fatalf("action items = %d, want %d: %+v", len(got.ActionItems), len(tt.want.ActionItems), got)
			}
			if len(got.Decisions) != len(tt.want.Decisions) {
				t.Fatalf("decisions = %d, want %d: %+v", len(got.Decisions), len(tt.want.Decisions), got)
			}
			if got.ActionItems == nil || got.Decisions == nil {
				t.Fatalf("slices must be non-nil: %+v", got)
			}
			if len(got.ActionItems) > 0 && got.ActionItems[0] != tt.want.ActionItems[0] {
				t.Fatalf("action item = %+v, want %+v", got.ActionItems[0], tt.want.ActionItems[0])
			}
			if len(got.Decisions) > 0 && got.Decisions[0] != tt.want.Decisions[0] {
				t.Fatalf("decision = %+v, want %+v", got.Decisions[0], tt.want.Decisions[0])
			}
		})
	}
}

func TestExtractUsesGeneratorAndParsesResponse(t *testing.T) {
	input := fixtureInput()
	gen := &stubGenerator{
		raw: `{"action_items":[{"text":"ship the doc","owner":"Alice","due_hint":"Friday"}],"decisions":[{"text":"use the weekly cadence"}]}`,
	}
	svc := New(gen)

	got, err := svc.Extract(context.Background(), input)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(got.ActionItems) != 1 || got.ActionItems[0].Owner != "Alice" {
		t.Fatalf("action items = %+v", got.ActionItems)
	}
	if len(got.Decisions) != 1 || got.Decisions[0].Text != "use the weekly cadence" {
		t.Fatalf("decisions = %+v", got.Decisions)
	}
	if gen.prompt == "" {
		t.Fatal("generator prompt was empty")
	}
	if !strings.Contains(gen.prompt, "We need to ship the doc by Friday.") {
		t.Fatalf("prompt missing transcript: %s", gen.prompt)
	}
	if !strings.Contains(gen.prompt, "Decisions: Ship the doc by Friday.") {
		t.Fatalf("prompt missing summary: %s", gen.prompt)
	}
}
