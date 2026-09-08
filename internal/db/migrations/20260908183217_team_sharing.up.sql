-- team_sharing (up)
--
-- Deployment team boundary (issue #12): roles, admin-issued single-use
-- invites, and explicit read-only folder grants.

-- Deployment roles. Every user in a deployment is either 'admin' or
-- 'member'. Default 'member' so the promotion below is the only row that
-- becomes admin during this migration.
ALTER TABLE users
    ADD COLUMN role TEXT NOT NULL DEFAULT 'member' CHECK (role IN ('admin', 'member'));

-- Promote the earliest user (by created_at, id) to admin. In embedded mode
-- this is normally the sole user; in an already-hosted deployment it is the
-- account that ran first-run setup.
WITH first_user AS (
    SELECT id FROM users ORDER BY created_at, id LIMIT 1
)
UPDATE users SET role = 'admin' WHERE id IN (SELECT id FROM first_user);

-- Supports ORDER BY (created_at, id) pagination for user listing.
CREATE INDEX users_created_at_id_idx ON users (created_at, id);

-- Admin-issued, single-use invites. Only the SHA-256 hash of the raw token
-- is ever persisted; the raw value is shown to the inviting admin once, in
-- the issue response.
CREATE TABLE user_invites (
    id              UUID PRIMARY KEY,
    token_hash      TEXT NOT NULL UNIQUE,
    role            TEXT NOT NULL CHECK (role IN ('admin', 'member')),
    created_by      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at      TIMESTAMPTZ NOT NULL,
    consumed_at     TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Explicit, non-inherited, read-only folder grants. A row here authorizes
-- exactly (folder_id, user_id) -- never ancestors, descendants, or any other
-- folder containing the same note. Hard-deleting the folder or user cascades
-- membership removal; soft-deleting the folder deliberately leaves the row
-- in place (viewer queries independently require a live folder).
CREATE TABLE folder_members (
    folder_id  UUID NOT NULL REFERENCES folders(id) ON DELETE CASCADE,
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role       TEXT NOT NULL CHECK (role IN ('viewer')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (folder_id, user_id)
);

-- Membership lookup by user (e.g. "which folders can I see").
CREATE INDEX folder_members_user_folder_idx ON folder_members (user_id, folder_id);
-- Membership pagination for GET /api/folders/{id}/members, ordered (created_at, user_id).
CREATE INDEX folder_members_folder_created_user_idx ON folder_members (folder_id, created_at, user_id);
-- Shared-folder discovery pagination for GET /api/folders/shared, ordered (created_at, folder_id).
CREATE INDEX folder_members_user_created_folder_idx ON folder_members (user_id, created_at, folder_id);
