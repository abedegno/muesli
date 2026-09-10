-- folder_visibility (down)
-- Schema-reversible, not data-preserving: dropping the column discards every
-- sharing choice. Redeploying the new version afterwards makes every folder
-- private again until owners re-share (see design doc for issue #12).
DROP INDEX IF EXISTS folders_shared_live_idx;
ALTER TABLE folders DROP COLUMN IF EXISTS visibility;
