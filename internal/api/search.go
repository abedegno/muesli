package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
)

// SearchMatch is one typed search hit. Results are returned in ranked note order
// and may contain multiple entries per note when multiple locations match.
type SearchMatch struct {
	NoteID    string `json:"note_id"`
	MatchType string `json:"match_type"`
	SegmentID string `json:"segment_id,omitempty"`
	StartMS   int    `json:"start_ms,omitempty"`
	Snippet   string `json:"snippet,omitempty"`
}

// handleSearch implements GET /api/search?q=… — owner-scoped hybrid search that
// blends a lexical title/snippet match with semantic nearest-neighbour ranking
// (when an embedder is configured) and returns typed match objects.
//
// The response is a JSON array of SearchMatch objects (never null), highest-
// ranked note first, capped at 30 notes. An empty query returns [].
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	uid, ok := userIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	ctx := r.Context()

	q := strings.TrimSpace(r.URL.Query().Get("q"))
	from, to, err := parseSearchDateRange(r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if q == "" {
		writeJSON(w, http.StatusOK, []SearchMatch{})
		return
	}

	scores := map[string]float64{} // noteID -> blended score
	noteByID := map[string]model.Note{}

	// Lexical: load the owner's live notes and match title/snippet
	// (case-insensitive substring). Each match adds a flat bonus so keyword
	// hits always surface.
	notes, err := s.deps.Store.ListNotes(ctx, uid, store.ListNotesFilter{
		CreatedFrom: from,
		CreatedTo:   to,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "search failed")
		return
	}
	allowed := make(map[string]struct{}, len(notes))
	lq := strings.ToLower(q)
	for _, n := range notes {
		noteByID[n.ID] = n
		allowed[n.ID] = struct{}{}
		if strings.Contains(strings.ToLower(n.Title), lq) ||
			strings.Contains(strings.ToLower(n.Snippet), lq) {
			scores[n.ID] += 0.5
		}
	}

	// Semantic: embed the query and add cosine-similarity scores (0..1). Only
	// runs when an embedder is configured; any failure degrades to lexical. A
	// minimum-score cutoff keeps weakly-related notes out of the results — without
	// it, nearest-neighbour search returns the whole (small) library, just ranked.
	//
	// hybrid is true only when BOTH Embed and SearchEmbeddings succeed; if
	// either fails we fall back to pure-lexical and must not apply the title
	// boost (which is only meaningful when vector scores are in the mix).
	hybrid := false
	if s.deps.Embedder != nil {
		minScore := s.deps.Config.EmbeddingsMinScore
		if vec, eerr := s.deps.Embedder.Embed(ctx, s.deps.Config.EmbeddingsQueryPrefix+q); eerr != nil {
			slog.WarnContext(ctx, "search: embed query", "error", eerr)
		} else if hits, serr := s.deps.Store.SearchEmbeddings(ctx, uid, s.deps.Config.EmbeddingsModel, vec, s.deps.Embedder.Dim(), 20); serr != nil {
			slog.WarnContext(ctx, "search: vector search", "error", serr)
		} else {
			hitsAdded := 0
			for _, h := range hits {
				if _, ok := allowed[h.ID]; !ok {
					continue
				}
				if h.Score >= minScore {
					scores[h.ID] += h.Score
					hitsAdded++
				}
			}
			// Both vector steps succeeded AND at least one hit survived the
			// minScore cutoff: vector scores are actually in the mix.
			// If every hit was below minScore, zero vector scores were added
			// and the result set is effectively pure-lexical — the title boost
			// must not fire.
			hybrid = hitsAdded > 0
		}
	}

	// Build a title-lookup map so rankScores can apply the hybrid title boost.
	titleByID := make(map[string]string, len(notes))
	for _, n := range notes {
		titleByID[n.ID] = n.Title
	}

	ids := rankScores(scores, titleByID, q, hybrid, 30)
	matches, err := buildNoteMatches(ctx, s.deps.Store, ids, q, noteByID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "search failed")
		return
	}
	writeJSON(w, http.StatusOK, matches)
}

