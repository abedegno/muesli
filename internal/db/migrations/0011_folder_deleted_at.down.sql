DROP INDEX IF EXISTS folders_deleted_idx;
ALTER TABLE folders DROP COLUMN deleted_at;
