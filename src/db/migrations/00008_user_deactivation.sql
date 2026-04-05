-- +goose Up

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS deactivated_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_users_deactivated_at ON users(deactivated_at);

-- +goose Down

DROP INDEX IF EXISTS idx_users_deactivated_at;

ALTER TABLE users
    DROP COLUMN IF EXISTS deactivated_at;
