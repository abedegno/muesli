package noteexport

import (
	"strings"

	"github.com/abedegno/muesli/internal/model"
)

// RenderNoteMarkdown renders one note as a Markdown document with a title,
// zero or more enhanced-summary sections, and an optional transcript section.
func RenderNoteMarkdown(note model.Note, summarySections []model.SummarySection, segments []model.Segment, aliases map[string]string, opts Options) string {
	return renderNoteMarkdown(note, summarySections, segments, aliases, opts)
}

func renderNoteMarkdown(note model.Note, summarySections []model.SummarySection, segments []model.Segment, aliases map[string]string, opts Options) string {
	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(note.Title)

	for _, section := range summarySections {
		b.WriteString("\n\n## ")
		b.WriteString(section.Heading)
		b.WriteString("\n")
		b.WriteString(section.ContentMarkdown)
	}

	if opts.IncludeTranscript {
		b.WriteString("\n\n## Transcript")
		redacted := map[string]string(nil)
		if opts.RedactSpeakers {
			redacted = buildRedactedSpeakerLabels(segments)
		}
		for _, segment := range segments {
			b.WriteString("\n")
			b.WriteString(renderTranscriptLine(segment, aliases, opts, redacted))
		}
	}

	return b.String()
}

// SlugifyFilename converts a note title into a filesystem-safe attachment
// filename stem.
func SlugifyFilename(title string) string {
	return slugifyFilename(title)
}

func slugifyFilename(title string) string {
	title = strings.ToLower(strings.TrimSpace(title))
	if title == "" {
		return "note"
	}

	var b strings.Builder
	prevDash := false
	for _, r := range title {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
			prevDash = false
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}

	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "note"
	}
	return out
}
