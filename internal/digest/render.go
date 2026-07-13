package digest

import (
	"strings"
	"time"
)

// Render formats a digest as a plain text body.
func Render(d Digest) string {
	var b strings.Builder

	b.WriteString("Digest")
	b.WriteString("\n")
	b.WriteString("Window: ")
	b.WriteString(formatTime(d.WindowFrom))
	b.WriteString(" to ")
	b.WriteString(formatTime(d.WindowTo))

	b.WriteString("\n\nRecent Meetings")
	if len(d.RecentMeetings) == 0 {
		b.WriteString("\nNo recent meetings.")
	} else {
		for _, note := range d.RecentMeetings {
			b.WriteString("\n- ")
			b.WriteString(note.Title)
			b.WriteString(" (")
			b.WriteString(formatTime(noteEffectiveTime(note)))
			b.WriteString(")")
		}
	}

	b.WriteString("\n\nOpen Action Items")
	if len(d.OpenActionItems) == 0 {
		b.WriteString("\nNo open action items.")
	} else {
		for _, item := range d.OpenActionItems {
			b.WriteString("\n- ")
			b.WriteString(item.Text)
			if item.DueHint != "" {
				b.WriteString(" [")
				b.WriteString(item.DueHint)
				b.WriteString("]")
			}
		}
	}

	b.WriteString("\n\nNeeds Follow-up")
	if len(d.NeedsFollowUp) == 0 {
		b.WriteString("\nNo items need follow-up.")
	} else {
		for _, item := range d.NeedsFollowUp {
			b.WriteString("\n- ")
			b.WriteString(item.Text)
			if item.DueHint != "" {
				b.WriteString(" [")
				b.WriteString(item.DueHint)
				b.WriteString("]")
			}
		}
	}

	return b.String()
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}
