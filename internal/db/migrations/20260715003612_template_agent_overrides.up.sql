-- template_agent_overrides (up)
ALTER TABLE templates
ADD COLUMN system_prompt TEXT,
ADD COLUMN model TEXT,
ADD COLUMN temperature DOUBLE PRECISION;
