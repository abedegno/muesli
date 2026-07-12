package people

import (
	"testing"

	"github.com/abedegno/muesli/internal/model"
)

func TestMatchPersonByName(t *testing.T) {
	people := []model.Person{
		{ID: "p1", DisplayName: "Alice"},
		{ID: "p2", DisplayName: "Bob"},
		{ID: "p3", DisplayName: "Alice"},
	}

	tests := []struct {
		name      string
		input     string
		wantID    string
		wantMatch bool
	}{
		{
			name:      "ambiguous duplicate match",
			input:     "alice",
			wantID:    "",
			wantMatch: false,
		},
		{
			name:      "case insensitive unique match",
			input:     "BOB",
			wantID:    "p2",
			wantMatch: true,
		},
		{
			name:      "no match",
			input:     "Carol",
			wantID:    "",
			wantMatch: false,
		},
		{
			name:      "blank input",
			input:     "  ",
			wantID:    "",
			wantMatch: false,
		},
		{
			name:      "exact case match",
			input:     "Alice",
			wantID:    "",
			wantMatch: false,
		},
		{
			name:      "trimmed input still matches",
			input:     "  bob  ",
			wantID:    "p2",
			wantMatch: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotID, gotMatch := MatchPersonByName(tt.input, people)
			if gotID != tt.wantID || gotMatch != tt.wantMatch {
				t.Fatalf("MatchPersonByName(%q) = (%q, %v), want (%q, %v)", tt.input, gotID, gotMatch, tt.wantID, tt.wantMatch)
			}
		})
	}
}
