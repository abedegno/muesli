ALTER TABLE transcript_segments
ADD COLUMN text_tsv tsvector
GENERATED ALWAYS AS (to_tsvector('english', text)) STORED;

CREATE INDEX idx_transcript_segments_text_tsv
ON transcript_segments USING GIN (text_tsv);
