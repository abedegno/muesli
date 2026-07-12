package people

import (
	"testing"

	"github.com/abedegno/muesli/internal/model"
)

func TestMatchPersonByName(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		people    []model.Person
		wantID    string
		wantMatch bool
	}{
		{
			name:      "ambiguous duplicate match",
			input:     "alice",
			people:    []model.Person{{ID: "p1", DisplayName: "Alice"}, {ID: "p2", DisplayName: "Bob"}, {ID: "p3", DisplayName: "Alice"}},
			wantID:    "",
			wantMatch: false,
		},
		{
			name:      "case insensitive unique match",
			input:     "BOB",
			people:    []model.Person{{ID: "p1", DisplayName: "Alice"}, {ID: "p2", DisplayName: "Bob"}, {ID: "p3", DisplayName: "Alice"}},
			wantID:    "p2",
			wantMatch: true,
		},
		{
			name:      "no match",
			input:     "Carol",
			people:    []model.Person{{ID: "p1", DisplayName: "Alice"}, {ID: "p2", DisplayName: "Bob"}, {ID: "p3", DisplayName: "Alice"}},
			wantID:    "",
			wantMatch: false,
		},
		{
			name:      "blank input",
			input:     "  ",
			people:    []model.Person{{ID: "p1", DisplayName: "Alice"}, {ID: "p2", DisplayName: "Bob"}, {ID: "p3", DisplayName: "Alice"}},
			wantID:    "",
			wantMatch: false,
		},
		{
			name:      "exact case match",
			input:     "Alice",
			people:    []model.Person{{ID: "p1", DisplayName: "Alice"}, {ID: "p2", DisplayName: "Bob"}, {ID: "p3", DisplayName: "Alice"}},
			wantID:    "",
			wantMatch: false,
		},
		{
			name:      "trimmed input still matches",
			input:     "  bob  ",
			people:    []model.Person{{ID: "p1", DisplayName: "Alice"}, {ID: "p2", DisplayName: "Bob"}, {ID: "p3", DisplayName: "Alice"}},
			wantID:    "p2",
			wantMatch: true,
		},
		{
			name:      "title stripped and matched",
			input:     "Dr. Alice Smith",
			people:    []model.Person{{ID: "p1", DisplayName: "alice smith"}},
			wantID:    "p1",
			wantMatch: true,
		},
		{
			name:      "comma reorder and nickname fold matched",
			input:     "Smith, Bob",
			people:    []model.Person{{ID: "p1", DisplayName: "Robert Smith"}},
			wantID:    "p1",
			wantMatch: true,
		},
		{
			name:      "fuzzy collision remains ambiguous",
			input:     "Smith, Bob",
			people:    []model.Person{{ID: "p1", DisplayName: "Bob Smith"}, {ID: "p2", DisplayName: "Robert Smith"}},
			wantID:    "",
			wantMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotID, gotMatch := MatchPersonByName(tt.input, tt.people)
			if gotID != tt.wantID || gotMatch != tt.wantMatch {
				t.Fatalf("MatchPersonByName(%q) = (%q, %v), want (%q, %v)", tt.input, gotID, gotMatch, tt.wantID, tt.wantMatch)
			}
		})
	}
}

func TestNormalizeName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "lowercases and trims",
			in:   "  ALICE  ",
			want: "alice",
		},
		{
			name: "strips title with period",
			in:   "Dr. Alice Smith",
			want: "alice smith",
		},
		{
			name: "strips title without period",
			in:   "Ms Alice Smith",
			want: "alice smith",
		},
		{
			name: "reorders comma name",
			in:   "Smith, Alice",
			want: "alice smith",
		},
		{
			name: "folds diacritics",
			in:   "José",
			want: "jose",
		},
		{
			name: "collapses whitespace",
			in:   "Alice\t  Smith\nJr",
			want: "alice smith jr",
		},
		{
			name: "folds nickname on first token only",
			in:   "Bob Smith",
			want: "robert smith",
		},
		{
			name: "does not fold nickname in later token",
			in:   "Smith Bob",
			want: "smith bob",
		},
		{
			name: "handles title and comma edge case",
			in:   "Dr. Smith, Alice",
			want: "alice smith",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeName(tt.in); got != tt.want {
				t.Fatalf("NormalizeName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
