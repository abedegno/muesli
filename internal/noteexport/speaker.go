package noteexport

import (
	"fmt"

	"github.com/abedegno/muesli/internal/model"
)

func transcriptSpeakerAliases(segments []model.Segment, aliases map[string]string, options Options) map[string]string {
	if !options.RedactSpeakers {
		return aliases
	}

	out := make(map[string]string)
	next := 1
	for _, segment := range segments {
		if segment.Speaker == "" {
			continue
		}
		if _, ok := out[segment.Speaker]; ok {
			continue
		}
		out[segment.Speaker] = fmt.Sprintf("Speaker %d", next)
		next++
	}
	return out
}
