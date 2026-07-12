ALTER TABLE note_speaker_aliases
  ADD COLUMN person_id UUID REFERENCES people(id) ON DELETE SET NULL;
CREATE INDEX note_speaker_aliases_person_idx ON note_speaker_aliases (person_id);
