package calendar

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/abedegno/muesli/internal/model"
	ics "github.com/arran4/golang-ical"
)

const maxICSSize = 10 << 20

func ParseICS(data []byte) ([]NormalizedEvent, error) {
	cal, err := ics.ParseCalendar(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("parse ics: %w", err)
	}

	events := cal.Events()
	out := make([]NormalizedEvent, 0, len(events))
	for _, evt := range events {
		startsAt, err := evt.GetStartAt()
		if err != nil {
			return nil, fmt.Errorf("parse event %q start: %w", evt.Id(), err)
		}

		endsAt, err := evt.GetEndAt()
		if err != nil {
			endsAt = startsAt
		}

		location := propValue(evt.GetProperty(ics.ComponentPropertyLocation))
		description := propValue(evt.GetProperty(ics.ComponentPropertyDescription))

		norm := NormalizedEvent{
			ExternalID:  evt.Id(),
			Title:       propValue(evt.GetProperty(ics.ComponentPropertySummary)),
			StartsAt:    startsAt,
			EndsAt:      endsAt,
			Description: description,
			Location:    location,
			ConferencingURL: ExtractConferencingURL(
				location,
				description,
				"",
			),
		}

		for _, prop := range evt.GetProperties(ics.ComponentPropertyAttendee) {
			norm.Attendees = append(norm.Attendees, attendeeFromProperty(prop))
		}

		out = append(out, norm)
	}

	return out, nil
}

func FetchICS(ctx context.Context, hc *http.Client, url string) ([]NormalizedEvent, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if hc == nil {
		hc = http.DefaultClient
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build ics request: %w", err)
	}

	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch ics: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch ics: unexpected status %s", resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxICSSize))
	if err != nil {
		return nil, fmt.Errorf("read ics: %w", err)
	}

	return ParseICS(body)
}

func propValue(prop *ics.IANAProperty) string {
	if prop == nil {
		return ""
	}
	return prop.Value
}

func attendeeFromProperty(prop *ics.IANAProperty) model.Attendee {
	if prop == nil {
		return model.Attendee{}
	}

	email := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(prop.Value)), "mailto:")
	name := ""
	if cn := prop.ICalParameters["CN"]; len(cn) > 0 {
		name = cn[0]
	}

	return model.Attendee{
		Email: email,
		Name:  name,
	}
}
