package calendar

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const sampleICS = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:evt-1\r\nSUMMARY:Standup\r\nDTSTART:20260710T090000Z\r\nDTEND:20260710T091500Z\r\nLOCATION:https://meet.google.com/abc-defg-hij\r\nATTENDEE;CN=Alice:mailto:alice@example.com\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"

func TestParseICS(t *testing.T) {
	events, err := ParseICS([]byte(sampleICS))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("want 1 event, got %d", len(events))
	}

	event := events[0]
	if event.ExternalID != "evt-1" {
		t.Fatalf("external id: got %q want %q", event.ExternalID, "evt-1")
	}
	if event.Title != "Standup" {
		t.Fatalf("title: got %q want %q", event.Title, "Standup")
	}
	if event.ConferencingURL != "https://meet.google.com/abc-defg-hij" {
		t.Fatalf("conferencing url: got %q want %q", event.ConferencingURL, "https://meet.google.com/abc-defg-hij")
	}
	if len(event.Attendees) != 1 {
		t.Fatalf("attendees: got %d want %d", len(event.Attendees), 1)
	}
	if event.Attendees[0].Email != "alice@example.com" {
		t.Fatalf("attendee email: got %q want %q", event.Attendees[0].Email, "alice@example.com")
	}
}

func TestFetchICS(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		wantErr    string
		wantEvents int
	}{
		{name: "happy path", status: http.StatusOK, body: sampleICS, wantEvents: 1},
		{name: "non-2xx status", status: http.StatusBadGateway, body: sampleICS, wantErr: "unexpected status 502"},
		{name: "invalid calendar", status: http.StatusOK, body: "not an iCalendar", wantErr: "parse ics"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()

			events, err := FetchICS(context.Background(), server.Client(), server.URL)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("FetchICS() error = %v, want containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(events) != tc.wantEvents {
				t.Fatalf("FetchICS() returned %d events, want %d", len(events), tc.wantEvents)
			}
			if events[0].ExternalID != "evt-1" || events[0].Title != "Standup" {
				t.Fatalf("FetchICS() event = %#v", events[0])
			}
		})
	}
}

func TestFetchICSContextCancellation(t *testing.T) {
	requestStarted := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(requestStarted)
		<-r.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := FetchICS(ctx, server.Client(), server.URL)
		done <- err
	}()
	<-requestStarted
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("FetchICS() error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("FetchICS() did not abort after context cancellation")
	}
}
