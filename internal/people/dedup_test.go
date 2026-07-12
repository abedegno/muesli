package people

import (
	"reflect"
	"testing"

	"github.com/abedegno/muesli/internal/model"
)

func TestDedupByEmail(t *testing.T) {
	tests := []struct {
		name  string
		input []model.Attendee
		want  []model.Attendee
	}{
		{
			name: "dedups case-insensitively and preserves first non-empty name",
			input: []model.Attendee{
				{Email: "alice@x.com", Name: "Alice"},
				{Email: "ALICE@x.com", Name: ""},
				{Email: "bob@y.com", Name: ""},
				{Email: "alice@x.com", Name: "Alicia"},
			},
			want: []model.Attendee{
				{Email: "alice@x.com", Name: "Alice"},
				{Email: "bob@y.com", Name: ""},
			},
		},
		{
			name: "trims email and allows later non-empty name to fill blank first entry",
			input: []model.Attendee{
				{Email: "  carol@x.com  ", Name: ""},
				{Email: "CAROL@X.COM", Name: "Carol"},
				{Email: "   ", Name: "Ignored"},
			},
			want: []model.Attendee{
				{Email: "  carol@x.com  ", Name: "Carol"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DedupByEmail(tt.input)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("DedupByEmail(%#v) = %#v, want %#v", tt.input, got, tt.want)
			}
		})
	}
}
