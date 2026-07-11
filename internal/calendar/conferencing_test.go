package calendar

import "testing"

func TestExtractConferencingURL(t *testing.T) {
	cases := []struct{ name, loc, desc, native, want string }{
		{"native wins", "", "", "https://zoom.us/j/123", "https://zoom.us/j/123"},
		{"meet in location", "https://meet.google.com/abc-defg-hij", "", "", "https://meet.google.com/abc-defg-hij"},
		{"teams in description", "", "Join here https://teams.microsoft.com/l/meetup-join/x now", "", "https://teams.microsoft.com/l/meetup-join/x"},
		{"none", "Room 4", "no link here", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ExtractConferencingURL(c.loc, c.desc, c.native); got != c.want {
				t.Fatalf("got %q want %q", got, c.want)
			}
		})
	}
}
