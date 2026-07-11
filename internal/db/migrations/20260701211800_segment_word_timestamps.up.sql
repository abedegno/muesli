-- segment_word_timestamps (up)
ALTER TABLE transcript_segments ADD COLUMN IF NOT EXISTS words JSONB;
