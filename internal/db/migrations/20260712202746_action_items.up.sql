CREATE TABLE action_items (
    id               UUID PRIMARY KEY,
    note_id          UUID NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
    owner_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    text             TEXT NOT NULL,
    owner_person_id  UUID NULL REFERENCES people(id) ON DELETE SET NULL,
    status           TEXT NOT NULL DEFAULT 'open',
    due_hint         TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE decisions (
    id         UUID PRIMARY KEY,
    note_id    UUID NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
    owner_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    text       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX action_items_note_idx ON action_items (note_id);
CREATE INDEX action_items_owner_status_idx ON action_items (owner_id, status);
CREATE INDEX decisions_note_idx ON decisions (note_id);
CREATE INDEX decisions_owner_idx ON decisions (owner_id);
