package calendar

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	ical "github.com/emersion/go-ical"
	"github.com/emersion/go-webdav"
	"github.com/emersion/go-webdav/caldav"
)

func FetchCalDAV(ctx context.Context, baseURL, user, pass string, from, to time.Time) ([]NormalizedEvent, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	recorder := &statusRecordingTransport{base: http.DefaultTransport}
	hc := webdav.HTTPClientWithBasicAuth(&http.Client{Transport: recorder}, user, pass)
	client, err := caldav.NewClient(hc, baseURL)
	if err != nil {
		return nil, fmt.Errorf("create caldav client: %w", err)
	}

	query := &caldav.CalendarQuery{
		CompRequest: caldav.CalendarCompRequest{
			Name:     "VCALENDAR",
			AllProps: true,
			AllComps: true,
		},
		CompFilter: caldav.CompFilter{
			Name: "VCALENDAR",
			Comps: []caldav.CompFilter{{
				Name:  "VEVENT",
				Start: from.UTC(),
				End:   to.UTC(),
			}},
		},
	}

	calendarPaths := []string{baseURL}
	var discoveryErr error
	if discovered, err := discoverCalDAVCalendarPath(ctx, client); err == nil && discovered != "" && discovered != baseURL {
		calendarPaths = append([]string{discovered}, calendarPaths...)
	} else if err != nil {
		discoveryErr = err
	}

	var errs []error
	for _, calendarPath := range calendarPaths {
		objects, err := client.QueryCalendar(ctx, calendarPath, query)
		if err != nil {
			errs = append(errs, fmt.Errorf("query caldav calendar %q: %w", calendarPath, err))
			continue
		}

		return flattenCalDAVObjects(objects)
	}

	err = fmt.Errorf("fetch caldav events from %q: %w", baseURL, errors.Join(append([]error{discoveryErr}, errs...)...))
	if status := recorder.StatusCode(); status != 0 && (status < 200 || status > 299) {
		err = &HTTPError{StatusCode: status, Err: err}
	}
	return nil, err
}

// statusRecordingTransport retains the status from the most recent completed
// request. The CalDAV dependency exposes its HTTP error type only from an
// internal package, so this lets FetchCalDAV preserve that status in our
// exported HTTPError without interpreting the dependency's error text.
type statusRecordingTransport struct {
	base   http.RoundTripper
	mu     sync.Mutex
	status int
}

func (t *statusRecordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	t.mu.Lock()
	t.status = 0
	if resp != nil {
		t.status = resp.StatusCode
	}
	t.mu.Unlock()
	return resp, err
}

func (t *statusRecordingTransport) StatusCode() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.status
}

func discoverCalDAVCalendarPath(ctx context.Context, client *caldav.Client) (string, error) {
	principal, err := client.FindCurrentUserPrincipal(ctx)
	if err != nil {
		return "", fmt.Errorf("find current user principal: %w", err)
	}

	homeSet, err := client.FindCalendarHomeSet(ctx, principal)
	if err != nil {
		return "", fmt.Errorf("find calendar home set: %w", err)
	}

	calendars, err := client.FindCalendars(ctx, homeSet)
	if err != nil {
		return "", fmt.Errorf("find calendars: %w", err)
	}
	if len(calendars) == 0 {
		return "", fmt.Errorf("find calendars: no calendars discovered")
	}

	return calendars[0].Path, nil
}

func flattenCalDAVObjects(objects []caldav.CalendarObject) ([]NormalizedEvent, error) {
	out := make([]NormalizedEvent, 0)
	for _, object := range objects {
		if object.Data == nil {
			return nil, fmt.Errorf("calendar object %q has no data", object.Path)
		}

		var buf bytes.Buffer
		if err := ical.NewEncoder(&buf).Encode(object.Data); err != nil {
			return nil, fmt.Errorf("encode calendar object %q: %w", object.Path, err)
		}

		events, err := ParseICS(buf.Bytes())
		if err != nil {
			return nil, fmt.Errorf("parse calendar object %q: %w", object.Path, err)
		}

		out = append(out, events...)
	}

	return out, nil
}
