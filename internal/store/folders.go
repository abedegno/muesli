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

// queryRower is satisfied by both *pgxpool.Pool and pgx.Tx, so authorization
// helpers can run either standalone or inside an already-open transaction.
type queryRower interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// txQuerier extends queryRower with Query, so multi-row read helpers (e.g.
// note tags, readable folder ids) can also run either standalone or inside an
// already-open transaction. Satisfied by both *pgxpool.Pool and pgx.Tx.
type txQuerier interface {
	queryRower
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// CheckFolderOwnerMutation is the exported form of
// authorizeLiveFolderOwnerMutation, for API handlers that must check
// ownership BEFORE decoding/validating a mutation's request body (issue
// #12): calling this first ensures a non-owner gets the same 404/403 denial
// regardless of whether their body is well-formed, so an invalid body from a
// non-owner never produces a different response than a valid one. Owner-only
// mutation handlers that also call a store method performing this same check
// internally (e.g. UpdateFolder, ReorderFolder) may safely call both — the
// second check is redundant but harmless once the first has already gated
// the body decode.
func (s *Store) CheckFolderOwnerMutation(ctx context.Context, requesterID, folderID string) error {
	return authorizeLiveFolderOwnerMutation(ctx, s.pool, requesterID, folderID)
}

// authorizeLiveFolderOwnerMutation enforces the mutation-authorization order
// named by the design (issue #12) for every owner-only operation on a LIVE
// folder: an absent, trashed, or private non-owned folder is ErrNotFound (so a
// guessed id is never an existence oracle); a live shared non-owned folder is
// ErrForbidden (the folder is visible but the requester lacks authority); an
// owned folder proceeds (nil). Trashed-folder operations (restore/purge) do
// not use this helper — a trashed folder is never shared-visible.
func authorizeLiveFolderOwnerMutation(ctx context.Context, q queryRower, requesterID, folderID string) error {
	var ownerID, visibility string
	err := q.QueryRow(ctx,
		`SELECT owner_id, visibility FROM folders WHERE id=$1 AND deleted_at IS NULL`, folderID).
		Scan(&ownerID, &visibility)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if ownerID == requesterID {
		return nil
	}
	if visibility == model.FolderShared {
		return ErrForbidden
	}
	return ErrNotFound
}

// validateParent checks a proposed parent for folder `id` (id may be "" on create).
// Returns ErrInvalidParent on missing/not-owned parent, cycle, or depth overflow.
// The parent must be owned by ownerID even when it is shared — nesting stays
// same-owner regardless of visibility (issue #12).
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
		 RETURNING id, owner_id, name, parent_id, visibility, created_at`,
		uuid.NewString(), ownerID, name, parentID).
		Scan(&f.ID, &f.OwnerID, &f.Name, &f.ParentID, &f.Visibility, &f.CreatedAt)
	if isUniqueViolation(err) {
		return model.Folder{}, ErrDuplicate
	}
	if err != nil {
		return model.Folder{}, err
	}
	f.IsOwner = true
	return f, nil
}

// ListFolders returns every live folder the requester can see: folders they
// own (any visibility) plus every other live folder with visibility='shared'.
// NoteCount is recursive over each folder's descendant subtree (matching the
// pre-sharing behavior), but scoped to notes the requester can actually
// read: within the requester's own subtree every descendant is traversed
// (folder ownership already authorizes seeing everything filed there,
// including a teammate's own note contributed to a shared folder); within a
// subtree owned by someone else, only the live-shared descendants themselves
// contribute notes to the count, so a private folder nested under a shared
// one never leaks its note count to a non-owner (issue #12). Nesting stays
// same-owner throughout a subtree, so descendant traversal never crosses an
// owner boundary. ParentID is nulled in the response (never in storage) when
// the requester cannot see the parent, so a promoted shared descendant never
// references a missing node client-side.
func (s *Store) ListFolders(ctx context.Context, requesterID string) ([]model.Folder, error) {
	rows, err := s.pool.Query(ctx, `
		WITH RECURSIVE descendants AS (
		  -- anchor: every folder visible to the requester is a root of its own subtree
		  SELECT id AS root_id, id AS descendant_id, owner_id, visibility
		  FROM folders
		  WHERE deleted_at IS NULL AND (owner_id = $1 OR visibility = 'shared')
		  UNION ALL
		  -- recurse: add children, staying within the same owner (same-owner nesting)
		  SELECT d.root_id, f.id, f.owner_id, f.visibility
		  FROM folders f
		  JOIN descendants d ON f.parent_id = d.descendant_id
		  WHERE f.deleted_at IS NULL AND f.owner_id = d.owner_id
		),
		readable_descendants AS (
		  -- keep only descendants the requester can actually read: their own
		  -- (whole subtree, any visibility) or anyone's live-shared folder
		  SELECT root_id, descendant_id
		  FROM descendants
		  WHERE owner_id = $1 OR visibility = 'shared'
		),
		folder_note_counts AS (
		  SELECT rd.root_id AS folder_id, COUNT(DISTINCT n.id) AS note_count
		  FROM readable_descendants rd
		  JOIN note_folders nf ON nf.folder_id = rd.descendant_id
		  JOIN notes n ON n.id = nf.note_id AND n.deleted_at IS NULL
		  GROUP BY rd.root_id
		)
		SELECT f.id, f.owner_id, f.name, f.parent_id, f.visibility, f.created_at,
		       COALESCE(fnc.note_count, 0) AS note_count
		FROM folders f
		LEFT JOIN folder_note_counts fnc ON fnc.folder_id = f.id
		WHERE f.deleted_at IS NULL AND (f.owner_id = $1 OR f.visibility = 'shared')
		ORDER BY f.position, lower(f.name)
	`, requesterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Folder{}
	for rows.Next() {
		var f model.Folder
		if err := rows.Scan(&f.ID, &f.OwnerID, &f.Name, &f.ParentID, &f.Visibility, &f.CreatedAt, &f.NoteCount); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	visible := make(map[string]bool, len(out))
	for _, f := range out {
		visible[f.ID] = true
	}
	for i := range out {
		out[i].IsOwner = out[i].OwnerID == requesterID
		if out[i].ParentID != nil && !visible[*out[i].ParentID] {
			out[i].ParentID = nil
		}
	}
	return out, nil
}

// GetFolder returns one live folder owned by ownerID.
func (s *Store) GetFolder(ctx context.Context, ownerID, id string) (model.Folder, error) {
	var f model.Folder
	err := s.pool.QueryRow(ctx,
		`SELECT id, owner_id, name, parent_id, visibility, created_at
		 FROM folders
		 WHERE id=$1 AND owner_id=$2 AND deleted_at IS NULL`,
		id, ownerID).Scan(&f.ID, &f.OwnerID, &f.Name, &f.ParentID, &f.Visibility, &f.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.Folder{}, ErrNotFound
	}
	if err != nil {
		return model.Folder{}, err
	}
	f.IsOwner = true
	return f, nil
}

func (s *Store) UpdateFolder(ctx context.Context, requesterID, id, name string, parentID *string) (model.Folder, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.Folder{}, err
	}
	defer tx.Rollback(ctx)

	// Authorization must precede validation of the requester-supplied name/parent:
	// a non-owner must get ErrForbidden (or ErrNotFound) even when their input is
	// otherwise invalid, not a ValidationError/ErrInvalidParent that would leak
	// input-shape feedback to someone unauthorized to make the change.
	if err := authorizeLiveFolderOwnerMutation(ctx, tx, requesterID, id); err != nil {
		return model.Folder{}, err
	}

	name, err = validateFolderName(name)
	if err != nil {
		return model.Folder{}, err
	}
	if err := s.validateParent(ctx, requesterID, id, parentID); err != nil {
		return model.Folder{}, err
	}

	// Load the current parent so we can detect a re-parent.
	var curParent *string
	if err := tx.QueryRow(ctx,
		`SELECT parent_id FROM folders WHERE id=$1 AND deleted_at IS NULL`, id).
		Scan(&curParent); err != nil {
		return model.Folder{}, err
	}
	// When the parent changes, the folder lands at the end of its new parent's
	// sibling list; otherwise its position is left untouched.
	reparented := !samePtr(curParent, parentID)
	var f model.Folder
	if reparented {
		err = tx.QueryRow(ctx,
			`UPDATE folders SET name=$1, parent_id=$2,
			   position = COALESCE((SELECT max(position)+1 FROM folders
			     WHERE owner_id=$4 AND parent_id IS NOT DISTINCT FROM $2 AND deleted_at IS NULL AND id<>$3), 0)
			 WHERE id=$3 AND owner_id=$4 AND deleted_at IS NULL
			 RETURNING id, owner_id, name, parent_id, visibility, created_at`, name, parentID, id, requesterID).
			Scan(&f.ID, &f.OwnerID, &f.Name, &f.ParentID, &f.Visibility, &f.CreatedAt)
	} else {
		err = tx.QueryRow(ctx,
			`UPDATE folders SET name=$1, parent_id=$2 WHERE id=$3 AND owner_id=$4 AND deleted_at IS NULL
			 RETURNING id, owner_id, name, parent_id, visibility, created_at`, name, parentID, id, requesterID).
			Scan(&f.ID, &f.OwnerID, &f.Name, &f.ParentID, &f.Visibility, &f.CreatedAt)
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
	f.IsOwner = true
	return f, tx.Commit(ctx)
}

// samePtr reports whether two *string parent pointers refer to the same value
// (both nil counts as equal).
func samePtr(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// SetFolderVisibility sets a folder's visibility to "private" or "shared".
// Owner-only: an absent/trashed/private-foreign folder is ErrNotFound; a live
// shared folder owned by someone else is ErrForbidden (visible, not theirs to
// change); an unknown visibility value is a ValidationError.
func (s *Store) SetFolderVisibility(ctx context.Context, requesterID, folderID, visibility string) (model.Folder, error) {
	if visibility != model.FolderPrivate && visibility != model.FolderShared {
		return model.Folder{}, ValidationError("invalid visibility")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.Folder{}, err
	}
	defer tx.Rollback(ctx)

	if err := authorizeLiveFolderOwnerMutation(ctx, tx, requesterID, folderID); err != nil {
		return model.Folder{}, err
	}

	var f model.Folder
	err = tx.QueryRow(ctx,
		`UPDATE folders SET visibility=$1 WHERE id=$2 AND owner_id=$3 AND deleted_at IS NULL
		 RETURNING id, owner_id, name, parent_id, visibility, created_at`,
		visibility, folderID, requesterID).
		Scan(&f.ID, &f.OwnerID, &f.Name, &f.ParentID, &f.Visibility, &f.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.Folder{}, ErrNotFound
	}
	if err != nil {
		return model.Folder{}, err
	}
	f.IsOwner = true
	return f, tx.Commit(ctx)
}

// ReorderFolder moves folder id among its current siblings (same parent) so it
// sits immediately after afterID (afterID == nil → first). Sibling positions are
// rewritten sequentially. Returns ErrNotFound if id is absent/trashed/private and
// not owned, ErrForbidden if id is live and shared but not owned, and
// ErrInvalidParent if afterID is non-nil but not a sibling of id (or == id).
func (s *Store) ReorderFolder(ctx context.Context, requesterID, id string, afterID *string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if err := authorizeLiveFolderOwnerMutation(ctx, tx, requesterID, id); err != nil {
		return err
	}

	var parentID *string
	if err := tx.QueryRow(ctx,
		`SELECT parent_id FROM folders WHERE id=$1 AND deleted_at IS NULL`, id).
		Scan(&parentID); err != nil {
		return err
	}

	rows, err := tx.Query(ctx,
		`SELECT id FROM folders
		 WHERE owner_id=$1 AND parent_id IS NOT DISTINCT FROM $2 AND deleted_at IS NULL
		 ORDER BY position, lower(name)`, requesterID, parentID)
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
			`UPDATE folders SET position=$1 WHERE id=$2 AND owner_id=$3`, pos, sid, requesterID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// ReorderNoteInFolder moves noteID among the notes currently filed in folderID so
// it sits immediately after afterID (afterID == nil → first). Sibling positions
// are rewritten sequentially. Authority is folder-owner-only (issue #12): the
// requester need not own every note in the folder, only the folder itself, so a
// folder owner may reorder a teammate's contributed note alongside their own.
// Returns ErrNotFound if id is absent/trashed/private and not owned, ErrForbidden
// if id is live and shared but not owned, and ErrInvalidParent if afterID is
// non-nil but not a sibling in the same folder (or == noteID).
func (s *Store) ReorderNoteInFolder(ctx context.Context, requesterID, folderID, noteID string, afterID *string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if err := authorizeLiveFolderOwnerMutation(ctx, tx, requesterID, folderID); err != nil {
		return err
	}

	var ok bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS(
		   SELECT 1
		   FROM note_folders nf
		   JOIN notes n ON n.id = nf.note_id
		   WHERE nf.note_id=$1
		     AND nf.folder_id=$2
		     AND n.deleted_at IS NULL
		 )`, noteID, folderID).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return ErrNotFound
	}

	rows, err := tx.Query(ctx,
		`SELECT nf.note_id
		 FROM note_folders nf
		 JOIN notes n ON n.id = nf.note_id
		 WHERE nf.folder_id=$1
		   AND n.deleted_at IS NULL
		 ORDER BY nf.position, n.created_at DESC, nf.note_id`, folderID)
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
// absent, private and not owned, or already trashed, and ErrForbidden when the
// root is live, shared, and not owned.
func (s *Store) DeleteFolder(ctx context.Context, requesterID, id string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if err := authorizeLiveFolderOwnerMutation(ctx, tx, requesterID, id); err != nil {
		return err
	}

	ct, err := tx.Exec(ctx,
		`WITH RECURSIVE sub AS (
		   SELECT id FROM folders WHERE id=$1 AND owner_id=$2 AND deleted_at IS NULL
		   UNION ALL
		   SELECT f.id FROM folders f JOIN sub ON f.parent_id = sub.id WHERE f.owner_id=$2
		 )
		 UPDATE folders SET deleted_at=now() WHERE id IN (SELECT id FROM sub) AND deleted_at IS NULL`,
		id, requesterID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return tx.Commit(ctx)
}

// ListTrashedFolders returns only the trash roots (folders whose parent is null
// or not itself trashed) — i.e. the folders the user actually deleted, not their
// auto-trashed descendants. Most-recently trashed first. Owner-only: trashed
// folders are never shared-visible regardless of their pre-trash visibility.
func (s *Store) ListTrashedFolders(ctx context.Context, ownerID string) ([]model.Folder, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT f.id, f.owner_id, f.name, f.parent_id, f.visibility, f.created_at, f.deleted_at
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
		if err := rows.Scan(&f.ID, &f.OwnerID, &f.Name, &f.ParentID, &f.Visibility, &f.CreatedAt, &f.DeletedAt); err != nil {
			return nil, err
		}
		f.IsOwner = true
		out = append(out, f)
	}
	return out, rows.Err()
}

