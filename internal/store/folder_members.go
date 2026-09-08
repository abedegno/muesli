package store

import (
	"context"
	"errors"
	"time"

	"github.com/abedegno/muesli/internal/model"
	"github.com/jackc/pgx/v5"
)

// UpsertFolderMember grants userID read-only access to folderID, idempotently
// (a repeat call updates nothing but role and never resets created_at). The
// caller must own a live folderID; userID must be a different, existing
// deployment user; role must be model.FolderRoleViewer. Returns ErrNotFound
// if the folder is absent, not owned, or trashed.
func (s *Store) UpsertFolderMember(ctx context.Context, ownerID, folderID, userID, role string) (model.FolderMember, error) {
	if role != model.FolderRoleViewer {
		return model.FolderMember{}, ValidationError("invalid role")
	}
	if userID == ownerID {
		return model.FolderMember{}, ValidationError("owner cannot grant themselves membership")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.FolderMember{}, err
	}
	defer tx.Rollback(ctx)

	var folderOK bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM folders WHERE id=$1 AND owner_id=$2 AND deleted_at IS NULL)`,
		folderID, ownerID).Scan(&folderOK); err != nil {
		return model.FolderMember{}, err
	}
	if !folderOK {
		return model.FolderMember{}, ErrNotFound
	}

	var email string
	if err := tx.QueryRow(ctx, `SELECT email FROM users WHERE id=$1`, userID).Scan(&email); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.FolderMember{}, ValidationError("user not found")
		}
		return model.FolderMember{}, err
	}

	var fm model.FolderMember
	if err := tx.QueryRow(ctx,
		`INSERT INTO folder_members (folder_id, user_id, role)
		 VALUES ($1,$2,$3)
		 ON CONFLICT (folder_id, user_id) DO UPDATE SET role = EXCLUDED.role
		 RETURNING folder_id, user_id, role, created_at`,
		folderID, userID, role).Scan(&fm.FolderID, &fm.UserID, &fm.Role, &fm.CreatedAt); err != nil {
		return model.FolderMember{}, err
	}
	fm.Email = email

	return fm, tx.Commit(ctx)
}

// DeleteFolderMember revokes userID's access to folderID, idempotently (no
// error if the grant is already absent). The caller must own a live
// folderID; returns ErrNotFound if the folder is absent, not owned, or
// trashed.
func (s *Store) DeleteFolderMember(ctx context.Context, ownerID, folderID, userID string) error {
	var folderOK bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM folders WHERE id=$1 AND owner_id=$2 AND deleted_at IS NULL)`,
		folderID, ownerID).Scan(&folderOK); err != nil {
		return err
	}
	if !folderOK {
		return ErrNotFound
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM folder_members WHERE folder_id=$1 AND user_id=$2`, folderID, userID)
	return err
}

// ListFolderMembers lists up to limit members of folderID, ordered by
// (created_at,user_id), visible to the folder's owner or any of its current
// members -- anyone else, or a trashed/absent folder, gets ErrNotFound.
// Callers pass limit+1 to detect a next page.
func (s *Store) ListFolderMembers(ctx context.Context, requesterID, folderID string, limit int, afterCreatedAt time.Time, afterUserID string) ([]model.FolderMember, error) {
	var visible bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(
		   SELECT 1 FROM folders f
		   WHERE f.id=$1 AND f.deleted_at IS NULL
		     AND (f.owner_id=$2 OR EXISTS(
		       SELECT 1 FROM folder_members fm WHERE fm.folder_id=f.id AND fm.user_id=$2))
		 )`, folderID, requesterID).Scan(&visible); err != nil {
		return nil, err
	}
	if !visible {
		return nil, ErrNotFound
	}

	var rows pgx.Rows
	var err error
	if afterUserID == "" {
		rows, err = s.pool.Query(ctx,
			`SELECT fm.folder_id, fm.user_id, u.email, fm.role, fm.created_at
			 FROM folder_members fm JOIN users u ON u.id = fm.user_id
			 WHERE fm.folder_id=$1
			 ORDER BY fm.created_at, fm.user_id LIMIT $2`, folderID, limit)
	} else {
		rows, err = s.pool.Query(ctx,
			`SELECT fm.folder_id, fm.user_id, u.email, fm.role, fm.created_at
			 FROM folder_members fm JOIN users u ON u.id = fm.user_id
			 WHERE fm.folder_id=$1 AND (fm.created_at, fm.user_id) > ($2,$3)
			 ORDER BY fm.created_at, fm.user_id LIMIT $4`, folderID, afterCreatedAt, afterUserID, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []model.FolderMember{}
	for rows.Next() {
		var fm model.FolderMember
		if err := rows.Scan(&fm.FolderID, &fm.UserID, &fm.Email, &fm.Role, &fm.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, fm)
	}
	return out, rows.Err()
}
