CREATE TABLE note_shares (
    id uuid PRIMARY KEY,
    token text NOT NULL UNIQUE,
    note_id uuid NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
    owner_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz,
    revoked_at timestamptz
);

CREATE INDEX note_shares_note_id_idx ON note_shares(note_id);
