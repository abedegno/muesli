// This file defines the citation wire shape returned alongside assistant
// chat responses and a pure-logic parser that extracts citation markers
// ("[n]") from response text and resolves them against the sources that were
// offered to the model (see BuildPrompt/RenderRetrievalBlock in context.go).
// It performs no I/O, DB, or HTTP calls.
package chat

import "regexp"

// Source is the wire shape for one citation returned alongside assistant
// text: the 1-indexed marker number the response used, plus enough
// information to render/link to the underlying transcript segment.
type Source struct {
	N            int    `json:"n"`
	NoteID       string `json:"note_id"`
	SegmentIndex int    `json:"segment_index"`
	Timestamp    int    `json:"timestamp"` // milliseconds, from TranscriptRef.StartMS
	Snippet      string `json:"snippet"`
}

// citationMarkerRE matches bracketed digit-only markers like "[1]" or "[42]".
// Malformed brackets ("[abc]", unterminated "[1") do not match.
var citationMarkerRE = regexp.MustCompile(`\[(\d+)\]`)

// ParseCitations scans responseText for "[n]" markers and, for each DISTINCT
// marker found (deduplicated, keeping first-appearance order) whose n falls
// within [1, len(sources)], emits a Source built from sources[n-1]. Markers
// referencing an out-of-range n (n < 1 or n > len(sources)) are silently
// dropped -- never an error, never a panic. A response with no valid markers
// returns an empty (len 0, non-nil) slice.
func ParseCitations(responseText string, sources []TranscriptRef) []Source {
	out := make([]Source, 0)
	seen := make(map[int]struct{})

	for _, match := range citationMarkerRE.FindAllStringSubmatch(responseText, -1) {
		n, ok := parseMarkerNumber(match[1])
		if !ok {
			continue
		}
		if _, dup := seen[n]; dup {
			continue
		}
		seen[n] = struct{}{}

		if n < 1 || n > len(sources) {
			continue
		}
		ref := sources[n-1]
		out = append(out, Source{
			N:            n,
			NoteID:       ref.NoteID,
			SegmentIndex: ref.SegmentIndex,
			Timestamp:    ref.StartMS,
			Snippet:      ref.Snippet,
		})
	}

	return out
}

// parseMarkerNumber converts the regex-captured digit string to an int. The
// regex guarantees digits-only input, so the only failure mode is overflow
// on an implausibly long digit run, which we treat as out-of-range (ok=false)
// rather than erroring.
func parseMarkerNumber(digits string) (int, bool) {
	n := 0
	for _, c := range digits {
		n = n*10 + int(c-'0')
		if n > 1<<30 {
			return 0, false
		}
	}
	return n, true
}
