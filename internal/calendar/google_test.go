package calendar

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/model"
	"golang.org/x/oauth2"
	gcal "google.golang.org/api/calendar/v3"
)

func TestSelectedCalendarIDs(t *testing.T) {
	selected := map[string]bool{
		"":         true,
		"primary":  true,
		"disabled": false,
		"zeta":     true,
		"alpha":    true,
	}
	want := []string{"primary", "alpha", "zeta"}
	for i := 0; i < 20; i++ {
		if got := selectedCalendarIDs(selected); !reflect.DeepEqual(got, want) {
			t.Fatalf("selectedCalendarIDs() = %q, want %q", got, want)
		}
	}
}

func TestFetchGoogleListsSelectedCalendars(t *testing.T) {
	from := time.Date(2026, 7, 10, 15, 4, 5, 0, time.FixedZone("west", -7*60*60))
	to := from.Add(2 * time.Hour)
	requested := make([]string, 0, 2)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"test-token","token_type":"Bearer","expires_in":3600}`))
			return
		}

		const prefix = "/calendars/"
		const suffix = "/events"
		if !strings.HasPrefix(r.URL.Path, prefix) || !strings.HasSuffix(r.URL.Path, suffix) {
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
			return
		}
		calendarID, err := url.PathUnescape(strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, prefix), suffix))
		if err != nil {
			t.Errorf("unescape calendar ID: %v", err)
		}
		requested = append(requested, calendarID)
		if got := r.URL.Query().Get("singleEvents"); got != "true" {
			t.Errorf("singleEvents = %q, want true", got)
		}
		if got, want := r.URL.Query().Get("timeMin"), from.UTC().Format(time.RFC3339); got != want {
			t.Errorf("timeMin = %q, want %q", got, want)
		}
		if got, want := r.URL.Query().Get("timeMax"), to.UTC().Format(time.RFC3339); got != want {
			t.Errorf("timeMax = %q, want %q", got, want)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{
			nil,
			map[string]any{"id": "event-" + calendarID, "summary": calendarID},
		}})
	}))
	t.Cleanup(server.Close)

	oldEndpoint := googleAPIEndpoint
	googleAPIEndpoint = server.URL + "/"
	t.Cleanup(func() { googleAPIEndpoint = oldEndpoint })
	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, googleTestHTTPClient(server))

	got, err := FetchGoogle(ctx, "client", "secret", "refresh", map[string]bool{"work@example.com": true}, from, to)
	if err != nil {
		t.Fatalf("FetchGoogle() error = %v", err)
	}
	if want := []string{"primary", "work@example.com"}; !reflect.DeepEqual(requested, want) {
		t.Fatalf("requested calendars = %q, want %q", requested, want)
	}
	if len(got) != 2 || got[0].ExternalID != "event-primary" || got[1].ExternalID != "event-work@example.com" {
		t.Fatalf("FetchGoogle() = %#v, want events from both calendars", got)
	}
}

func TestFetchGoogleCalendarErrorNamesCalendar(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"test-token","token_type":"Bearer","expires_in":3600}`))
			return
		}
		if strings.Contains(r.URL.Path, "failing") {
			http.Error(w, "calendar unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"items":[]}`)
	}))
	t.Cleanup(server.Close)

	oldEndpoint := googleAPIEndpoint
	googleAPIEndpoint = server.URL + "/"
	t.Cleanup(func() { googleAPIEndpoint = oldEndpoint })
	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, googleTestHTTPClient(server))

	_, err := FetchGoogle(ctx, "client", "secret", "refresh", map[string]bool{"failing": true}, time.Now(), time.Now().Add(time.Hour))
	if err == nil || !strings.Contains(err.Error(), `list google calendar "failing"`) {
		t.Fatalf("FetchGoogle() error = %v, want wrapped error naming failing calendar", err)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func googleTestHTTPClient(server *httptest.Server) *http.Client {
	transport := server.Client().Transport
	return &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "oauth2.googleapis.com" {
			cloned := req.Clone(req.Context())
			cloned.URL.Scheme = "http"
			cloned.URL.Host = strings.TrimPrefix(server.URL, "http://")
			cloned.URL.Path = "/token"
			req = cloned
		}
		return transport.RoundTrip(req)
	})}
}

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
