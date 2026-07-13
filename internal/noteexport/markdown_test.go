package noteexport

import (
	"testing"

	"github.com/abedegno/muesli/internal/model"
)

func TestRenderNoteMarkdown(t *testing.T) {
	t.Parallel()

	note, summaries, segments, aliases := exportTestFixture()

	tests := []struct {
		name      string
		note      model.Note
		summaries []model.SummarySection
		segments  []model.Segment
		aliases   map[string]string
		opts      Options
		want      string
	}{
		{
			name:     "redacted transcript is stable",
			note:     note,
			segments: segments,
			aliases:  aliases,
			opts:     Options{IncludeTranscript: true, RedactSpeakers: true},
			want:     "# Planning Review\n\n## Transcript\nSpeaker 1: We should ship it.\nSpeaker 2: Then we can announce it.\nSpeaker 1: Agreed.",
		},
		{
			name:      "summary is preserved when transcript is omitted",
			note:      note,
			summaries: summaries,
			segments:  segments,
			aliases:   aliases,
			opts:      Options{IncludeTranscript: false},
			want:      "# Planning Review\n\n## Overview\nFirst paragraph.\n\nSecond line of the same section.",
		},
		{
			name:      "summary remains present with transcript enabled",
			note:      note,
			summaries: summaries,
			segments:  segments,
			aliases:   aliases,
			opts:      Options{IncludeTranscript: true},
			want:      "# Planning Review\n\n## Overview\nFirst paragraph.\n\nSecond line of the same section.\n\n## Transcript\nAlice: We should ship it.\nBob: Then we can announce it.\nAlice: Agreed.",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := RenderNoteMarkdown(tc.note, tc.summaries, tc.segments, tc.aliases, tc.opts)
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
