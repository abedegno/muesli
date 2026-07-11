ALTER TABLE smart_lists ADD COLUMN deleted_at TIMESTAMPTZ;
CREATE INDEX smart_lists_deleted_idx ON smart_lists (deleted_at) WHERE deleted_at IS NOT NULL;
