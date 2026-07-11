-- note_event_id (down)
DROP INDEX IF EXISTS notes_event_id_idx;
ALTER TABLE notes DROP COLUMN event_id;
