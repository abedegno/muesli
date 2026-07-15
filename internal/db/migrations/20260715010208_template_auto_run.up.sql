-- template_auto_run (up)
ALTER TABLE templates
ADD COLUMN auto_run BOOLEAN NOT NULL DEFAULT true;
