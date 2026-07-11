package calendar

import "regexp"

var confRe = regexp.MustCompile(`https?://(?:[a-z0-9-]+\.)*(?:zoom\.us|meet\.google\.com|teams\.microsoft\.com)/[^\s"'<>]+`)

// ExtractConferencingURL returns the first Zoom/Meet/Teams URL found in native,
// then location, then description; "" if none.
func ExtractConferencingURL(location, description, native string) string {
	for _, s := range []string{native, location, description} {
		if m := confRe.FindString(s); m != "" {
			return m
		}
	}
	return ""
}
