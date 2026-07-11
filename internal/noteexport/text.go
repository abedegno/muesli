package noteexport

import (
	"strings"

	"github.com/abedegno/muesli/internal/model"
)

// RenderNoteText renders one note as a plain-text document with a title,
// zero or more enhanced-summary sections, and a transcript section.
func RenderNoteText(note model.Note, summarySections []model.SummarySection, segments []model.Segment, aliases map[string]string) string {
	var b strings.Builder
	b.WriteString(note.Title)

	for _, section := range summarySections {
		b.WriteString("\n\n")
		b.WriteString(section.Heading)
		b.WriteString("\n")
		b.WriteString(section.ContentMarkdown)
	}

	b.WriteString("\n\nTranscript")
	for _, segment := range segments {
		b.WriteString("\n")
		b.WriteString(renderTranscriptLine(segment, aliases))
	}

	return b.String()
}
