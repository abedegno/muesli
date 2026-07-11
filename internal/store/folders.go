package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/abedegno/muesli/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ErrDuplicate is returned when a uniquely-constrained value already exists
// (e.g. a folder name for the owner).
var ErrDuplicate = errors.New("duplicate")

// ErrInvalidParent is returned when a folder's parent is missing/not-owned, would
// create a cycle, or would exceed the max nesting depth.
var ErrInvalidParent = errors.New("invalid parent")

const maxFolderDepth = 5

func validateFolderName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 80 {
		return "", ValidationError("folder name invalid")
	}
	return name, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// validateParent checks a proposed parent for folder `id` (id may be "" on create).
// Returns ErrInvalidParent on missing/not-owned parent, cycle, or depth overflow.
func (s *Store) validateParent(ctx context.Context, ownerID, id string, parentID *string) error {
	if parentID == nil {
		return nil
	}
	if *parentID == id {
		return ErrInvalidParent
	}
	// parent must exist and be owned
	var ok bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM folders WHERE id=$1 AND owner_id=$2 AND deleted_at IS NULL)`, *parentID, ownerID).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return ErrInvalidParent
	}
	// cycle: parent must not be a descendant of id (only relevant when id already exists)
	if id != "" {
		descend, err := s.folderDescendants(ctx, ownerID, id)
		if err != nil {
			return err
		}
		if descend[*parentID] {
			return ErrInvalidParent
		}
	}
	// depth: parent's depth + 1 must be <= maxFolderDepth
	var depth int
	if err := s.pool.QueryRow(ctx,
		`WITH RECURSIVE anc AS (
		   SELECT id, parent_id, 1 AS d FROM folders WHERE id=$1 AND owner_id=$2
		   UNION ALL
		   SELECT f.id, f.parent_id, anc.d+1 FROM folders f JOIN anc ON f.id = anc.parent_id WHERE f.owner_id=$2
		 ) SELECT COALESCE(max(d),0) FROM anc`, *parentID, ownerID).Scan(&depth); err != nil {
		return err
	}
	// Also account for the height of the moved subtree so that the deepest
	// post-move node stays within maxFolderDepth.
	h := 0
	if id != "" {
		var err error
		h, err = s.subtreeHeight(ctx, ownerID, id)
		if err != nil {
			return err
		}
	}
	if depth+h+1 > maxFolderDepth {
		return ErrInvalidParent
	}
	return nil
}

// subtreeHeight returns the maximum depth of any descendant relative to folderID
// (0 if the folder has no children). Owner-scoped.
func (s *Store) subtreeHeight(ctx context.Context, ownerID, folderID string) (int, error) {
	var h int
	err := s.pool.QueryRow(ctx,
		`WITH RECURSIVE sub AS (
		   SELECT id, 0 AS h FROM folders WHERE id=$1 AND owner_id=$2 AND deleted_at IS NULL
		   UNION ALL
		   SELECT f.id, sub.h+1 FROM folders f JOIN sub ON f.parent_id = sub.id WHERE f.owner_id=$2 AND f.deleted_at IS NULL
		 ) SELECT COALESCE(max(h),0) FROM sub`, folderID, ownerID).Scan(&h)
	if err != nil {
		return 0, err
	}
	return h, nil
}

// folderDescendants returns the set of folder ids in the subtree rooted at id
// (INCLUDING id itself), owner-scoped.
func (s *Store) folderDescendants(ctx context.Context, ownerID, id string) (map[string]bool, error) {
	rows, err := s.pool.Query(ctx,
		`WITH RECURSIVE sub AS (
		   SELECT id FROM folders WHERE id=$1 AND owner_id=$2
		   UNION ALL
		   SELECT f.id FROM folders f JOIN sub ON f.parent_id = sub.id WHERE f.owner_id=$2
		 ) SELECT id FROM sub`, id, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var fid string
		if err := rows.Scan(&fid); err != nil {
			return nil, err
		}
		out[fid] = true
	}
	return out, rows.Err()
}

func (s *Store) CreateFolder(ctx context.Context, ownerID, name string, parentID *string) (model.Folder, error) {
	name, err := validateFolderName(name)
	if err != nil {
		return model.Folder{}, err
	}
	if err := s.validateParent(ctx, ownerID, "", parentID); err != nil {
		return model.Folder{}, err
	}
	var f model.Folder
	err = s.pool.QueryRow(ctx,
		`INSERT INTO folders (id, owner_id, name, parent_id, position)
		 VALUES ($1,$2,$3,$4, COALESCE(
		   (SELECT max(position)+1 FROM folders
		    WHERE owner_id=$2 AND parent_id IS NOT DISTINCT FROM $4 AND deleted_at IS NULL), 0))
		 RETURNING id, name, parent_id, created_at`,
		uuid.NewString(), ownerID, name, parentID).Scan(&f.ID, &f.Name, &f.ParentID, &f.CreatedAt)
	if isUniqueViolation(err) {
		return model.Folder{}, ErrDuplicate
	}
	return f, err
}

func (s *Store) ListFolders(ctx context.Context, ownerID string) ([]model.Folder, error) {
	// Recursive CTE: for each folder, count all live notes in its subtree.
	rows, err := s.pool.Query(ctx, `
		WITH RECURSIVE descendants AS (
		  -- anchor: each live folder is its own descendant
		  SELECT id AS root_id, id AS descendant_id
		  FROM folders
		  WHERE owner_id = $1 AND deleted_at IS NULL
		  UNION ALL
		  -- recurse: add children of already-found descendants
		  SELECT d.root_id, f.id
		  FROM folders f
		  JOIN descendants d ON f.parent_id = d.descendant_id
		  WHERE f.owner_id = $1 AND f.deleted_at IS NULL
		),
		folder_note_counts AS (
		  SELECT d.root_id AS folder_id, COUNT(n.id) AS note_count
		  FROM descendants d
		  LEFT JOIN note_folders nf ON nf.folder_id = d.descendant_id
		  LEFT JOIN notes n ON n.id = nf.note_id
		                    AND n.deleted_at IS NULL
		                    AND n.owner_id = $1
		  GROUP BY d.root_id
		)
		SELECT f.id, f.name, f.parent_id, f.created_at,
		       COALESCE(fnc.note_count, 0) AS note_count
		FROM folders f
		LEFT JOIN folder_note_counts fnc ON fnc.folder_id = f.id
		WHERE f.owner_id = $1 AND f.deleted_at IS NULL
		ORDER BY f.position, lower(f.name)
	`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Folder{}
	for rows.Next() {
		var f model.Folder
		if err := rows.Scan(&f.ID, &f.Name, &f.ParentID, &f.CreatedAt, &f.NoteCount); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *Store) UpdateFolder(ctx context.Context, ownerID, id, name string, parentID *string) (model.Folder, error) {
	name, err := validateFolderName(name)
	if err != nil {
		return model.Folder{}, err
	}
	if err := s.validateParent(ctx, ownerID, id, parentID); err != nil {
		return model.Folder{}, err
	}
	// Load the current parent so we can detect a re-parent. If the folder is
	// absent/trashed the UPDATE below will match 0 rows → ErrNotFound.
	var curParent *string
	parentKnown := true
	if err := s.pool.QueryRow(ctx,
		`SELECT parent_id FROM folders WHERE id=$1 AND owner_id=$2 AND deleted_at IS NULL`, id, ownerID).
		Scan(&curParent); errors.Is(err, pgx.ErrNoRows) {
		parentKnown = false
	} else if err != nil {
		return model.Folder{}, err
	}
	// When the parent changes, the folder lands at the end of its new parent's
	// sibling list; otherwise its position is left untouched.
	reparented := parentKnown && !samePtr(curParent, parentID)
	var f model.Folder
	if reparented {
		err = s.pool.QueryRow(ctx,
			`UPDATE folders SET name=$1, parent_id=$2,
			   position = COALESCE((SELECT max(position)+1 FROM folders
			     WHERE owner_id=$4 AND parent_id IS NOT DISTINCT FROM $2 AND deleted_at IS NULL AND id<>$3), 0)
			 WHERE id=$3 AND owner_id=$4 AND deleted_at IS NULL
			 RETURNING id, name, parent_id, created_at`, name, parentID, id, ownerID).
			Scan(&f.ID, &f.Name, &f.ParentID, &f.CreatedAt)
	} else {
		err = s.pool.QueryRow(ctx,
			`UPDATE folders SET name=$1, parent_id=$2 WHERE id=$3 AND owner_id=$4 AND deleted_at IS NULL
			 RETURNING id, name, parent_id, created_at`, name, parentID, id, ownerID).
			Scan(&f.ID, &f.Name, &f.ParentID, &f.CreatedAt)
	}
	if isUniqueViolation(err) {
		return model.Folder{}, ErrDuplicate
	}
	// A RETURNING update that matches no rows yields pgx.ErrNoRows on Scan.
	if errors.Is(err, pgx.ErrNoRows) {
		return model.Folder{}, ErrNotFound
	}
	if err != nil {
		return model.Folder{}, err
	}
	return f, nil
}

// samePtr reports whether two *string parent pointers refer to the same value
// (both nil counts as equal).
func samePtr(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// ReorderFolder moves folder id among its current siblings (same parent) so it
// sits immediately after afterID (afterID == nil → first). Sibling positions are
// rewritten sequentially. Returns ErrNotFound if id is absent/trashed/not owned,
// and ErrInvalidParent if afterID is non-nil but not a sibling of id (or == id).
func (s *Store) ReorderFolder(ctx context.Context, ownerID, id string, afterID *string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var parentID *string
	if err := tx.QueryRow(ctx,
		`SELECT parent_id FROM folders WHERE id=$1 AND owner_id=$2 AND deleted_at IS NULL`, id, ownerID).
		Scan(&parentID); errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}

	rows, err := tx.Query(ctx,
		`SELECT id FROM folders
		 WHERE owner_id=$1 AND parent_id IS NOT DISTINCT FROM $2 AND deleted_at IS NULL
		 ORDER BY position, lower(name)`, ownerID, parentID)
	if err != nil {
		return err
	}
	siblings := []string{}
	for rows.Next() {
		var sid string
		if err := rows.Scan(&sid); err != nil {
			rows.Close()
			return err
		}
		siblings = append(siblings, sid)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	if afterID != nil {
		if *afterID == id {
			return ErrInvalidParent
		}
		found := false
		for _, sid := range siblings {
			if sid == *afterID {
				found = true
				break
			}
		}
		if !found {
			return ErrInvalidParent
		}
	}

	// Rebuild order: drop id, then re-insert after afterID (or at the front).
	order := make([]string, 0, len(siblings))
	for _, sid := range siblings {
		if sid == id {
			continue
		}
		order = append(order, sid)
		if afterID != nil && sid == *afterID {
			order = append(order, id)
		}
	}
	if afterID == nil {
		order = append([]string{id}, order...)
	}

	for pos, sid := range order {
		if _, err := tx.Exec(ctx,
			`UPDATE folders SET position=$1 WHERE id=$2 AND owner_id=$3`, pos, sid, ownerID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// ReorderNoteInFolder moves noteID among the notes currently in folderID so it
// sits immediately after afterID (afterID == nil → first). Sibling positions are
// rewritten sequentially. Returns ErrNotFound if the note-folder membership is
// absent or not owned/live, and ErrInvalidParent if afterID is non-nil but not a
// sibling in the same folder (or == noteID).
func (s *Store) ReorderNoteInFolder(ctx context.Context, ownerID, folderID, noteID string, afterID *string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var ok bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS(
		   SELECT 1
		   FROM note_folders nf
		   JOIN notes n ON n.id = nf.note_id
		   JOIN folders f ON f.id = nf.folder_id
		   WHERE nf.note_id=$1
		     AND nf.folder_id=$2
		     AND n.owner_id=$3 AND n.deleted_at IS NULL
		     AND f.owner_id=$3 AND f.deleted_at IS NULL
		 )`, noteID, folderID, ownerID).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return ErrNotFound
	}

	rows, err := tx.Query(ctx,
		`SELECT nf.note_id
		 FROM note_folders nf
		 JOIN notes n ON n.id = nf.note_id
		 JOIN folders f ON f.id = nf.folder_id
		 WHERE nf.folder_id=$1
		   AND n.owner_id=$2 AND n.deleted_at IS NULL
		   AND f.owner_id=$2 AND f.deleted_at IS NULL
		 ORDER BY nf.position, n.created_at DESC, nf.note_id`, folderID, ownerID)
	if err != nil {
		return err
	}
	siblings := []string{}
	for rows.Next() {
		var sid string
		if err := rows.Scan(&sid); err != nil {
			rows.Close()
			return err
		}
		siblings = append(siblings, sid)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	if afterID != nil {
		if *afterID == noteID {
			return ErrInvalidParent
		}
		found := false
		for _, sid := range siblings {
			if sid == *afterID {
				found = true
				break
			}
		}
		if !found {
			return ErrInvalidParent
		}
	}

	order := make([]string, 0, len(siblings))
	for _, sid := range siblings {
		if sid == noteID {
			continue
		}
		order = append(order, sid)
		if afterID != nil && sid == *afterID {
			order = append(order, noteID)
		}
	}
	if afterID == nil {
		order = append([]string{noteID}, order...)
	}

	for pos, sid := range order {
		if _, err := tx.Exec(ctx,
			`UPDATE note_folders SET position=$1 WHERE note_id=$2 AND folder_id=$3`, pos, sid, folderID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// DeleteFolder soft-deletes the folder and its entire subtree (descendants),
// stamping deleted_at=now(). Notes and note_folders memberships are preserved
// (FK cascades fire only on hard delete). Returns ErrNotFound when the root is
// absent, not owned, or already trashed.
func (s *Store) DeleteFolder(ctx context.Context, ownerID, id string) error {
	ct, err := s.pool.Exec(ctx,
		`WITH RECURSIVE sub AS (
		   SELECT id FROM folders WHERE id=$1 AND owner_id=$2 AND deleted_at IS NULL
		   UNION ALL
		   SELECT f.id FROM folders f JOIN sub ON f.parent_id = sub.id WHERE f.owner_id=$2
		 )
		 UPDATE folders SET deleted_at=now() WHERE id IN (SELECT id FROM sub) AND deleted_at IS NULL`,
		id, ownerID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListTrashedFolders returns only the trash roots (folders whose parent is null
// or not itself trashed) — i.e. the folders the user actually deleted, not their
// auto-trashed descendants. Most-recently trashed first.
func (s *Store) ListTrashedFolders(ctx context.Context, ownerID string) ([]model.Folder, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT f.id, f.name, f.parent_id, f.created_at, f.deleted_at
		 FROM folders f
		 WHERE f.owner_id=$1 AND f.deleted_at IS NOT NULL
		   AND (f.parent_id IS NULL
		        OR NOT EXISTS (SELECT 1 FROM folders p WHERE p.id=f.parent_id AND p.deleted_at IS NOT NULL))
		 ORDER BY f.deleted_at DESC`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Folder{}
	for rows.Next() {
		var f model.Folder
		if err := rows.Scan(&f.ID, &f.Name, &f.ParentID, &f.CreatedAt, &f.DeletedAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// RestoreFolder un-trashes the subtree rooted at id (in a transaction). If the
// restored root's parent is not live, the root is moved to top level so it
// reappears in the tree. Returns ErrNotFound when nothing was restored.
func (s *Store) RestoreFolder(ctx context.Context, ownerID, id string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	ct, err := tx.Exec(ctx,
		`WITH RECURSIVE sub AS (
		   SELECT id FROM folders WHERE id=$1 AND owner_id=$2 AND deleted_at IS NOT NULL
		   UNION ALL
		   SELECT f.id FROM folders f JOIN sub ON f.parent_id = sub.id WHERE f.owner_id=$2 AND f.deleted_at IS NOT NULL
		 )
		 UPDATE folders SET deleted_at=NULL WHERE id IN (SELECT id FROM sub)`,
		id, ownerID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	// Defensive: if the restored root's parent isn't live, orphan it to top level.
	if _, err := tx.Exec(ctx,
		`UPDATE folders SET parent_id=NULL
		 WHERE id=$1 AND owner_id=$2 AND parent_id IS NOT NULL
		   AND NOT EXISTS (SELECT 1 FROM folders p WHERE p.id=folders.parent_id AND p.deleted_at IS NULL)`,
		id, ownerID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// PurgeFolder permanently deletes a trashed folder; FK cascades remove its
// descendants and note_folders memberships. Returns ErrNotFound when the folder
// is absent, not owned, or not trashed.
func (s *Store) PurgeFolder(ctx context.Context, ownerID, id string) error {
	ct, err := s.pool.Exec(ctx,
		`DELETE FROM folders WHERE id=$1 AND owner_id=$2 AND deleted_at IS NOT NULL`, id, ownerID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// PurgeExpiredFolders permanently deletes all folders trashed longer ago than
// olderThan (cascades handle descendants). Not owner-scoped — it's the purge job.
// Returns the number of rows deleted.
func (s *Store) PurgeExpiredFolders(ctx context.Context, olderThan time.Duration) (int, error) {
	ct, err := s.pool.Exec(ctx,
		`DELETE FROM folders WHERE deleted_at IS NOT NULL AND deleted_at < now() - $1::interval`,
		fmt.Sprintf("%d seconds", int64(olderThan.Seconds())))
	if err != nil {
		return 0, err
	}
	return int(ct.RowsAffected()), nil
}

// AddNoteFolder adds a note to a folder (idempotent). Both must belong to the owner.
func (s *Store) AddNoteFolder(ctx context.Context, ownerID, noteID, folderID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var ok bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM notes WHERE id=$1 AND owner_id=$2 AND deleted_at IS NULL)
		    AND EXISTS(SELECT 1 FROM folders WHERE id=$3 AND owner_id=$2 AND deleted_at IS NULL)`,
		noteID, ownerID, folderID).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return ErrNotFound
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO note_folders (note_id, folder_id, position)
		 VALUES ($1,$2, COALESCE((SELECT max(position)+1 FROM note_folders WHERE folder_id=$2), 0))
		 ON CONFLICT DO NOTHING`,
		noteID, folderID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// RemoveNoteFolder removes a note from a folder (no-op if absent). Owner-scoped.
func (s *Store) RemoveNoteFolder(ctx context.Context, ownerID, noteID, folderID string) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM note_folders WHERE note_id=$1 AND folder_id=$2
		   AND note_id IN (SELECT id FROM notes WHERE owner_id=$3)`,
		noteID, folderID, ownerID)
	return err
}

// NoteFolderIDs returns one note's folder ids.
func (s *Store) NoteFolderIDs(ctx context.Context, noteID string) ([]string, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT folder_id FROM note_folders WHERE note_id=$1`, noteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// foldersForNotes returns folder ids per note id in one query (avoids N+1).
func (s *Store) foldersForNotes(ctx context.Context, noteIDs []string) (map[string][]string, error) {
	out := map[string][]string{}
	if len(noteIDs) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx,
		`SELECT note_id, folder_id FROM note_folders WHERE note_id = ANY($1)`, noteIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var nid, fid string
		if err := rows.Scan(&nid, &fid); err != nil {
			return nil, err
		}
		out[nid] = append(out[nid], fid)
	}
	return out, rows.Err()
}
