package noteexport

import (
	"fmt"

	"github.com/abedegno/muesli/internal/model"
)

// Options controls export rendering features shared across all formats.
type Options struct {
	IncludeTranscript bool
	RedactSpeakers    bool
}

func buildRedactedSpeakerLabels(segments []model.Segment) map[string]string {
	labels := make(map[string]string)
	next := 1
	for _, segment := range segments {
		if segment.Speaker == "" {
			continue
		}
		if _, ok := labels[segment.Speaker]; ok {
			continue
		}
		labels[segment.Speaker] = fmt.Sprintf("Speaker %d", next)
		next++
	}
	return labels
}

func renderTranscriptLine(segment model.Segment, aliases map[string]string, opts Options, redacted map[string]string) string {
	speaker := segment.Speaker
	if opts.RedactSpeakers {
		if speaker != "" {
			if label, ok := redacted[speaker]; ok {
				speaker = label
			}
		}
	} else if aliases != nil {
		if alias, ok := aliases[speaker]; ok && alias != "" {
			speaker = alias
		}
	}
	if speaker != "" {
		return speaker + ": " + segment.Text
	}
	return segment.Text
}
