package calendar

import "testing"

func TestDiffExternalIDs(t *testing.T) {
	cases := []struct {
		name    string
		fetched []NormalizedEvent
		want    []string
	}{
		{
			name: "preserves order",
			fetched: []NormalizedEvent{
				{ExternalID: "a"},
				{ExternalID: "b"},
			},
			want: []string{"a", "b"},
		},
		{
			name:    "empty",
			fetched: nil,
			want:    nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DiffExternalIDs(tc.fetched)
			if len(got) != len(tc.want) {
				t.Fatalf("len=%d want %d (%v)", len(got), len(tc.want), got)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("got %v want %v", got, tc.want)
				}
			}
		})
	}
}
