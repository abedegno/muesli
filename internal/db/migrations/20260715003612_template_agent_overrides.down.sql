-- template_agent_overrides (down)
ALTER TABLE templates
DROP COLUMN system_prompt,
DROP COLUMN model,
DROP COLUMN temperature;
