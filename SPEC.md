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

## Pinned messages — backend slice (v0.3, 2026-06-14)

Messages can be pinned in their channel. Backend slice (UI follows).
- Schema: `messages.pinned BOOLEAN NOT NULL DEFAULT false` (idempotent ALTER).
- `Message.Pinned` surfaced in `Recent` (and WS history) so clients can badge it.
- `Store.SetMessagePinned(id, actorID, pinned)` → returns a stub for broadcast.
  Authz mirrors moderation: server channels → admins only; serverless channels
  (global, DM) → any member who can access. ErrMessageNotFound / ErrForbidden.
- REST: `PUT /api/messages/{id}/pin` and `DELETE …/pin` → broadcast a
  `message-pinned` event to the channel.
- Tests: bad id 400, non-admin 403 (server channel), admin pin 204 → pinned=true →
  unpin 204 → pinned=false (round-trip via Recent). DATABASE_URL-gated.

_UI shipped 2026-06-14 (iter 54): pin/unpin in the message hover actions (shown to admins in server channels, any member elsewhere via `canPin`), a "📌 pinned" badge, and a `message-pinned` WS handler for live updates; browser QA `07g-pin.png` + AI-vision verified._

## Pins list endpoint (v0.3, 2026-06-14)

`GET /api/messages/pins?channel=<id>` returns a channel's pinned (non-deleted) messages, oldest
first — so a "view all pins" panel sees every pin, not just those in the loaded last-50 history.
Access-gated to channel members (403 otherwise), mirroring `HandleRecent`. `Store.PinnedMessages`
is the backing query. Tested: non-member 403; owner gets exactly the pinned message (pinned=true),
the non-pinned one excluded. (Pins panel UI next.)

_Panel UI shipped 2026-06-14 (iter 56): a "pins" header button opens a panel (reusing the search-results panel) that lists the channel's pins via `fetchPins`; mutually exclusive with the search/members panels; browser QA `07h-pins-panel.png` + AI-vision verified._

## Markdown lists (v0.3, 2026-06-14)

Bulleted (`- `/`* `) and numbered (`1. `) lists render as `<ul>`/`<ol>` via `renderBlocks`
(line-grouped like blockquotes). The bullet rule requires a space (`- `/`* `) so `*italic*`
(no space) stays inline. Verified via react-dom/server probe (bullets, `*`, numbered, italic-not-
a-bullet, mixed) + browser QA (`.body ul li` count) + AI-vision.

## Voice channels — MVP (v0.4, 2026-06-14)

**Goal:** real-time audio so you can invite someone and talk. First slice is mesh
peer-to-peer audio for small calls (2–4); an SFU is the later scale path.

**Why mesh + no SFU (Rule A):** an SFU (mediasoup) is a heavyweight native dependency
and would break "self-hostable, one command, zero required paid service." Mesh WebRTC
needs only the browser + a STUN server (use a public STUN; TURN is optional, only for
hostile NATs). So the one-command stack stays intact.

**Architecture:**
- **Signaling rides the existing WebSocket gateway** — no new server, no new port. The
  server is a *dumb relay*: it stamps the sender and rebroadcasts voice frames to the
  channel; clients do the WebRTC work.
- Three client→server frames (all relayed to the channel):
  - `voice-join` → server broadcasts `{type:"voice-join", from, username}`.
  - `voice-leave` → `{type:"voice-leave", from}`.
  - `voice-signal` `{target, signal}` → `{type:"voice-signal", from, target, signal}`;
    each client ignores it unless `target === me`. `signal` is opaque WebRTC JSON
    (SDP offer/answer or ICE candidate; trickle ICE keeps frames small).
- **Roster is client-derived** from join/leave frames — no server-side voice state.
- **WS read limit raised to 16 KiB** (`maxFrameSize`) so SDP fits; text bodies stay
  bounded to `maxMessageSize` (4 KiB). Voice frames are exempt from the strict text
  token bucket (ICE is bursty) but ride their **own, more generous voice bucket**
  (`voiceBurst` 100, `+voiceRefillPerSec` 50/s) so a flood can't be amplified to the
  whole channel — closed 2026-06-14, adversarially tested (`TestServeWSVoiceFloodGuard`).

**Client (next slice):** `getUserMedia({audio})` → an `RTCPeerConnection` per peer
(public STUN), exchange offer/answer/ICE via the frames above, play remote audio; a
"Join voice" control + a roster of who's in the call + leave. New peers: the existing
member offers to the joiner (deterministic by user id to avoid glare). The joiner
needs no server roster — it discovers everyone by simply answering the incoming
offers; only existing members ever create offers, so there is no glare except on
simultaneous joins (broken by user id). Map signal payloads: SDP offer/answer and
each ICE candidate are separate `voice-signal` frames (trickle ICE).

