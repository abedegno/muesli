package worker

import (
	"encoding/json"
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

// testAgentForHash returns a fixed agent identity plus non-secret config,
// used as the "agent-1" baseline everywhere below.
func testAgentForHash() preBriefAgentIdentity {
	temp := 0.2
	return preBriefAgentIdentity{
		ID:          "agent-1",
		Name:        "Agent One",
		Model:       "llama3.2:3b",
		Temperature: &temp,
	}
}

// TestComputePreBriefHashStableUnderAttendeeReorder proves reordering the
// same attendee set never changes the hash (reconciliation hashes attendees
// stably ordered, per the accepted spec).
func TestComputePreBriefHashStableUnderAttendeeReorder(t *testing.T) {
	t.Parallel()

	ev := testEventForHash()
	tmpl := testTemplateForHash()
	h1 := computePreBriefHash(ev, tmpl, testAgentForHash())

	reordered := ev
	reordered.Attendees = []model.Attendee{ev.Attendees[1], ev.Attendees[0]}
	h2 := computePreBriefHash(reordered, tmpl, testAgentForHash())

	if h1 != h2 {
		t.Fatalf("hash changed under attendee reorder: %s != %s", h1, h2)
	}
}

// TestComputePreBriefHashChangesForEveryInput proves each individually-varied
// input (event fields, template fields, and agent identity/config) changes
// the hash relative to a fixed baseline.
func TestComputePreBriefHashChangesForEveryInput(t *testing.T) {
	t.Parallel()

	base := computePreBriefHash(testEventForHash(), testTemplateForHash(), testAgentForHash())

	mutations := map[string]func() string{
		"title": func() string {
			ev := testEventForHash()
			ev.Title = "Different title"
			return computePreBriefHash(ev, testTemplateForHash(), testAgentForHash())
		},
		"starts_at": func() string {
			ev := testEventForHash()
			ev.StartsAt = ev.StartsAt.Add(time.Hour)
			return computePreBriefHash(ev, testTemplateForHash(), testAgentForHash())
		},
		"ends_at": func() string {
			ev := testEventForHash()
			ev.EndsAt = ev.EndsAt.Add(time.Hour)
			return computePreBriefHash(ev, testTemplateForHash(), testAgentForHash())
		},
		"description": func() string {
			ev := testEventForHash()
			ev.Description = "Different agenda"
			return computePreBriefHash(ev, testTemplateForHash(), testAgentForHash())
		},
		"location": func() string {
			ev := testEventForHash()
			ev.Location = "Room 9"
			return computePreBriefHash(ev, testTemplateForHash(), testAgentForHash())
		},
		"conferencing_url": func() string {
			ev := testEventForHash()
			ev.ConferencingURL = "https://meet.example.com/xyz"
			return computePreBriefHash(ev, testTemplateForHash(), testAgentForHash())
		},
		"attendee added": func() string {
			ev := testEventForHash()
			ev.Attendees = append(ev.Attendees, model.Attendee{Name: "Cal", Email: "cal@example.com", Response: "declined"})
			return computePreBriefHash(ev, testTemplateForHash(), testAgentForHash())
		},
		"attendee response changed": func() string {
			ev := testEventForHash()
			ev.Attendees[0].Response = "declined"
			return computePreBriefHash(ev, testTemplateForHash(), testAgentForHash())
		},
		"template name": func() string {
			tmpl := testTemplateForHash()
			tmpl.Name = "Different name"
			return computePreBriefHash(testEventForHash(), tmpl, testAgentForHash())
		},
		"template sections": func() string {
			tmpl := testTemplateForHash()
			tmpl.Sections = append(tmpl.Sections, model.TemplateSection{Heading: "Extra", Instruction: "More."})
			return computePreBriefHash(testEventForHash(), tmpl, testAgentForHash())
		},
		"template system prompt": func() string {
			tmpl := testTemplateForHash()
			tmpl.SystemPrompt = "Be terse."
			return computePreBriefHash(testEventForHash(), tmpl, testAgentForHash())
		},
		"template model": func() string {
			tmpl := testTemplateForHash()
			tmpl.Model = "llama3.2:3b"
			return computePreBriefHash(testEventForHash(), tmpl, testAgentForHash())
		},
		"template temperature": func() string {
			tmpl := testTemplateForHash()
			temp := 0.9
			tmpl.Temperature = &temp
			return computePreBriefHash(testEventForHash(), tmpl, testAgentForHash())
		},
		"agent identity": func() string {
			agent := testAgentForHash()
			agent.ID = "agent-2"
			return computePreBriefHash(testEventForHash(), testTemplateForHash(), agent)
		},
		// Guards against the once-shipped regression where the hash covered
		// only the agent's ID|Name and never the plugin's own generation
		// config (model, temperature, etc): changing config with identity
		// held fixed must still change the hash, or editing the default
		// agent's model/temperature would leave every existing brief stale
		// forever (see the accepted spec's "resolved default-agent identity
		// and non-secret generation configuration" hash input requirement).
		"agent config model": func() string {
			agent := testAgentForHash()
			agent.Model = "llama3.2:70b"
			return computePreBriefHash(testEventForHash(), testTemplateForHash(), agent)
		},
		"agent config temperature": func() string {
			agent := testAgentForHash()
			temp := 0.9
			agent.Temperature = &temp
			return computePreBriefHash(testEventForHash(), testTemplateForHash(), agent)
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
	h1 := computePreBriefHash(ev, tmpl, testAgentForHash())
	h2 := computePreBriefHash(ev, tmpl, testAgentForHash())
	if h1 != h2 {
		t.Fatalf("hash not deterministic: %s != %s", h1, h2)
	}
}

// TestExtractNonSecretPluginConfigExcludesUnknownFields proves the
// allowlisted extraction keeps only model/temperature and silently drops
// every other field a plugin's Config might carry -- including
// credential-shaped ones an arbitrary plugin's own config schema could
// define (e.g. api_key) -- per the accepted spec's "non-secret generation
// configuration" hash input requirement.
func TestExtractNonSecretPluginConfigExcludesUnknownFields(t *testing.T) {
	t.Parallel()

	withSecret := extractNonSecretPluginConfig(json.RawMessage(`{"model":"llama3.2:3b","temperature":0.2,"api_key":"sk-super-secret-value"}`))
	withoutSecret := extractNonSecretPluginConfig(json.RawMessage(`{"model":"llama3.2:3b","temperature":0.2}`))

	if withSecret.Model != withoutSecret.Model {
		t.Fatalf("model mismatch: %q != %q", withSecret.Model, withoutSecret.Model)
	}
	if (withSecret.Temperature == nil) != (withoutSecret.Temperature == nil) || *withSecret.Temperature != *withoutSecret.Temperature {
		t.Fatalf("temperature mismatch: %v != %v", withSecret.Temperature, withoutSecret.Temperature)
	}
}

// TestComputePreBriefHashIgnoresSecretPluginConfigFields proves the
// freshness hash is unaffected by a secret-shaped field appearing (or not)
// alongside the allowlisted model/temperature fields in a plugin's raw
// Config -- guarding the fix for the reviewed finding where the hash once
// covered the plugin's entire decrypted Config blob (which "may include
// secrets", per model.Plugin's doc comment) rather than only its non-secret
// generation configuration.
func TestComputePreBriefHashIgnoresSecretPluginConfigFields(t *testing.T) {
	t.Parallel()

	withSecretCfg := extractNonSecretPluginConfig(json.RawMessage(`{"model":"llama3.2:3b","temperature":0.2,"api_key":"sk-super-secret-value"}`))
	withoutSecretCfg := extractNonSecretPluginConfig(json.RawMessage(`{"model":"llama3.2:3b","temperature":0.2}`))

	withSecret := preBriefAgentIdentity{ID: "agent-1", Name: "Agent One", Model: withSecretCfg.Model, Temperature: withSecretCfg.Temperature}
	withoutSecret := preBriefAgentIdentity{ID: "agent-1", Name: "Agent One", Model: withoutSecretCfg.Model, Temperature: withoutSecretCfg.Temperature}

	h1 := computePreBriefHash(testEventForHash(), testTemplateForHash(), withSecret)
	h2 := computePreBriefHash(testEventForHash(), testTemplateForHash(), withoutSecret)
	if h1 != h2 {
		t.Fatalf("hash changed due to a secret-only plugin config field: %s != %s", h1, h2)
	}
}
