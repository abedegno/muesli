package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/abedegno/muesli/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func actionItemOwnerPersonID(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	v := ns.String
	return &v
}

func actionItemArg(v *string) any {
	if v == nil {
		return nil
	}
	return *v
}

// ReplaceActionItemsForNote replaces a note's stored action items and decisions
// in one transaction. The note must exist, belong to ownerID, and be live.
func (s *Store) ReplaceActionItemsForNote(ctx context.Context, ownerID, noteID string, items []model.ActionItem, decisions []model.Decision) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var exists bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM notes WHERE id=$1 AND owner_id=$2 AND deleted_at IS NULL)`,
		noteID, ownerID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}

	if _, err := tx.Exec(ctx, `DELETE FROM action_items WHERE note_id=$1 AND owner_id=$2`, noteID, ownerID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM decisions WHERE note_id=$1 AND owner_id=$2`, noteID, ownerID); err != nil {
		return err
	}

	for _, item := range items {
		if _, err := tx.Exec(ctx,
			`INSERT INTO action_items (id, note_id, owner_id, text, owner_person_id, status, due_hint, created_at)
			 VALUES ($1,$2,$3,$4,NULL,$5,$6,NOW())`,
			uuid.NewString(), noteID, ownerID, item.Text, model.ActionItemOpen, item.DueHint); err != nil {
			return err
		}
	}
	for _, decision := range decisions {
		if _, err := tx.Exec(ctx,
			`INSERT INTO decisions (id, note_id, owner_id, text, created_at)
			 VALUES ($1,$2,$3,$4,NOW())`,
			uuid.NewString(), noteID, ownerID, decision.Text); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

// ListForNote returns all action items and decisions for one live note owned by
// ownerID. If the note is missing, deleted, or not owned by ownerID it returns
// ErrNotFound. Empty result sets are returned as empty, non-nil slices.
func (s *Store) ListForNote(ctx context.Context, ownerID, noteID string) ([]model.ActionItem, []model.Decision, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM notes WHERE id=$1 AND owner_id=$2 AND deleted_at IS NULL)`,
		noteID, ownerID).Scan(&exists); err != nil {
		return nil, nil, err
	}
	if !exists {
		return nil, nil, ErrNotFound
	}

	items, err := s.listActionItems(ctx,
		`SELECT id, note_id, owner_id, text, owner_person_id, status, due_hint, created_at
		   FROM action_items
		  WHERE note_id=$1 AND owner_id=$2
		  ORDER BY created_at, id`,
		noteID, ownerID)
	if err != nil {
		return nil, nil, err
	}
	decisions, err := s.listDecisions(ctx,
		`SELECT id, note_id, owner_id, text, created_at
		   FROM decisions
		  WHERE note_id=$1 AND owner_id=$2
		  ORDER BY created_at, id`,
		noteID, ownerID)
	if err != nil {
		return nil, nil, err
	}
	return items, decisions, nil
}

// ListForOwner returns the owner's action items across live notes. When status
// is non-empty it filters by the exact stored value.
func (s *Store) ListForOwner(ctx context.Context, ownerID string, status string) ([]model.ActionItem, error) {
	query := `SELECT ai.id, ai.note_id, ai.owner_id, ai.text, ai.owner_person_id, ai.status, ai.due_hint, ai.created_at
	            FROM action_items ai
	            JOIN notes n ON n.id = ai.note_id
	           WHERE ai.owner_id=$1 AND n.owner_id=$1 AND n.deleted_at IS NULL`
	args := []any{ownerID}
	if status != "" {
		query += ` AND ai.status=$2`
		args = append(args, status)
	}
	query += ` ORDER BY ai.created_at, ai.id`
	return s.listActionItems(ctx, query, args...)
}

// SetStatus updates one action item's status for ownerID and returns the row.
// ErrNotFound is returned when the item does not exist, is not owned, or its
// parent note is missing/trashed.
func (s *Store) SetStatus(ctx context.Context, ownerID, id, status string) (model.ActionItem, error) {
	var item model.ActionItem
	var ownerPerson sql.NullString
	err := s.pool.QueryRow(ctx,
		`UPDATE action_items ai
		    SET status=$1
		   FROM notes n
		  WHERE ai.id=$2
		    AND ai.owner_id=$3
		    AND n.owner_id=$3
		    AND ai.note_id = n.id
		    AND n.deleted_at IS NULL
		 RETURNING ai.id, ai.note_id, ai.owner_id, ai.text, ai.owner_person_id, ai.status, ai.due_hint, ai.created_at`,
		status, id, ownerID).
		Scan(&item.ID, &item.NoteID, &item.OwnerID, &item.Text, &ownerPerson, &item.Status, &item.DueHint, &item.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.ActionItem{}, ErrNotFound
	}
	if err != nil {
		return model.ActionItem{}, err
	}
	item.OwnerPersonID = actionItemOwnerPersonID(ownerPerson)
	return item, nil
}

// UpdateActionItem updates the non-nil action item fields for ownerID and
// returns the row. ErrNotFound is returned when the item does not exist, is
// not owned, or its parent note is missing/trashed.
func (s *Store) UpdateActionItem(ctx context.Context, ownerID, id string, text *string, status *string) (model.ActionItem, error) {
	var item model.ActionItem
	var ownerPerson sql.NullString
	err := s.pool.QueryRow(ctx,
		`UPDATE action_items ai
		    SET text = COALESCE($1, ai.text),
		        status = COALESCE($2, ai.status)
		   FROM notes n
		  WHERE ai.id=$3
		    AND ai.owner_id=$4
		    AND n.owner_id=$4
		    AND ai.note_id = n.id
		    AND n.deleted_at IS NULL
		 RETURNING ai.id, ai.note_id, ai.owner_id, ai.text, ai.owner_person_id, ai.status, ai.due_hint, ai.created_at`,
		actionItemArg(text), actionItemArg(status), id, ownerID).
		Scan(&item.ID, &item.NoteID, &item.OwnerID, &item.Text, &ownerPerson, &item.Status, &item.DueHint, &item.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.ActionItem{}, ErrNotFound
	}
	if err != nil {
		return model.ActionItem{}, err
	}
	item.OwnerPersonID = actionItemOwnerPersonID(ownerPerson)
	return item, nil
}

// AssignOwner updates an action item's owner_person_id. When personID is nil it
// clears the owner; otherwise the person must exist and belong to ownerID.
func (s *Store) AssignOwner(ctx context.Context, ownerID, id string, personID *string) (model.ActionItem, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(
		   SELECT 1
		     FROM action_items ai
		     JOIN notes n ON n.id = ai.note_id
		    WHERE ai.id=$1
		      AND ai.owner_id=$2
		      AND n.owner_id=$2
		      AND n.deleted_at IS NULL
		)`,
		id, ownerID).Scan(&exists); err != nil {
		return model.ActionItem{}, err
	}
	if !exists {
		return model.ActionItem{}, ErrNotFound
	}

	if personID != nil {
		if _, err := s.GetPerson(ctx, ownerID, *personID); err != nil {
			if errors.Is(err, ErrNotFound) {
				return model.ActionItem{}, ErrInvalidOwner
			}
			return model.ActionItem{}, err
		}
	}

	var item model.ActionItem
	var ownerPerson sql.NullString
	var query string
	var args []any
	if personID == nil {
		query = `UPDATE action_items ai
		    SET owner_person_id = NULL
		   FROM notes n
		  WHERE ai.id=$1
		    AND ai.owner_id=$2
		    AND n.owner_id=$2
		    AND ai.note_id = n.id
		    AND n.deleted_at IS NULL
		 RETURNING ai.id, ai.note_id, ai.owner_id, ai.text, ai.owner_person_id, ai.status, ai.due_hint, ai.created_at`
		args = []any{id, ownerID}
	} else {
		query = `UPDATE action_items ai
		    SET owner_person_id = $1::uuid
		   FROM notes n
		  WHERE ai.id=$2
		    AND ai.owner_id=$3
		    AND n.owner_id=$3
		    AND ai.note_id = n.id
		    AND n.deleted_at IS NULL
		 RETURNING ai.id, ai.note_id, ai.owner_id, ai.text, ai.owner_person_id, ai.status, ai.due_hint, ai.created_at`
		args = []any{*personID, id, ownerID}
	}
	err := s.pool.QueryRow(ctx, query, args...).
		Scan(&item.ID, &item.NoteID, &item.OwnerID, &item.Text, &ownerPerson, &item.Status, &item.DueHint, &item.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.ActionItem{}, ErrNotFound
	}
	if err != nil {
		return model.ActionItem{}, err
	}
	item.OwnerPersonID = actionItemOwnerPersonID(ownerPerson)
	return item, nil
}

func (s *Store) listActionItems(ctx context.Context, query string, args ...any) ([]model.ActionItem, error) {
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []model.ActionItem{}
	for rows.Next() {
		var item model.ActionItem
		var ownerPerson sql.NullString
		if err := rows.Scan(&item.ID, &item.NoteID, &item.OwnerID, &item.Text, &ownerPerson, &item.Status, &item.DueHint, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.OwnerPersonID = actionItemOwnerPersonID(ownerPerson)
		out = append(out, item)
	}
	if out == nil {
		out = []model.ActionItem{}
	}
	return out, rows.Err()
}

func (s *Store) listDecisions(ctx context.Context, query string, args ...any) ([]model.Decision, error) {
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []model.Decision{}
	for rows.Next() {
		var decision model.Decision
		if err := rows.Scan(&decision.ID, &decision.NoteID, &decision.OwnerID, &decision.Text, &decision.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, decision)
	}
	if out == nil {
		out = []model.Decision{}
	}
	return out, rows.Err()
}
