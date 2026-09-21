-- notes_owner_pagination (up)
-- Bounded keyset pagination for the mobile notes list (issue #767): supports
-- ORDER BY pinned DESC, created_at DESC, id DESC scoped to one owner's live
-- (non-deleted) notes without a sequential scan, even at large table sizes.
CREATE INDEX idx_notes_owner_pagination
ON notes (owner_id, pinned DESC, created_at DESC, id DESC)
WHERE deleted_at IS NULL;
