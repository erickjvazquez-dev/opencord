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
-- Uploaded avatars (v0.4): a user may set a profile picture stored on local disk
-- (avatar_key = opaque on-disk name, avatar_type = sniffed image type). NULL = no
-- avatar → the client falls back to deterministic initials.
ALTER TABLE users ADD COLUMN IF NOT EXISTS avatar_key TEXT;
ALTER TABLE users ADD COLUMN IF NOT EXISTS avatar_type TEXT;
-- Custom status (v0.4): a short user-set status line shown by the name. NULL = none.
ALTER TABLE users ADD COLUMN IF NOT EXISTS status TEXT;
-- Status emoji (v0.4): an optional short emoji shown before the status line. NULL = none.
ALTER TABLE users ADD COLUMN IF NOT EXISTS status_emoji TEXT;
-- Presence state (v0.4): user-chosen availability (online|idle|dnd|invisible).
-- NULL reads as 'online'. 'invisible' appears offline to others (live conn still required).
ALTER TABLE users ADD COLUMN IF NOT EXISTS presence_state TEXT;
-- Profile (v0.5): a longer "About Me" bio + short pronouns, shown on the profile card.
-- Both NULL = none. React-escaped on render; length-capped server-side (Rule B).
ALTER TABLE users ADD COLUMN IF NOT EXISTS about TEXT;
ALTER TABLE users ADD COLUMN IF NOT EXISTS pronouns TEXT;

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
-- Pinned (v0.3): a message can be pinned in its channel (server-channel admins;
-- any member elsewhere). Surfaced on the message so clients can badge it.
ALTER TABLE messages ADD COLUMN IF NOT EXISTS pinned BOOLEAN NOT NULL DEFAULT false;
-- Replies (v0.3): a message may reference an earlier message in the SAME channel.
-- Nullable; soft-deleted targets keep their row so the reference stays valid and the
-- client renders "[deleted]". Cross-channel/bogus refs are dropped server-side.
ALTER TABLE messages ADD COLUMN IF NOT EXISTS reply_to BIGINT REFERENCES messages(id);

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

-- Attachments (v0.4): files/images carried by a message. The bytes live on the
-- server's local disk under OPENCORD_UPLOAD_DIR keyed by `storage_key` (a
-- server-generated opaque random name — the client filename is display-only and is
-- NEVER used as a path, so traversal is impossible). content_type is the sniffed
-- type (never the client's claim). Deleting a message cascades its rows.
CREATE TABLE IF NOT EXISTS attachments (
    id           BIGSERIAL PRIMARY KEY,
    message_id   BIGINT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    storage_key  TEXT NOT NULL,
    filename     TEXT NOT NULL,
    content_type TEXT NOT NULL,
    size         BIGINT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS attachments_message_id_idx ON attachments (message_id);

-- Direct messages (v0.2): a DM is a channel of kind 'dm' with exactly two members.
-- Public channels keep kind='public' and have no membership rows (open to everyone).
ALTER TABLE channels ADD COLUMN IF NOT EXISTS kind TEXT NOT NULL DEFAULT 'public';
-- Channel topic (v0.3): a short description shown in the channel header (server channels).
ALTER TABLE channels ADD COLUMN IF NOT EXISTS topic TEXT NOT NULL DEFAULT '';
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

-- Read state (v0.4): the highest message id a user has read in a channel. Drives the
-- sidebar unread indicators. last_read_id = 0 means "never read".
CREATE TABLE IF NOT EXISTS channel_reads (
    user_id      BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    channel_id   BIGINT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    last_read_id BIGINT NOT NULL DEFAULT 0,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, channel_id)
);

-- Per-channel notification mute (v0.5): a row here means userID muted channelID — it's
-- excluded from their unread results (sidebar dots, mention badges, browser-tab badge).
CREATE TABLE IF NOT EXISTS channel_mutes (
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    channel_id BIGINT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, channel_id)
);

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
-- Roles (v0.3): a member's role in a server — 'owner' | 'admin' | 'member'. The creator
-- is 'owner'; admin+ may create channels. Per-channel overrides come later.
ALTER TABLE server_members ADD COLUMN IF NOT EXISTS role TEXT NOT NULL DEFAULT 'member';
-- Timeout (v0.4): an owner/admin can temporarily mute a member — while timeout_until is
-- in the future the member can read but can't post (server-enforced, like slowmode).
-- NULL/past = not timed out. Discord's "timeout"; cleared early or auto-expires.
ALTER TABLE server_members ADD COLUMN IF NOT EXISTS timeout_until TIMESTAMPTZ;
ALTER TABLE channels ADD COLUMN IF NOT EXISTS server_id BIGINT REFERENCES servers(id) ON DELETE CASCADE;
CREATE INDEX IF NOT EXISTS channels_server_id_idx ON channels (server_id);