**Slices:** (1) backend signaling relay [done] · (2) client WebRTC + UI [done] ·
(3) later: optional TURN config, SFU for larger calls, video/screenshare.

### Voice channels — client slice (v0.4, 2026-06-14)

Slice 2: the mesh WebRTC client + UI. `web/src/voice.ts` holds a `VoiceSession`
class (plain, non-React, so the connection graph survives re-renders); `Chat.tsx`
owns one instance per call, forwards relayed `voice-*` frames to it, and renders
the roster it emits.

- **One `RTCPeerConnection` per peer.** Glare (simultaneous joins) is resolved
  with the WebRTC **perfect-negotiation** pattern; politeness is `myId > peerId`
  so the two sides always pick opposite roles with no server coordination. The
  existing member learns of a joiner via `voice-join` and opens the peer; the
  joiner needs no roster — it answers the incoming offer. STUN-only (public);
  on loopback/LAN host candidates connect without it. TURN is a later slice.
- **Crisp audio (owner directive 2026-06-14: "as crisp as possible, close the
  gap Discord has").** Capture uses echo cancellation + noise suppression +
  auto-gain, 48 kHz mono; the sender's `maxBitrate` is raised to ~96 kbps Opus
  (above Discord's ~64 kbps default), best-effort via `setParameters`.
- **Device auto-detect + override (owner directive).** With no `deviceId`,
  `getUserMedia` captures the OS's *current* default — whatever headset/mic the
  user is actually on. On `devicechange` (plug/unplug) while set to "Auto", the
  client re-acquires so the new default takes over. A manual 🎙 input + 🎧 output
  picker overrides: input hot-swaps the track via `replaceTrack` (no
  renegotiation); output routes via `HTMLAudioElement.setSinkId`.
- **UI:** a "🎙 Join voice" header control; an in-call voice bar with a live
  roster (each remote peer shows a connecting/connected/failed dot), the device
  pickers, mute (toggles the local track's `enabled`), and leave. Leaving the
  channel or logging out leaves the call (sends `voice-leave`, frees the mic).

**Verification (Rule 14):** `qa/voice.mjs` launches two Chromium contexts with
fake media (`--use-fake-device/-ui-for-media-stream`), both Join voice in
`#general`, and asserts a real `connectionState === "connected"` mesh link on
*both* sides, live remote audio tracks, the device picker, mute, and live roster
teardown on leave. Wired into `qa/run.sh` after the realtime suite.

## WS gateway hardening — voice flood guard + send-channel close race (v0.4, 2026-06-14)

Hardening the voice signaling surface (Rule 15) shipped two backend fixes:

- **Dedicated voice rate bucket.** `voice-join`/`voice-leave`/`voice-signal` are
  exempt from the strict text token bucket (ICE is legitimately bursty) but now ride
  their own bucket (`voiceBurst` 100, `+voiceRefillPerSec` 50/s). Every voice frame is
  fanned out to the whole channel, so an unthrottled flood was an amplification DoS;
  the bucket caps it at the source while still passing a real 2–4 peer setup burst.

- **`send` channel is never closed (close-race fix).** The flood test surfaced a
  pre-existing panic: `Hub.emitToChannel` did `close(c.send)` to drop a stuck
  consumer, but `ServeWS` (history) and `readPump` (the read-only error reply) also
  write to `c.send` — a concurrent close panics the connection goroutine with "send on
  closed channel". Fix: `c.send` is **never** closed; the hub closes a per-client
  `done` channel instead (only on the hub goroutine, so at most once). Producers use
  `sendSafe` (`select { case c.send<-data: case <-c.done: }`) and `writePump` exits on
  `<-c.done`. Verified race-free (`go test -race ./internal/ws`).

**Verification:** `TestServeWSVoiceFloodGuard` (Rule 15) — a legitimate burst is
relayed in full; a 500-frame flood is bounded (no crash). Reproduced the panic first,
applied the fix, re-attacked → bounded + no panic. Full `go test ./...` + `-race` green.

## Voice — speaking indicator (active-speaker highlight) (v0.4, 2026-06-14)

Polish toward the owner's "crisp / close the gap Discord has" bar: show **who is
talking**, like Discord's green speaking ring. Fully client-side — no new signaling,
no server state (Rule A).

- **Detection (Web Audio VAD).** One `AudioContext`; a `MediaStreamSource` +
  `AnalyserNode` per stream — the local mic and each peer's remote stream. A single
  ~120 ms timer samples every analyser's time-domain RMS; above a threshold = loud.
  **Hysteresis**: a source stays "speaking" for ~250 ms after its last loud sample so
  the ring doesn't flicker between syllables.
- **Local + remote.** Each client detects remote speakers directly from the audio it
  already receives (no "I'm speaking" frames to trust or rate-limit — Rule B). Muting
  forces local speaking off (a disabled track is silence anyway; also gated on `muted`).
- **UI.** `VoicePeer` gains `speaking`; the session also reports local speaking. The
  voice-bar chip (incl. "you") gets a `.speaking` class → a green ring/glow.
- **Verification:** browser QA — with Chromium's fake mic (a tone), join voice and
  assert a chip gains the speaking state; `data-speaking` is asserted in `qa/voice.mjs`.

## Voice — per-user volume + browser-QA entry guard (v0.4, 2026-06-14)

Two small additions:

- **Per-user volume.** Each remote peer's voice-bar chip gets a compact slider
  (0–100%, default 100%) that sets *only your* playback of that peer via
  `HTMLAudioElement.volume`. Purely local — never signaled, so it can't be abused to
  make yourself louder for everyone (Rule B). `VoicePeer.volume` carries it; the
  session clamps to 0..1 and applies it on the peer's `<audio>` (also re-applied when
  the track arrives). Verified in `qa/voice.mjs`: a slider set to 40% drives that
  peer's audio element to `volume ≈ 0.4`.

- **Browser-QA voice entry guard.** `qa/browser.mjs` (single client, launched with
  fake media) now asserts the "🎙 Join voice" control renders, is enabled once the WS
  connects, joins solo (the in-voice bar with your chip appears), and that leaving
  removes the bar — so the voice entry point is guarded even without the multi-peer
  `qa/voice.mjs` run.

## Voice — push-to-talk (v0.4, 2026-06-14)

A press-and-hold Talk control so the mic only transmits while you're holding it —
the last non-trivial voice-control gap toward Discord parity.

- **Model.** The mic track's `enabled` is driven by one rule: PTT off → live unless
  muted; PTT on → live only while `transmitting`. PTT supersedes mute (the mute
  button is swapped for the Talk button while PTT is on). A `setPushToTalk(on)` /
  `setTransmitting(on)` pair on `VoiceSession` funnels through a single
  `applyMicState()`, which also clears your speaking ring the instant the mic goes
  silent (no VAD hang).
- **Control.** A **press-and-hold button** (`pointerdown`→transmit, `pointerup`/
  `pointerleave`→stop) — works on desktop *and* touch with no key-vs-composer
  conflict (a global PTT hotkey is a later nicety). It turns green ("🎙 Talking…")
  while live; `data-transmitting` mirrors the state.
- **Verification:** `qa/voice.mjs` toggles PTT on (Talk button appears, mute hides),
  asserts not-transmitting by default, `pointerdown`→transmitting, `pointerup`→not,
  and PTT-off restores mute (`voice-05-ptt-talking.png` for the vision pass).

## Voice — mesh → OSS SFU (LiveKit) scale path (DECISION + plan, 2026-06-14)

**Status: APPROVED architecture, not yet built.** The audio north star is thousands
per call with no fidelity loss; **mesh can't get there** (every client uploads its
mic to every other — N² fan-out, ~4 participants max). The fix is an **SFU**: each
client uploads once, the server forwards selectively (only the top-N active speakers
at scale). This is a Rule-16 adoption → vetted by `stack-guardian` (2026-06-14).

**Decision: LiveKit** (`github.com/livekit/livekit`, Apache-2.0). Why over the
alternatives: it's **written in Go with a first-class server SDK**
(`server-sdk-go` — room service + JWT token minting + webhooks), so it's the lowest
integration cost from this exact backend; **genuinely free to self-host** (no paid
tier/cap/key; single-node needs no Redis); license-compatible with a permissive
self-hostable project. mediasoup = C++/Node, no Go API (extra runtime + IPC bridge);
Janus = GPLv3 copyleft + C, no Go SDK (license friction + highest integration cost).

**Non-negotiable: the SFU is strictly OPT-IN; mesh stays the default (Rule A).**
- Gated on config: `OPENCORD_SFU_URL` (+ `OPENCORD_SFU_KEY` / `OPENCORD_SFU_SECRET`).
  **Empty ⇒ mesh**, byte-for-byte as today. The one-command `docker-compose.yml`
  stack stays SFU-free; a self-hoster who wants scale opts in by running LiveKit and
  setting the env. No required paid service, ever.
- Do NOT adopt the canned Railway LiveKit template — it bundles Redis + a *required*
  OpenAI key (a hidden bill). Use the bare single-node OSS server.

**Planned slices (build later, one per tick):**
1. **[this commit] decision + SPEC** (stack-guardian APPROVE recorded).
2. **[DONE 2026-06-14] Server config + token endpoint.** Optional SFU config
   (`OPENCORD_SFU_URL`/`_KEY`/`_SECRET`, all default empty). `POST
   /api/voice/token?channel=<id>` → mints a LiveKit access token (`internal/voice`),
   **server-side from the verified JWT** (Rule C), **room = `opencord-ch-<id>`**,
   **gated by `CanAccessChannel`** (Rule B — non-member → 403, no token). Returns
   `{sfu,url,room,token}` when configured, else `{sfu:false}` → client uses mesh.
   A LiveKit token is just an HS256 JWT with a `video` grant, so it's minted with the
   existing `golang-jwt` lib — **no `server-sdk-go` dependency** (go.mod stays lean).
   Verified: unit test (decodes to the right LiveKit claims, secret-bound) +
   DB-integration test (unauth 401 / non-member 403 / member → room-scoped token) +
   live binary check with/without the env. ~~Deferred: acceptance by a real LiveKit
   server.~~
2.5. **[DONE 2026-06-15] Real-LiveKit acceptance proof + E2E harness.** Closes the
   deferred gap above. `qa/sfu-run.sh` boots a local LiveKit (`livekit-server --dev
   --node-ip 127.0.0.1`, placeholder keys devkey/secret) + an Opencord server wired
   to it, and `qa/sfu.mjs` has **two real browsers** connect to that LiveKit with
   tokens minted by `/api/voice/token` and assert each sees the other — proving the
   token format is **accepted by a real server** and the SFU forwards participants.
   (Docker gotcha encoded: LiveKit must advertise `--node-ip 127.0.0.1` or its WebRTC
   candidates point at the container IP and ICE fails from the host browser.)
3. **[DONE 2026-06-15] Client SFU path.** `web/src/sfu.ts` `SfuSession` implements the
   same `VoiceTransport` surface as the mesh `VoiceSession`, over `livekit-client`
   (**lazy-imported** → code-split into its own 133 KB-gzip chunk, so mesh-only
   deployments never download it). `joinVoice` fetches `/api/voice/token`: `sfu:true`
   → SfuSession (publishes mic; roster/active-speaker/mute/PTT/volume/devices mapped
   to LiveKit), else the mesh path, unchanged. The voice-bar UI is transport-agnostic.
   E2E'd through `qa/sfu-run.sh` (now boots vite too): two real browsers Join voice
   against a local LiveKit and see each other connected via the SFU; the mesh harness
   (`qa/run.sh`, no SFU env) still passes — `{sfu:false}` falls back to mesh.
   *Follow-up: browser-autoplay gesture handling (`room.startAudio()` + an
   "enable audio" prompt) for the rare blocked-autoplay case.*
4. **Scale polish.** Active-speaker selection (forward top-N loudest), optional
   self-hosted TURN for hostile NATs, later cascaded SFUs.

**Railway demo: DEFERRED.** Running a LiveKit instance on the `talented-curiosity`
Railway project is a *separate* decision, not required to ship the integration.
Flags if/when adopted: Railway has no native UDP (LiveKit there is **TCP-only** =
degraded media, won't demonstrate the no-fidelity-loss bar), and SFU egress is
**usage-unbounded** ($0.05/GB fan-out → tens of $/mo under load). If ever run: cap
it + wire its Railway usage into the statusline (Rule 16.4) so egress can't bill
silently. The integration itself (slices 2–3) ships and is testable without it.

## Slowmode — per-channel post cooldown (v0.3, 2026-06-15)

Discord-style **slowmode**: an admin sets a per-channel cooldown (seconds); a
non-admin must wait that long between messages. Abuse-protection (security north
star) — **enforced server-side** (Rule B), never trusted from the client.

- **Schema:** `channels.slowmode_seconds INT NOT NULL DEFAULT 0` (idempotent). 0 = off.
- **Enforcement (in `Store.Save`, the one post path):** if `slowmode_seconds > 0`
  and the poster is **not** a server admin, compare server-side (`now() - created_at`,
  no client/app clock trust) against their last message in the channel → too soon =
  `ErrSlowMode`. First message always allowed; admins/owner exempt (like read-only).
- **Config:** admins set it via the existing `PATCH /api/channels/{id}`
  (`slowmodeSeconds`, 0..21600 = 6h max, mirrors `postPolicy`/`topic`); non-admin → 403.
- **WS:** a throttled send gets an `error` event ("slow mode…") to the sender only —
  same pattern as a read-only channel, never a silent drop.
- **UI:** an admin "slowmode" control + a 🐌 badge showing the interval; the composer
  surfaces the throttle error.
- **Verify:** store-integration (first msg ok · 2nd within window → ErrSlowMode ·
  admin exempt · expiry allows again) + browser-QA badge assertion.
