CREATE EXTENSION IF NOT EXISTS vector;
CREATE TABLE note_embeddings (
    note_id    UUID PRIMARY KEY REFERENCES notes(id) ON DELETE CASCADE,
    embedding  vector(768) NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
