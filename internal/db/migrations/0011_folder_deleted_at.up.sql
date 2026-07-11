ALTER TABLE folders ADD COLUMN deleted_at TIMESTAMPTZ;
CREATE INDEX folders_deleted_idx ON folders (deleted_at) WHERE deleted_at IS NOT NULL;
