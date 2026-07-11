DROP INDEX IF EXISTS idx_notes_normalized_audio_hash;
DROP INDEX IF EXISTS idx_notes_audio_hash;
ALTER TABLE notes DROP COLUMN IF EXISTS normalized_audio_hash;
ALTER TABLE notes DROP COLUMN IF EXISTS audio_hash;
