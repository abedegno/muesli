package execution

import (
	"fmt"
	"strings"

	"github.com/abedegno/muesli/internal/model"
)

// SystemDirective is the fixed system-role instruction for every
// cross-meeting analysis run: analyze the complete selected corpus (never
// semantic retrieval), keep every meeting distinct, and cite claims with the
// bracketed numbers assigned below. Deliberately NOT byte-identical to
// internal/chat.SystemDirective (ordinary chat's own directive) -- the two
// prompts serve different, independently-evolving features.
const SystemDirective = "You are analyzing multiple distinct meetings together. " +
	"Base every claim strictly on the meetings below; do not use outside knowledge or speculate beyond what they say. " +
	"Treat each meeting as a separate, distinct source; never merge, conflate, or assume continuity between them unless the text itself says so. " +
	"Cite the source(s) supporting each claim inline with bracketed markers matching the numbers shown, e.g. [1] or [2][3]. " +
	"Use speaker names exactly as given verbatim; do not infer, abbreviate, or merge names."

const noFocusNotice = "(none)"

// buildCorpus renders the shared, section-independent portion of the
// prompt: the system directive, one delimited block per document (title,
// date, note id, its speaker-alias preface if the caller already applied
// one into Segments/NotesMarkdown, and its numbered segments), and the
// optional run focus. This exact text is sent verbatim as the plugin
// request's NotesMarkdown -- see internal/execution's package doc and
// Executor.Run. This is a pure, deterministic string transformation; no I/O.
func buildCorpus(input ExecutionInput, sources []SourceRef) string {
	// Build a lookup from (note_id, segment_index) to its assigned citation N.
	numberOf := make(map[string]int, len(sources))
	for _, s := range sources {
		numberOf[fmt.Sprintf("%s\x00%d", s.NoteID, s.SegmentIndex)] = s.N
	}

	var b strings.Builder
	b.WriteString(SystemDirective)
	b.WriteString("\n\nMEETINGS:\n")
	for i, doc := range input.Documents {
		occurred := doc.OccurredAt
		if occurred == "" {
			occurred = "date unknown"
		}
		fmt.Fprintf(&b, "--- MEETING %d: %s | %s | note_id=%s ---\n", i+1, doc.Title, occurred, doc.NoteID)
		if doc.NotesMarkdown != "" {
			b.WriteString(doc.NotesMarkdown)
			b.WriteString("\n")
		}
		for segIdx, seg := range doc.Segments {
			n := numberOf[fmt.Sprintf("%s\x00%d", doc.NoteID, segIdx)]
			if seg.Speaker != "" {
				fmt.Fprintf(&b, "[%d] %s: %s\n", n, seg.Speaker, seg.Text)
			} else {
				fmt.Fprintf(&b, "[%d] %s\n", n, seg.Text)
			}
		}
	}

	b.WriteString("\nFOCUS: ")
	focus := strings.TrimSpace(input.RunInstruction)
	if focus == "" {
		b.WriteString(noFocusNotice)
	} else {
		b.WriteString(focus)
	}
	b.WriteString("\n\nCITATIONS: Cite claims using the bracketed numbers shown above, e.g. [1], [2]. " +
		"Only use numbers that appear above; do not invent new ones.")
	return b.String()
}

// buildSectionPrompt appends one section's heading/instruction to the
// shared corpus, forming the complete, self-contained prompt Admit uses to
// conservatively bound that section (see admission.go). It is never sent to
// the plugin as a single blob -- only used for the budget estimate.
func buildSectionPrompt(corpus string, section model.TemplateSection) string {
	var b strings.Builder
	b.WriteString(corpus)
	b.WriteString("\n\nSECTION: ")
	b.WriteString(section.Heading)
	b.WriteString("\nINSTRUCTION: ")
	b.WriteString(section.Instruction)
	b.WriteString("\n\nOUTPUT: Respond with the content for this section only, in markdown, using the citations as specified above.")
	return b.String()
}
