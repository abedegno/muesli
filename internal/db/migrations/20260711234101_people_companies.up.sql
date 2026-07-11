CREATE TABLE companies (
    id         UUID PRIMARY KEY,
    owner_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    domain     TEXT NOT NULL,
    name       TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (owner_id, domain)
);
CREATE TABLE people (
    id            UUID PRIMARY KEY,
    owner_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    primary_email TEXT NOT NULL,
    display_name  TEXT NOT NULL DEFAULT '',
    company_id    UUID REFERENCES companies(id) ON DELETE SET NULL,
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (owner_id, primary_email)
);
CREATE INDEX people_owner_idx    ON people (owner_id);
CREATE INDEX people_company_idx  ON people (company_id);
