-- note_event_id (up)
ALTER TABLE notes ADD COLUMN event_id UUID REFERENCES calendar_events(id) ON DELETE SET NULL;
CREATE INDEX notes_event_id_idx ON notes (event_id);
