-- note_embeddings_dim (up)
-- Add dim column to note_embeddings to support dimension-aware embedding models.
ALTER TABLE note_embeddings ADD COLUMN dim INT NOT NULL DEFAULT 768;
