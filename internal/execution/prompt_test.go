package execution

import (
	"os"
	"strings"
	"testing"

	"github.com/abedegno/muesli/internal/model"
)

func twoDocInput() ExecutionInput {
	return ExecutionInput{
		Template: model.Template{
			Sections: []model.TemplateSection{
				{Heading: "Decisions", Instruction: "List decisions."},
				{Heading: "Risks", Instruction: "List unresolved risks."},
			},
		},
		Documents: []Document{
			{
				NoteID:               "note-1",
				Title:                "Sprint planning",
				OccurredAt:           "2026-07-01T09:00:00Z",
				TranscriptGeneration: 2,
				Segments: []model.Segment{
					{StartMS: 0, EndMS: 1000, Text: "Let's ship by Friday.", Speaker: "Alice"},
					{StartMS: 1000, EndMS: 2000, Text: "Agreed.", Speaker: "Bob"},
				},
			},
			{
				NoteID:               "note-2",
				Title:                "Retro",
				OccurredAt:           "2026-07-08T09:00:00Z",
				TranscriptGeneration: 1,
				Segments: []model.Segment{
					{StartMS: 0, EndMS: 500, Text: "We missed the deadline."},
				},
			},
		},
		RunInstruction: "Compare decisions and unresolved risks",
	}
}

// TestPrepareDocumentsOneDocumentCompatibility proves a single-document
// input (the existing one-note callers' shape) still prepares correctly:
// exactly one meeting block, sequential citations starting at 1.
func TestPrepareDocumentsOneDocumentCompatibility(t *testing.T) {
	input := ExecutionInput{
		Template:  model.Template{Sections: []model.TemplateSection{{Heading: "Overview", Instruction: "Summarize."}}},
		Documents: []Document{{NoteID: "note-1", Title: "Solo meeting", Segments: []model.Segment{{StartMS: 0, EndMS: 100, Text: "hello"}}}},
	}
	prepared, err := PrepareDocuments(input)
	if err != nil {
		t.Fatalf("PrepareDocuments: %v", err)
	}
	if len(prepared.Sources) != 1 || prepared.Sources[0].N != 1 || prepared.Sources[0].NoteID != "note-1" {
		t.Fatalf("unexpected sources: %+v", prepared.Sources)
	}
	if strings.Count(prepared.Corpus, "--- MEETING") != 1 {
		t.Fatalf("expected exactly one meeting delimiter, got corpus:\n%s", prepared.Corpus)
	}
}

// TestPrepareDocumentsBoundariesAndCitations proves document boundaries are
// distinct (no transcript concatenation) and citation numbers are assigned
// globally in document, then segment, order.
func TestPrepareDocumentsBoundariesAndCitations(t *testing.T) {
	prepared, err := PrepareDocuments(twoDocInput())
	if err != nil {
		t.Fatalf("PrepareDocuments: %v", err)
	}
	if len(prepared.Sources) != 3 {
		t.Fatalf("expected 3 sources (2+1 segments), got %d: %+v", len(prepared.Sources), prepared.Sources)
	}
	wantNoteByN := map[int]string{1: "note-1", 2: "note-1", 3: "note-2"}
	for _, s := range prepared.Sources {
		if wantNoteByN[s.N] != s.NoteID {
			t.Fatalf("source N=%d note=%q, want %q", s.N, s.NoteID, wantNoteByN[s.N])
		}
	}
	if strings.Count(prepared.Corpus, "--- MEETING") != 2 {
		t.Fatalf("expected 2 distinct meeting delimiters, got corpus:\n%s", prepared.Corpus)
	}
	if !strings.Contains(prepared.Corpus, "note_id=note-1") || !strings.Contains(prepared.Corpus, "note_id=note-2") {
		t.Fatalf("expected both note ids present in corpus:\n%s", prepared.Corpus)
	}
	if !strings.Contains(prepared.Corpus, "Compare decisions and unresolved risks") {
		t.Fatal("expected the run focus to appear in the corpus")
	}
}

