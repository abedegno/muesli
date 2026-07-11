package noteexport

import (
	"bytes"
	"strings"

	"github.com/abedegno/muesli/internal/model"
	"github.com/go-pdf/fpdf"
)

// RenderNotePdf renders one note as a PDF document with a title, zero or more
// enhanced-summary sections, and a transcript section.
func RenderNotePdf(note model.Note, summarySections []model.SummarySection, segments []model.Segment, aliases map[string]string) ([]byte, error) {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(18, 18, 18)
	pdf.SetAutoPageBreak(true, 18)
	pdf.SetCompression(true)

	pdf.AddPage()

	writeBlock := func(text string, size float64) {
		pdf.SetFont("Helvetica", "", size)
		pdf.MultiCell(0, size*0.6, text, "", "L", false)
	}

	writeParagraph := func(text string) {
		pdf.SetFont("Helvetica", "", 11)
		pdf.MultiCell(0, 5, text, "", "L", false)
	}

	writeBlock(note.Title, 18)
	pdf.Ln(3)

	for _, section := range summarySections {
		writeBlock(section.Heading, 13)
		pdf.Ln(1)
		for _, paragraph := range splitPdfParagraphs(section.ContentMarkdown) {
			writeParagraph(paragraph)
		}
		pdf.Ln(2)
	}

	writeBlock("Transcript", 13)
	pdf.Ln(1)
	for _, segment := range segments {
		writeParagraph(renderTranscriptLine(segment, aliases))
	}

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func splitPdfParagraphs(content string) []string {
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
