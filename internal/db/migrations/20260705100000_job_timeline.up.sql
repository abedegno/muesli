-- job_timeline (up)
-- Add started_at/finished_at to jobs so the admin UI can render a per-note
-- pipeline timeline (attempt start/end + computed duration).
ALTER TABLE jobs ADD COLUMN started_at TIMESTAMPTZ;
ALTER TABLE jobs ADD COLUMN finished_at TIMESTAMPTZ;
