// Package chat implements retrieval for the chat-with-your-notes feature:
// building note-scoped transcript context (with note_speaker_aliases applied,
// mirroring the summarize pipeline's substitution) and ranking cross-note
// top-k candidates for a free-text query using the same hybrid
// lexical+semantic approach as internal/api's /api/search.
//
// This package has no HTTP surface of its own -- a later item (CHT04) wires
// an endpoint on top of Retriever.
package chat

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/abedegno/muesli/internal/config"
	"github.com/abedegno/muesli/internal/embed"
	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
)

// speakerAliasDirective is the fixed instruction line appended after the
// "Speakers: ..." mapping line in the assembled transcript preface. It must
// stay byte-identical to internal/worker/speaker_alias.go's
// speakerAliasDirective -- both tell the agent to use the supplied alias
// names verbatim rather than inventing its own.
const speakerAliasDirective = "Use the provided speaker names verbatim; do not infer, abbreviate, or merge names."

// defaultSemanticCandidates bounds how many nearest-neighbour notes we pull
// from the vector index before blending with lexical scores and capping at k.
// Mirrors internal/api/search.go's handleSearch, which also queries 20.
const defaultSemanticCandidates = 20

// Retriever builds chat context for a note-scoped transcript or a cross-note
// top-k search, reusing the store's data and (optionally) an embedder for
// semantic ranking. It holds only the config fields the retrieval paths need
// -- not the whole config.Config -- so callers can construct it directly in
// tests without a full config.
type Retriever struct {
	Store    *store.Store
	Embedder embed.Embedder // nil disables semantic ranking; degrades to lexical-only

	EmbeddingsModel       string
	EmbeddingsQueryPrefix string
	EmbeddingsMinScore    float64
}

// NewRetriever builds a Retriever from a store, an (optional, possibly nil)
// embedder, and the application config.
func NewRetriever(st *store.Store, emb embed.Embedder, cfg config.Config) *Retriever {
	return &Retriever{
		Store:                 st,
		Embedder:              emb,
		EmbeddingsModel:       cfg.EmbeddingsModel,
		EmbeddingsQueryPrefix: cfg.EmbeddingsQueryPrefix,
		EmbeddingsMinScore:    cfg.EmbeddingsMinScore,
	}
}

// NoteTranscript is the alias-substituted transcript assembly for one note:
// both the ready-to-use chat context text and the underlying segments (with
// note-scoped aliases already substituted into Segment.Speaker), so a later
// item can build per-segment citations directly from Segments.
type NoteTranscript struct {
	NoteID string
	// Segments are the note's transcript segments with any applicable
	// note_speaker_aliases substituted into Speaker. When no alias applies,
	// this is the transcript's original segments, unchanged.
	Segments []model.Segment
	// Text is the assembled transcript, one line per segment ("Speaker: Text"
	// or bare Text when Speaker is empty). When at least one alias applies to
	// a segment present in the transcript, Text is prefixed with a two-line
	// preface (a deterministic "Speakers: RAW -> alias, ..." line sorted by
	// raw label, then the fixed verbatim-use directive) followed by a blank
	// line. When no aliases apply, Text is exactly the unprefixed assembly.
	Text string
}

// NoteTranscript returns the alias-substituted transcript assembly for
// (ownerID, noteID). It returns store.ErrNotFound if the note does not exist,
// is not owned by ownerID, or has no transcript -- the same sentinel in both
// cases, so callers can't distinguish "not yours" from "no transcript" and
// leak ownership information.
func (r *Retriever) NoteTranscript(ctx context.Context, ownerID, noteID string) (*NoteTranscript, error) {
	owned, err := r.Store.NoteOwnedBy(ctx, ownerID, noteID)
	if err != nil {
		return nil, err
	}
	if !owned {
		return nil, store.ErrNotFound
	}

	tr, err := r.Store.GetTranscript(ctx, noteID)
	if err != nil {
		// Propagates store.ErrNotFound as-is when the note has no transcript.
		return nil, err
	}

	aliases, err := r.Store.SpeakerAliasMap(ctx, ownerID, noteID)
	if err != nil {
		return nil, err
	}

	segments, applied := applyAliases(tr.Segments, aliases)
	text := assembleTranscriptText(segments)
	if len(applied) > 0 {
		text = aliasPreface(applied) + "\n\n" + text
	}

	return &NoteTranscript{NoteID: noteID, Segments: segments, Text: text}, nil
}

