package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/abedegno/muesli/internal/model"
)

// ErrCrossAnalysisTooFewNotes is returned by LoadCrossAnalysisSnapshot when
// fewer than model.CrossAnalysisMinNotes distinct note IDs were requested.
// Maps to 400 at the API boundary.
var ErrCrossAnalysisTooFewNotes = errors.New("cross-analysis: at least two notes are required")

// ErrCrossAnalysisTooManyNotes is returned when more than the caller-supplied
// ceiling (at most model.CrossAnalysisMaxNotes) was requested. Maps to 413 --
// distinct from ErrCrossAnalysisTooFewNotes's 400, per the accepted spec's
// status mapping.
var ErrCrossAnalysisTooManyNotes = errors.New("cross-analysis: too many notes requested")

// ErrCrossAnalysisDuplicateNotes is returned when the requested note ID list
// contains a duplicate. Maps to 400.
var ErrCrossAnalysisDuplicateNotes = errors.New("cross-analysis: duplicate note ids")

// ErrCrossAnalysisNoteNotEligible is returned when a requested note exists
// and is visible to ownerID, but is not in successful ready state, has no
// durable transcript, or is partial. Maps to 400 -- distinct from
// ErrNotFound (404), which covers a missing or invisible/other-owner note
// (collapsed to the same generic sentinel so an invalid id is never an
// existence oracle -- see the accepted spec).
var ErrCrossAnalysisNoteNotEligible = errors.New("cross-analysis: note is not ready, durable, and complete")

// CrossAnalysisDocument is one loaded, eligible, alias-substituted note
// ready to feed internal/execution.PrepareDocuments.
type CrossAnalysisDocument struct {
	NoteID               string
	Title                string
	OccurredAt           *time.Time
	TranscriptGeneration int
	Segments             []model.Segment
}

// CrossAnalysisSnapshot is LoadCrossAnalysisSnapshot's immutable result: the
// requested documents in REQUEST order (never reordered, never sampled) plus
// the captured generation for each, keyed by note ID, for the
// immediately-pre-invocation re-check (VerifyCrossAnalysisGenerations).
type CrossAnalysisSnapshot struct {
	Documents   []CrossAnalysisDocument
	Generations map[string]int
}

