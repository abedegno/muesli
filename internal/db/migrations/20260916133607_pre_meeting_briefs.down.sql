-- pre_meeting_briefs (down)

-- Pre jobs only exist because of this migration's schema; delete them first
-- so dropping note_id's NOT NULL restore below never trips on their NULL
-- note_id.
DELETE FROM jobs WHERE type = 'pre_generate';

DROP TABLE IF EXISTS event_briefs;

DROP INDEX IF EXISTS jobs_pre_generate_active_unique_idx;
DROP INDEX IF EXISTS jobs_brief_idx;
DROP INDEX IF EXISTS jobs_calendar_event_idx;

ALTER TABLE jobs DROP CONSTRAINT IF EXISTS jobs_brief_fields_check;
ALTER TABLE jobs DROP CONSTRAINT IF EXISTS jobs_target_type_check;
ALTER TABLE jobs DROP CONSTRAINT IF EXISTS jobs_target_xor_check;

ALTER TABLE jobs DROP CONSTRAINT jobs_type_check;
ALTER TABLE jobs ADD CONSTRAINT jobs_type_check
    CHECK (type IN ('transcribe','summarize','embed'));

ALTER TABLE jobs
    DROP COLUMN calendar_event_id,
    DROP COLUMN brief_id,
    DROP COLUMN brief_generation;

ALTER TABLE jobs ALTER COLUMN note_id SET NOT NULL;
