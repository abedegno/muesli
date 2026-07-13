package noteexport

import (
	"strings"

	"github.com/abedegno/muesli/internal/model"
)

// RenderNoteMarkdown renders one note as a Markdown document with a title,
// zero or more enhanced-summary sections, and an optional transcript section.
func RenderNoteMarkdown(note model.Note, summarySections []model.SummarySection, segments []model.Segment, aliases map[string]string, options Options) string {
	return renderNoteMarkdown(note, summarySections, segments, aliases, options)
}

func renderNoteMarkdown(note model.Note, summarySections []model.SummarySection, segments []model.Segment, aliases map[string]string, options Options) string {
	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(note.Title)

	for _, section := range summarySections {
		b.WriteString("\n\n## ")
		b.WriteString(section.Heading)
		b.WriteString("\n")
		b.WriteString(section.ContentMarkdown)
	}

	if options.IncludeTranscript {
		speakerAliases := transcriptSpeakerAliases(segments, aliases, options)
		b.WriteString("\n\n## Transcript")
		for _, segment := range segments {
			b.WriteString("\n")
			b.WriteString(renderTranscriptLine(segment, speakerAliases))
		}
	}

	return b.String()
}

func renderTranscriptLine(segment model.Segment, aliases map[string]string) string {
	speaker := segment.Speaker
	if aliases != nil {
		if alias, ok := aliases[speaker]; ok && alias != "" {
			speaker = alias
		}
	}
	if speaker != "" {
		return speaker + ": " + segment.Text
	}
	return segment.Text
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
