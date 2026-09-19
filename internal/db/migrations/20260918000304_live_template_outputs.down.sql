-- live_template_outputs (down)

ALTER TABLE jobs DROP CONSTRAINT IF EXISTS jobs_live_fields_check;
ALTER TABLE jobs DROP CONSTRAINT IF EXISTS jobs_type_check;
ALTER TABLE jobs ADD CONSTRAINT jobs_type_check
    CHECK (type IN ('transcribe','summarize','embed','pre_generate'));

DROP TABLE IF EXISTS live_note_capacity;
DROP TABLE IF EXISTS live_note_subscriptions;
DROP INDEX IF EXISTS jobs_live_output_idx;
DROP INDEX IF EXISTS jobs_live_active_uniq;
ALTER TABLE jobs DROP COLUMN IF EXISTS target_revision;
ALTER TABLE jobs DROP COLUMN IF EXISTS live_output_id;
ALTER TABLE jobs DROP COLUMN IF EXISTS stream_id;
ALTER TABLE jobs DROP COLUMN IF EXISTS template_id;
DROP TABLE IF EXISTS live_template_outputs;
