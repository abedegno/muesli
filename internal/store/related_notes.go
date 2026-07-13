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
		`SELECT other.note_id, 1 - (target.embedding <=> other.embedding) AS score
		 FROM note_embeddings target
		 JOIN notes target_note ON target_note.id = target.note_id
		 JOIN note_embeddings other ON other.model = target.model AND other.dim = target.dim
		 JOIN notes other_note ON other_note.id = other.note_id
		 WHERE target.note_id = $3
		   AND target.model = $4
		   AND target.dim = $5
		   AND target_note.owner_id = $1
		   AND target_note.deleted_at IS NULL
		   AND other_note.owner_id = $1
		   AND other_note.deleted_at IS NULL
		   AND other.note_id <> target.note_id
		   AND NOT EXISTS (
		     SELECT 1
		     FROM note_links nl
		     WHERE nl.owner_id = $1
		       AND (
		         (nl.from_note_id = target.note_id AND nl.to_note_id = other.note_id)
		         OR
		         (nl.from_note_id = other.note_id AND nl.to_note_id = target.note_id)
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
