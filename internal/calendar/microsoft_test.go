package calendar

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/model"
	"golang.org/x/oauth2"
)

func TestFetchMicrosoftFollowsPagination(t *testing.T) {
	from := time.Date(2026, 7, 10, 15, 4, 5, 0, time.FixedZone("east", 2*60*60))
	to := from.Add(90 * time.Minute)
	calendarRequests := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"test-token","token_type":"Bearer","expires_in":3600}`))
		case "/me/calendarView":
			calendarRequests++
			if got := r.Header.Get("Prefer"); got != `outlook.timezone="UTC"` {
				t.Errorf("Prefer header = %q", got)
			}
			if got, want := r.URL.Query().Get("startDateTime"), from.UTC().Format(time.RFC3339); got != want {
				t.Errorf("startDateTime = %q, want %q", got, want)
			}
			if got, want := r.URL.Query().Get("endDateTime"), to.UTC().Format(time.RFC3339); got != want {
				t.Errorf("endDateTime = %q, want %q", got, want)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"value":[{"id":"page-one"}],"@odata.nextLink":"` + serverURL(r) + `/page-two"}`))
		case "/page-two":
			calendarRequests++
			if got := r.Header.Get("Prefer"); got != `outlook.timezone="UTC"` {
				t.Errorf("Prefer header on second page = %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"value":[{"id":"page-two"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	setMicrosoftTestEndpoints(t, server)
	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, server.Client())

	got, err := FetchMicrosoft(ctx, "client", "secret", "refresh", nil, from, to)
	if err != nil {
		t.Fatalf("FetchMicrosoft() error = %v", err)
	}
	if calendarRequests != 2 {
		t.Fatalf("calendar requests = %d, want 2", calendarRequests)
	}
	if len(got) != 2 || got[0].ExternalID != "page-one" || got[1].ExternalID != "page-two" {
		t.Fatalf("FetchMicrosoft() = %#v, want merged events from both pages", got)
	}
}

func TestFetchMicrosoftNon2xx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"test-token","token_type":"Bearer","expires_in":3600}`))
			return
		}
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("  graph unavailable  \n"))
	}))
	t.Cleanup(server.Close)
	setMicrosoftTestEndpoints(t, server)
	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, server.Client())

	_, err := FetchMicrosoft(ctx, "client", "secret", "refresh", nil, time.Now(), time.Now().Add(time.Hour))
	if err == nil || !strings.Contains(err.Error(), "unexpected status 502 Bad Gateway: graph unavailable") {
		t.Fatalf("FetchMicrosoft() error = %v, want status and trimmed response body", err)
	}
}

func setMicrosoftTestEndpoints(t *testing.T, server *httptest.Server) {
	t.Helper()
	oldBaseURL := graphBaseURL
	oldOAuthEndpoint := microsoftOAuthEndpoint
	graphBaseURL = server.URL
	microsoftOAuthEndpoint = oauth2.Endpoint{TokenURL: server.URL + "/token"}
	t.Cleanup(func() {
		graphBaseURL = oldBaseURL
		microsoftOAuthEndpoint = oldOAuthEndpoint
	})
}

func serverURL(r *http.Request) string {
	return "http://" + r.Host
}

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
