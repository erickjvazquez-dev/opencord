# Opencord — Spec (v0.1, Minimal Realtime MVP)

> Written before building, per the framework's "spec before >3-file change" rule.
> Scope is deliberately tiny: prove the realtime loop end-to-end, then expand.

## North star

A fully open-source, self-hostable Discord alternative anyone can run locally at
little to no cost.

## v0.1 goal

A logged-in user can send a message in one global channel and every other
logged-in, connected user sees it in real time. Refreshing reloads recent
history. That's it — the smallest thing that proves the architecture.

## In scope (v0.1)

1. **Accounts** — register + login with username/password. Passwords bcrypt-hashed.
   Sessions are JWTs (7-day TTL).
2. **One global channel** (`#general`) — no servers/channels table yet.
3. **Real-time messaging** — WebSocket gateway; on connect a client receives the
   last 50 messages, then live messages as they're sent.
4. **Presence count** — number of currently connected clients.
5. **Persistence** — messages survive restarts (Postgres).
6. **One-command run** — `docker compose up` → db + server + web.

## Out of scope (later milestones — see GOAL.md)

Servers/guilds, multiple channels, DMs, roles/permissions, message
edit/delete, reactions, typing indicators, file uploads, search, voice/video,
federation.

## v0.2 — Channels (in progress)

Goal: move from one hardcoded global channel to first-class channels a user can
list, pick, and post into — the foundation for servers/guilds later.

Incremental, non-breaking slices (the working global flow stays up until the UI
switches over):

1. **[this slice] `channels` table + read-only API.** Seed a default `general`
   channel; expose `GET /api/channels` (auth'd) returning the channel list. No
   behavior change to messaging/WS yet.
2. Add `messages.channel_id` (FK → channels), default existing rows to `general`;
   `Save`/`Recent` become channel-scoped.
3. WS gateway: client subscribes to a channel (`/ws?token=…&channel=<id>`); the
   hub fans out per-channel instead of one global room.
4. Web: channel sidebar; selecting a channel switches the socket + history.
5. `POST /api/channels` (create) + name validation + per-channel auth later.

Data model (slice 1): `channels(id, name UNIQUE, created_at)`.
Acceptance (slice 1): `GET /api/channels` returns `[{id, name:"general", …}]`
on a fresh DB; existing auth + single-channel WS flow unchanged; `go test` green.

## Architecture

- **Single Go binary** serves REST (`/api/*`, `/healthz`) and the WebSocket
  gateway (`/ws`). Self-migrates an idempotent schema on boot.
- **Hub pattern** for fan-out: one goroutine owns the client set; register /
  unregister / broadcast flow through channels (no locks). One channel today;
  the hub is where per-channel routing lands later.
- **React SPA** talks to one origin (nginx proxy in prod, Vite proxy in dev).
- **Auth on the socket** via `?token=` query param (browsers can't set headers
  on a WebSocket).

## Data model

```
users(id, username UNIQUE, password_hash, created_at)
messages(id, user_id → users, body, created_at)
```

## Acceptance criteria

- [ ] `docker compose up --build` serves the app at http://localhost:3000.
- [ ] Register → returns a token; reusing a username → 409.
- [ ] Login with wrong password → 401.
- [ ] Two browsers logged in as different users: a message from A appears for B
      within ~100ms without refresh.
- [ ] Presence count reflects connect/disconnect.
- [ ] Restarting the server preserves message history.
- [ ] `go test ./...` passes.

## Security notes (carried forward)

- Passwords never stored or logged in plaintext; only bcrypt hashes.
- `JWT_SECRET` must be overridden in production (documented; default is clearly
  marked insecure).
- Request bodies capped (64 KiB); WS messages capped (4 KiB) and trimmed.
- Treat every inbound frame/body as hostile (validate, bound, reject).

## Reactions UI (v0.2 — client for shipped backend, 2026-06-13)

The reactions **backend** shipped in `7e8ecfd` (PUT/DELETE `/api/messages/{id}/reactions`,
per-viewer counts, live WS `reaction` events) but had **no web UI**. This adds the client:

- **Render** reaction chips under each non-deleted message from `message.reactions`
  (`[{emoji, count, mine}]`). A chip the viewer reacted with is highlighted (`.mine`).
- **Add** via a small quick-emoji palette (👍 ❤️ 😂 🎉 😮 😢) opened from a per-message
  `react` button (available on ALL messages, not just your own — unlike edit/delete).
- **Toggle** by clicking a chip or palette emoji → `addReaction`/`removeReaction`.
- **Live**: the WS `reaction` event carries count-only summaries (`mine` always false —
  broadcast uses viewerID 0). So the client tracks "mine" locally in a `Set<"msgId:emoji">`
  seeded from the history event's `reactions[].mine`, and overlays it on count updates.
  Counts are authoritative from the server; mine is authoritative from the local set.
- Optimistic: clicking toggles the local set + count immediately; the WS event reconciles
  counts; an HTTP failure reverts the optimistic change.

## Direct messages — backend slice (v0.2, 2026-06-13)

Private 1:1 conversations. Built as a new channel **kind** rather than a parallel
system, so the existing message store + per-channel WS fan-out are reused; the genuinely
new piece is **per-channel access control** (public channels stay open; DM channels are
members-only). UI ships in a later tick (reactions pattern: backend → UI).

**Schema (idempotent):**
- `channels.kind TEXT NOT NULL DEFAULT 'public'` ('public' | 'dm').
- `channels.name` made nullable (DMs are unnamed). UNIQUE stays (Postgres allows many NULLs).
- `channel_members (channel_id, user_id, PRIMARY KEY(channel_id,user_id))` — membership
  for DM (and future private) channels. Public channels have no rows (open to all).

**Store:**
- `CreateOrGetDM(a, b)` — returns the existing 2-member DM channel for {a,b}, else creates
  one (`kind='dm'`, members a+b) in a tx. Idempotent for sequential calls. (Known: no
  cross-process lock yet — a concurrent double-create could make two DM channels; follow-up.)
- `ListDMs(user)` — the user's DM channels, each with the *other* participant.
- `CanAccessChannel(channelID, userID)` — true for public channels; for DMs, membership.
- `LookupUserByUsername(name)` — resolve a DM target.
- `ListChannels` now filters `kind='public'` so DMs never leak into the public sidebar.

**REST (auth-gated):**
- `POST /api/dms {username}` → find-or-create a DM with that user; returns `{id, createdAt,
  user:{id,username}}`. Rejects DM-with-self (400) and unknown user (404).
- `GET /api/dms` → the caller's DM list.

**Access control (the privacy core):**
- WS `ServeWS`: after resolving the channel, `CanAccessChannel` must pass or the upgrade is
  refused (403) — a non-member cannot open, read history, or send in a DM.
- REST `GET /api/messages?channel=`: same `CanAccessChannel` gate.
- Reaction access control (done 2026-06-13): `AddReaction`/`RemoveReaction` now require
  channel access (`requireChannelAccess` → `ErrForbidden` → HTTP 403), so a non-member
  can't react to a DM message by guessing its id. Edit/delete were already safe (they're
  ownership-scoped — a non-member has no messages in a channel they can't post to).
  Reproduced + re-attacked at the store and live-HTTP layers; guarded by
  `TestDMReactionAccessControlIntegration`.

**Tests (DB integration):** create-or-get idempotency (same channel twice); ListDMs returns
the other user; CanAccessChannel (public→anyone, dm→members only, non-member→false); unknown
channel→false; ListChannels excludes DMs.

## Servers / guilds — backend foundation slice (v0.2, 2026-06-13)

Group channels under a named server with membership, the way Discord guilds work.
Built additively so the existing global channels keep working: a channel with
`server_id IS NULL` stays a global public room (current behaviour); a channel with a
`server_id` belongs to that server and is visible only to its members. UI ships later
(backend-first, the DM pattern). This is the third access-control surface, after DMs.

**Schema (idempotent):**
- `servers (id, name, owner_id → users, created_at)`.
- `server_members (server_id, user_id, PRIMARY KEY)` — who can see a server's channels.
- `channels.server_id BIGINT REFERENCES servers(id)` (nullable; NULL = global room).

**Store:**
- `CreateServer(owner, name)` — insert the server + owner membership in a tx; returns it.
- `ListServers(user)` — the servers the user is a member of.
- `CreateServerChannel(serverID, name)` — a channel scoped to a server (members-only).
- `ListServerChannels(serverID)` — that server's channels.
- `IsServerMember(serverID, userID)` / join via `AddServerMember` (invites come later;
  for now a creator is the sole member, plus an explicit join endpoint for testing/MVP).
- `CanAccessChannel` extended: dm → channel membership; server-scoped → server membership;
  else (global public) → open. `ListChannels` now also requires `server_id IS NULL` so
  server channels never leak into the global list.

**REST (auth-gated):**
- `POST /api/servers {name}` → create (owner auto-joins). `GET /api/servers` → mine.
- `POST /api/servers/{id}/channels {name}` → create a channel in a server (members only).
- `GET /api/servers/{id}/channels` → that server's channels (members only; 403 otherwise).
- `POST /api/servers/{id}/join` → join a server (MVP; invite flow is a later slice).

**Access control (Rule 15):** a non-member's read of a server channel (WS upgrade + REST
history, already gated by `CanAccessChannel`) and the server-channel endpoints all return
403. Reproduced + re-attacked at the store and live-HTTP layers; integration test guards it.

