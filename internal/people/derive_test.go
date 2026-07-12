package people

import (
	"reflect"
	"testing"

	"github.com/abedegno/muesli/internal/model"
)

func TestUniqueAttendeesFromEvents(t *testing.T) {
	events := []model.CalendarEvent{
		{
			ExternalID: "evt-1",
			Attendees: []model.Attendee{
				{Email: "alice@example.com", Name: "Alice"},
				{Email: "BOB@EXAMPLE.COM", Name: ""},
			},
		},
		{
			ExternalID: "evt-2",
			Attendees: []model.Attendee{
				{Email: "alice@example.com", Name: ""},
				{Email: "bob@example.com", Name: "Bob"},
				{Email: "   ", Name: "Ignored"},
			},
		},
	}

	got := uniqueAttendeesFromEvents(events)
	want := []model.Attendee{
		{Email: "alice@example.com", Name: "Alice"},
		{Email: "BOB@EXAMPLE.COM", Name: "Bob"},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("uniqueAttendeesFromEvents(...) = %#v, want %#v", got, want)
	}
}
