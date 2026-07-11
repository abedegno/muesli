ALTER TABLE folders ADD COLUMN parent_id UUID REFERENCES folders(id) ON DELETE CASCADE;
CREATE INDEX folders_parent_idx ON folders (parent_id);