// applyAliases substitutes note-scoped speaker aliases into segments,
// mirroring internal/worker/speaker_alias.go's applySpeakerAliases gating
// exactly: only aliases whose raw speaker label is actually present on a
// segment in this transcript are applied -- both for substitution and for the
// preface built from the returned map. When aliases is empty or none apply,
// it returns the input segments unchanged and a nil map.
func applyAliases(segments []model.Segment, aliases map[string]string) ([]model.Segment, map[string]string) {
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

// aliasPreface renders the deterministic two-line "Speakers: RAW -> alias,
// ..." + directive preface from an applied raw->alias map, sorted by raw
// label. Byte-identical in wording/format to
// internal/worker/speaker_alias.go's preface.
func aliasPreface(applied map[string]string) string {
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

// assembleTranscriptText renders segments into a single chat-context string,
// one line per segment: "Speaker: Text" when the segment has a speaker,
// otherwise the bare Text.
func assembleTranscriptText(segments []model.Segment) string {
	lines := make([]string, 0, len(segments))
	for _, seg := range segments {
		if seg.Speaker != "" {
			lines = append(lines, seg.Speaker+": "+seg.Text)
		} else {
			lines = append(lines, seg.Text)
		}
	}
	return strings.Join(lines, "\n")
}

// TranscriptRef is one cross-note top-k hit: enough to cite and to fetch more
// context downstream. SegmentID/SegmentIndex/StartMS are populated when a
// transcript segment lexically matching the query was found in the note;
// SegmentIndex is -1 and SegmentID is empty when no such segment was found
// (e.g. a purely semantic hit, or a note with no transcript), in which case
// Snippet falls back to the note's own Snippet field.
type TranscriptRef struct {
	NoteID    string
	NoteTitle string

	SegmentID    string // empty when no transcript segment matched
	SegmentIndex int    // index into the note's transcript segments; -1 when SegmentID is empty
	StartMS      int    // start_ms of the matched segment; 0 when SegmentID is empty

	Snippet string
	Score   float64
}

// TopK ranks the owner's live notes against query using the same hybrid
// lexical+semantic approach as internal/api's handleSearch (lexical
// title/snippet substring scoring blended with optional cosine-similarity
// semantic scoring), caps the result at k, and resolves a citeable segment
// (or timestamp) for each hit from the note's transcript, falling back to the
// note's own Snippet when no transcript segment matches the query text.
//
// It degrades gracefully to lexical-only ranking -- never an error -- when
// the embedder is nil or the embed/vector-search calls fail.
func (r *Retriever) TopK(ctx context.Context, ownerID, query string, k int) ([]TranscriptRef, error) {
	q := strings.TrimSpace(query)
	if q == "" || k <= 0 {
		return []TranscriptRef{}, nil
	}

	notes, err := r.Store.ListNotes(ctx, ownerID, store.ListNotesFilter{})
	if err != nil {
		return nil, err
	}

	scores := map[string]float64{}
	noteByID := make(map[string]model.Note, len(notes))
	allowed := make(map[string]struct{}, len(notes))
	lq := strings.ToLower(q)
	for _, n := range notes {
		noteByID[n.ID] = n
		allowed[n.ID] = struct{}{}
		if strings.Contains(strings.ToLower(n.Title), lq) || strings.Contains(strings.ToLower(n.Snippet), lq) {
			scores[n.ID] += 0.5
		}
	}

	// Semantic: same graceful-degradation contract as handleSearch -- any
	// failure (nil embedder, embed error, vector-search error) simply skips
	// the semantic contribution rather than failing retrieval.
	hybrid := false
	if r.Embedder != nil {
		if vec, eerr := r.Embedder.Embed(ctx, r.EmbeddingsQueryPrefix+q); eerr != nil {
			slog.WarnContext(ctx, "chat retrieval: embed query", "error", eerr)
		} else if hits, serr := r.Store.SearchEmbeddings(ctx, ownerID, r.EmbeddingsModel, vec, r.Embedder.Dim(), defaultSemanticCandidates); serr != nil {
			slog.WarnContext(ctx, "chat retrieval: vector search", "error", serr)
		} else {
			hitsAdded := 0
			for _, h := range hits {
				if _, ok := allowed[h.ID]; !ok {
					continue
				}
				if h.Score >= r.EmbeddingsMinScore {
					scores[h.ID] += h.Score
					hitsAdded++
				}
			}
			hybrid = hitsAdded > 0
		}
	}

	titleByID := make(map[string]string, len(notes))
	for _, n := range notes {
		titleByID[n.ID] = n.Title
	}

	ids := rankTopK(scores, titleByID, q, hybrid, k)

	refs := make([]TranscriptRef, 0, len(ids))
	for _, id := range ids {
		note := noteByID[id]
		ref := TranscriptRef{
			NoteID:       id,
			NoteTitle:    note.Title,
			SegmentIndex: -1,
			Score:        scores[id],
		}

		tr, terr := r.Store.GetTranscript(ctx, id)
		if terr != nil && terr != store.ErrNotFound {
			return nil, terr
		}
		matched := false
		if terr == nil {
			for i, seg := range tr.Segments {
				if snippet, ok := snippetAroundMatch(seg.Text, q); ok {
					ref.SegmentID = seg.ID
					ref.SegmentIndex = i
					ref.StartMS = seg.StartMS
					ref.Snippet = snippet
					matched = true
					break
				}
			}
		}
		if !matched {
			ref.Snippet = note.Snippet
		}
		refs = append(refs, ref)
	}
	return refs, nil
}

// rankTopK sorts candidate notes by blended score (descending, id ascending
// to break ties) and returns up to cap note IDs. Mirrors
// internal/api/search.go's rankScores, including its hybrid-only x1.5
// title-match boost (only meaningful once vector scores are in the mix; the
// lexical-only path already surfaces title matches via the flat 0.5 bonus).
func rankTopK(scores map[string]float64, titleByID map[string]string, q string, hybrid bool, cap int) []string {
	type scored struct {
		id    string
		score float64
	}

	lq := strings.ToLower(q)

	ranked := make([]scored, 0, len(scores))
	for id, sc := range scores {
		if hybrid && strings.Contains(strings.ToLower(titleByID[id]), lq) {
			sc *= 1.5
		}
		ranked = append(ranked, scored{id: id, score: sc})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		return ranked[i].id < ranked[j].id
	})
	if len(ranked) > cap {
		ranked = ranked[:cap]
	}

	ids := make([]string, 0, len(ranked))
	for _, r := range ranked {
		ids = append(ids, r.id)
	}
	return ids
}

// snippetAroundMatch returns a rune-aware context window around the first
// case-insensitive occurrence of q in text, with "…" markers when the window
// is truncated. Mirrors internal/api/search.go's snippetAroundMatch exactly
// (kept as a separate copy since that one is unexported in package api).
func snippetAroundMatch(text, q string) (string, bool) {
	if q == "" {
		return "", false
	}
	textRunes := []rune(text)
	qRunes := []rune(q)
	if len(qRunes) == 0 || len(qRunes) > len(textRunes) {
		return "", false
	}
	for i := 0; i+len(qRunes) <= len(textRunes); i++ {
		if !strings.EqualFold(string(textRunes[i:i+len(qRunes)]), q) {
			continue
		}
		const contextRunes = 24
		start := i - contextRunes
		if start < 0 {
			start = 0
		}
		end := i + len(qRunes) + contextRunes
		if end > len(textRunes) {
			end = len(textRunes)
		}
		snippet := string(textRunes[start:end])
		if start > 0 {
			snippet = "…" + snippet
		}
		if end < len(textRunes) {
			snippet += "…"
		}
		return snippet, true
	}
	return "", false
}
