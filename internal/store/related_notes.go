package store

import "context"

// RelatedNotes returns up to limit other notes closest to the given note's own
// stored embedding, owner-scoped and excluding already-linked notes in either
// direction.
func (s *Store) RelatedNotes(ctx context.Context, ownerID, noteID, model string, dim, limit int) ([]ScoredNote, error) {
	ok, err := s.noteOwnedLive(ctx, ownerID, noteID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNotFound
	}

	rows, err := s.pool.Query(ctx,
		`WITH target AS (
		   SELECT e.embedding
		   FROM note_embeddings e
		   JOIN notes n ON n.id = e.note_id
		   WHERE e.note_id = $3
		     AND e.model = $4
		     AND e.dim = $5
		     AND n.owner_id = $1
		     AND n.deleted_at IS NULL
		   LIMIT 1
		 )
		 SELECT other.note_id, 1 - (target.embedding <=> other.embedding) AS score
		 FROM target
		 JOIN note_embeddings other ON other.model = $4 AND other.dim = $5
		 JOIN notes other_note ON other_note.id = other.note_id
		 WHERE other_note.owner_id = $1
		   AND other_note.deleted_at IS NULL
		   AND other.note_id <> $3
		   AND NOT EXISTS (
		     SELECT 1
		     FROM note_links nl
		     WHERE nl.owner_id = $1
		       AND (
		         (nl.from_note_id = $3 AND nl.to_note_id = other.note_id)
		         OR
		         (nl.from_note_id = other.note_id AND nl.to_note_id = $3)
		       )
		   )
		 ORDER BY target.embedding <=> other.embedding
		 LIMIT $2`, ownerID, limit, noteID, model, dim)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]ScoredNote, 0)
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
