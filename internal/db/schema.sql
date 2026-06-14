-- Opencord schema. Idempotent so it can run on every boot.

-- Channels (v0.2). The MVP shipped a single hardcoded global room; this is the
-- first-class table behind it. A default 'general' channel is always present.
CREATE TABLE IF NOT EXISTS channels (
    id         BIGSERIAL PRIMARY KEY,
    name       TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- Idempotent seed that does NOT depend on a UNIQUE(name) constraint: servers replace
-- that constraint with partial indexes later, so `ON CONFLICT (name)` would break on
-- re-runs. (server_id doesn't exist yet at this point, so the guard can't reference it;
-- the global general is created on the first boot and persists, so this stays correct.)
INSERT INTO channels (name)
SELECT 'general' WHERE NOT EXISTS (SELECT 1 FROM channels WHERE name = 'general');

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

-- Reactions (Discord parity): one row per (message, user, emoji).
CREATE TABLE IF NOT EXISTS reactions (
    id         BIGSERIAL PRIMARY KEY,
    message_id BIGINT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    emoji      TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (message_id, user_id, emoji)
);
CREATE INDEX IF NOT EXISTS reactions_message_id_idx ON reactions (message_id);

-- Direct messages (v0.2): a DM is a channel of kind 'dm' with exactly two members.
-- Public channels keep kind='public' and have no membership rows (open to everyone).
ALTER TABLE channels ADD COLUMN IF NOT EXISTS kind TEXT NOT NULL DEFAULT 'public';
-- DM channels are unnamed, so name must be nullable (the UNIQUE constraint still
-- holds: Postgres permits many NULLs).
ALTER TABLE channels ALTER COLUMN name DROP NOT NULL;

-- Channel membership — drives DM (and future private-channel) access control.
CREATE TABLE IF NOT EXISTS channel_members (
    channel_id BIGINT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (channel_id, user_id)
);
CREATE INDEX IF NOT EXISTS channel_members_user_id_idx ON channel_members (user_id);

-- Servers / guilds (v0.2): channels can be grouped under a named server with its own
-- membership. A channel with server_id IS NULL stays a global public room (the current
-- behaviour); a channel with a server_id is visible only to that server's members.
CREATE TABLE IF NOT EXISTS servers (
    id         BIGSERIAL PRIMARY KEY,
    name       TEXT NOT NULL,
    owner_id   BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS server_members (
    server_id  BIGINT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (server_id, user_id)
);
CREATE INDEX IF NOT EXISTS server_members_user_id_idx ON server_members (user_id);
ALTER TABLE channels ADD COLUMN IF NOT EXISTS server_id BIGINT REFERENCES servers(id) ON DELETE CASCADE;
CREATE INDEX IF NOT EXISTS channels_server_id_idx ON channels (server_id);
-- Channel names are unique per scope, not globally: each server can have its own
-- #general. Replace the table-wide UNIQUE(name) with two partial unique indexes.
ALTER TABLE channels DROP CONSTRAINT IF EXISTS channels_name_key;
CREATE UNIQUE INDEX IF NOT EXISTS channels_global_name_uniq
    ON channels (name) WHERE server_id IS NULL AND name IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS channels_server_name_uniq
    ON channels (server_id, name) WHERE server_id IS NOT NULL;

-- Server invites (v0.3): joining a server requires a valid, unguessable code created
-- by a member — replaces the original open join-by-id.
CREATE TABLE IF NOT EXISTS server_invites (
    code       TEXT PRIMARY KEY,
    server_id  BIGINT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    created_by BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS server_invites_server_id_idx ON server_invites (server_id);
