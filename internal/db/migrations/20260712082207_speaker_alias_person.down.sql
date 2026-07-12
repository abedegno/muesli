DROP INDEX IF EXISTS note_speaker_aliases_person_idx;
ALTER TABLE note_speaker_aliases DROP COLUMN IF EXISTS person_id;
