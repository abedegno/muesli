DROP TABLE IF EXISTS transcript_gaps;
ALTER TABLE notes DROP COLUMN IF EXISTS transcribing_job_id;
ALTER TABLE transcript_segments DROP COLUMN IF EXISTS boundary;
ALTER TABLE transcripts
    DROP COLUMN IF EXISTS generation,
    DROP COLUMN IF EXISTS sealed,
    DROP COLUMN IF EXISTS stream_id;