// LoadCrossAnalysisSnapshot performs the bounded, owner-scoped preflight
// load for a cross-meeting analysis run (issue #765): validates cardinality
// and uniqueness, then loads every requested note's transcript (segments,
// alias-substituted) and generation in a small, fixed number of bulk queries
// (never one round trip per note). A duplicate ID, an ID outside
// [minNotes,maxNotes], a missing/invisible/other-owner note, or a note that
// is not ready, has no durable transcript, or is partial fails the whole
// call -- there is no partial snapshot.
func (s *Store) LoadCrossAnalysisSnapshot(ctx context.Context, ownerID string, noteIDs []string, maxNotes int) (CrossAnalysisSnapshot, error) {
	if len(noteIDs) < model.CrossAnalysisMinNotes {
		return CrossAnalysisSnapshot{}, ErrCrossAnalysisTooFewNotes
	}
	if maxNotes <= 0 || maxNotes > model.CrossAnalysisMaxNotes {
		maxNotes = model.CrossAnalysisMaxNotes
	}
	if len(noteIDs) > maxNotes {
		return CrossAnalysisSnapshot{}, ErrCrossAnalysisTooManyNotes
	}
	seen := make(map[string]struct{}, len(noteIDs))
	for _, id := range noteIDs {
		if _, dup := seen[id]; dup {
			return CrossAnalysisSnapshot{}, ErrCrossAnalysisDuplicateNotes
		}
		seen[id] = struct{}{}
	}

	type noteRow struct {
		id                string
		title             string
		status            string
		startedAt         *time.Time
		createdAt         time.Time
		partialTranscript bool
	}
	notesByID := make(map[string]noteRow, len(noteIDs))
	rows, err := s.pool.Query(ctx,
		`SELECT id, title, status, started_at, created_at, partial_transcript
		   FROM notes
		  WHERE owner_id = $1 AND id = ANY($2) AND deleted_at IS NULL`,
		ownerID, noteIDs)
	if err != nil {
		return CrossAnalysisSnapshot{}, err
	}
	for rows.Next() {
		var r noteRow
		if err := rows.Scan(&r.id, &r.title, &r.status, &r.startedAt, &r.createdAt, &r.partialTranscript); err != nil {
			rows.Close()
			return CrossAnalysisSnapshot{}, err
		}
		notesByID[r.id] = r
	}
	if err := rows.Err(); err != nil {
		return CrossAnalysisSnapshot{}, err
	}
	rows.Close()

	// Every requested id must resolve to an owner-visible note; a missing or
	// other-owner id collapses to the same generic ErrNotFound (never
	// disclosed as "belongs to someone else") so a guessed id is never an
	// existence oracle.
	for _, id := range noteIDs {
		if _, ok := notesByID[id]; !ok {
			return CrossAnalysisSnapshot{}, ErrNotFound
		}
	}

	type transcriptRow struct {
		id         string
		noteID     string
		generation int
	}
	transcriptByNoteID := make(map[string]transcriptRow, len(noteIDs))
	trRows, err := s.pool.Query(ctx,
		`SELECT id, note_id, generation FROM transcripts WHERE note_id = ANY($1)`, noteIDs)
	if err != nil {
		return CrossAnalysisSnapshot{}, err
	}
	for trRows.Next() {
		var t transcriptRow
		if err := trRows.Scan(&t.id, &t.noteID, &t.generation); err != nil {
			trRows.Close()
			return CrossAnalysisSnapshot{}, err
		}
		transcriptByNoteID[t.noteID] = t
	}
	if err := trRows.Err(); err != nil {
		return CrossAnalysisSnapshot{}, err
	}
	trRows.Close()

	// Eligibility: ready status, a durable transcript (present in
	// transcriptByNoteID), and not partial. Checked BEFORE loading segments
	// so an ineligible note never triggers the segment query at all.
	for _, id := range noteIDs {
		n := notesByID[id]
		if n.status != model.NoteReady || n.partialTranscript {
			return CrossAnalysisSnapshot{}, ErrCrossAnalysisNoteNotEligible
		}
		if _, ok := transcriptByNoteID[id]; !ok {
			return CrossAnalysisSnapshot{}, ErrCrossAnalysisNoteNotEligible
		}
	}

	transcriptIDs := make([]string, 0, len(noteIDs))
	transcriptIDToNoteID := make(map[string]string, len(noteIDs))
	for _, id := range noteIDs {
		tr := transcriptByNoteID[id]
		transcriptIDs = append(transcriptIDs, tr.id)
		transcriptIDToNoteID[tr.id] = id
	}

	segmentsByNoteID := make(map[string][]model.Segment, len(noteIDs))
	segRows, err := s.pool.Query(ctx,
		`SELECT transcript_id, id, start_ms, end_ms, text, source, COALESCE(speaker,''), words, confidence,
		        provisional, COALESCE(boundary,'')
		   FROM transcript_segments
		  WHERE transcript_id = ANY($1)
		  ORDER BY transcript_id, start_ms`, transcriptIDs)
	if err != nil {
		return CrossAnalysisSnapshot{}, err
	}
	for segRows.Next() {
		var transcriptID string
		var seg model.Segment
		var wordsJSON []byte
		var confidence *float64
		if err := segRows.Scan(&transcriptID, &seg.ID, &seg.StartMS, &seg.EndMS, &seg.Text, &seg.Source, &seg.Speaker,
			&wordsJSON, &confidence, &seg.Provisional, &seg.Boundary); err != nil {
			segRows.Close()
			return CrossAnalysisSnapshot{}, err
		}
		seg.Confidence = confidence
		if len(wordsJSON) > 0 {
			if err := json.Unmarshal(wordsJSON, &seg.Words); err != nil {
				segRows.Close()
				return CrossAnalysisSnapshot{}, err
			}
		}
		noteID := transcriptIDToNoteID[transcriptID]
		segmentsByNoteID[noteID] = append(segmentsByNoteID[noteID], seg)
	}
	if err := segRows.Err(); err != nil {
		return CrossAnalysisSnapshot{}, err
	}
	segRows.Close()

	documents := make([]CrossAnalysisDocument, 0, len(noteIDs))
	generations := make(map[string]int, len(noteIDs))
	for _, id := range noteIDs {
		n := notesByID[id]
		tr := transcriptByNoteID[id]

		aliases, err := s.SpeakerAliasMap(ctx, ownerID, id)
		if err != nil {
			return CrossAnalysisSnapshot{}, err
		}
		segments, _ := ApplyAliases(segmentsByNoteID[id], aliases)

		occurredAt := n.startedAt
		if occurredAt == nil {
			c := n.createdAt
			occurredAt = &c
		}

		documents = append(documents, CrossAnalysisDocument{
			NoteID:               id,
			Title:                n.title,
			OccurredAt:           occurredAt,
			TranscriptGeneration: tr.generation,
			Segments:             segments,
		})
		generations[id] = tr.generation
	}

	return CrossAnalysisSnapshot{Documents: documents, Generations: generations}, nil
}

// VerifyCrossAnalysisGenerations compares the captured generation map
// (Snapshot.Generations, taken at LoadCrossAnalysisSnapshot time) against
// each note's CURRENT transcript generation in one query, immediately before
// invocation. Returns ErrGenerationMismatch if any of them changed --
// including a transcript that no longer exists (replaced by, e.g., a
// re-transcription that briefly drops the row). A caller with no captured
// generations trivially succeeds (nothing to check).
func (s *Store) VerifyCrossAnalysisGenerations(ctx context.Context, generations map[string]int) error {
	if len(generations) == 0 {
		return nil
	}
	noteIDs := make([]string, 0, len(generations))
	for id := range generations {
		noteIDs = append(noteIDs, id)
	}

	current := make(map[string]int, len(noteIDs))
	rows, err := s.pool.Query(ctx,
		`SELECT note_id, generation FROM transcripts WHERE note_id = ANY($1)`, noteIDs)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var noteID string
		var generation int
		if err := rows.Scan(&noteID, &generation); err != nil {
			return err
		}
		current[noteID] = generation
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for id, captured := range generations {
		got, ok := current[id]
		if !ok || got != captured {
			return ErrGenerationMismatch
		}
	}
	return nil
}
