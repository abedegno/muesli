DROP INDEX IF EXISTS idx_transcript_segments_text_tsv;
ALTER TABLE transcript_segments DROP COLUMN IF EXISTS text_tsv;
