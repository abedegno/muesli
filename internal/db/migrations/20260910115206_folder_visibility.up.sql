-- folder_visibility (up)
-- Adds a binary visibility flag to folders: 'private' (default, matches
-- existing behavior for every current row) or 'shared' (whole-deployment
-- read-only, see issue #12). The partial index supports the shared-read
-- authorization predicate without scanning private rows.
ALTER TABLE folders
  ADD COLUMN visibility TEXT NOT NULL DEFAULT 'private'
  CHECK (visibility IN ('private', 'shared'));

CREATE INDEX folders_shared_live_idx ON folders (id)
WHERE visibility = 'shared' AND deleted_at IS NULL;
