package noteexport

import (
	"testing"

	"github.com/abedegno/muesli/internal/model"
)

func TestRenderNoteText(t *testing.T) {
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
			want: "Weekly Sync\n\nTranscript",
		},
		{
			name: "multi-section summary",
			note: model.Note{Title: "Planning"},
			summaries: []model.SummarySection{
				{Heading: "Overview", ContentMarkdown: "It was a meeting."},
				{Heading: "Decisions", ContentMarkdown: "- Ship it"},
			},
			want: "Planning\n\nOverview\nIt was a meeting.\n\nDecisions\n- Ship it\n\nTranscript",
		},
		{
			name: "speaker attributed transcript",
			note: model.Note{Title: "Standup"},
			segments: []model.Segment{
				{Text: "Hello", Speaker: "SPEAKER_00"},
				{Text: "Hi there"},
			},
			aliases: map[string]string{"SPEAKER_00": "Alice"},
			want:    "Standup\n\nTranscript\nAlice: Hello\nHi there",
		},
		{
			name: "not ready note omits summaries",
			note: model.Note{Title: "Draft"},
			segments: []model.Segment{
				{Text: "Working note"},
			},
			want: "Draft\n\nTranscript\nWorking note",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := RenderNoteText(tc.note, tc.summaries, tc.segments, tc.aliases)
			if got != tc.want {
				t.Fatalf("rendered text mismatch\nwant:\n%s\n\ngot:\n%s", tc.want, got)
			}
		})
	}
}
