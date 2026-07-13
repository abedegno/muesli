package noteexport

import "github.com/abedegno/muesli/internal/model"

func exportTestFixture() (model.Note, []model.SummarySection, []model.Segment, map[string]string) {
	return model.Note{Title: "Planning Review"},
		[]model.SummarySection{
			{
				Heading:         "Overview",
				ContentMarkdown: "First paragraph.\n\nSecond line of the same section.",
			},
		},
		[]model.Segment{
			{Speaker: "SPEAKER_00", Text: "We should ship it."},
			{Speaker: "SPEAKER_01", Text: "Then we can announce it."},
			{Speaker: "SPEAKER_00", Text: "Agreed."},
		},
		map[string]string{"SPEAKER_00": "Alice", "SPEAKER_01": "Bob"}
}
