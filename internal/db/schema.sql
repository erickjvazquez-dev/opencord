-- Opencord schema. Idempotent so it can run on every boot.

-- Channels (v0.2). The MVP shipped a single hardcoded global room; this is the
-- first-class table behind it. A default 'general' channel is always present.
CREATE TABLE IF NOT EXISTS channels (
    id         BIGSERIAL PRIMARY KEY,
    name       TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO channels (name) VALUES ('general') ON CONFLICT (name) DO NOTHING;

CREATE TABLE IF NOT EXISTS users (
    id            BIGSERIAL PRIMARY KEY,
    username      TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS messages (
    id         BIGSERIAL PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    body       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS messages_created_at_idx ON messages (created_at);

-- Messages are channel-scoped (v0.2). Add the FK and backfill existing rows to
-- the default 'general' channel. Idempotent: safe to re-run on every boot.
ALTER TABLE messages ADD COLUMN IF NOT EXISTS channel_id BIGINT REFERENCES channels(id);
UPDATE messages SET channel_id = (SELECT id FROM channels WHERE name = 'general') WHERE channel_id IS NULL;
CREATE INDEX IF NOT EXISTS messages_channel_id_idx ON messages (channel_id);

-- Soft delete (v0.2): deleted messages are retained and rendered as "[deleted]".
ALTER TABLE messages ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;
-- Edit (v0.2): edited_at is set when a message's body is changed.
ALTER TABLE messages ADD COLUMN IF NOT EXISTS edited_at TIMESTAMPTZ;