-- Custom colored roles (v0.7): Discord-style COSMETIC roles, separate from the
-- owner/admin/member permission tier above (which stays in server_members.role). An admin
-- creates named, colored roles and assigns them to members; a member's display color is
-- their highest-`position` assigned role's color (Discord's top-role rule).
CREATE TABLE IF NOT EXISTS server_roles (
    id         BIGSERIAL PRIMARY KEY,
    server_id  BIGINT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    color      TEXT NOT NULL,
    position   INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS server_roles_server_id_idx ON server_roles (server_id);
-- Role assignments: a member can hold many roles; role_id implies the server (via
-- server_roles.server_id), so it isn't denormalized here. Deleting a role or user cascades.
CREATE TABLE IF NOT EXISTS member_roles (
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id BIGINT NOT NULL REFERENCES server_roles(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, role_id)
);
CREATE INDEX IF NOT EXISTS member_roles_role_idx ON member_roles (role_id);
-- Per-channel posting policy (v0.3): 'everyone' (default) or 'admins' (read-only /
-- announcement channel — only a server owner/admin may post).
ALTER TABLE channels ADD COLUMN IF NOT EXISTS post_policy TEXT NOT NULL DEFAULT 'everyone';
-- Per-channel slowmode (v0.3): a non-admin must wait this many seconds between
-- messages. 0 = off. Enforced server-side in Store.Save.
ALTER TABLE channels ADD COLUMN IF NOT EXISTS slowmode_seconds INT NOT NULL DEFAULT 0;
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
-- Invite expiry (v0.4): NULL = never (legacy invites); new invites get now()+7d.
ALTER TABLE server_invites ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ;
-- Invite max-uses (v0.4): NULL = unlimited; `uses` counts successful joins. Enforced +
-- incremented atomically at redeem so concurrent joins can't overshoot the cap.
ALTER TABLE server_invites ADD COLUMN IF NOT EXISTS max_uses INT;
ALTER TABLE server_invites ADD COLUMN IF NOT EXISTS uses INT NOT NULL DEFAULT 0;

-- Channel categories (v0.4): a server groups its channels under named, collapsible
-- categories (Discord-style). A channel with category_id NULL is uncategorized. Deleting
-- a category sets its channels' category_id to NULL (never deletes the channels).
CREATE TABLE IF NOT EXISTS channel_categories (
    id         BIGSERIAL PRIMARY KEY,
    server_id  BIGINT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS channel_categories_server_id_idx ON channel_categories (server_id);
ALTER TABLE channels ADD COLUMN IF NOT EXISTS category_id BIGINT REFERENCES channel_categories(id) ON DELETE SET NULL;

-- Server bans (v0.4): a banned user is removed from the server AND blocked from
-- rejoining — RedeemInvite rejects them — until an owner/admin unbans them. Owner/admin
-- action, mirroring kick's authz; the stronger form of kick (kick lets them rejoin).
CREATE TABLE IF NOT EXISTS server_bans (
    server_id  BIGINT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    banned_by  BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    reason     TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (server_id, user_id)
);
CREATE INDEX IF NOT EXISTS server_bans_server_id_idx ON server_bans (server_id);

-- User blocks (v0.5): a directed block — blocker_id has blocked blocked_id. A block is
-- enforced SYMMETRICALLY for DMs: if A blocked B OR B blocked A, neither can open, read,
-- or send in their DM (slice 1). Hiding a blocked user's messages in SERVER channels is
-- a later slice. ON DELETE CASCADE drops a user's blocks when their account is removed.
CREATE TABLE IF NOT EXISTS user_blocks (
    blocker_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    blocked_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (blocker_id, blocked_id)
);
CREATE INDEX IF NOT EXISTS user_blocks_blocked_id_idx ON user_blocks (blocked_id);

-- Custom server emoji (v0.5): a server may upload named image emoji usable by its
-- members. The bytes live on the server's local disk under OPENCORD_UPLOAD_DIR keyed
-- by `emoji_key` (a server-generated opaque random name — never client input, so
-- traversal is impossible). content_type is the sniffed image type (never the
-- client's claim). `name` is a Discord-style slug, unique within the server. Deleting
-- the server cascades its emoji away.
CREATE TABLE IF NOT EXISTS server_emoji (
    id           BIGSERIAL PRIMARY KEY,
    server_id    BIGINT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    emoji_key    TEXT NOT NULL,
    content_type TEXT NOT NULL,
    created_by   BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS server_emoji_server_id_idx ON server_emoji (server_id);
CREATE UNIQUE INDEX IF NOT EXISTS server_emoji_name_uniq ON server_emoji (server_id, name);
