package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/abedegno/muesli/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const maxTagNameLen = 64

// ErrTagNameInvalid is returned when a tag name is empty or too long.
var ErrTagNameInvalid = errors.New("tag name invalid")

// isInvalidInputErr reports whether err is a Postgres 22P02 error
// (invalid_text_representation), e.g. a malformed UUID passed as a parameter.
func isInvalidInputErr(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "22P02"
}

// AddNoteTag attaches a tag (by name, case-insensitive) to an owner's note,
// creating the tag if needed. Idempotent. Returns the tag.
func (s *Store) AddNoteTag(ctx context.Context, ownerID, noteID, name string) (model.Tag, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return model.Tag{}, fmt.Errorf("%w: empty", ErrTagNameInvalid)
	}
	if len([]rune(name)) > maxTagNameLen {
		return model.Tag{}, fmt.Errorf("%w: too long (max %d)", ErrTagNameInvalid, maxTagNameLen)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.Tag{}, err
	}
	defer tx.Rollback(ctx)

	var exists bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM notes WHERE id=$1 AND owner_id=$2)`, noteID, ownerID).Scan(&exists); err != nil {
		return model.Tag{}, err
	}
	if !exists {
		return model.Tag{}, ErrNotFound
	}

	id := uuid.NewString()
	if _, err := tx.Exec(ctx,
		`INSERT INTO tags (id, owner_id, name) VALUES ($1, $2, $3) ON CONFLICT (owner_id, lower(name)) DO NOTHING`,
		id, ownerID, name); err != nil {
		return model.Tag{}, err
	}
	var tag model.Tag
	if err := tx.QueryRow(ctx,
		`SELECT id, name FROM tags WHERE owner_id=$1 AND lower(name)=lower($2)`, ownerID, name).
		Scan(&tag.ID, &tag.Name); err != nil {
		return model.Tag{}, err
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO note_tags (note_id, tag_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, noteID, tag.ID); err != nil {
		return model.Tag{}, err
	}
	return tag, tx.Commit(ctx)
}

// RemoveNoteTag detaches a tag (by name, case-insensitive) from an owner's note;
// deletes the tag if it is left orphaned. ErrNotFound if tag/link absent for owner.
func (s *Store) RemoveNoteTag(ctx context.Context, ownerID, noteID, name string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var tagID string
	err = tx.QueryRow(ctx,
		`SELECT id FROM tags WHERE owner_id=$1 AND lower(name)=lower($2)`, ownerID, name).Scan(&tagID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}

	ct, err := tx.Exec(ctx,
		`DELETE FROM note_tags WHERE note_id=$1 AND tag_id=$2
		   AND note_id IN (SELECT id FROM notes WHERE owner_id=$3)`, noteID, tagID, ownerID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM tags WHERE id=$1 AND owner_id=$2
		   AND NOT EXISTS (SELECT 1 FROM note_tags WHERE tag_id=$1)`, tagID, ownerID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// NoteTags returns one note's tag names, sorted case-insensitively.
func (s *Store) NoteTags(ctx context.Context, noteID string) ([]string, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT t.name FROM note_tags nt JOIN tags t ON t.id = nt.tag_id
		 WHERE nt.note_id=$1 ORDER BY lower(t.name)`, noteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

// ListTags returns every tag for an owner paired with the count of its live
// (non-deleted) notes, ordered case-insensitively by name. Tags whose only
// notes are trashed are excluded (the JOIN drops them, count 0 rows). The slice
// is never nil so it serializes as [] rather than null.
func (s *Store) ListTags(ctx context.Context, ownerID string) ([]model.TagCount, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT t.id, t.name, count(nt.note_id)
		 FROM tags t
		 JOIN note_tags nt ON nt.tag_id = t.id
		 JOIN notes n ON n.id = nt.note_id AND n.deleted_at IS NULL
		 WHERE t.owner_id = $1
		 GROUP BY t.id, t.name
		 ORDER BY lower(t.name)`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.TagCount{}
	for rows.Next() {
		var tc model.TagCount
		if err := rows.Scan(&tc.ID, &tc.Name, &tc.Count); err != nil {
			return nil, err
		}
		out = append(out, tc)
	}
	return out, rows.Err()
}

// RenameTag renames an owner-scoped tag row by id. The display name on every
// note carrying the tag updates automatically because note_tags joins on
// tag_id and reads tags.name. Returns ErrTagNameInvalid for a blank/too-long
// name, ErrNotFound if the tag id is absent for the owner, and ErrDuplicate if
// another tag for the owner already uses the name (case-insensitive). Renaming
// a tag to its own current spelling/case succeeds (no conflict, same row).
func (s *Store) RenameTag(ctx context.Context, ownerID, tagID, name string) (model.Tag, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return model.Tag{}, fmt.Errorf("%w: empty", ErrTagNameInvalid)
	}
	if len([]rune(name)) > maxTagNameLen {
		return model.Tag{}, fmt.Errorf("%w: too long (max %d)", ErrTagNameInvalid, maxTagNameLen)
	}

	var tag model.Tag
	err := s.pool.QueryRow(ctx,
		`UPDATE tags SET name=$1 WHERE id=$2 AND owner_id=$3 RETURNING id, name`,
		name, tagID, ownerID).Scan(&tag.ID, &tag.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.Tag{}, ErrNotFound
	} else if isUniqueViolation(err) {
		return model.Tag{}, ErrDuplicate
	} else if err != nil {
		return model.Tag{}, err
	}
	return tag, nil
}

// DeleteTag removes a tag and all its note associations for the given owner.
// Returns ErrNotFound if the tag does not exist for the owner.
func (s *Store) DeleteTag(ctx context.Context, ownerID, tagID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Verify the tag belongs to the owner.
	var exists bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM tags WHERE id=$1 AND owner_id=$2)`, tagID, ownerID).Scan(&exists); err != nil {
		if isInvalidInputErr(err) {
			return ErrNotFound
		}
		return err
	}
	if !exists {
		return ErrNotFound
	}

	// Remove all note associations for this tag.
	if _, err := tx.Exec(ctx,
		`DELETE FROM note_tags WHERE tag_id=$1`, tagID); err != nil {
		return err
	}

	// Delete the tag itself.
	if _, err := tx.Exec(ctx,
		`DELETE FROM tags WHERE id=$1 AND owner_id=$2`, tagID, ownerID); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// tagsForNotes returns tag names per note id in one query (avoids N+1).
func (s *Store) tagsForNotes(ctx context.Context, noteIDs []string) (map[string][]string, error) {
	out := map[string][]string{}
	if len(noteIDs) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx,
		`SELECT nt.note_id, t.name FROM note_tags nt JOIN tags t ON t.id = nt.tag_id
		 WHERE nt.note_id = ANY($1) ORDER BY lower(t.name)`, noteIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		out[id] = append(out[id], name)
	}
	return out, rows.Err()
}
