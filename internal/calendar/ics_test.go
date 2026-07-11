package calendar

import "testing"

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