// RestoreFolder un-trashes the subtree rooted at id (in a transaction). If the
// restored root's parent is not live, the root is moved to top level so it
// reappears in the tree. Returns ErrNotFound when nothing was restored.
// Owner-only, unaffected by sharing: a trashed folder is never shared-visible.
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
// is absent, not owned, or not trashed. Owner-only, unaffected by sharing.
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

// ResolveFolders returns the owner's live and soft-deleted folders matching any
// of ids. Non-recursive, exposes no memberships, and does not filter deleted_at
// so callers can distinguish live from trashed rows. Unknown, purged, and
// other-owner ids are silently omitted. Callers are responsible for bounding
// and de-duplicating ids. Owner-only by design — rule-tree folder references
// resolve only within the requester's own folders.
func (s *Store) ResolveFolders(ctx context.Context, ownerID string, ids []string) ([]model.Folder, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, owner_id, name, parent_id, visibility, created_at, deleted_at
		 FROM folders
		 WHERE owner_id=$1 AND id = ANY($2)`, ownerID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Folder{}
	for rows.Next() {
		var f model.Folder
		if err := rows.Scan(&f.ID, &f.OwnerID, &f.Name, &f.ParentID, &f.Visibility, &f.CreatedAt, &f.DeletedAt); err != nil {
			return nil, err
		}
		f.IsOwner = true
		out = append(out, f)
	}
	return out, rows.Err()
}

// noteFolderAuthContext loads what AddNoteFolder/RemoveNoteFolder need to
// authorize a filing mutation: the note's live/owner/CanReadNote-readable
// state and the folder's live/owner/visibility state.
type noteFolderAuthContext struct {
	noteExists       bool
	noteOwnerID      string
	noteReadable     bool
	folderExists     bool
	folderOwnerID    string
	folderVisibility string
}

func (c noteFolderAuthContext) noteVisible(requesterID string) bool {
	return c.noteExists && (c.noteOwnerID == requesterID || c.noteReadable)
}

func (c noteFolderAuthContext) folderVisible(requesterID string) bool {
	return c.folderExists && (c.folderOwnerID == requesterID || c.folderVisibility == model.FolderShared)
}

func loadNoteFolderAuthContext(ctx context.Context, tx pgx.Tx, requesterID, noteID, folderID string) (noteFolderAuthContext, error) {
	var c noteFolderAuthContext
	err := tx.QueryRow(ctx,
		`SELECT n.owner_id,
		        n.owner_id = $2 OR EXISTS (
		          SELECT 1 FROM note_folders nf JOIN folders f ON f.id = nf.folder_id
		          WHERE nf.note_id = n.id AND f.visibility = 'shared' AND f.deleted_at IS NULL)
		 FROM notes n WHERE n.id=$1 AND n.deleted_at IS NULL`,
		noteID, requesterID).Scan(&c.noteOwnerID, &c.noteReadable)
	if errors.Is(err, pgx.ErrNoRows) {
		c.noteExists = false
	} else if err != nil {
		return c, err
	} else {
		c.noteExists = true
	}

	err = tx.QueryRow(ctx,
		`SELECT owner_id, visibility FROM folders WHERE id=$1 AND deleted_at IS NULL`, folderID).
		Scan(&c.folderOwnerID, &c.folderVisibility)
	if errors.Is(err, pgx.ErrNoRows) {
		c.folderExists = false
	} else if err != nil {
		return c, err
	} else {
		c.folderExists = true
	}

	return c, nil
}

// AddNoteFolder files a note into a folder (idempotent). The requester must
// own the live note; the target folder must be owned by the requester or be
// live and shared — any authenticated user may file their own note into any
// shared folder (issue #12). Returns ErrNotFound when either resource is
// invisible to the requester, ErrForbidden when both are visible but the
// requester does not own the note.
func (s *Store) AddNoteFolder(ctx context.Context, requesterID, noteID, folderID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	c, err := loadNoteFolderAuthContext(ctx, tx, requesterID, noteID, folderID)
	if err != nil {
		return err
	}
	if !c.noteVisible(requesterID) || !c.folderVisible(requesterID) {
		return ErrNotFound
	}
	if c.noteOwnerID != requesterID {
		return ErrForbidden
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

// RemoveNoteFolder removes a note from a folder (idempotent no-op if the
// membership is already absent). Allowed when the requester owns the live
// note, or owns the live folder — a folder owner may remove any note from
// their own folder, including a teammate's contribution (issue #12). Returns
// ErrNotFound when either resource is invisible to the requester, ErrForbidden
// when both are visible but the requester has neither authority.
func (s *Store) RemoveNoteFolder(ctx context.Context, requesterID, noteID, folderID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	c, err := loadNoteFolderAuthContext(ctx, tx, requesterID, noteID, folderID)
	if err != nil {
		return err
	}
	if !c.noteVisible(requesterID) || !c.folderVisible(requesterID) {
		return ErrNotFound
	}
	if c.noteOwnerID != requesterID && c.folderOwnerID != requesterID {
		return ErrForbidden
	}

	if _, err := tx.Exec(ctx,
		`DELETE FROM note_folders WHERE note_id=$1 AND folder_id=$2`, noteID, folderID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// NoteFolderIDs returns one note's complete folder ids (unfiltered — callers
// needing the shared-read-safe filtered view use readableNoteFolderIDs).
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

// readableNoteFolderIDs returns noteID's folder memberships filtered to
// folders the requester can see (owned or live shared) — so a shared reader
// never learns the note owner's private filing structure (issue #12).
func (s *Store) readableNoteFolderIDs(ctx context.Context, requesterID, noteID string) ([]string, error) {
	return readableNoteFolderIDsTx(ctx, s.pool, requesterID, noteID)
}

// readableNoteFolderIDsTx is readableNoteFolderIDs against an explicit
// executor (pool or tx) so guarded multi-part reads (e.g. GetReadableNote)
// can load folder memberships inside the same transaction/snapshot as their
// authorization check (issue #12).
func readableNoteFolderIDsTx(ctx context.Context, q txQuerier, requesterID, noteID string) ([]string, error) {
	rows, err := q.Query(ctx,
		`SELECT nf.folder_id
		 FROM note_folders nf
		 JOIN folders f ON f.id = nf.folder_id
		 WHERE nf.note_id=$1 AND f.deleted_at IS NULL AND (f.owner_id = $2 OR f.visibility = 'shared')`,
		noteID, requesterID)
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

// readableFoldersForNotes is foldersForNotes filtered per-note to folders the
// requester can see (owned or live shared) — used to populate FolderIDs on
// the shared-readable note-list route without per-note leakage of private
// filing structure (issue #12).
func (s *Store) readableFoldersForNotes(ctx context.Context, requesterID string, noteIDs []string) (map[string][]string, error) {
	return readableFoldersForNotesTx(ctx, s.pool, requesterID, noteIDs)
}

// readableFoldersForNotesTx is readableFoldersForNotes against an explicit
// executor (pool or tx) so guarded multi-part reads (e.g. ListReadableNotes)
// can load the batch readable-folder-id map inside the same
// transaction/snapshot as their authorization check (issue #12).
func readableFoldersForNotesTx(ctx context.Context, q txQuerier, requesterID string, noteIDs []string) (map[string][]string, error) {
	out := map[string][]string{}
	if len(noteIDs) == 0 {
		return out, nil
	}
	rows, err := q.Query(ctx,
		`SELECT nf.note_id, nf.folder_id
		 FROM note_folders nf
		 JOIN folders f ON f.id = nf.folder_id
		 WHERE nf.note_id = ANY($1) AND f.deleted_at IS NULL AND (f.owner_id = $2 OR f.visibility = 'shared')`,
		noteIDs, requesterID)
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
