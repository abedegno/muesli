package calendar

import (
	"reflect"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/model"
	gcal "google.golang.org/api/calendar/v3"
)

func TestGoogleEventToNormalized(t *testing.T) {
	utc := time.UTC
	allDayStart := time.Date(2026, 7, 10, 0, 0, 0, 0, utc)
	allDayEnd := time.Date(2026, 7, 11, 0, 0, 0, 0, utc)
	dtStart := time.Date(2026, 7, 10, 15, 4, 5, 0, utc)
	dtEnd := time.Date(2026, 7, 10, 16, 4, 5, 0, utc)

	cases := []struct {
		name string
		ev   *gcal.Event
		want NormalizedEvent
	}{
		{
			name: "normal event",
			ev: &gcal.Event{
				Id:          "evt-normal",
				Summary:     "Standup",
				Description: "Daily team sync",
				Location:    "Room 1",
				Start:       &gcal.EventDateTime{DateTime: dtStart.Format(time.RFC3339)},
				End:         &gcal.EventDateTime{DateTime: dtEnd.Format(time.RFC3339)},
			},
			want: NormalizedEvent{
				ExternalID:  "evt-normal",
				Title:       "Standup",
				StartsAt:    dtStart,
				EndsAt:      dtEnd,
				Description: "Daily team sync",
				Location:    "Room 1",
			},
		},
		{
			name: "all-day event",
			ev: &gcal.Event{
				Id:      "evt-all-day",
				Summary: "Out of office",
				Start:   &gcal.EventDateTime{Date: allDayStart.Format("2006-01-02")},
				End:     &gcal.EventDateTime{Date: allDayEnd.Format("2006-01-02")},
			},
			want: NormalizedEvent{
				ExternalID: "evt-all-day",
				Title:      "Out of office",
				StartsAt:   allDayStart,
				EndsAt:     allDayEnd,
			},
		},
		{
			name: "location conferencing link",
			ev: &gcal.Event{
				Id:       "evt-location-link",
				Start:    &gcal.EventDateTime{DateTime: dtStart.Format(time.RFC3339)},
				End:      &gcal.EventDateTime{DateTime: dtEnd.Format(time.RFC3339)},
				Location: "https://meet.google.com/abc-defg-hij",
			},
			want: NormalizedEvent{
				ExternalID:      "evt-location-link",
				StartsAt:        dtStart,
				EndsAt:          dtEnd,
				Location:        "https://meet.google.com/abc-defg-hij",
				ConferencingURL: "https://meet.google.com/abc-defg-hij",
			},
		},
		{
			name: "hangout and conference data",
			ev: &gcal.Event{
				Id:          "evt-hangout",
				Start:       &gcal.EventDateTime{DateTime: dtStart.Format(time.RFC3339)},
				End:         &gcal.EventDateTime{DateTime: dtEnd.Format(time.RFC3339)},
				HangoutLink: "https://meet.google.com/hangout-link",
				ConferenceData: &gcal.ConferenceData{
					EntryPoints: []*gcal.EntryPoint{{Uri: "https://meet.google.com/conf-uri"}},
				},
			},
			want: NormalizedEvent{
				ExternalID:      "evt-hangout",
				StartsAt:        dtStart,
				EndsAt:          dtEnd,
				ConferencingURL: "https://meet.google.com/hangout-link",
			},
		},
		{
			name: "attendees",
			ev: &gcal.Event{
				Id:    "evt-attendees",
				Start: &gcal.EventDateTime{DateTime: dtStart.Format(time.RFC3339)},
				End:   &gcal.EventDateTime{DateTime: dtEnd.Format(time.RFC3339)},
				Attendees: []*gcal.EventAttendee{
					{Email: "ALICE@example.com", DisplayName: "Alice", ResponseStatus: "accepted"},
					{Email: "bob@example.com", ResponseStatus: "declined"},
				},
			},
			want: NormalizedEvent{
				ExternalID: "evt-attendees",
				StartsAt:   dtStart,
				EndsAt:     dtEnd,
				Attendees: []model.Attendee{
					{Email: "alice@example.com", Name: "Alice", Response: "accepted"},
					{Email: "bob@example.com", Name: "", Response: "declined"},
				},
			},
		},
		{
			name: "no optional fields",
			ev: &gcal.Event{
				Id:    "evt-minimal",
				Start: &gcal.EventDateTime{DateTime: dtStart.Format(time.RFC3339)},
				End:   &gcal.EventDateTime{DateTime: dtEnd.Format(time.RFC3339)},
			},
			want: NormalizedEvent{
				ExternalID: "evt-minimal",
				StartsAt:   dtStart,
				EndsAt:     dtEnd,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := googleEventToNormalized(tc.ev)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("googleEventToNormalized() = %#v, want %#v", got, tc.want)
			}
		})
	}
}
