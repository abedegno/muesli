package worker

import (
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/model"
)

func testEventForHash() model.CalendarEvent {
	starts := time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC)
	return model.CalendarEvent{
		ID:              "event_1",
		Title:           "Quarterly planning",
		StartsAt:        starts,
		EndsAt:          starts.Add(30 * time.Minute),
		Description:     "Review roadmap",
		Location:        "Room 2",
		ConferencingURL: "https://meet.example.com/abc",
		Attendees: []model.Attendee{
			{Name: "Bea", Email: "bea@example.com", Response: "accepted"},
			{Name: "Alan", Email: "alan@example.com", Response: "tentative"},
		},
	}
}

func testTemplateForHash() model.Template {
	return model.Template{
		ID:   "template_1",
		Name: "Pre-read",
		Sections: []model.TemplateSection{
			{Heading: "Context", Instruction: "Summarize the agenda."},
		},
	}
}

// TestComputePreBriefHashStableUnderAttendeeReorder proves reordering the
// same attendee set never changes the hash (reconciliation hashes attendees
// stably ordered, per the accepted spec).
func TestComputePreBriefHashStableUnderAttendeeReorder(t *testing.T) {
	t.Parallel()

	ev := testEventForHash()
	tmpl := testTemplateForHash()
	h1 := computePreBriefHash(ev, tmpl, "agent-1")

	reordered := ev
	reordered.Attendees = []model.Attendee{ev.Attendees[1], ev.Attendees[0]}
	h2 := computePreBriefHash(reordered, tmpl, "agent-1")

	if h1 != h2 {
		t.Fatalf("hash changed under attendee reorder: %s != %s", h1, h2)
	}
}

// TestComputePreBriefHashChangesForEveryInput proves each individually-varied
// input (event fields, template fields, and agent identity) changes the hash
// relative to a fixed baseline.
func TestComputePreBriefHashChangesForEveryInput(t *testing.T) {
	t.Parallel()

	base := computePreBriefHash(testEventForHash(), testTemplateForHash(), "agent-1")

	mutations := map[string]func() string{
		"title": func() string {
			ev := testEventForHash()
			ev.Title = "Different title"
			return computePreBriefHash(ev, testTemplateForHash(), "agent-1")
		},
		"starts_at": func() string {
			ev := testEventForHash()
			ev.StartsAt = ev.StartsAt.Add(time.Hour)
			return computePreBriefHash(ev, testTemplateForHash(), "agent-1")
		},
		"ends_at": func() string {
			ev := testEventForHash()
			ev.EndsAt = ev.EndsAt.Add(time.Hour)
			return computePreBriefHash(ev, testTemplateForHash(), "agent-1")
		},
		"description": func() string {
			ev := testEventForHash()
			ev.Description = "Different agenda"
			return computePreBriefHash(ev, testTemplateForHash(), "agent-1")
		},
		"location": func() string {
			ev := testEventForHash()
			ev.Location = "Room 9"
			return computePreBriefHash(ev, testTemplateForHash(), "agent-1")
		},
		"conferencing_url": func() string {
			ev := testEventForHash()
			ev.ConferencingURL = "https://meet.example.com/xyz"
			return computePreBriefHash(ev, testTemplateForHash(), "agent-1")
		},
		"attendee added": func() string {
			ev := testEventForHash()
			ev.Attendees = append(ev.Attendees, model.Attendee{Name: "Cal", Email: "cal@example.com", Response: "declined"})
			return computePreBriefHash(ev, testTemplateForHash(), "agent-1")
		},
		"attendee response changed": func() string {
			ev := testEventForHash()
			ev.Attendees[0].Response = "declined"
			return computePreBriefHash(ev, testTemplateForHash(), "agent-1")
		},
		"template name": func() string {
			tmpl := testTemplateForHash()
			tmpl.Name = "Different name"
			return computePreBriefHash(testEventForHash(), tmpl, "agent-1")
		},
		"template sections": func() string {
			tmpl := testTemplateForHash()
			tmpl.Sections = append(tmpl.Sections, model.TemplateSection{Heading: "Extra", Instruction: "More."})
			return computePreBriefHash(testEventForHash(), tmpl, "agent-1")
		},
		"template system prompt": func() string {
			tmpl := testTemplateForHash()
			tmpl.SystemPrompt = "Be terse."
			return computePreBriefHash(testEventForHash(), tmpl, "agent-1")
		},
		"template model": func() string {
			tmpl := testTemplateForHash()
			tmpl.Model = "llama3.2:3b"
			return computePreBriefHash(testEventForHash(), tmpl, "agent-1")
		},
		"template temperature": func() string {
			tmpl := testTemplateForHash()
			temp := 0.9
			tmpl.Temperature = &temp
			return computePreBriefHash(testEventForHash(), tmpl, "agent-1")
		},
		"agent identity": func() string {
			return computePreBriefHash(testEventForHash(), testTemplateForHash(), "agent-2")
		},
	}

	for name, mutate := range mutations {
		if got := mutate(); got == base {
			t.Errorf("mutation %q did not change the hash (base=%s got=%s)", name, base, got)
		}
	}
}

// TestComputePreBriefHashDeterministic proves the same input always produces
// the same hash (no map-ordering or other nondeterminism).
func TestComputePreBriefHashDeterministic(t *testing.T) {
	t.Parallel()
	ev := testEventForHash()
	tmpl := testTemplateForHash()
	h1 := computePreBriefHash(ev, tmpl, "agent-1")
	h2 := computePreBriefHash(ev, tmpl, "agent-1")
	if h1 != h2 {
		t.Fatalf("hash not deterministic: %s != %s", h1, h2)
	}
}
