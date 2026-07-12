package notelinks

import "strings"

// ParseMentions extracts note titles referenced as [[title]] in body.
//
// A literal opening delimiter can be escaped by placing a backslash directly
// before it, for example `\[[not a mention]]`. Escaped openers are skipped and
// never returned as mentions.
func ParseMentions(body string) []string {
	var mentions []string
	for i := 0; i < len(body)-1; {
		if body[i] == '\\' && i+2 < len(body) && body[i+1] == '[' && body[i+2] == '[' {
			i += 3
			continue
		}
		if body[i] == '[' && body[i+1] == '[' {
			closeIdx := strings.Index(body[i+2:], "]]")
			if closeIdx < 0 {
				i += 2
				continue
			}
			title := strings.TrimSpace(body[i+2 : i+2+closeIdx])
			if title != "" {
				mentions = append(mentions, title)
			}
			i += closeIdx + 4
			continue
		}
		i++
	}
	return mentions
}
