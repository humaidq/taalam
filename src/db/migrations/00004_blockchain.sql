-- +goose Up

CREATE TABLE IF NOT EXISTS chain_blocks (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    height              BIGINT NOT NULL,
    previous_block_hash TEXT,
    block_hash          TEXT NOT NULL,
    event_type          TEXT NOT NULL,
    event_hash          TEXT NOT NULL,
    actor_user_id       UUID REFERENCES users(id) ON DELETE SET NULL,
    occurred_at         TIMESTAMPTZ NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chain_blocks_height_unique UNIQUE (height),
    CONSTRAINT chain_blocks_block_hash_unique UNIQUE (block_hash),
    CONSTRAINT chain_blocks_height_nonnegative CHECK (height >= 0),
    CONSTRAINT chain_blocks_block_hash_hex CHECK (block_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT chain_blocks_event_hash_hex CHECK (event_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT chain_blocks_previous_block_hash_hex CHECK (
        previous_block_hash IS NULL OR previous_block_hash ~ '^[0-9a-f]{64}$'
    )
);

CREATE INDEX IF NOT EXISTS idx_chain_blocks_created_at ON chain_blocks(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_chain_blocks_actor_user_id ON chain_blocks(actor_user_id);
CREATE INDEX IF NOT EXISTS idx_chain_blocks_event_type ON chain_blocks(event_type);

CREATE TABLE IF NOT EXISTS chain_events (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    block_id      UUID NOT NULL UNIQUE REFERENCES chain_blocks(id) ON DELETE CASCADE,
    event_type    TEXT NOT NULL,
    entity_type   TEXT NOT NULL,
    entity_id     TEXT,
    payload_json  JSONB NOT NULL,
    payload_hash  TEXT NOT NULL,
    actor_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    occurred_at   TIMESTAMPTZ NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chain_events_payload_hash_hex CHECK (payload_hash ~ '^[0-9a-f]{64}$')
);

CREATE INDEX IF NOT EXISTS idx_chain_events_event_type ON chain_events(event_type);
CREATE INDEX IF NOT EXISTS idx_chain_events_entity ON chain_events(entity_type, entity_id);
CREATE INDEX IF NOT EXISTS idx_chain_events_actor_user_id ON chain_events(actor_user_id);
CREATE INDEX IF NOT EXISTS idx_chain_events_occurred_at ON chain_events(occurred_at DESC);

-- +goose Down

DROP INDEX IF EXISTS idx_chain_events_occurred_at;
DROP INDEX IF EXISTS idx_chain_events_actor_user_id;
DROP INDEX IF EXISTS idx_chain_events_entity;
DROP INDEX IF EXISTS idx_chain_events_event_type;
DROP TABLE IF EXISTS chain_events;

DROP INDEX IF EXISTS idx_chain_blocks_event_type;
DROP INDEX IF EXISTS idx_chain_blocks_actor_user_id;
DROP INDEX IF EXISTS idx_chain_blocks_created_at;
DROP TABLE IF EXISTS chain_blocks;
