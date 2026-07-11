package calendar

import (
	"reflect"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/model"
)

func TestGraphEventToNormalized(t *testing.T) {
	utc := time.UTC
	start := time.Date(2026, 7, 10, 15, 4, 5, 123000000, utc)
	end := time.Date(2026, 7, 10, 16, 4, 5, 123000000, utc)
	startRaw := "2026-07-10T15:04:05.1230000"
	endRaw := "2026-07-10T16:04:05.1230000"

	cases := []struct {
		name string
		ev   *graphEvent
		want NormalizedEvent
	}{
		{
			name: "normal event with attendees",
			ev: &graphEvent{
				ID:       "evt-normal",
				Subject:  "Planning",
				Start:    &graphDateTime{DateTime: startRaw, TimeZone: "UTC"},
				End:      &graphDateTime{DateTime: endRaw, TimeZone: "UTC"},
				Body:     &graphBody{Content: "Weekly planning", ContentType: "text"},
				Location: &graphLocation{DisplayName: "Room 1"},
				Attendees: []graphAttendee{
					{
						EmailAddress: &graphEmailAddress{Address: "ALICE@example.com", Name: "Alice"},
						Status:       &graphAttendeeStatus{Response: "accepted"},
					},
					{
						EmailAddress: &graphEmailAddress{Address: "bob@example.com"},
						Status:       &graphAttendeeStatus{Response: "declined"},
					},
				},
			},
			want: NormalizedEvent{
				ExternalID:  "evt-normal",
				Title:       "Planning",
				StartsAt:    start,
				EndsAt:      end,
				Description: "Weekly planning",
				Location:    "Room 1",
				Attendees: []model.Attendee{
					{Email: "alice@example.com", Name: "Alice", Response: "accepted"},
					{Email: "bob@example.com", Name: "", Response: "declined"},
				},
			},
		},
		{
			name: "missing and empty fields",
			ev: &graphEvent{
				ID:    "evt-empty",
				Start: &graphDateTime{},
				End:   &graphDateTime{},
			},
			want: NormalizedEvent{
				ExternalID: "evt-empty",
			},
		},
		{
			name: "online meeting join url",
			ev: &graphEvent{
				ID:            "evt-join",
				Subject:       "Remote call",
				Start:         &graphDateTime{DateTime: startRaw, TimeZone: "UTC"},
				End:           &graphDateTime{DateTime: endRaw, TimeZone: "UTC"},
				OnlineMeeting: &graphOnlineMeeting{JoinURL: "https://teams.microsoft.com/l/meetup-join/join-url"},
			},
			want: NormalizedEvent{
				ExternalID:      "evt-join",
				Title:           "Remote call",
				StartsAt:        start,
				EndsAt:          end,
				ConferencingURL: "https://teams.microsoft.com/l/meetup-join/join-url",
			},
		},
		{
			name: "body embedded conferencing link",
			ev: &graphEvent{
				ID:       "evt-body-link",
				Start:    &graphDateTime{DateTime: startRaw, TimeZone: "UTC"},
				End:      &graphDateTime{DateTime: endRaw, TimeZone: "UTC"},
				Body:     &graphBody{Content: "Join at https://meet.google.com/abc-defg-hij", ContentType: "text"},
				Location: &graphLocation{DisplayName: "Remote"},
			},
			want: NormalizedEvent{
				ExternalID:      "evt-body-link",
				StartsAt:        start,
				EndsAt:          end,
				Description:     "Join at https://meet.google.com/abc-defg-hij",
				Location:        "Remote",
				ConferencingURL: "https://meet.google.com/abc-defg-hij",
			},
		},
		{
			name: "location embedded conferencing link",
			ev: &graphEvent{
				ID:       "evt-location-link",
				Start:    &graphDateTime{DateTime: startRaw, TimeZone: "UTC"},
				End:      &graphDateTime{DateTime: endRaw, TimeZone: "UTC"},
				Body:     &graphBody{Content: "See you there", ContentType: "text"},
				Location: &graphLocation{DisplayName: "https://zoom.us/j/123456789"},
			},
			want: NormalizedEvent{
				ExternalID:      "evt-location-link",
				StartsAt:        start,
				EndsAt:          end,
				Description:     "See you there",
				Location:        "https://zoom.us/j/123456789",
				ConferencingURL: "https://zoom.us/j/123456789",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := graphEventToNormalized(tc.ev)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("graphEventToNormalized() = %#v, want %#v", got, tc.want)
			}
		})
	}
}
