-- +goose Up

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS username TEXT,
    ADD COLUMN IF NOT EXISTS password_hash TEXT,
    ADD COLUMN IF NOT EXISTS role TEXT;

UPDATE users
SET role = CASE WHEN is_admin THEN 'admin' ELSE 'student' END
WHERE role IS NULL;

ALTER TABLE users
    ALTER COLUMN role SET DEFAULT 'student',
    ALTER COLUMN role SET NOT NULL;

-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'users_role_check'
    ) THEN
        ALTER TABLE users
            ADD CONSTRAINT users_role_check
            CHECK (role IN ('admin', 'teacher', 'student'));
    END IF;
END $$;
-- +goose StatementEnd

CREATE UNIQUE INDEX IF NOT EXISTS idx_users_username_unique ON users(username) WHERE username IS NOT NULL;

ALTER TABLE user_invites
    ADD COLUMN IF NOT EXISTS role TEXT;

UPDATE user_invites
SET role = 'student'
WHERE role IS NULL;

ALTER TABLE user_invites
    ALTER COLUMN role SET DEFAULT 'student',
    ALTER COLUMN role SET NOT NULL;

-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'user_invites_role_check'
    ) THEN
        ALTER TABLE user_invites
            ADD CONSTRAINT user_invites_role_check
            CHECK (role IN ('admin', 'teacher', 'student'));
    END IF;
END $$;
-- +goose StatementEnd

-- +goose Down

ALTER TABLE user_invites DROP CONSTRAINT IF EXISTS user_invites_role_check;
ALTER TABLE user_invites DROP COLUMN IF EXISTS role;

DROP INDEX IF EXISTS idx_users_username_unique;

ALTER TABLE users DROP CONSTRAINT IF EXISTS users_role_check;
ALTER TABLE users DROP COLUMN IF EXISTS role;
ALTER TABLE users DROP COLUMN IF EXISTS password_hash;
ALTER TABLE users DROP COLUMN IF EXISTS username;
