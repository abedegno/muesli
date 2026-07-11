ALTER TABLE plugins ADD COLUMN config_schema JSONB NOT NULL DEFAULT '{}'::jsonb;
