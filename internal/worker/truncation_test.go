package worker_test

import (
	"testing"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/plugin"
	"github.com/abedegno/muesli/internal/worker"
)

func TestDetectTruncation(t *testing.T) {
	sections := func(texts ...string) []model.SummarySection {
		var out []model.SummarySection
		for i, txt := range texts {
			out = append(out, model.SummarySection{Heading: "H", ContentMarkdown: txt, Refs: []int{i}})
		}
		return out
	}

	tests := []struct {
		name     string
		sections []model.SummarySection
		usage    *plugin.GenerateUsage
		want     bool
	}{
		{
			name:     "usage at/above 95% threshold flags truncated even with terminal punctuation",
			sections: sections("It happened."),
			usage:    &plugin.GenerateUsage{TokensUsed: 950, MaxTokens: 1000},
			want:     true,
		},
		{
			name:     "usage comfortably under threshold with terminal punctuation is not truncated",
			sections: sections("It happened."),
			usage:    &plugin.GenerateUsage{TokensUsed: 100, MaxTokens: 1000},
			want:     false,
		},
		{
			name:     "nil usage, text not ending in terminal punctuation is truncated",
			sections: sections("It was going great and then the summary just stops mid"),
			usage:    nil,
			want:     true,
		},
		{
			name:     "nil usage, text properly terminated is not truncated",
			sections: sections("Overview here.", "Decisions: postpone the launch!"),
			usage:    nil,
			want:     false,
		},
		{
			name:     "empty text is not truncated even with nil usage",
			sections: sections("   ", ""),
			usage:    nil,
			want:     false,
		},
		{
			name:     "zero MaxTokens usage is ignored (falls through to text heuristic)",
			sections: sections("Ends properly)"),
			usage:    &plugin.GenerateUsage{TokensUsed: 999, MaxTokens: 0},
			want:     false,
		},
		{
			name:     "trailing whitespace after terminal punctuation is trimmed before checking",
			sections: sections("All good.   \n\t"),
			usage:    nil,
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := worker.DetectTruncation(tt.sections, tt.usage)
			if got != tt.want {
				t.Fatalf("DetectTruncation() = %v, want %v", got, tt.want)
			}
		})
	}
}
