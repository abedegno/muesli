ALTER TABLE note_embeddings ADD COLUMN model TEXT NOT NULL DEFAULT 'nomic-embed-text';
ALTER TABLE note_embeddings ALTER COLUMN embedding TYPE vector;
