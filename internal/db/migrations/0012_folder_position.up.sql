ALTER TABLE folders ADD COLUMN position INTEGER NOT NULL DEFAULT 0;
-- seed deterministic positions from the current name order, per (owner, parent):
WITH ranked AS (
  SELECT id, row_number() OVER (PARTITION BY owner_id, parent_id ORDER BY lower(name)) - 1 AS rn
  FROM folders
)
UPDATE folders f SET position = r.rn FROM ranked r WHERE r.id = f.id;
