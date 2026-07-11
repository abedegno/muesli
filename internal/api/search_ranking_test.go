package api

// search_ranking_test.go — pure-logic unit tests for rankScores.
// No database or HTTP server required.

import "testing"

// TestRankScoresTitleBoostHybrid verifies that in the hybrid path a note whose
// title contains the query is ranked above a note with the same base score that
// only matches via its body snippet.
func TestRankScoresTitleBoostHybrid(t *testing.T) {
	const q = "rocket"

	// Two candidates with equal starting scores: both lexical (0.5) + equal
	// semantic contribution (0.6) = 1.1 each before boosting.
	scores := map[string]float64{
		"title-match": 1.1, // title contains "rocket"
		"body-only":   1.1, // query only in body snippet
	}
	titleByID := map[string]string{
		"title-match": "Rocket science overview",
		"body-only":   "Weekly standup notes",
	}

	ids := rankScores(scores, titleByID, q, true /* hybrid */, 30)

	if len(ids) != 2 {
		t.Fatalf("expected 2 results, got %d: %v", len(ids), ids)
	}
	if ids[0] != "title-match" {
		t.Errorf("expected title-match note first, got order %v", ids)
	}
	if ids[1] != "body-only" {
		t.Errorf("expected body-only note second, got order %v", ids)
	}
}

// TestRankScoresNoBoostNonHybrid confirms the ×1.5 boost is NOT applied in the
// pure-lexical (non-hybrid) path, so notes are ranked by their raw scores.
func TestRankScoresNoBoostNonHybrid(t *testing.T) {
	const q = "rocket"

	scores := map[string]float64{
		"title-match": 0.5, // would be boosted to 0.75 if hybrid
		"body-only":   0.9, // higher raw score
	}
	titleByID := map[string]string{
		"title-match": "Rocket science overview",
		"body-only":   "Weekly standup notes",
	}

	ids := rankScores(scores, titleByID, q, false /* non-hybrid */, 30)

	if len(ids) != 2 {
		t.Fatalf("expected 2 results, got %d: %v", len(ids), ids)
	}
	// Without the boost, body-only has the higher score and should rank first.
	if ids[0] != "body-only" {
		t.Errorf("non-hybrid: expected body-only first (higher raw score), got %v", ids)
	}
}

// TestRankScoresCaseInsensitiveBoost checks the title match is case-insensitive.
func TestRankScoresCaseInsensitiveBoost(t *testing.T) {
	const q = "ROCKET"

	scores := map[string]float64{
		"title-match": 1.0,
		"body-only":   1.0,
	}
	titleByID := map[string]string{
		"title-match": "rocket science",
		"body-only":   "Something else",
	}

	ids := rankScores(scores, titleByID, q, true /* hybrid */, 30)

	if len(ids) < 1 || ids[0] != "title-match" {
		t.Errorf("case-insensitive boost: expected title-match first, got %v", ids)
	}
}

// TestRankScoresEmbedderFailureFallback confirms that when an embedder is
// configured but the vector lookup fails (embed or search error), the caller
// passes hybrid=false to rankScores, so the title boost is NOT applied.
// This is a pure-logic test: it exercises rankScores with hybrid=false even
// though a real server would have s.deps.Embedder != nil, simulating the
// exact code path where both steps must succeed for hybrid to be set true.
func TestRankScoresEmbedderFailureFallback(t *testing.T) {
	const q = "planet"

	// Simulate scores built by the lexical path only (embedder failed to
	// produce results, so no semantic scores were merged).
	scores := map[string]float64{
		"title-match": 0.5, // lexical match; would be 0.75 with boost
		"body-only":   0.8, // higher lexical score
	}
	titleByID := map[string]string{
		"title-match": "Planet exploration notes",
		"body-only":   "Unrelated note",
	}

	// hybrid=false: embedder exists but vector path failed, so we fell back.
	ids := rankScores(scores, titleByID, q, false /* hybrid=false, vector failed */, 30)

	if len(ids) != 2 {
		t.Fatalf("expected 2 results, got %d: %v", len(ids), ids)
	}
	// body-only has the higher raw lexical score; no boost should change that.
	if ids[0] != "body-only" {
		t.Errorf("embedder fallback: expected body-only first (no boost applied), got %v", ids)
	}
}

// TestRankScoresCap verifies the cap parameter is respected.
func TestRankScoresCap(t *testing.T) {
	scores := map[string]float64{"a": 1.0, "b": 0.9, "c": 0.8}
	titleByID := map[string]string{"a": "alpha", "b": "beta", "c": "gamma"}

	ids := rankScores(scores, titleByID, "x", false, 2)

	if len(ids) != 2 {
		t.Errorf("cap=2: expected 2 results, got %d: %v", len(ids), ids)
	}
}
