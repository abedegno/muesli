package noteexport

import (
	"testing"

	"github.com/abedegno/muesli/internal/model"
)

func TestRenderNoteMarkdown(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		note      model.Note
		summaries []model.SummarySection
		segments  []model.Segment
		aliases   map[string]string
		want      string
	}{
		{
			name: "title only",
			note: model.Note{Title: "Weekly Sync"},
			want: "# Weekly Sync\n\n## Transcript",
		},
		{
			name: "multi-section summary",
			note: model.Note{Title: "Planning"},
			summaries: []model.SummarySection{
				{Heading: "Overview", ContentMarkdown: "It was a meeting."},
				{Heading: "Decisions", ContentMarkdown: "- Ship it"},
			},
			want: "# Planning\n\n## Overview\nIt was a meeting.\n\n## Decisions\n- Ship it\n\n## Transcript",
		},
		{
			name: "speaker attributed transcript",
			note: model.Note{Title: "Standup"},
			segments: []model.Segment{
				{Text: "Hello", Speaker: "SPEAKER_00"},
				{Text: "Hi there"},
			},
			aliases: map[string]string{"SPEAKER_00": "Alice"},
			want:    "# Standup\n\n## Transcript\nAlice: Hello\nHi there",
		},
		{
			name: "not ready note omits summaries",
			note: model.Note{Title: "Draft"},
			segments: []model.Segment{
				{Text: "Working note"},
			},
			want: "# Draft\n\n## Transcript\nWorking note",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := RenderNoteMarkdown(tc.note, tc.summaries, tc.segments, tc.aliases)
			if got != tc.want {
				t.Fatalf("rendered markdown mismatch\nwant:\n%s\n\ngot:\n%s", tc.want, got)
			}
		})
	}
}

func TestSlugifyFilename(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"Weekly Sync":     "weekly-sync",
		"  A/B: Plan  ":   "a-b-plan",
		"!!!":             "note",
		"Hello__World.md": "hello-world-md",
	}
	for in, want := range tests {
		if got := SlugifyFilename(in); got != want {
			t.Fatalf("SlugifyFilename(%q) = %q, want %q", in, got, want)
		}
	}
}
