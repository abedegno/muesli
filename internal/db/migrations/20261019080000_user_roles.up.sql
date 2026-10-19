ALTER TABLE users ADD COLUMN role TEXT NOT NULL DEFAULT 'member' CHECK (role IN ('admin','member'));
-- Preserve access for an existing installation without granting every member
-- administration. The original account becomes the initial administrator.
UPDATE users SET role='admin' WHERE id=(SELECT id FROM users ORDER BY created_at,id LIMIT 1);
