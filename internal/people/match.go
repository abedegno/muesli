package people

import (
	"strings"

	"github.com/abedegno/muesli/internal/model"
)

func MatchPersonByName(name string, people []model.Person) (personID string, ok bool) {
	normalizedName := strings.ToLower(strings.TrimSpace(name))
	if normalizedName == "" {
		return "", false
	}

	var matchCount int
	var matchedID string
	for _, person := range people {
		if strings.ToLower(strings.TrimSpace(person.DisplayName)) != normalizedName {
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
