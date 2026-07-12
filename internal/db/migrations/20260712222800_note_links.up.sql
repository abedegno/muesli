CREATE TABLE note_links (
    id uuid PRIMARY KEY,
    owner_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    from_note_id uuid NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
    to_note_id uuid NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT note_links_unique_link UNIQUE (from_note_id, to_note_id, owner_id)
);

CREATE INDEX note_links_from_note_id_idx ON note_links(from_note_id);
CREATE INDEX note_links_to_note_id_idx ON note_links(to_note_id);
