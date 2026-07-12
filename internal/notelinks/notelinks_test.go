package notelinks_test

import (
	"reflect"
	"testing"

	"github.com/abedegno/muesli/internal/notelinks"
)

func TestParseMentions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want []string
	}{
		{
			name: "multiple mentions",
			body: "See [[Alpha]] and [[Beta]] for context.",
			want: []string{"Alpha", "Beta"},
		},
		{
			name: "escaped opener",
			body: `Literal \[[not a mention]] plus [[Real Note]]`,
			want: []string{"Real Note"},
		},
		{
			name: "empty body",
			body: "",
			want: nil,
		},
		{
			name: "unterminated opener",
			body: "Broken [[mention",
			want: nil,
		},
		{
			name: "empty mention",
			body: "Ignore [[]] please",
			want: nil,
		},
		{
			name: "adjacent mentions",
			body: "[[One]][[Two]]",
			want: []string{"One", "Two"},
		},
		{
			name: "trimmed titles",
			body: "Link [[  Spaced Title  ]] here",
			want: []string{"Spaced Title"},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := notelinks.ParseMentions(tc.body)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ParseMentions(%q) = %#v, want %#v", tc.body, got, tc.want)
			}
		})
	}
}
