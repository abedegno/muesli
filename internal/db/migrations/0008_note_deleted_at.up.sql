ALTER TABLE notes ADD COLUMN deleted_at TIMESTAMPTZ;
CREATE INDEX notes_deleted_idx ON notes (deleted_at) WHERE deleted_at IS NOT NULL;