**Tests (DB integration):** create server → owner is a member; create a channel under it;
member can access, non-member cannot; ListServers/ListServerChannels; global ListChannels
excludes server channels; join makes a non-member able to access.

## Server invites (v0.3, 2026-06-13) — closes the open-join gap

The first servers slice shipped `POST /api/servers/{id}/join`, which let ANY authenticated
user join ANY server by guessing its (sequential) id — so "members-only" servers weren't
actually private. Replace open join-by-id with **invite codes**: joining requires a valid,
unguessable code created by a member.

**Schema (idempotent):**
- `server_invites (code TEXT PRIMARY KEY, server_id → servers ON DELETE CASCADE,
  created_by → users, created_at)`. Code = 8 url-safe chars from crypto/rand (6 bytes).

**Store:**
- `CreateInvite(serverID, userID)` → generate a unique code, insert, return it (retries on
  the astronomically-rare PK collision).
- `RedeemInvite(code, userID)` → look up the invite's server, add the user as a member,
  return the Server; `ErrInvalidInvite` for an unknown code.

**REST (auth-gated):**
- `POST /api/servers/{id}/invites` (members only → 403) → `{code}`.
- `POST /api/invites/{code}` → join the invite's server; returns the Server; 404 on bad code.
- **Removed:** `POST /api/servers/{id}/join` (the open gap).

**Access control (Rule 15):** a non-member can no longer join by id — only a valid invite
code admits them; an invalid/guessed code → 404; a non-member can't mint an invite (403).
Reproduced + re-attacked at the store and live-HTTP layers; integration test guards it.

**UI:** each server gets an "invite" action (creates a code, shows it to copy); "Join
server" now prompts for a CODE (not an id) and redeems it.

**Tests (DB integration):** create invite → redeem makes a non-member a member with access;
invalid code → ErrInvalidInvite; the redeemed server is returned.
