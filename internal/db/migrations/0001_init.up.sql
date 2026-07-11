CREATE TABLE users (
    id            UUID PRIMARY KEY,
    email         TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE app_tokens (
    id         UUID PRIMARY KEY,
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    token_hash TEXT NOT NULL UNIQUE,
    kind       TEXT NOT NULL CHECK (kind IN ('app','session')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at TIMESTAMPTZ
);

CREATE TABLE notes (
    id               UUID PRIMARY KEY,
    owner_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title            TEXT NOT NULL DEFAULT '',
    status           TEXT NOT NULL DEFAULT 'recording',
    started_at       TIMESTAMPTZ,
    ended_at         TIMESTAMPTZ,
    audio_object_key TEXT,
    retention_state  TEXT NOT NULL DEFAULT 'pending',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_notes_owner ON notes(owner_id);

CREATE TABLE note_bodies (
    note_id UUID PRIMARY KEY REFERENCES notes(id) ON DELETE CASCADE,
    content TEXT NOT NULL DEFAULT ''
);

CREATE TABLE transcripts (
    id                UUID PRIMARY KEY,
    note_id           UUID NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
    transcriber_plugin TEXT NOT NULL,
    model             TEXT NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE transcript_segments (
    id             UUID PRIMARY KEY,
    transcript_id  UUID NOT NULL REFERENCES transcripts(id) ON DELETE CASCADE,
    start_ms       INTEGER NOT NULL,
    end_ms         INTEGER NOT NULL,
    text           TEXT NOT NULL,
    source         TEXT NOT NULL,
    speaker        TEXT
);
CREATE INDEX idx_segments_transcript ON transcript_segments(transcript_id);

CREATE TABLE templates (
    id       UUID PRIMARY KEY,
    owner_id UUID REFERENCES users(id) ON DELETE CASCADE,
    name     TEXT NOT NULL,
    sections JSONB NOT NULL
);

CREATE TABLE summaries (
    id           UUID PRIMARY KEY,
    note_id      UUID NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
    template_id  UUID REFERENCES templates(id) ON DELETE SET NULL,
    agent_plugin TEXT NOT NULL DEFAULT '',
    model        TEXT NOT NULL DEFAULT '',
    content      JSONB NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE plugins (
    id           UUID PRIMARY KEY,
    kind         TEXT NOT NULL CHECK (kind IN ('transcriber','agent')),
    name         TEXT NOT NULL,
    endpoint_url TEXT NOT NULL,
    config       JSONB NOT NULL DEFAULT '{}'::jsonb,
    enabled      BOOLEAN NOT NULL DEFAULT true,
    is_default   BOOLEAN NOT NULL DEFAULT false
);

CREATE TABLE jobs (
    id              UUID PRIMARY KEY,
    note_id         UUID NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
    type            TEXT NOT NULL CHECK (type IN ('transcribe','summarize')),
    status          TEXT NOT NULL DEFAULT 'pending',
    attempts        INTEGER NOT NULL DEFAULT 0,
    last_error      TEXT,
    payload         JSONB NOT NULL DEFAULT '{}'::jsonb,
    lease_expires_at TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_jobs_claimable ON jobs(status, lease_expires_at);
