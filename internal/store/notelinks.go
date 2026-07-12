package store

import (
	"context"
	"errors"

	"github.com/abedegno/muesli/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func scanNoteLink(row interface{ Scan(...any) error }) (model.NoteLink, error) {
	var link model.NoteLink
	err := row.Scan(&link.ID, &link.OwnerID, &link.FromNoteID, &link.ToNoteID, &link.CreatedAt)
	return link, err
}

// AddLink creates a directed note-to-note link owned by ownerID.
// Returns ErrSelfLink for from==to, ErrNotFound when either note is missing
// or not owned by ownerID, and ErrDuplicate when the link already exists.
func (s *Store) AddLink(ctx context.Context, ownerID, fromNoteID, toNoteID string) (model.NoteLink, error) {
	if fromNoteID == toNoteID {
		return model.NoteLink{}, ErrSelfLink
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.NoteLink{}, err
	}
	defer tx.Rollback(ctx)

	var link model.NoteLink
	err = tx.QueryRow(ctx,
		`INSERT INTO note_links (id, owner_id, from_note_id, to_note_id)
		 SELECT $4, $1, f.id, t.id
		 FROM notes f
		 JOIN notes t ON t.id = $3 AND t.owner_id = $1 AND t.deleted_at IS NULL
		 WHERE f.id = $2 AND f.owner_id = $1 AND f.deleted_at IS NULL
		 RETURNING id, owner_id, from_note_id, to_note_id, created_at`,
		uuid.NewString(), ownerID, fromNoteID, toNoteID).
		Scan(&link.ID, &link.OwnerID, &link.FromNoteID, &link.ToNoteID, &link.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.NoteLink{}, ErrNotFound
	}
	if isUniqueViolation(err) {
		return model.NoteLink{}, ErrDuplicate
	}
	if err != nil {
		return model.NoteLink{}, err
	}
	return link, tx.Commit(ctx)
}

// RemoveLink deletes one owner-scoped directed note link.
// Returns ErrNotFound when either note is missing/not owned or the link is absent.
func (s *Store) RemoveLink(ctx context.Context, ownerID, fromNoteID, toNoteID string) error {
	if fromNoteID == toNoteID {
		return ErrSelfLink
	}

	ct, err := s.pool.Exec(ctx,
		`DELETE FROM note_links nl
		 USING notes f, notes t
		 WHERE nl.owner_id=$1
		   AND nl.from_note_id=$2
		   AND nl.to_note_id=$3
		   AND f.id = nl.from_note_id AND f.owner_id = $1 AND f.deleted_at IS NULL
		   AND t.id = nl.to_note_id AND t.owner_id = $1 AND t.deleted_at IS NULL`,
		ownerID, fromNoteID, toNoteID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// OutgoingLinks returns links FROM the given note, owner-scoped.
func (s *Store) OutgoingLinks(ctx context.Context, ownerID, noteID string) ([]model.NoteLink, error) {
	ok, err := s.noteOwnedLive(ctx, ownerID, noteID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNotFound
	}

	rows, err := s.pool.Query(ctx,
		`SELECT id, owner_id, from_note_id, to_note_id, created_at
		 FROM note_links
		 WHERE owner_id=$1 AND from_note_id=$2
		 ORDER BY created_at, id`,
		ownerID, noteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []model.NoteLink{}
	for rows.Next() {
		link, err := scanNoteLink(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, link)
	}
	return out, rows.Err()
}

// Backlinks returns links TO the given note, owner-scoped.
func (s *Store) Backlinks(ctx context.Context, ownerID, noteID string) ([]model.NoteLink, error) {
	ok, err := s.noteOwnedLive(ctx, ownerID, noteID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNotFound
	}

	rows, err := s.pool.Query(ctx,
		`SELECT id, owner_id, from_note_id, to_note_id, created_at
		 FROM note_links
		 WHERE owner_id=$1 AND to_note_id=$2
		 ORDER BY created_at, id`,
		ownerID, noteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []model.NoteLink{}
	for rows.Next() {
		link, err := scanNoteLink(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, link)
	}
	return out, rows.Err()
}

func (s *Store) noteOwnedLive(ctx context.Context, ownerID, noteID string) (bool, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM notes WHERE id=$1 AND owner_id=$2 AND deleted_at IS NULL)`,
		noteID, ownerID).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}
