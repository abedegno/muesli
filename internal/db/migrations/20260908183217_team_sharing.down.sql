-- team_sharing (down)

DROP TABLE IF EXISTS folder_members;
DROP TABLE IF EXISTS user_invites;

DROP INDEX IF EXISTS users_created_at_id_idx;
ALTER TABLE users DROP COLUMN IF EXISTS role;
