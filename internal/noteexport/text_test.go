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
		opts      Options
		want      string
	}{
		{
			name: "title only",
			note: model.Note{Title: "Weekly Sync"},
			opts: Options{IncludeTranscript: true},
			want: "Weekly Sync\n\nTranscript",
		},
		{
			name: "multi-section summary",
			note: model.Note{Title: "Planning"},
			opts: Options{IncludeTranscript: true},
			summaries: []model.SummarySection{
				{Heading: "Overview", ContentMarkdown: "It was a meeting."},
				{Heading: "Decisions", ContentMarkdown: "- Ship it"},
			},
			want: "Planning\n\nOverview\nIt was a meeting.\n\nDecisions\n- Ship it\n\nTranscript",
		},
		{
			name: "speaker attributed transcript",
			note: model.Note{Title: "Standup"},
			opts: Options{IncludeTranscript: true},
			segments: []model.Segment{
				{Text: "Hello", Speaker: "SPEAKER_00"},
				{Text: "Hi there"},
			},
			aliases: map[string]string{"SPEAKER_00": "Alice"},
			want:    "Standup\n\nTranscript\nAlice: Hello\nHi there",
		},
		{
			name: "redacted transcript",
			note: model.Note{Title: "Standup"},
			opts: Options{IncludeTranscript: true, RedactSpeakers: true},
			segments: []model.Segment{
				{Text: "Hello", Speaker: "SPEAKER_01"},
				{Text: "Hi there", Speaker: "SPEAKER_00"},
				{Text: "Again", Speaker: "SPEAKER_01"},
				{Text: "No speaker"},
			},
			aliases: map[string]string{"SPEAKER_00": "Alice", "SPEAKER_01": "Bob"},
			want:    "Standup\n\nTranscript\nSpeaker 1: Hello\nSpeaker 2: Hi there\nSpeaker 1: Again\nNo speaker",
		},
		{
			name: "summary only omits transcript",
			note: model.Note{Title: "Draft"},
			opts: Options{IncludeTranscript: false},
			summaries: []model.SummarySection{
				{Heading: "Overview", ContentMarkdown: "It was a meeting."},
			},
			segments: []model.Segment{
				{Text: "Working note"},
			},
			want: "Draft\n\nOverview\nIt was a meeting.",
		},
		{
			name: "not ready note omits summaries",
			note: model.Note{Title: "Draft"},
			opts: Options{IncludeTranscript: true},
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
			got := RenderNoteText(tc.note, tc.summaries, tc.segments, tc.aliases, tc.opts)
			if got != tc.want {
				t.Fatalf("rendered text mismatch\nwant:\n%s\n\ngot:\n%s", tc.want, got)
			}
		})
	}
}
