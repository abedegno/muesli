package calendar

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	ical "github.com/emersion/go-ical"
	"github.com/emersion/go-webdav/caldav"
)

const calDAVEvent = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Muesli Test//EN\r\nBEGIN:VEVENT\r\nUID:caldav-1\r\nDTSTAMP:20260701T000000Z\r\nSUMMARY:Planning\r\nDTSTART:20260710T100000Z\r\nDTEND:20260710T110000Z\r\nDESCRIPTION:Quarterly planning\r\nLOCATION:Room 2\r\nATTENDEE;CN=Alice:mailto:alice@example.com\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"

func decodeCalDAVCalendar(t *testing.T, value string) *ical.Calendar {
	t.Helper()
	calendar, err := ical.NewDecoder(strings.NewReader(value)).Decode()
	if err != nil {
		t.Fatalf("decode test calendar: %v", err)
	}
	return calendar
}

func TestFlattenCalDAVObjects(t *testing.T) {
	tests := []struct {
		name    string
		objects []caldav.CalendarObject
		wantIDs []string
		wantErr string
	}{
		{
			name: "flattens multiple objects and events",
			objects: []caldav.CalendarObject{
				{Path: "/one.ics", Data: decodeCalDAVCalendar(t, strings.Replace(calDAVEvent, "END:VCALENDAR", "BEGIN:VEVENT\r\nUID:caldav-2\r\nDTSTAMP:20260701T000000Z\r\nSUMMARY:Review\r\nDTSTART:20260710T120000Z\r\nDTEND:20260710T123000Z\r\nEND:VEVENT\r\nEND:VCALENDAR", 1))},
				{Path: "/two.ics", Data: decodeCalDAVCalendar(t, strings.Replace(calDAVEvent, "caldav-1", "caldav-3", 1))},
			},
			wantIDs: []string{"caldav-1", "caldav-2", "caldav-3"},
		},
		{
			name: "object without events contributes nothing",
			objects: []caldav.CalendarObject{{Path: "/empty.ics", Data: decodeCalDAVCalendar(t,
				"BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Muesli Test//EN\r\nBEGIN:VTODO\r\nUID:todo-1\r\nDTSTAMP:20260701T000000Z\r\nEND:VTODO\r\nEND:VCALENDAR\r\n")}},
			wantIDs: []string{},
		},
		{
			name: "malformed component returns error",
			objects: []caldav.CalendarObject{{Path: "/bad.ics", Data: decodeCalDAVCalendar(t,
				"BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Muesli Test//EN\r\nBEGIN:VEVENT\r\nUID:bad\r\nDTSTAMP:20260701T000000Z\r\nDTSTART:not-a-date\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n")}},
			wantErr: "parse calendar object \"/bad.ics\"",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			events, err := flattenCalDAVObjects(tc.objects)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("flattenCalDAVObjects() error = %v, want containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			ids := make([]string, len(events))
			for i := range events {
				ids[i] = events[i].ExternalID
			}
			if !reflect.DeepEqual(ids, tc.wantIDs) {
				t.Fatalf("event IDs = %v, want %v", ids, tc.wantIDs)
			}
		})
	}
}

func calDAVMultiStatus(href, properties string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<d:multistatus xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">
  <d:response><d:href>%s</d:href><d:propstat><d:prop>%s</d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>
</d:multistatus>`, href, properties)
}

func TestFetchCalDAV(t *testing.T) {
	queryEnd := time.Date(2026, 7, 11, 0, 0, 0, 0, time.UTC)

	t.Run("successful discovery is used and credentials are sent", func(t *testing.T) {
		var mu sync.Mutex
		var reportPaths []string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, pass, ok := r.BasicAuth()
			if !ok || user != "user" || pass != "pass" {
				t.Errorf("BasicAuth() = %q, %q, %v", user, pass, ok)
			}
			w.Header().Set("Content-Type", "application/xml")
			switch {
			case r.Method == "PROPFIND" && r.URL.Path == "/base":
				w.WriteHeader(http.StatusMultiStatus)
				_, _ = fmt.Fprint(w, calDAVMultiStatus("/base", "<d:current-user-principal><d:href>/principal</d:href></d:current-user-principal>"))
			case r.Method == "PROPFIND" && r.URL.Path == "/principal":
				w.WriteHeader(http.StatusMultiStatus)
				_, _ = fmt.Fprint(w, calDAVMultiStatus("/principal", "<c:calendar-home-set><d:href>/home</d:href></c:calendar-home-set>"))
			case r.Method == "PROPFIND" && r.URL.Path == "/home":
				w.WriteHeader(http.StatusMultiStatus)
				_, _ = fmt.Fprint(w, calDAVMultiStatus("/discovered", "<d:resourcetype><d:collection/><c:calendar/></d:resourcetype><d:displayname>Calendar</d:displayname>"))
			case r.Method == "REPORT":
				mu.Lock()
				reportPaths = append(reportPaths, r.URL.Path)
				mu.Unlock()
				w.WriteHeader(http.StatusMultiStatus)
				_, _ = fmt.Fprint(w, calDAVMultiStatus("/discovered/event.ics", "<d:getetag>&quot;1&quot;</d:getetag><c:calendar-data>"+calDAVEvent+"</c:calendar-data>"))
			default:
				http.Error(w, "unexpected request", http.StatusNotFound)
			}
		}))
		defer server.Close()

		events, err := FetchCalDAV(context.Background(), server.URL+"/base", "user", "pass", time.Time{}, queryEnd)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(reportPaths, []string{"/discovered"}) {
			t.Fatalf("REPORT paths = %v, want [/discovered]", reportPaths)
		}
		wantStart := time.Date(2026, 7, 10, 10, 0, 0, 0, time.UTC)
		if len(events) != 1 || events[0].ExternalID != "caldav-1" || events[0].Title != "Planning" || !events[0].StartsAt.Equal(wantStart) || events[0].Description != "Quarterly planning" || events[0].Location != "Room 2" || len(events[0].Attendees) != 1 || events[0].Attendees[0].Email != "alice@example.com" {
			t.Fatalf("FetchCalDAV() events = %#v", events)
		}
	})

	t.Run("failed discovery falls back to base URL", func(t *testing.T) {
		var reportPath string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "PROPFIND" {
				http.Error(w, "discovery unavailable", http.StatusInternalServerError)
				return
			}
			reportPath = r.URL.Path
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusMultiStatus)
			_, _ = fmt.Fprint(w, calDAVMultiStatus("/base/event.ics", "<c:calendar-data>"+calDAVEvent+"</c:calendar-data>"))
		}))
		defer server.Close()

		events, err := FetchCalDAV(context.Background(), server.URL+"/base", "user", "pass", time.Time{}, queryEnd)
		if err != nil {
			t.Fatal(err)
		}
		if reportPath == "" || !strings.HasSuffix(reportPath, "/base") || len(events) != 1 {
			t.Fatalf("fallback REPORT path = %q, events = %d; want a request for base URL", reportPath, len(events))
		}
	})

	t.Run("all candidate paths fail", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "unavailable", http.StatusInternalServerError)
		}))
		defer server.Close()
		baseURL := server.URL + "/base"

		_, err := FetchCalDAV(context.Background(), baseURL, "user", "pass", time.Time{}, queryEnd)
		if err == nil || !strings.Contains(err.Error(), baseURL) || !strings.Contains(err.Error(), "find current user principal") || !strings.Contains(err.Error(), "query caldav calendar") {
			t.Fatalf("FetchCalDAV() error = %v, want joined discovery and query errors naming %q", err, baseURL)
		}
	})
}
