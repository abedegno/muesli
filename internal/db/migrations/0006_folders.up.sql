CREATE TABLE folders (
    id         UUID PRIMARY KEY,
    owner_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX folders_owner_name_unique ON folders (owner_id, lower(name));
CREATE INDEX folders_owner_idx ON folders (owner_id);

CREATE TABLE note_folders (
    note_id   UUID NOT NULL REFERENCES notes(id)   ON DELETE CASCADE,
    folder_id UUID NOT NULL REFERENCES folders(id) ON DELETE CASCADE,
    PRIMARY KEY (note_id, folder_id)
);
CREATE INDEX note_folders_folder_id_idx ON note_folders (folder_id);
