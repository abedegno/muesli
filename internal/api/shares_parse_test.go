package api

import (
	"testing"
	"time"
)

func TestParseShareExpiresAt(t *testing.T) {
	t.Parallel()

	want := time.Date(2026, 7, 13, 6, 25, 4, 0, time.UTC)

	tests := []struct {
		name string
		raw  string
		want *time.Time
		ok   bool
	}{
		{name: "empty", raw: "", want: nil, ok: true},
		{name: "whitespace", raw: "   ", want: nil, ok: true},
		{name: "valid", raw: "2026-07-13T06:25:04Z", want: &want, ok: true},
		{name: "invalid", raw: "not-a-timestamp", want: nil, ok: false},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseShareExpiresAt(tc.raw)
			if tc.ok && err != nil {
				t.Fatalf("parseShareExpiresAt(%q) error = %v", tc.raw, err)
			}
			if !tc.ok && err == nil {
				t.Fatalf("parseShareExpiresAt(%q) error = nil, want error", tc.raw)
			}
			if tc.want == nil {
				if got != nil {
					t.Fatalf("parseShareExpiresAt(%q) = %+v, want nil", tc.raw, got)
				}
				return
			}
			if got == nil || !got.Equal(*tc.want) {
				t.Fatalf("parseShareExpiresAt(%q) = %+v, want %+v", tc.raw, got, tc.want)
			}
		})
	}
}
