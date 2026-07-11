CREATE TABLE note_speaker_aliases (
  id            BIGSERIAL PRIMARY KEY,
  note_id       UUID NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
  owner_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  speaker_label TEXT NOT NULL,
  alias_name    TEXT NOT NULL,
  UNIQUE(note_id, speaker_label)
);
CREATE INDEX idx_note_speaker_aliases_owner_note ON note_speaker_aliases(owner_id, note_id);
