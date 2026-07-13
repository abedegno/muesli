package noteexport

import (
	"testing"

	"github.com/abedegno/muesli/internal/model"
)

func TestRenderNoteText(t *testing.T) {
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
			want:     "Planning Review\n\nTranscript\nSpeaker 1: We should ship it.\nSpeaker 2: Then we can announce it.\nSpeaker 1: Agreed.",
		},
		{
			name:      "summary is preserved when transcript is omitted",
			note:      note,
			summaries: summaries,
			segments:  segments,
			aliases:   aliases,
			opts:      Options{IncludeTranscript: false},
			want:      "Planning Review\n\nOverview\nFirst paragraph.\n\nSecond line of the same section.",
		},
		{
			name:      "summary remains present with transcript enabled",
			note:      note,
			summaries: summaries,
			segments:  segments,
			aliases:   aliases,
			opts:      Options{IncludeTranscript: true},
			want:      "Planning Review\n\nOverview\nFirst paragraph.\n\nSecond line of the same section.\n\nTranscript\nAlice: We should ship it.\nBob: Then we can announce it.\nAlice: Agreed.",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := RenderNoteText(tc.note, tc.summaries, tc.segments, tc.aliases, tc.opts)
			if got != tc.want {
				t.Fatalf("rendered text mismatch\nwant:\n%s\n\ngot:\n%s", tc.want, got)
			}
		})
	}
}
