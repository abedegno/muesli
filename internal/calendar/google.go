package calendar

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/abedegno/muesli/internal/model"
	gcal "google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const googleCalendarReadOnlyScope = "https://www.googleapis.com/auth/calendar.readonly"

func googleEventToNormalized(ev *gcal.Event) NormalizedEvent {
	if ev == nil {
		return NormalizedEvent{}
	}

	native := googleEventNativeLink(ev)
	norm := NormalizedEvent{
		ExternalID:      ev.Id,
		Title:           ev.Summary,
		StartsAt:        googleEventTime(ev.Start),
		EndsAt:          googleEventTime(ev.End),
		Description:     ev.Description,
		Location:        ev.Location,
		ConferencingURL: ExtractConferencingURL(ev.Location, ev.Description, native),
	}
	if norm.EndsAt.IsZero() {
		norm.EndsAt = norm.StartsAt
	}

	for _, attendee := range ev.Attendees {
		if attendee == nil {
			continue
		}
		norm.Attendees = append(norm.Attendees, model.Attendee{
			Email:    strings.ToLower(strings.TrimSpace(attendee.Email)),
			Name:     attendee.DisplayName,
			Response: attendee.ResponseStatus,
		})
	}

	return norm
}

func FetchGoogle(ctx context.Context, clientID, clientSecret, refreshToken string, selected map[string]bool, from, to time.Time) ([]NormalizedEvent, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	cfg := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Endpoint:     google.Endpoint,
		Scopes:       []string{googleCalendarReadOnlyScope},
	}
	ts := cfg.TokenSource(ctx, &oauth2.Token{RefreshToken: refreshToken})

	svc, err := gcal.NewService(ctx, option.WithTokenSource(ts))
	if err != nil {
		return nil, fmt.Errorf("create google calendar service: %w", err)
	}

	calendarIDs := []string{"primary"}
	seen := map[string]bool{"primary": true}
	for calendarID, ok := range selected {
		if !ok || calendarID == "" || seen[calendarID] {
			continue
		}
		seen[calendarID] = true
		calendarIDs = append(calendarIDs, calendarID)
	}

	out := make([]NormalizedEvent, 0)
	for _, calendarID := range calendarIDs {
		call := svc.Events.List(calendarID).
			Context(ctx).
			TimeMin(from.UTC().Format(time.RFC3339)).
			TimeMax(to.UTC().Format(time.RFC3339)).
			SingleEvents(true)

		err := call.Pages(ctx, func(page *gcal.Events) error {
			for _, ev := range page.Items {
				if ev == nil {
					continue
				}
				out = append(out, googleEventToNormalized(ev))
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("list google calendar %q: %w", calendarID, err)
		}
	}

	return out, nil
}

func googleEventTime(dt *gcal.EventDateTime) time.Time {
	if dt == nil {
		return time.Time{}
	}
	if dt.DateTime != "" {
		parsed, err := time.Parse(time.RFC3339, dt.DateTime)
		if err == nil {
			return parsed
		}
		return time.Time{}
	}
	if dt.Date != "" {
		parsed, err := time.Parse("2006-01-02", dt.Date)
		if err == nil {
			return parsed.UTC()
		}
	}
	return time.Time{}
}

func googleEventNativeLink(ev *gcal.Event) string {
	if ev == nil {
		return ""
	}
	if ev.HangoutLink != "" {
		return ev.HangoutLink
	}
	if ev.ConferenceData == nil || len(ev.ConferenceData.EntryPoints) == 0 {
		return ""
	}
	parts := make([]string, 0, len(ev.ConferenceData.EntryPoints))
	for _, ep := range ev.ConferenceData.EntryPoints {
		if ep == nil || ep.Uri == "" {
			continue
		}
		parts = append(parts, ep.Uri)
	}
	return strings.Join(parts, " ")
}
