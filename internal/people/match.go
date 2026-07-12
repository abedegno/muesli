package people

import (
	"strings"
	"unicode"

	"github.com/abedegno/muesli/internal/model"
	"golang.org/x/text/unicode/norm"
)

var nicknameFold = map[string]string{
	"bob":  "robert",
	"bill": "william",
	"mike": "michael",
	"liz":  "elizabeth",
	"jim":  "james",
}

func MatchPersonByName(name string, people []model.Person) (personID string, ok bool) {
	normalizedName := NormalizeName(name)
	if normalizedName == "" {
		return "", false
	}

	var matchCount int
	var matchedID string
	for _, person := range people {
		if NormalizeName(person.DisplayName) != normalizedName {
			continue
		}

		matchCount++
		matchedID = person.ID
		if matchCount > 1 {
			return "", false
		}
	}

	if matchCount != 1 {
		return "", false
	}

	return matchedID, true
}

func NormalizeName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}

	s = stripLeadingTitle(s)
	s = reorderCommaName(s)
	s = stripLeadingTitle(s)
	s = foldDiacritics(s)
	s = applyNicknameFold(s)
	s = strings.Join(strings.Fields(s), " ")

	return strings.TrimSpace(s)
}

func stripLeadingTitle(s string) string {
	for _, title := range []string{"dr", "mr", "mrs", "ms", "prof"} {
		switch {
		case s == title:
			return ""
		case strings.HasPrefix(s, title+"."):
			return strings.TrimLeft(s[len(title)+1:], " \t\r\n")
		case strings.HasPrefix(s, title):
			rest := s[len(title):]
			if rest != "" && (rest[0] == ' ' || rest[0] == '\t' || rest[0] == '\n' || rest[0] == '\r') {
				return strings.TrimLeft(rest, " \t\r\n")
			}
		}
	}

	return s
}

func reorderCommaName(s string) string {
	first, second, found := strings.Cut(s, ",")
	if !found {
		return s
	}

	first = strings.TrimSpace(first)
	second = strings.TrimSpace(second)
	if first == "" || second == "" {
		return s
	}

	return second + " " + first
}

func foldDiacritics(s string) string {
	decomposed := norm.NFD.String(s)
	var b strings.Builder
	b.Grow(len(decomposed))
	for _, r := range decomposed {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func applyNicknameFold(s string) string {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return ""
	}

	if replacement, ok := nicknameFold[fields[0]]; ok {
		fields[0] = replacement
	}

	return strings.Join(fields, " ")
}
