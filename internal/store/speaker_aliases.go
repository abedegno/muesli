package store

import (
	"context"

	"github.com/abedegno/muesli/internal/model"
)

// ListSpeakerAliases returns all speaker aliases for the given note, scoped to
// the owner. Returns an empty (non-nil) slice when no aliases exist.
func (s *Store) ListSpeakerAliases(ctx context.Context, ownerID, noteID string) ([]model.SpeakerAlias, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT note_id, speaker_label, alias_name
		 FROM note_speaker_aliases
		 WHERE owner_id = $1 AND note_id = $2
		 ORDER BY speaker_label`,
		ownerID, noteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.SpeakerAlias{}
	for rows.Next() {
		var a model.SpeakerAlias
		if err := rows.Scan(&a.NoteID, &a.SpeakerLabel, &a.AliasName); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// UpsertSpeakerAlias creates or updates a speaker alias for the given note and
// owner. It verifies the note belongs to the owner before upserting, returning
// ErrNotFound if the note does not exist or is not owned by ownerID.
func (s *Store) UpsertSpeakerAlias(ctx context.Context, ownerID, noteID, speakerLabel, aliasName string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Verify note belongs to owner and is not deleted.
	var exists bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM notes WHERE id = $1 AND owner_id = $2 AND deleted_at IS NULL)`,
		noteID, ownerID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}

	_, err = tx.Exec(ctx,
		`INSERT INTO note_speaker_aliases (note_id, owner_id, speaker_label, alias_name)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (note_id, speaker_label) DO UPDATE SET alias_name = EXCLUDED.alias_name`,
		noteID, ownerID, speakerLabel, aliasName)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// DeleteSpeakerAlias removes a speaker alias for the given note, owner, and
// speaker label. Returns ErrNotFound if no such alias exists for the owner.
func (s *Store) DeleteSpeakerAlias(ctx context.Context, ownerID, noteID, speakerLabel string) error {
	ct, err := s.pool.Exec(ctx,
		`DELETE FROM note_speaker_aliases
		 WHERE owner_id = $1 AND note_id = $2 AND speaker_label = $3`,
		ownerID, noteID, speakerLabel)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SpeakerAliasMap returns a map of speaker_label -> alias_name for the given
// note, scoped to the owner. Used by the full-note read path for read-time
// substitution without modifying the stored transcript_segments rows.
func (s *Store) SpeakerAliasMap(ctx context.Context, ownerID, noteID string) (map[string]string, error) {
	aliases, err := s.ListSpeakerAliases(ctx, ownerID, noteID)
	if err != nil {
		return nil, err
	}
	m := make(map[string]string, len(aliases))
	for _, a := range aliases {
		m[a.SpeakerLabel] = a.AliasName
	}
	return m, nil
}

// NoteOwnedBy reports whether the given note exists, is not deleted, and
// belongs to ownerID. Used by handlers to gate access before querying aliases.
func (s *Store) NoteOwnedBy(ctx context.Context, ownerID, noteID string) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM notes WHERE id = $1 AND owner_id = $2 AND deleted_at IS NULL)`,
		noteID, ownerID).Scan(&exists)
	return exists, err
}
