package store

import (
	"testing"

	"github.com/abedegno/muesli/internal/model"
)

func TestBuiltInTemplates(t *testing.T) {
	t.Parallel()

	wantNames := map[string]struct{}{
		"General meeting": {},
		"Action items":    {},
		"1:1":             {},
		"Standup":         {},
		"Interview":       {},
		"Sales call":      {},
	}

	gotNames := make(map[string]struct{}, len(builtInTemplates))
	for _, tmpl := range builtInTemplates {
		gotNames[tmpl.Name] = struct{}{}

		sections := make([]model.TemplateSection, len(tmpl.Sections))
		for i, section := range tmpl.Sections {
			sections[i] = model.TemplateSection{
				Heading:     section.Heading,
				Instruction: section.Instruction,
			}
		}
		if err := validateTemplate(tmpl.Name, sections); err != nil {
			t.Fatalf("validateTemplate(%q) = %v", tmpl.Name, err)
		}
	}

	for name := range wantNames {
		if _, ok := gotNames[name]; !ok {
			t.Fatalf("builtInTemplates missing %q", name)
		}
	}
}
