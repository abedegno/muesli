package exportutil

import "testing"

func TestDedupeFilename(t *testing.T) {
	t.Parallel()

	seen := map[string]int{}
	tests := []struct {
		name string
		want string
	}{
		{name: "weekly-sync.md", want: "weekly-sync.md"},
		{name: "weekly-sync.md", want: "weekly-sync-2.md"},
		{name: "weekly-sync.md", want: "weekly-sync-3.md"},
		{name: "archive", want: "archive"},
		{name: "archive", want: "archive-2"},
	}

	for _, tc := range tests {
		if got := DedupeFilename(tc.name, seen); got != tc.want {
			t.Fatalf("DedupeFilename(%q) = %q, want %q", tc.name, got, tc.want)
		}
	}
}
