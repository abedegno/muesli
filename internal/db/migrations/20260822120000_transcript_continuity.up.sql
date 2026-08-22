ALTER TABLE transcripts
    ADD COLUMN stream_id  TEXT,
    ADD COLUMN sealed     BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN generation INTEGER NOT NULL DEFAULT 1;

ALTER TABLE transcript_segments
    ADD COLUMN boundary TEXT;

-- Identifies which transcribe job wrote the current 'transcribing' status, so a
-- job can recognise and undo its own write without clobbering another job's.
ALTER TABLE notes
    ADD COLUMN transcribing_job_id UUID;

CREATE TABLE transcript_gaps (
    id              UUID PRIMARY KEY,
    transcript_id   UUID NOT NULL REFERENCES transcripts(id) ON DELETE CASCADE,
    stream_id       TEXT NOT NULL,
    start_sample    BIGINT NOT NULL,
    dropped_samples BIGINT,
    origin          TEXT NOT NULL
);
CREATE INDEX idx_gaps_transcript ON transcript_gaps(transcript_id);
