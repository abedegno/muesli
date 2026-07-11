CREATE TABLE calendar_sources (
    id                 UUID PRIMARY KEY,
    owner_id           UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind               TEXT NOT NULL CHECK (kind IN ('ics','caldav','google','microsoft')),
    display_name       TEXT NOT NULL DEFAULT '',
    credentials        TEXT NOT NULL,
    selected_calendars JSONB NOT NULL DEFAULT '{}'::jsonb,
    status             TEXT NOT NULL DEFAULT 'ok' CHECK (status IN ('ok','auth_error','error')),
    last_synced_at     TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX calendar_sources_owner_idx ON calendar_sources (owner_id);

CREATE TABLE calendar_events (
    id               UUID PRIMARY KEY,
    owner_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    source_id        UUID NOT NULL REFERENCES calendar_sources(id) ON DELETE CASCADE,
    external_id      TEXT NOT NULL,
    title            TEXT NOT NULL DEFAULT '',
    starts_at        TIMESTAMPTZ NOT NULL,
    ends_at          TIMESTAMPTZ NOT NULL,
    description      TEXT NOT NULL DEFAULT '',
    location         TEXT NOT NULL DEFAULT '',
    conferencing_url TEXT NOT NULL DEFAULT '',
    attendees        JSONB NOT NULL DEFAULT '[]'::jsonb,
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (source_id, external_id)
);
CREATE INDEX calendar_events_owner_time_idx ON calendar_events (owner_id, starts_at);
