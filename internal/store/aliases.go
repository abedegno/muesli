package store

import (
	"fmt"
	"sort"
	"strings"

	"github.com/abedegno/muesli/internal/model"
)

// speakerAliasDirective is the fixed instruction line appended after the
// "Speakers: ..." mapping line in an assembled transcript preface. Kept
// byte-identical to internal/worker/speaker_alias.go's speakerAliasDirective
// and internal/chat/retrieval.go's own copy of the same constant -- all
// three tell the agent to use the supplied alias names verbatim rather than
// inventing its own. Store owns its own copy (this file) rather than
// importing internal/chat, so internal/store never imports internal/chat
// (see the accepted plan's ruling 6).
const speakerAliasDirective = "Use the provided speaker names verbatim; do not infer, abbreviate, or merge names."

// ApplyAliases substitutes note-scoped speaker aliases into segments,
// copy-on-write: when no alias in aliases applies to any segment actually
// present, it returns segments UNCHANGED (the same slice, not a copy) and a
// nil map, so a caller with no aliases pays no allocation. When at least one
// alias applies, it returns a new slice (the input is never mutated) and the
// map of raw-label -> alias actually applied (only those present on a
// segment here -- gating identical to
// internal/worker/speaker_alias.go's applySpeakerAliases and
// internal/chat/retrieval.go's now-independent copy).
func ApplyAliases(segments []model.Segment, aliases map[string]string) ([]model.Segment, map[string]string) {
	if len(aliases) == 0 {
		return segments, nil
	}

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
		return segments, nil
	}

	out := make([]model.Segment, len(segments))
	copy(out, segments)
	for i := range out {
		if alias, ok := applied[out[i].Speaker]; ok {
			out[i].Speaker = alias
		}
	}
	return out, applied
}

// AliasPreface renders the deterministic two-line "Speakers: RAW -> alias,
// ..." + directive preface from an applied raw->alias map (as returned by
// ApplyAliases), sorted by raw label for determinism. Byte-identical in
// wording/format to internal/worker/speaker_alias.go's and
// internal/chat/retrieval.go's own preface renderers.
func AliasPreface(applied map[string]string) string {
	rawLabels := make([]string, 0, len(applied))
	for raw := range applied {
		rawLabels = append(rawLabels, raw)
	}
	sort.Strings(rawLabels)

	pairs := make([]string, 0, len(rawLabels))
	for _, raw := range rawLabels {
		pairs = append(pairs, fmt.Sprintf("%s -> %s", raw, applied[raw]))
	}
	return "Speakers: " + strings.Join(pairs, ", ") + "\n" + speakerAliasDirective
}
