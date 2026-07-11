ALTER TABLE notes ADD COLUMN IF NOT EXISTS audio_hash TEXT;
ALTER TABLE notes ADD COLUMN IF NOT EXISTS normalized_audio_hash TEXT;
CREATE INDEX IF NOT EXISTS idx_notes_audio_hash ON notes (audio_hash) WHERE audio_hash IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_notes_normalized_audio_hash ON notes (normalized_audio_hash) WHERE normalized_audio_hash IS NOT NULL;
