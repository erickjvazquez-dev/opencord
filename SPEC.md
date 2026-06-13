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
