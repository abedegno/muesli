DROP INDEX IF EXISTS notes_deleted_idx;
ALTER TABLE notes DROP COLUMN deleted_at;
