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

## Message search (v0.3, 2026-06-14) — in-channel

Search messages within a channel by text. Minimal slice: scoped to one channel (the one
you're viewing), members-only via the same access gate; channel-spanning search is later.

**Store:** `SearchMessages(channelID, viewerID, query, limit)` — `body ILIKE %query%`,
excludes deleted, newest-first then reversed to chronological. The query is parameterized
AND its `%`/`_`/`\` are escaped so a user typing `%` can't turn the search into match-all
(Rule B — wildcard-injection hardening). Empty/over-long queries are rejected.

**REST:** `GET /api/messages/search?channel=<id>&q=<text>` (auth'd) → `CanAccessChannel`
gate (403 for non-members of a DM/server channel) → matching messages. Bad/empty q → 400.

**UI:** a search box in the chat header; submitting shows a results panel (count + each
match: author · time · body) replacing the message list, with a clear (✕) to return live.

**Tests (DB integration):** matches case-insensitively; excludes deleted; a `%` query
matches literally (not everything); respects the limit. Access gate covered via CanAccessChannel.

## Server roles — first slice (v0.3, 2026-06-14)

The start of roles & permissions. Minimal, Discord-shaped: server membership carries a
**role** (owner | admin | member); some actions require admin+. This slice gates **channel
creation** (Discord default: plain members can't create channels) and adds **promotion**.
Per-channel permission overrides + a UI come in later slices.

**Schema (idempotent):** `server_members.role TEXT NOT NULL DEFAULT 'member'`. The server
creator is `owner` (set in CreateServer). Invite-redeemers join as `member`.

**Store:**
- `ServerRole(serverID, userID)` → 'owner' | 'admin' | 'member' | '' (not a member).
- `SetServerRole(serverID, actorID, targetID, role)` → only the **owner** may change roles;
  role ∈ {admin, member}; `ErrForbidden` otherwise. Can't change the owner's own role.

**Access control (Rule 15):**
- `POST /api/servers/{id}/channels` now requires admin+ (owner/admin), not just membership —
  a plain member → 403.
- `POST /api/servers/{id}/roles {userId, role}` → owner only (403 otherwise); promotes/demotes.
- Reproduced + re-attacked at store + live HTTP: member can't create a channel (403) until the
  owner promotes them to admin, then they can.

**Tests (DB integration):** creator is owner; a redeemed member can't create a channel; the
owner promotes them → admin → now they can; a non-owner can't promote (ErrForbidden).

## Per-channel posting policy (v0.3, 2026-06-14) — read-only / announcement channels

The last roles slice: a server channel can be marked **admin-only posting** (a read-only
announcement channel) — everyone reads, only owner/admins post. Minimal binary policy now;
full per-role/per-channel permission matrices are a later milestone.

**Schema (idempotent):** `channels.post_policy TEXT NOT NULL DEFAULT 'everyone'`
('everyone' | 'admins').

**Store:**
- `CanPostInChannel(channelID, userID)` → true unless the channel's policy is 'admins' AND
  it's a server channel AND the user isn't a server admin. (Public/serverless channels and
  the default policy always allow.)
- `Save` now calls it and returns `ErrForbidden` when posting isn't allowed — defense in
  depth, so every message path is gated, not just the WS handler.
- `SetChannelPostPolicy(channelID, actorID, policy)` — server admins only; valid policy
  only; `ErrForbidden` otherwise.

**WS:** `readPump` maps a dropped `ErrForbidden` Save to an `error` frame back to the sender
("you can't post in this channel") instead of silently swallowing it.

**REST:** `PATCH /api/channels/{id} {postPolicy}` — admin of the channel's server only (403);
invalid policy → 400.

**Access control (Rule 15):** a non-admin's WS send to an 'admins' channel is dropped (error
frame, no broadcast); an admin's posts. A non-admin can't change the policy (403). Reproduced
+ re-attacked at the store and live-WS/HTTP layers; integration test guards it.

**Tests:** admin sets 'admins' → member CanPost false / admin true; member Save → ErrForbidden;
public channels always allow; non-admin SetChannelPostPolicy → ErrForbidden.

## Message Markdown rendering (v0.3, 2026-06-14) — client-only, XSS-safe

**Goal:** render a small Discord-like Markdown subset in message bodies — `**bold**`,
`*italic*`/`_italic_`, `~~strike~~`, `` `inline code` ``, and ```` ```fenced code``` ````.

**Approach (security-first, Rule B/15):** `web/src/markdown.tsx::renderMarkdown(text)` returns
**React elements, never an HTML string** — no `dangerouslySetInnerHTML`. React escapes all text,
so only the fixed safe tag set (`strong`/`em`/`del`/`code`/`pre`) is emitted and any raw HTML in a
message (e.g. `<script>…</script>`) renders as literal text. Inline code and fenced blocks are
literal (no nested formatting); bold is matched before single-`*` italic.

**Wiring:** both message render sites in `Chat.tsx` use `renderMarkdown(m.body)` (deleted messages
keep the plain `[deleted]` stub). CSS for `code`/`pre`/`del` added to `styles.css`.

**Verification:** `qa/browser.mjs` sends `**bold** and \`code\` and <script>alert(1)</script>` and
asserts the `<strong>`/`<code>` render while the `<script>` shows as literal text with no injected
script element (XSS guard); AI-vision review of the screenshot confirms it looks right.

## Multi-line message composer (v0.3, 2026-06-14)

**Goal:** the composer was a single-line `<input>`, so users couldn't send multi-line
messages (and Markdown blockquotes / multi-line code blocks were untypeable).

**Change:** the composer is now a `<textarea>` (`Chat.tsx`) that auto-grows with its
content (JS sets `height = scrollHeight`, bounded by `max-height: 40vh` in CSS).
Keyboard: **Enter sends**, **Shift+Enter inserts a newline** (Discord convention).
`submitDraft()` is shared by the form `onSubmit` (Send button) and the Enter handler.
Bodies already render with `white-space: pre-wrap`, so newlines display.

**Verification:** `qa/browser.mjs` types two lines via Shift+Enter, sends with Enter, and
asserts both lines land in ONE message (and that "line one" wasn't sent on its own) —
proving Shift+Enter doesn't submit. AI-vision review of `03d-multiline.png` confirms it.

## Markdown — blockquote + spoiler (v0.3, 2026-06-14)

Completes the Markdown subset now that the composer is multi-line. `markdown.tsx` gained
block-level parsing: consecutive `> `-prefixed lines become one `<blockquote>`, and a
stateful `Spoiler` component renders `||text||` as a click-to-reveal span (hidden via CSS
`color: transparent` until clicked). Still React-elements-only (XSS-safe). CSS for
`blockquote` + `.spoiler`/`.spoiler.shown` added.

**QA note (meta):** `browser.mjs` initially saw this 5th message silently dropped — the
per-connection **rate limiter** (`rateBurst=5`, `+2/s`; every message *or* typing frame
costs a token) had emptied under the bot's rapid sends. The product is correct (limiter
working, renderer proven via react-dom/server); the QA now paces (waits for the bucket to
refill) before the send. Lesson: an automated client sends faster than a human and can trip
real abuse limits — pace the QA, don't weaken the limit.

## @mentions — rendering slice (v0.3, 2026-06-14)

First mention slice (client-only, builds on the Markdown renderer). `markdown.tsx` gains an
inline `@([A-Za-z0-9_]{2,32})` rule emitting `<span class="mention">`; `renderMarkdown(text,
{ me })` highlights a mention of the **current user** with an extra `mention-me` class (amber)
vs the regular accent chip. Both `Chat.tsx` render sites pass `{ me: user.username }`. CSS for
`.mention` / `.mention-me`. Still React-elements-only (XSS-safe).

**Scope/limits (logged, not hidden):** rendering only — no autocomplete, no resolution against
real users, no notifications, no @role/@everyone/@here, no replies/threads. Known quirk: an
email like `foo@bar` renders `@bar` as a mention (no left-boundary check); acceptable for the
MVP slice. **Verification:** browser QA sends `hey @<self> and @someone_else`, asserts the
self-mention gets `mention-me` and the other a plain `mention`; AI-vision of `03f-mention.png`
confirms the amber vs accent styling. Renderer also proven via react-dom/server.

## Single-binary production build — Go serves the embedded SPA (2026-06-14)

For cloud/one-binary deploys, the Go server now serves the built web SPA itself, so
production needs just the one binary + Postgres (no nginx, no private networking).

- `internal/webui` embeds `dist/` (`//go:embed all:dist`) and serves it: real assets
  when the path maps to a file, else `index.html` (SPA fallback for client routes).
- `router.go` mounts it as the `/*` catch-all — `/api`, `/ws`, `/healthz` match first.
- A committed placeholder `internal/webui/dist/index.html` keeps `go build` green in CI;
  the real `index.html` + `assets/` are produced by the web build and copied in at
  Docker build time (root `Dockerfile`, multi-stage: node build → go build → distroless).
- `config.go` binds `$PORT` when set (Railway/Heroku), else `OPENCORD_ADDR` (:8080).
- The existing `docker compose up` (separate nginx `web` + `server` + `db`) still works
  for local self-host; this single-image path is additive, used by the cloud deploy.

Verified locally: one binary on `$PORT` serves `/healthz`, the SPA at `/`, hashed
assets, the `/login` SPA fallback, and `POST /api/auth/register` — all 200.

## Channel topics — backend slice (v0.3, 2026-06-14)

Server channels gain a `topic` (header description). Backend-only this tick (header UI
follows, per the backend-first pattern).

- Schema: `ALTER TABLE channels ADD COLUMN IF NOT EXISTS topic TEXT NOT NULL DEFAULT ''`.
- `chat.Channel.Topic` (json `topic,omitempty`); `ListServerChannels` returns it.
- `Store.SetChannelTopic(channelID, actorID, topic)` — server admins only, `ErrForbidden`
  for non-admin/unknown/non-server channel, `ErrTopicTooLong` over 1024 chars (Rule B).
- `PATCH /api/channels/{id}` now takes optional `postPolicy` and/or `topic` (pointer
  fields, applied only when present → old postPolicy-only clients unaffected); empty patch
  → 400.
- Tests: admin sets topic (204), non-admin (403), >1024 (400), empty patch (400),
  postPolicy backward-compat (204), topic echoed in the channel list. DATABASE_URL-gated.

_UI shipped 2026-06-14 (iter 51): the header shows the topic (muted, divider, ellipsis) and admins get an "edit topic" prompt control; browser QA `07f-topic.png` + AI-vision verified._

## Message link autolink (v0.3, 2026-06-14)

`http(s)://` URLs in messages render as clickable `<a target="_blank" rel="noopener noreferrer">`
(markdown.tsx, new `link` inline rule). XSS-safe by construction: only `https?://` is matched, so
the href is never `javascript:`/`data:`; React escapes the attribute. Trailing sentence punctuation
(`).,!?;:]`) is trimmed back to text so "(see http://x.com)." links cleanly. URLs inside inline code
stay literal (code matches first). Verified via react-dom/server probe (basic/paren/xss/in-code) +
browser QA (clickable link + rel=noopener assertions) + AI-vision.
