package noteexport

import (
	"strconv"

	"github.com/abedegno/muesli/internal/model"
)

// Options controls note export rendering behavior.
type Options struct {
	IncludeTranscript bool
	RedactSpeakers    bool
}

func buildRedactedSpeakerAliases(segments []model.Segment) map[string]string {
	aliases := make(map[string]string)
	next := 1
	for _, segment := range segments {
		if segment.Speaker == "" {
			continue
		}
		if _, ok := aliases[segment.Speaker]; ok {
			continue
		}
		aliases[segment.Speaker] = "Speaker " + strconv.Itoa(next)
		next++
	}
	return aliases
}
