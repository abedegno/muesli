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
		options   Options
		want      string
	}{
		{
			name:    "title only",
			note:    model.Note{Title: "Weekly Sync"},
			options: Options{IncludeTranscript: true},
			want:    "# Weekly Sync\n\n## Transcript",
		},
		{
			name:    "multi-section summary",
			note:    model.Note{Title: "Planning"},
			options: Options{IncludeTranscript: true},
			summaries: []model.SummarySection{
				{Heading: "Overview", ContentMarkdown: "It was a meeting."},
				{Heading: "Decisions", ContentMarkdown: "- Ship it"},
			},
			want: "# Planning\n\n## Overview\nIt was a meeting.\n\n## Decisions\n- Ship it\n\n## Transcript",
		},
		{
			name:    "speaker attributed transcript",
			note:    model.Note{Title: "Standup"},
			options: Options{IncludeTranscript: true},
			segments: []model.Segment{
				{Text: "Hello", Speaker: "SPEAKER_00"},
				{Text: "Hi there"},
			},
			aliases: map[string]string{"SPEAKER_00": "Alice"},
			want:    "# Standup\n\n## Transcript\nAlice: Hello\nHi there",
		},
		{
			name:    "redacted transcript",
			note:    model.Note{Title: "Standup"},
			options: Options{IncludeTranscript: true, RedactSpeakers: true},
			segments: []model.Segment{
				{Text: "Hello", Speaker: "SPEAKER_10"},
				{Text: "No speaker here."},
				{Text: "Hi there", Speaker: "SPEAKER_20"},
				{Text: "Again", Speaker: "SPEAKER_10"},
			},
			aliases: map[string]string{"SPEAKER_10": "Alice", "SPEAKER_20": "Bob"},
			want:    "# Standup\n\n## Transcript\nSpeaker 1: Hello\nNo speaker here.\nSpeaker 2: Hi there\nSpeaker 1: Again",
		},
		{
			name:    "transcript omitted",
			note:    model.Note{Title: "Draft"},
			options: Options{IncludeTranscript: false},
			summaries: []model.SummarySection{
				{Heading: "Overview", ContentMarkdown: "Summary"},
			},
			segments: []model.Segment{
				{Text: "Working note", Speaker: "SPEAKER_00"},
			},
			aliases: map[string]string{"SPEAKER_00": "Alice"},
			want:    "# Draft\n\n## Overview\nSummary",
		},
		{
			name:    "not ready note omits summaries",
			note:    model.Note{Title: "Draft"},
			options: Options{IncludeTranscript: true},
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
			got := RenderNoteMarkdown(tc.note, tc.summaries, tc.segments, tc.aliases, tc.options)
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
