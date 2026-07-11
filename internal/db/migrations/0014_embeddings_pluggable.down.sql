-- Narrowing back to vector(768) requires every row to be 768-dim. Drop any rows
-- embedded under a non-768 model first so `down` is always safe (those embeddings
-- would need regenerating after a re-`up` anyway).
DELETE FROM note_embeddings WHERE vector_dims(embedding) <> 768;
ALTER TABLE note_embeddings ALTER COLUMN embedding TYPE vector(768);
ALTER TABLE note_embeddings DROP COLUMN model;
