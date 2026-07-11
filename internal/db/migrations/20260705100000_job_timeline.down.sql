-- job_timeline (down)
-- Remove the started_at/finished_at columns from jobs.
ALTER TABLE jobs DROP COLUMN finished_at;
ALTER TABLE jobs DROP COLUMN started_at;