func parseSearchDateRange(fromRaw, toRaw string) (*time.Time, *time.Time, error) {
	parse := func(raw string, endOfDay bool, label string) (*time.Time, error) {
		if raw == "" {
			return nil, nil
		}
		if ts, err := time.Parse(time.RFC3339, raw); err == nil {
			return &ts, nil
		}
		if d, err := time.ParseInLocation("2006-01-02", raw, time.UTC); err == nil {
			if endOfDay {
				ts := d.AddDate(0, 0, 1).Add(-time.Nanosecond)
				return &ts, nil
			}
			return &d, nil
		}
		return nil, fmt.Errorf("invalid %s", label)
	}

	from, err := parse(fromRaw, false, "from")
	if err != nil {
		return nil, nil, err
	}
	to, err := parse(toRaw, true, "to")
	if err != nil {
		return nil, nil, err
	}
	return from, to, nil
}

func buildNoteMatches(ctx context.Context, st *store.Store, rankedIDs []string, q string, noteByID map[string]model.Note) ([]SearchMatch, error) {
	matches := make([]SearchMatch, 0, len(rankedIDs))
	for _, id := range rankedIDs {
		if _, ok := noteByID[id]; !ok {
			continue
		}

		noteMatches := make([]SearchMatch, 0, 4)
		if containsFold(noteByID[id].Title, q) {
			noteMatches = append(noteMatches, SearchMatch{
				NoteID:    id,
				MatchType: "title",
			})
		}

		tr, err := st.GetTranscript(ctx, id)
		if err != nil && err != store.ErrNotFound {
			return nil, err
		}
		if err == nil {
			for _, seg := range tr.Segments {
				if snippet, ok := snippetAroundMatch(seg.Text, q); ok {
					noteMatches = append(noteMatches, SearchMatch{
						NoteID:    id,
						MatchType: "transcript",
						SegmentID: seg.ID,
						StartMS:   seg.StartMS,
						Snippet:   snippet,
					})
				}
			}
		}

		sums, err := st.GetSummaries(ctx, id)
		if err != nil {
			return nil, err
		}
		for _, sum := range sums {
			for _, section := range sum.Sections {
				if snippet, ok := snippetAroundMatch(section.Heading, q); ok {
					noteMatches = append(noteMatches, SearchMatch{
						NoteID:    id,
						MatchType: "summary",
						Snippet:   snippet,
					})
				}
				if snippet, ok := snippetAroundMatch(section.ContentMarkdown, q); ok {
					noteMatches = append(noteMatches, SearchMatch{
						NoteID:    id,
						MatchType: "summary",
						Snippet:   snippet,
					})
				}
			}
		}

		if len(noteMatches) == 0 {
			noteMatches = append(noteMatches, SearchMatch{
				NoteID:    id,
				MatchType: "title",
			})
		}
		matches = append(matches, noteMatches...)
	}
	return matches, nil
}

func containsFold(text, q string) bool {
	if q == "" {
		return false
	}
	textRunes := []rune(text)
	qRunes := []rune(q)
	if len(qRunes) == 0 || len(qRunes) > len(textRunes) {
		return false
	}
	for i := 0; i+len(qRunes) <= len(textRunes); i++ {
		if strings.EqualFold(string(textRunes[i:i+len(qRunes)]), q) {
			return true
		}
	}
	return false
}

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

// rankScores sorts candidate notes by blended score (descending) and returns
// up to cap note IDs.
//
// In the hybrid path (vector + lexical scores both contributed), any note whose
// title contains q (case-insensitive substring) receives a ×1.5 score
// multiplier before the final sort.  Title presence is a strong topical signal
// that cosine similarity alone can under-weight, so this boost surfaces the
// most directly relevant notes above semantically-close but off-topic ones.
func rankScores(scores map[string]float64, titleByID map[string]string, q string, hybrid bool, cap int) []string {
	type scored struct {
		id    string
		score float64
	}

	lq := strings.ToLower(q)

	ranked := make([]scored, 0, len(scores))
	for id, sc := range scores {
		// Apply ×1.5 title-match boost in the hybrid path only.
		// Pure-lexical results already surface title matches via the 0.5 flat
		// bonus; the boost is only meaningful when vector scores are also in play.
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
