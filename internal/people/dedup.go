package people

import (
	"strings"

	"github.com/abedegno/muesli/internal/model"
)

// DedupByEmail removes duplicate attendees by normalized email address.
func DedupByEmail(attendees []model.Attendee) []model.Attendee {
	seen := make(map[string]int, len(attendees))
	out := make([]model.Attendee, 0, len(attendees))

	for _, attendee := range attendees {
		key := strings.ToLower(strings.TrimSpace(attendee.Email))
		if key == "" {
			continue
		}

		if idx, ok := seen[key]; ok {
			if out[idx].Name == "" && attendee.Name != "" {
				out[idx].Name = attendee.Name
			}
			continue
		}

		seen[key] = len(out)
		out = append(out, attendee)
	}

	return out
}
