package worker

import (
	"fmt"
	"sort"
	"strings"

	"github.com/abedegno/muesli/internal/model"
)

// speakerAliasDirective is the fixed instruction line appended after the
// "Speakers: ..." mapping line in the notes_markdown preface. It tells the
// agent to use the supplied alias names verbatim rather than inventing its
// own.
const speakerAliasDirective = "Use the provided speaker names verbatim; do not infer, abbreviate, or merge names."

// applySpeakerAliases builds the transcript segments and notes_markdown value
// to send to the summarize plugin, substituting any raw speaker labels (e.g.
// "SPEAKER_00") that have a note-scoped alias with the user-chosen alias name.
//
// It never mutates segments or notesMarkdown in place: when at least one alias
// applies to a segment actually present in the transcript, it returns a new
// segment slice (with only the aliased segments' Speaker fields replaced) and
// a new notes_markdown string with a two-line preface prepended (a
// deterministic "Speakers: RAW -> alias, ..." line, sorted by raw label, plus
// a verbatim-use directive), followed by a blank line and the original
// content.
//
// When aliases is empty, or none of its entries match a speaker label that
// actually appears in the transcript, segments and notesMarkdown are returned
// completely unchanged (same slice, same string) -- no preface, no behavior
// change.
func applySpeakerAliases(segments []model.Segment, notesMarkdown string, aliases map[string]string) ([]model.Segment, string) {
	if len(aliases) == 0 {
		return segments, notesMarkdown
	}

	// Only the aliases that actually apply to a speaker label present in this
	// transcript are used -- both for substitution and for the preface, so we
	// never mention aliases irrelevant to this note's transcript.
	applied := make(map[string]string)
	for _, seg := range segments {
		if seg.Speaker == "" {
			continue
		}
		if alias, ok := aliases[seg.Speaker]; ok && alias != "" {
			applied[seg.Speaker] = alias
		}
	}
	if len(applied) == 0 {
		return segments, notesMarkdown
	}

	outSegments := make([]model.Segment, len(segments))
	copy(outSegments, segments)
	for i := range outSegments {
		if alias, ok := applied[outSegments[i].Speaker]; ok {
			outSegments[i].Speaker = alias
		}
	}

	rawLabels := make([]string, 0, len(applied))
	for raw := range applied {
		rawLabels = append(rawLabels, raw)
	}
	sort.Strings(rawLabels)

	pairs := make([]string, 0, len(rawLabels))
	for _, raw := range rawLabels {
		pairs = append(pairs, fmt.Sprintf("%s -> %s", raw, applied[raw]))
	}
	preface := "Speakers: " + strings.Join(pairs, ", ") + "\n" + speakerAliasDirective

	outNotesMarkdown := preface + "\n\n" + notesMarkdown
	return outSegments, outNotesMarkdown
}
