package noteexport

import (
	"bytes"
	"strings"

	"github.com/abedegno/muesli/internal/model"
	"github.com/gomutex/godocx"
)

// RenderNoteDocx renders one note as a DOCX document with a title, zero or
// more enhanced-summary sections, and an optional transcript section.
func RenderNoteDocx(note model.Note, summarySections []model.SummarySection, segments []model.Segment, aliases map[string]string, options Options) ([]byte, error) {
	doc, err := godocx.NewDocument()
	if err != nil {
		return nil, err
	}

	if _, err := doc.AddHeading(note.Title, 1); err != nil {
		return nil, err
	}

	for _, section := range summarySections {
		if _, err := doc.AddHeading(section.Heading, 2); err != nil {
			return nil, err
		}
		for _, para := range splitDocxParagraphs(section.ContentMarkdown) {
			doc.AddParagraph(para)
		}
	}

	if options.IncludeTranscript {
		speakerAliases := transcriptSpeakerAliases(segments, aliases, options)
		if _, err := doc.AddHeading("Transcript", 2); err != nil {
			return nil, err
		}
		for _, segment := range segments {
			doc.AddParagraph(renderTranscriptLine(segment, speakerAliases))
		}
	}

	var buf bytes.Buffer
	if _, err := doc.WriteTo(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func splitDocxParagraphs(content string) []string {
	content = strings.TrimSpace(strings.ReplaceAll(content, "\r\n", "\n"))
	if content == "" {
		return nil
	}

	blocks := strings.Split(content, "\n\n")
	out := make([]string, 0, len(blocks))
	for _, block := range blocks {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		lines := strings.Split(block, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line != "" {
				out = append(out, line)
			}
		}
	}
	return out
}
