package store

import (
	"context"
	"strconv"
	"strings"
)

// ScoredNote is a note id with its semantic similarity score (higher = closer).
type ScoredNote struct {
	ID    string
	Score float64
}

// vectorLiteral renders a []float32 as a pgvector text literal "[v1,v2,...]"
// suitable for binding and casting with ::vector (no Go dependency required).
func vectorLiteral(vec []float32) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, v := range vec {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(v), 'f', -1, 32))
	}
	b.WriteByte(']')
	return b.String()
}

// UpsertEmbedding stores/replaces a note's embedding vector, tagged with the
// model and dimension. The dimension is derived from len(vec) to ensure the stored
// dim always matches the actual vector length (preventing caller mislabeling). The
// embedding column is unsized (mixed-dimension), so any model's vector is accepted;
// search filters by (model, dim) to keep <=> comparisons dimension-consistent.
func (s *Store) UpsertEmbedding(ctx context.Context, noteID, model string, vec []float32) error {
	dim := len(vec) // derive from vector length to maintain invariant
	_, err := s.pool.Exec(ctx,
		`INSERT INTO note_embeddings (note_id, embedding, model, dim) VALUES ($1, $2::vector, $3, $4)
		 ON CONFLICT (note_id) DO UPDATE SET embedding = excluded.embedding, model = excluded.model, dim = excluded.dim, updated_at = now()`,
		noteID, vectorLiteral(vec), model, dim)
	return err
}

// SearchEmbeddings returns up to limit notes nearest (cosine) to vec, owner-scoped,
// restricted to embeddings produced by the given model AND dimension (so all compared vectors
// share a dimension), excluding trashed, ordered closest-first. Score = 1 - cosine_distance.
func (s *Store) SearchEmbeddings(ctx context.Context, ownerID, model string, vec []float32, dim, limit int) ([]ScoredNote, error) {
	lit := vectorLiteral(vec)
	rows, err := s.pool.Query(ctx,
		`SELECT n.id, 1 - (e.embedding <=> $1::vector) AS score
		 FROM note_embeddings e JOIN notes n ON n.id = e.note_id
		 WHERE n.owner_id = $2 AND n.deleted_at IS NULL AND e.model = $4 AND e.dim = $5
		 ORDER BY e.embedding <=> $1::vector
		 LIMIT $3`, lit, ownerID, limit, model, dim)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ScoredNote
	for rows.Next() {
		var sn ScoredNote
		if err := rows.Scan(&sn.ID, &sn.Score); err != nil {
			return nil, err
		}
		out = append(out, sn)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// DeleteEmbeddingsForModel removes every embedding produced by the given model and dimension
// and returns the number of rows deleted. Used by `muesli reembed` to clear the
// current model's vectors before a forced full re-embed. Segregating by dim ensures a
// dimension change triggers a full re-embed (stale-dim rows are left intact but invisible).
func (s *Store) DeleteEmbeddingsForModel(ctx context.Context, model string, dim int) (int, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM note_embeddings WHERE model = $1 AND dim = $2`, model, dim)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

// EmbeddingStatus reports re-embed progress for the given (model, dim) pair:
// total is the count of ready, non-trashed notes (the eligible population);
// done is how many of those already have a note_embeddings row matching BOTH
// model AND dim. Dimension-aware so a stale-dim row for the same model name
// (e.g. after a dimension change) is correctly excluded from done. Used by the
// admin on-demand re-embed panel (EMB02) to show progress.
func (s *Store) EmbeddingStatus(ctx context.Context, model string, dim int) (done, total int, err error) {
	err = s.pool.QueryRow(ctx,
		`SELECT
		   count(*) AS total,
		   count(*) FILTER (
		     WHERE EXISTS (
		       SELECT 1 FROM note_embeddings e
		       WHERE e.note_id = n.id AND e.model = $1 AND e.dim = $2
		     )
		   ) AS done
		 FROM notes n
		 WHERE n.status = 'ready' AND n.deleted_at IS NULL`,
		model, dim).Scan(&total, &done)
	if err != nil {
		return 0, 0, err
	}
	return done, total, nil
}

// NotesMissingEmbedding returns ids of ready, non-trashed notes that have no
// embedding row for the given (model, dim) pair yet (for backfill), capped at limit.
// Switching the configured model or dimension makes every note "missing" again so the backfill re-embeds.
func (s *Store) NotesMissingEmbedding(ctx context.Context, model string, dim, limit int) ([]string, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id FROM notes
		 WHERE status = 'ready' AND deleted_at IS NULL
		   AND id NOT IN (SELECT note_id FROM note_embeddings WHERE model = $2 AND dim = $3)
		 ORDER BY created_at DESC
		 LIMIT $1`, limit, model, dim)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
