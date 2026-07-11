-- note_folder_position (up)
ALTER TABLE note_folders ADD COLUMN position INTEGER NOT NULL DEFAULT 0;

-- Seed deterministic sibling positions from the default note ordering
-- (newest notes first) within each folder.
WITH ranked AS (
  SELECT nf.note_id,
         nf.folder_id,
         row_number() OVER (
           PARTITION BY nf.folder_id
           ORDER BY n.created_at DESC, nf.note_id
         ) - 1 AS rn
  FROM note_folders nf
  JOIN notes n ON n.id = nf.note_id
)
UPDATE note_folders nf
SET position = r.rn
FROM ranked r
WHERE r.note_id = nf.note_id
  AND r.folder_id = nf.folder_id;
