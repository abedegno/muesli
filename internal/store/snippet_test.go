package store

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func Test_snippet(t *testing.T) {
	t.Parallel()

	// Build a string that is definitely more than 160 runes.
	longInput := strings.Repeat("abcdefghij", 20) // 200 ASCII runes

	// A multibyte unicode string — each character is a multi-byte rune.
	unicodeInput := "café résumé"

	tests := []struct {
		name  string
		input string
		check func(t *testing.T, got string)
	}{
		{
			name:  "empty string returns empty",
			input: "",
			check: func(t *testing.T, got string) {
				if got != "" {
					t.Errorf("got %q, want empty string", got)
				}
			},
		},
		{
			name:  "heading line strips leading hash-space",
			input: "# Heading\nBody text",
			check: func(t *testing.T, got string) {
				if !strings.HasPrefix(got, "Heading") {
					t.Errorf("got %q, want it to start with \"Heading\"", got)
				}
				if strings.HasPrefix(got, "#") {
					t.Errorf("got %q, want leading '#' stripped", got)
				}
			},
		},
		{
			name:  "CRLF input collapses to single line",
			input: "line one\r\nline two",
			check: func(t *testing.T, got string) {
				want := "line one line two"
				if got != want {
					t.Errorf("got %q, want %q", got, want)
				}
			},
		},
		{
			name:  "multibyte unicode is preserved and not split mid-rune",
			input: unicodeInput,
			check: func(t *testing.T, got string) {
				if got != unicodeInput {
					t.Errorf("got %q, want %q", got, unicodeInput)
				}
				if !utf8.ValidString(got) {
					t.Errorf("result is not valid UTF-8: %q", got)
				}
			},
		},
		{
			name:  "ordered list single-digit prefix stripped",
			input: "1. Buy milk",
			check: func(t *testing.T, got string) {
				want := "Buy milk"
				if got != want {
					t.Errorf("got %q, want %q", got, want)
				}
			},
		},
		{
			name:  "ordered list multi-digit prefix stripped",
			input: "10. item",
			check: func(t *testing.T, got string) {
				want := "item"
				if got != want {
					t.Errorf("got %q, want %q", got, want)
				}
			},
		},
		{
			name:  "ordered list empty body yields empty snippet",
			input: "2. ",
			check: func(t *testing.T, got string) {
				if got != "" {
					t.Errorf("got %q, want empty string", got)
				}
			},
		},
		{
			name:  "mixed bullet and numbered list lines all stripped",
			input: "- bullet\n3. numbered\nplain",
			check: func(t *testing.T, got string) {
				want := "bullet numbered plain"
				if got != want {
					t.Errorf("got %q, want %q", got, want)
				}
			},
		},
		{
			name:  "input longer than 160 runes is capped at 160 runes",
			input: longInput,
			check: func(t *testing.T, got string) {
				runes := []rune(got)
				if len(runes) > 160 {
					t.Errorf("got %d runes, want <= 160", len(runes))
				}
				if !utf8.ValidString(got) {
					t.Errorf("result is not valid UTF-8 after truncation: %q", got)
				}
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := snippet(tc.input)
			tc.check(t, got)
		})
	}
}