// TestPrepareDocumentsCitationDedup proves the same (note_id, segment_index)
// pair reuses one citation number if it were to appear twice within a run
// (the build-time dedup key) rather than being assigned a fresh number.
func TestPrepareDocumentsCitationDedup(t *testing.T) {
	seen := make(map[string]int)
	docs := []Document{{NoteID: "note-1", Segments: []model.Segment{{StartMS: 0, Text: "a"}, {StartMS: 1, Text: "b"}}}}
	input := ExecutionInput{
		Template:  model.Template{Sections: []model.TemplateSection{{Heading: "H", Instruction: "I"}}},
		Documents: docs,
	}
	prepared, err := PrepareDocuments(input)
	if err != nil {
		t.Fatalf("PrepareDocuments: %v", err)
	}
	for _, s := range prepared.Sources {
		key := s.NoteID
		if _, dup := seen[key+string(rune('0'+s.SegmentIndex))]; dup {
			t.Fatalf("unexpected duplicate assignment for %+v", s)
		}
		seen[key+string(rune('0'+s.SegmentIndex))] = s.N
	}
	if len(prepared.Sources) != 2 {
		t.Fatalf("expected 2 distinct sources for 2 distinct segments, got %d", len(prepared.Sources))
	}
}

// TestPrepareDocumentsImmutability proves PrepareDocuments does not mutate
// its input Documents slice (callers may reuse it).
func TestPrepareDocumentsImmutability(t *testing.T) {
	input := twoDocInput()
	before := input.Documents[0].Segments[0].Text
	if _, err := PrepareDocuments(input); err != nil {
		t.Fatalf("PrepareDocuments: %v", err)
	}
	if input.Documents[0].Segments[0].Text != before {
		t.Fatalf("PrepareDocuments mutated input segments: got %q, want %q", input.Documents[0].Segments[0].Text, before)
	}
}

func TestPrepareDocumentsRejectsEmptyDocumentsOrTemplate(t *testing.T) {
	if _, err := PrepareDocuments(ExecutionInput{Template: model.Template{Sections: []model.TemplateSection{{Heading: "H", Instruction: "I"}}}}); err != ErrEmptyDocuments {
		t.Fatalf("empty documents: got %v, want ErrEmptyDocuments", err)
	}
	if _, err := PrepareDocuments(ExecutionInput{Documents: []Document{{NoteID: "n"}}}); err != ErrEmptyTemplate {
		t.Fatalf("empty template: got %v, want ErrEmptyTemplate", err)
	}
}

// TestPrepareDocumentsGoldenCorpus asserts the rendered corpus for a fixed,
// checked-in two-meeting fixture is byte-for-byte the golden file consumed
// by both this package's admission tests (Task 5) and
// plugins/ollama-agent's prompt tests (Task 7) -- see
// testdata/cross_prompts/two_meetings.golden.
func TestPrepareDocumentsGoldenCorpus(t *testing.T) {
	prepared, err := PrepareDocuments(twoDocInput())
	if err != nil {
		t.Fatalf("PrepareDocuments: %v", err)
	}
	if os.Getenv("WRITE_GOLDEN") == "1" {
		if err := os.WriteFile("testdata/cross_prompts/two_meetings.golden", []byte(prepared.Corpus), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
	}
	golden, err := os.ReadFile("testdata/cross_prompts/two_meetings.golden")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if prepared.Corpus != string(golden) {
		t.Fatalf("corpus does not match golden.\ngot:\n%s\nwant:\n%s", prepared.Corpus, string(golden))
	}
}

// TestBuildSectionPromptMatchesProviderEnvelopeGolden proves the exact
// prompt Admit measures (buildSectionPrompt's output) is byte-for-byte the
// same envelope plugins/ollama-agent/ollama_app/prompt.py's
// build_documents_section_prompt sends to the model -- not a shorter
// synthetic placeholder. plugins/ollama-agent/tests's
// test_documents_prompt.py builds the identical prompt from the same
// shared golden corpus and section heading/instruction and asserts it
// matches this same fixture, so any future drift between the two envelopes
// fails a test on both sides rather than silently under-counting the real
// admitted byte budget.
func TestBuildSectionPromptMatchesProviderEnvelopeGolden(t *testing.T) {
	prepared, err := PrepareDocuments(twoDocInput())
	if err != nil {
		t.Fatalf("PrepareDocuments: %v", err)
	}
	if len(prepared.SectionPrompts) == 0 || prepared.SectionPrompts[0].Heading != "Decisions" {
		t.Fatalf("unexpected section prompts: %+v", prepared.SectionPrompts)
	}
	got := prepared.SectionPrompts[0].Prompt
	const goldenPath = "testdata/cross_prompts/two_meetings_decisions_section.golden"
	if os.Getenv("WRITE_GOLDEN") == "1" {
		if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
	}
	golden, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if got != string(golden) {
		t.Fatalf("section prompt does not match golden.\ngot:\n%s\nwant:\n%s", got, string(golden))
	}
}
