package calendar

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/abedegno/muesli/internal/model"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/microsoft"
)

const microsoftCalendarReadOnlyScope = "https://graph.microsoft.com/Calendars.Read"

type graphEvent struct {
	ID            string              `json:"id"`
	Subject       string              `json:"subject"`
	Start         *graphDateTime      `json:"start"`
	End           *graphDateTime      `json:"end"`
	Body          *graphBody          `json:"body"`
	Location      *graphLocation      `json:"location"`
	OnlineMeeting *graphOnlineMeeting `json:"onlineMeeting"`
	Attendees     []graphAttendee     `json:"attendees"`
}

type graphDateTime struct {
	DateTime string `json:"dateTime"`
	TimeZone string `json:"timeZone"`
}

type graphBody struct {
	ContentType string `json:"contentType"`
	Content     string `json:"content"`
}

type graphLocation struct {
	DisplayName string `json:"displayName"`
}

type graphOnlineMeeting struct {
	JoinURL string `json:"joinUrl"`
}

type graphAttendee struct {
	EmailAddress *graphEmailAddress   `json:"emailAddress"`
	Status       *graphAttendeeStatus `json:"status"`
}

type graphEmailAddress struct {
	Address string `json:"address"`
	Name    string `json:"name"`
}

type graphAttendeeStatus struct {
	Response string `json:"response"`
}

func graphEventToNormalized(ev *graphEvent) NormalizedEvent {
	if ev == nil {
		return NormalizedEvent{}
	}

	location := graphEventLocation(ev)
	description := graphEventDescription(ev)
	native := graphEventNativeLink(ev)
	norm := NormalizedEvent{
		ExternalID:      ev.ID,
		Title:           ev.Subject,
		StartsAt:        graphEventTime(ev.Start),
		EndsAt:          graphEventTime(ev.End),
		Description:     description,
		Location:        location,
		ConferencingURL: ExtractConferencingURL(location, description, native),
	}
	if norm.EndsAt.IsZero() {
		norm.EndsAt = norm.StartsAt
	}

	for _, attendee := range ev.Attendees {
		if attendee.EmailAddress == nil && attendee.Status == nil {
			continue
		}
		email := ""
		name := ""
		response := ""
		if attendee.EmailAddress != nil {
			email = strings.ToLower(strings.TrimSpace(attendee.EmailAddress.Address))
			name = attendee.EmailAddress.Name
		}
		if attendee.Status != nil {
			response = attendee.Status.Response
		}
		norm.Attendees = append(norm.Attendees, model.Attendee{
			Email:    email,
			Name:     name,
			Response: response,
		})
	}

	return norm
}

func FetchMicrosoft(ctx context.Context, clientID, clientSecret, refreshToken string, selected map[string]bool, from, to time.Time) ([]NormalizedEvent, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	_ = selected // Microsoft calendarView currently targets the primary calendar only.

	cfg := &oauth2.Config{
		Endpoint:     microsoft.AzureADEndpoint("common"),
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Scopes:       []string{"offline_access", microsoftCalendarReadOnlyScope},
	}
	ts := cfg.TokenSource(ctx, &oauth2.Token{RefreshToken: refreshToken})
	client := oauth2.NewClient(ctx, ts)

	nextURL := "https://graph.microsoft.com/v1.0/me/calendarView?" + url.Values{
		"startDateTime": {from.UTC().Format(time.RFC3339)},
		"endDateTime":   {to.UTC().Format(time.RFC3339)},
	}.Encode()

	out := make([]NormalizedEvent, 0)
	for nextURL != "" {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, nextURL, nil)
		if err != nil {
			return nil, fmt.Errorf("build microsoft calendar request: %w", err)
		}
		req.Header.Set("Prefer", `outlook.timezone="UTC"`)

		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("fetch microsoft calendar: %w", err)
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read microsoft calendar response: %w", readErr)
		}
		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			return nil, fmt.Errorf("fetch microsoft calendar: unexpected status %s: %s", resp.Status, strings.TrimSpace(string(body)))
		}

		var page struct {
			Value    []graphEvent `json:"value"`
			NextLink string       `json:"@odata.nextLink"`
		}
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, fmt.Errorf("decode microsoft calendar response: %w", err)
		}
		for i := range page.Value {
			out = append(out, graphEventToNormalized(&page.Value[i]))
		}
		nextURL = page.NextLink
	}

	return out, nil
}

func graphEventDescription(ev *graphEvent) string {
	if ev == nil || ev.Body == nil {
		return ""
	}
	return ev.Body.Content
}

func graphEventLocation(ev *graphEvent) string {
	if ev == nil || ev.Location == nil {
		return ""
	}
	return ev.Location.DisplayName
}

func graphEventNativeLink(ev *graphEvent) string {
	if ev == nil {
		return ""
	}
	parts := make([]string, 0, 3)
	if ev.OnlineMeeting != nil {
		if joinURL := strings.TrimSpace(ev.OnlineMeeting.JoinURL); joinURL != "" {
			parts = append(parts, joinURL)
		}
	}
	if location := graphEventLocation(ev); location != "" {
		parts = append(parts, location)
	}
	if description := graphEventDescription(ev); description != "" {
		parts = append(parts, description)
	}
	return strings.Join(parts, " ")
}

func graphEventTime(dt *graphDateTime) time.Time {
	if dt == nil {
		return time.Time{}
	}
	raw := strings.TrimSpace(dt.DateTime)
	if raw == "" {
		return time.Time{}
	}

	if parsed, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return parsed.UTC()
	}

	loc := time.UTC
	tz := strings.TrimSpace(dt.TimeZone)
	if tz != "" && !strings.EqualFold(tz, "UTC") {
		if loaded, err := time.LoadLocation(tz); err == nil {
			loc = loaded
		}
	}

	parsed, err := parseGraphDateTimeInLocation(raw, loc)
	if err != nil {
		return time.Time{}
	}
	return parsed.UTC()
}

func parseGraphDateTimeInLocation(raw string, loc *time.Location) (time.Time, error) {
	const baseLayout = "2006-01-02T15:04:05"

	if idx := strings.IndexByte(raw, '.'); idx >= 0 {
		base := raw[:idx]
		frac := raw[idx+1:]
		if frac == "" {
			return time.ParseInLocation(baseLayout, base, loc)
		}
		if len(frac) > 9 {
			frac = frac[:9]
		}
		for len(frac) < 9 {
			frac += "0"
		}
		parsed, err := time.ParseInLocation(baseLayout, base, loc)
		if err != nil {
			return time.Time{}, err
		}
		ns, err := strconv.Atoi(frac)
		if err != nil {
			return time.Time{}, err
		}
		return parsed.Add(time.Duration(ns) * time.Nanosecond), nil
	}

	return time.ParseInLocation(baseLayout, raw, loc)
}
