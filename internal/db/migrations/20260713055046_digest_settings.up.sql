-- digest_settings (up)
CREATE TABLE digest_settings (
    owner_id     UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    cadence      TEXT NOT NULL DEFAULT 'off' CHECK (cadence IN ('off', 'daily', 'weekly')),
    last_sent_at TIMESTAMPTZ NULL,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
