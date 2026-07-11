DROP INDEX IF EXISTS smart_lists_deleted_idx;
ALTER TABLE smart_lists DROP COLUMN deleted_at;
