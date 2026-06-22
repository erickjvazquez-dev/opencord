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
4. **Scale polish.** **[active-speaker selection DONE 2026-06-15]** The SFU client
   connects with `autoSubscribe:false` and subscribes to only the **top-N loudest**
   remote audio streams (`MAX_AUDIO_SUBSCRIPTIONS`=12), so a 1000-person room never
   tries to mix 1000 streams. The picker `selectAudioSubscriptions(all, activeNow,
   recent, max)` is a pure function (≤max → everyone; else current speakers → sticky
   recently-active → deterministic fill) — **unit-tested with vitest** (the web's
   first unit tests). Recomputed on every ActiveSpeakersChanged / join / leave.
   *Verified:* vitest (cap/priority/stickiness) + the SFU E2E now asserts the client
   actually *subscribes* to the peer's audio (small room → all). At-scale capping
   (>12) is unit-tested, not browser-E2E'd (would need >12 fake clients). *Remaining:*
   optional self-hosted TURN for hostile NATs, later cascaded SFUs.

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

## Accessibility — WCAG-AA contrast + axe in the QA (v0.3, 2026-06-15)

UI north star is "polished/fast/**accessible**". Integrated **axe-core** WCAG 2 A/AA
scanning into the browser QA (`qa/browser.mjs`) — a content-rich chat view (messages,
avatars, links, mentions) is scanned and any **serious/critical** violation fails the
run. The scan found one rule (color-contrast, 7 nodes); all fixed:

- **Links + @mentions** used `--accent` (#5865f2) as *text* — fails on the dark bg
  (3.2–3.6:1). Added a text-safe `--link` (#99a3ff, ≥4.5:1) for text use; `--accent`
  stays the button *fill* (white-on-accent is fine).
- **Green labels** (Join voice, voice-bar title, PTT-on, admin role badge) used
  `--online` (#23a55a, 4.33:1 borderline). Added `--online-text` (#36c46e) for text;
  `--online` stays the presence *dot* / speaking ring.
- **Avatar initials** were always white — failed on bright generated hues (green/
  yellow, 2.8:1). `avatarTextColor(name)` now computes the bg's relative luminance and
  picks black-on-bright / white-on-dark, so initials are readable on every hue while
  the backgrounds stay vibrant.

Result: auth + populated chat = **0 serious/critical axe violations**, now regression-
guarded. (Full keyboard-nav / screen-reader passes are future work.)

## Message replies (references) — chat parity (v0.3, 2026-06-15)

Chat north star is "instant + lossless realtime" + Discord parity. Replies are one of
the most-used Discord chat primitives and were the clear missing message primitive
(pin/edit/delete/react/search already shipped). A reply references an earlier message
in the **same channel** and renders a compact quoted preview above the new message.

- **Schema:** `messages.reply_to BIGINT REFERENCES messages(id)` (nullable; soft-delete
  keeps the target row so the reference stays valid and renders "[deleted]").
- **Server (`chat.Store`):** new `SaveReply(...replyTo *int64)`; `Save` delegates with
  `nil` (keeps all existing callers). **Rule B/C:** the reference is validated to exist
  AND live in the *same channel* — a bogus or cross-channel `reply_to` is **dropped**
  (message posts without a reply), never honored, so a client can't make a message it
  can't see leak through a reply preview. The reply preview (author + 80-rune snippet,
  or "[deleted]") is denormalized onto the `Message` (`replyTo/replyToAuthor/
  replyToBody`) so history + live broadcast render with no extra round-trip. `Recent`
  LEFT-JOINs the referenced row to populate the preview.
- **WS:** inbound frame gains optional `replyTo`; `readPump` passes it to `SaveReply`.
- **Web:** a `reply` action per message → a "Replying to <author> ✕" bar above the
  composer → the send frame carries `replyTo`; each message with a reference renders a
  clickable `↰ author: snippet` line above its body. Reply state clears on send and on
  channel switch.
- **Verify:** store-integration (same-channel ref populates preview · cross-channel ref
  dropped · nonexistent ref dropped · deleted target → "[deleted]") + browser-QA flow
  (click reply → bar shows → send → preview renders) + two-browser live check (Rule 14).

## Voice deafen — audio control (v0.3, 2026-06-15)

Audio north star is "thousands/HD/no-drops/free"; deafen is a baseline Discord voice
control that was missing alongside mute/PTT. Deafen silences ALL incoming audio AND
(Discord convention) forces your own mic off; un-deafening restores incoming audio and
returns the mic to its prior mute/PTT state.

- **Transport-agnostic:** `VoiceTransport.setDeafened(on)` implemented by BOTH the mesh
  `VoiceSession` and the SFU `SfuSession`, so the voice bar drives either.
- **Incoming:** each remote `<audio>` element's `.muted` is set to `deafened` (mesh:
  per-peer `audioEl`; SFU: the attached `audioEls`), and any peer/track that arrives
  while deafened is muted on creation. Playback only — the MediaStream is untouched, so
  **speaking rings still show** who's talking while deafened (matches Discord).
- **Mic:** `applyMicState` gains a `!deafened &&` guard so deafen forces the local mic
  off regardless of mute/PTT; un-deafen restores `!muted` (or the PTT hold state).
- **UI:** a `deafen`/`undeafen` toggle in the voice bar (always visible, next to leave);
  the self chip shows "· deafened" (takes precedence over "· muted"). Resets on leave.
- **Verify:** browser-QA flow (join → deafen → remote audio elements muted + self chip
  shows deafened → undeafen restores) + AI-vision on the voice bar.

## Voice — screen share (mesh) + screen-audio mixing controls (v0.4, 2026-06-15)

Discord-parity screen share on the mesh voice path: a participant shares their screen (with
optional system/tab audio) and every other participant sees it live; the pipeline is configured
for **up to 4K@60** where the source + link sustain it. Owner ask: configurable audio levels
when sharing video+audio, for BOTH the sharer and the viewers.

- **Capture (sharer):** `getDisplayMedia` with `width/height ideal 3840×2160`, `frameRate ideal 60`
  (caps, the browser negotiates down), `contentHint='detail'` (sharp text/UI). The screen video
  sender is tuned to `maxBitrate 8 Mbps` + `degradationPreference='maintain-resolution'`. The
  browser's native "Stop sharing" affordance ends our share too (`track.onended`).
- **Transport (mesh):** the screen video (+ processed audio) tracks are `addTrack`-ed to every
  peer associated with the screen MediaStream's id, so perfect-negotiation renegotiates them in;
  a late joiner gets the share on connect. A new **`voice-screen`** WS frame ({on, streamId},
  relayed by the server with Rule-B bounds) tells receivers which stream is the screen so its
  audio is told apart from the mic, and signals start/stop. Receivers classify video as screen
  always, and audio by stream id; a `track.onended` safety net also tears a stopped share down.
- **Audio mixing (the owner ask), three independent controls:**
  - **Sharer → all viewers:** outgoing screen-audio runs through a Web Audio `GainNode`
    (`setScreenSendGain`, 0..4, 1=as captured) so the sharer scales what everyone hears.
  - **Sharer → self:** a local monitor element (`setScreenMonitorVolume`, 0..1, **default 0** so
    it doesn't echo through the sharer's own speakers; raise it on headphones).
  - **Viewer → self:** each viewer has a per-share playback volume (`setPeerScreenVolume`, 0..1)
    on a dedicated `<audio>` element, independent of that peer's mic/voice volume; deafen also
    silences it.
- **UI:** a "🖥 share screen / stop sharing" toggle in the voice bar; a screen stage of 16:9
  video tiles below it — the sharer's own preview carries the out+monitor sliders, each peer's
  tile carries a 🔉 per-share volume slider.
- **SFU path:** stubbed for now — `SfuSession.startScreenShare` rejects with a clear message
  ("works on the default voice path"); LiveKit-native screen share + per-viewer mixing is a
  follow-up. Mesh is the default one-command stack (Rule A), so the feature ships there first.
- **Verify (Rule 14):** `qa/voice.mjs` (3-way mesh, headless Chromium with
  `--auto-select-desktop-capture-source`) — A shares → A sees the preview + out/monitor sliders →
  **B receives a live inbound video track** + a per-share volume slider → both audio levels
  adjust → A stops → tiles disappear on both. Full `qa/run.sh` (browser+realtime+voice) green;
  AI-vision on `voice-07/08-screenshare*.png`. NOTE: actual 4K@60 throughput depends on the
  user's machine + network (the headless fake source isn't 4K); large calls will want the SFU.

## Config — insecure-JWT-secret warning + config test coverage (v0.3, 2026-06-15)

Rule C requires `JWT_SECRET` be overridden outside local dev, but nothing enforced or even
surfaced it: both `config.Load()` and `docker-compose.yml` silently fall back to the public
default `dev-insecure-change-me`, so a self-hoster who forgets it runs wide open (anyone can
forge a JWT) with no signal. `internal/config` also had zero tests.

- **Warning, not hard-fail (Rule A):** `Config.InsecureJWTSecret` is set in `Load()` when the
  resolved secret equals `DevJWTSecret` (env unset OR explicitly set to the dev value). `main.go`
  logs a loud one-line WARNING at boot when it's true. Auth still works, so `go run` / `docker
  compose up` stay zero-config — a hard-fail would break the one-command stack (Rule A/E).
- **Tests:** new `config_test.go` covers defaults, `$PORT` precedence over `OPENCORD_ADDR`, env
  override, SFU opt-in staying empty by default, and the insecure-flag both ways (unset → true,
  overridden → false, explicit-dev-value → true). `env()` fallback tested directly.
- **Docs-sync:** README config table gains the warning note + the three `OPENCORD_SFU_*` vars
  (previously undocumented); `.env.example` gains the commented opt-in SFU block.
- **Verify (Rule 14):** booted the real binary — WITHOUT `JWT_SECRET` the WARNING prints at boot;
  WITH it set, no warning. Full `go build`/`vet`/`test` green.

## Mentions — @-autocomplete (v0.3, 2026-06-15)

Mention *rendering* (`@user` chips, `@everyone`/`@here`) already existed; this adds the
*composer* side — Discord-style autocomplete so you don't have to type a name exactly.

- **Trigger:** `activeMention(text, caret)` (pure, module-level) matches `@<partial>` when the
  `@` starts the text or follows whitespace and only username chars (`[\w-]`) run to the caret.
- **Candidates:** distinct usernames **active in the current channel** (authors in the loaded
  `messages`), minus yourself, that start with the partial (case-insensitive), capped at 6. No
  new endpoint/fetch — works in global, server, and DM channels. (Trade-off vs Discord: only
  people who've posted are suggested; a full member-scoped source is a later slice.)
- **Interaction:** a `.mention-autocomplete` listbox renders directly above the composer. While
  open it owns the keys — ↑/↓ cycle the highlight, Enter/Tab accept, Esc dismisses — so Enter
  does NOT send. Accepting replaces the `@partial` with `@username ` and restores the caret
  after it (`requestAnimationFrame` + `setSelectionRange`). Click uses `mousedown`+preventDefault
  so the textarea keeps focus. Closes on send and on channel switch.
- **Verify:** `qa/realtime.mjs` (two clients) — A types `@bo`, the dropdown suggests user B,
  Enter inserts `@bob… ` (and does NOT send), the dropdown closes, then Send delivers it and B
  sees the `@`-mention render as a highlighted chip. Web build + tsc + vitest green; AI-vision on
  `rt-08-mention-autocomplete.png` (highlighted suggestion above the composer).

## Voice — global push-to-talk hotkey (v0.3, 2026-06-15)

Push-to-talk previously only worked via the on-screen press-and-hold Talk button. Discord's
PTT is a keyboard hotkey you hold from anywhere; this adds that (web-scoped — a browser page
can only capture keys while focused, no OS-global hook).

- **Binding:** the key is stored by its physical `KeyboardEvent.code` (layout-independent) in
  `localStorage['opencord.pttKey']`, defaulting to `Backquote` (`` ` ``) — an unobtrusive key.
  Survives reloads. A "key: X / press a key…" control in the voice bar enters a capture mode;
  the next keydown becomes the binding (Escape cancels).
- **Behavior:** while `inCall && pttOn`, a window-level `keydown`/`keyup` listener on the bound
  key drives `setTransmitting(true/false)` (same path the on-screen Talk button uses, so the
  session's `setTransmitting` gates the mic via `track.enabled`). `keydown` ignores auto-repeat
  and meta/ctrl/alt combos; a window `blur` releases the mic so a held key can't leave it open
  after tab-away.
- **No mic hijack:** the listener **stands down when the event target is a text field**
  (`isEditableTarget`: INPUT/TEXTAREA/SELECT/contentEditable) — so holding it to talk never
  types into the composer, and a chat keystroke never opens the mic. The trade-off (vs Discord
  desktop's OS hook): the hotkey is inert while you're focused in an input.
- **Reset:** binding-capture mode and transmission reset on leave, channel switch, and PTT-off.
- **Verify:** `qa/voice.mjs` (3-way mesh) — default Backquote; hotkey-down transmits / up stops;
  **suppressed while focused in the composer**; rebind-capture (→ KeyV) then the rebound key
  transmits. Web build + tsc + vitest green; AI-vision on `voice-05-ptt-talking.png` (the
  `key:` control sits cleanly in the dense bar).

## File / image attachments (v0.4, 2026-06-15)

Discord's most-used messaging feature that Opencord still lacked: attaching files
and images to a message. Self-hostable with zero paid service (Rule A) — files
live on the server's **local disk**, never an external object store.

### Design

- **Storage (Rule A):** files are written under `OPENCORD_UPLOAD_DIR` (default
  `data/uploads`, gitignored). Each file's on-disk name is a server-generated
  32-hex-char **opaque key** (`crypto/rand`) — the client's filename is NEVER used
  as a path, so path-traversal is structurally impossible. The original filename is
  stored only as display metadata. (Container filesystems are ephemeral; a
  self-hoster mounts a volume at the upload dir, a cloud host uses a managed volume —
  out of scope for this slice.)
- **Upload (`POST /api/messages`, multipart/form-data):** the one path that creates
  a message carrying attachments. Reuses the existing post authorization
  (`CanPostInChannel` read-only + slowmode checks live in the store), derives the
  user from the JWT (Rule C), then creates the message + attachment rows in **one
  transaction** and broadcasts the finished message over the hub exactly like a WS
  message (`Event{Type:"message"}`). Plain text messages keep flowing over the WS;
  only attachment messages take this HTTP path (files don't fit a 4 KiB WS frame).
- **Serve (`GET /api/attachments/{id}`):** access-gated — resolves the attachment's
  message → channel and runs `CanAccessChannel(channel, viewer)` (Rule B/C); a
  non-member gets 403, never the bytes. Served with the **sniffed** content type
  (`http.DetectContentType`, never the client's claim), `X-Content-Type-Options:
  nosniff`, and `Content-Disposition: attachment` for everything except a small
  inline-image allowlist (png/jpeg/gif/webp) — so an uploaded `.html`/SVG can never
  execute script in our origin. The client fetches with its Bearer token and renders
  via an object URL, so the session JWT never leaks into an `<img src>`/URL/referer.

### Limits (Rule B — bound every inbound payload)

- ≤ 10 files per message; ≤ 8 MiB per file; ≤ 40 MiB total request
  (`http.MaxBytesReader`). Body (optional for an attachment message) ≤ 4096 bytes,
  matching the WS message cap. Zero files → 400 (this endpoint is attachments-only).

### Threat model (Rule 15 — verified by tests)

- **Path traversal** via a `../../etc/passwd` filename → impossible (opaque key; the
  client filename is metadata only). Test: a traversal filename stores under the dir.
- **Oversized** file/body → 413/400, nothing written. **Disallowed inline type** →
  still uploadable but served as a download (nosniff + attachment disposition).
- **Access bypass:** a non-member uploading to, or downloading from, a channel they
  can't access → 403. A revoked/garbage JWT → 401 (auth middleware).
- **XSS via served file:** nosniff + attachment disposition for non-images; inline
  only for the image allowlist.

### Client

A 📎 button + hidden multi-file input in the composer; selected files show as
removable chips above the input; Send uploads them (body optional). Messages render
images inline (capped size, click to open) and other files as a download chip
(icon + name + size) below the body. History (`GET /api/messages`) includes each
message's attachments so a reload still shows them.

## Uploaded avatars (v0.4, 2026-06-15)

Builds on the attachment storage from the prior slice: a user can upload a profile
picture that replaces their generated initials everywhere their avatar shows. Still
zero paid service (Rule A) — avatars live on local disk like attachments.

### Design (zero payload changes)

- **Storage:** reuses the attachment storage helpers (`storageKey`, `saveUpload`,
  sniffed content type, opaque on-disk key under `OPENCORD_UPLOAD_DIR`). A user row
  gains `avatar_key` + `avatar_type` (nullable = no avatar → initials).
- **Upload (`POST /api/avatar`, multipart):** sets the CALLER's own avatar (user
  derived from the JWT, Rule C — you can never set someone else's). Image-only
  (sniffed must be in the inline-image allowlist), ≤ 2 MiB. The previous avatar file
  is deleted on replace (no disk accretion).
- **Serve (`GET /api/users/{id}/avatar`):** any authenticated user may fetch any
  user's avatar (avatars are public within the instance, like Discord) — 404 when the
  user has none, so the client falls back to initials. Served inline with the sniffed
  image type + `nosniff`.
- **Client (no message/member payload change):** an `<Avatar userId username>`
  component blob-fetches `/api/users/{id}/avatar` with the bearer token, caches the
  result per user id (image **or** "none"), and renders the photo on 200 or the
  existing deterministic initials on 404/error. Swapping one component in at every
  avatar site (messages, member list, DM list, search/pins) means **no message or
  member JSON had to change** — the client already knows each user's id. The header
  shows your own avatar; clicking it opens the upload picker.

### Limits + threat model (Rule B/15 — verified by tests)

- ≤ 2 MiB, image-allowlist only (a non-image / oversized upload → 400/413, nothing
  stored). Opaque key → no path traversal. Upload sets only the JWT user's avatar
  (can't target another id). Unauthenticated upload/serve → 401.

### Known follow-ups (out of this slice)

- Within-session avatar change needs a reload to reflect (URL is stable, client
  caches the blob) — a cache-bust token is a later polish. Banners, per-server
  nicknames, voice-chip avatars (a different chip structure) are future slices.

## Server moderation — kick a member (v0.3 moderation, parity — iter 91)

**Goal:** an owner/admin removes a member from a server (Discord "Kick"). Builds on the
existing roles/members infra (`ServerRole`, `SetServerRole`, `ListServerMembers`).

**Authorization (`Store.RemoveServerMember(serverID, actorID, targetID)`):**
- actor must be `owner` or `admin` of the server (else `ErrForbidden`);
- can't kick yourself (`ErrForbidden`) — leaving is a separate, future action;
- nobody can kick the `owner` (`ErrForbidden`; `role <> 'owner'` in the DELETE as
  defense-in-depth);
- an `admin` can't kick another `admin` — only the owner can (`ErrForbidden`);
- target must be a member (`ErrUserNotFound`).
User is derived from the JWT (Rule C); path/body only supply the target id.

**REST:** `DELETE /api/servers/{id}/members/{userId}` → 204; 400/403/404 on the cases
above; 401 unauthenticated (parent group).

**Realtime eviction (security-critical):** WS channel access is checked only at *connect*
(`ServeWS`→`CanAccessChannel`); an open socket otherwise keeps streaming its channel.
Kick is the first access-*revoking* op, so on success the handler evicts the kicked
user's live sockets on this server's channels via `Hub.EvictUserFromChannels(userID,
channelIDs)` — mirrors the hub's existing drop path (delete + close(done) on the hub
goroutine, no DB, no locks; writePump sends a Close frame, readPump's deferred
unregister is a safe no-op). The kicked user immediately stops receiving/posting; a
reconnect 403s. Their authored messages remain (Discord keeps history).

**Client:** members panel gets a `kick` button shown only where the actor may act
(owner → any non-owner; admin → members only). Confirms, calls the endpoint, refreshes.

**Threat model (Rule B/15 — tested):** non-member/non-admin/admin-vs-admin/self/owner
kick → 403; non-member target → 404; **kicked user's open WS socket is evicted (proven:
it stops receiving a subsequently-posted message)** and a fresh WS connect 403s.

**Follow-ups (out of slice):** ban (kick + invite blocklist), timeout/mute, a live "you
were removed" toast + auto-drop of the server from the kicked user's sidebar (needs a
targeted WS user-event; today it updates on next refresh).

## Per-member online presence (member list — parity, iter 92)

**Goal:** the Discord-style member list (and members panel) shows who's **online** —
a green status dot on online members, grey + dimmed for offline. Closes the GOAL
member-list follow-up ("per-member online/idle presence still TODO").

**Source of truth:** a user is "online" if they hold ≥1 live WS socket on ANY channel
(instance-wide), which the hub already knows. New read-only hub query
`Hub.OnlineUserIDs() map[int64]bool` runs on the hub goroutine (no locks, mirrors the
evict plumbing): it collects the distinct `user.ID` of all connected clients and
returns the set via a reply channel.

**Wiring:** `ServerMember` gains `Online bool`. The members endpoint
(`GET /servers/{id}/members`) annotates each member from the hub set after
`ListServerMembers`. No new endpoint; the existing 15s member-list poll refreshes
presence (a live presence broadcast is a follow-up — see below).

**Client:** `ServerMember.online?: boolean`; the member-list sidebar + members panel
render a `.presence` dot (`online`/`offline`) on each row and dim offline rows. Role
grouping is unchanged (Discord also groups by role); a separate "Offline" group is a
later refinement.

**Tests:** ws integration — connect a socket, assert `OnlineUserIDs` contains the user;
disconnect, assert it's gone. Browser QA asserts the presence dot renders for a
connected member. AI-vision confirms the dot is visible and not clipped.

**Follow-ups:** live presence (broadcast online/offline on connect/disconnect so the
list updates instantly, not on the 15s poll); idle/DnD/invisible states; presence dots
on DM list + message avatars.

## Custom status (user profile — parity, iter 93)

**Goal:** a user sets a short **custom status** that shows by their name in the member
list + members panel (Discord's status line). Advances Users/Profiles parity.

**Storage:** `ALTER TABLE users ADD COLUMN IF NOT EXISTS status TEXT` (nullable = none).
Same idempotent pattern as the avatar columns; Migrate is hardened (lock_timeout +
retry, regression-tested) against the ACCESS-EXCLUSIVE-on-users deadlock from iter-89.

**Set (`PUT /api/me/status`, JSON `{status}`):** sets the CALLER's own status only
(user from JWT, Rule C — you can't set someone else's). Trimmed, bounded ≤128 chars
(Rule B); empty/whitespace clears it (NULL). Body capped with MaxBytesReader.

**Read:** `ServerMember` gains `Status`; `ListServerMembers` selects `u.status`, so the
existing members endpoint (+15s poll) carries it. No new read endpoint.

**Client:** `ServerMember.status?`; the header shows your own status next to your name
and clicking it opens a prompt to edit (pre-filled); each member row in the sidebar +
panel renders the status as a dimmed, truncated line under the username.

**Threat model (Rule B/15 — tested):** set only your own (no target id in the API);
oversized status → 400/truncated, nothing huge stored; unauthenticated → 401; the
status is rendered as text (React, no innerHTML) so a `<script>` status can't execute.

**Follow-ups:** status emoji, presence-state statuses (idle/DnD), "playing X" activity,
clearing-after-a-duration, live status broadcast (today it refreshes on the poll).

## Hub per-user push + live kick-notice (infra/realtime — iter 94)

**Problem:** the gateway fans out only per-channel (a client subscribes to ONE channel),
so there's no way to push an event to a *user* regardless of which channel they're on.
Concretely (logged iters 91–93): a kicked-while-connected user's socket is force-closed
(secure) but their client only sees an opaque close → it reconnect-loops on 403 instead
of cleanly dropping the server. This is the first of several features blocked by the
missing primitive (also: live presence, live member-joined, cross-channel unread).

**Primitive — `Hub.SendToUser(userID, Event)`:** delivers an event to *every* live
socket of userID (any channel). Runs on the hub goroutine (no locks); same send-or-drop
as `emitToChannel`. Mirrors the existing `EvictUserFromChannels`/`OnlineUserIDs` hub-ops.

**writePump drain-on-close:** so a *final* message reliably precedes the close frame,
`writePump` now flushes any already-queued frames before sending the WS Close on `done`
(previously a select could pick Close before a pending frame). Generally correct
("flush then close"), and what makes "notify then evict" deterministic.

**Live kick-notice:** the kick handler now, on success, `SendToUser(target,
Event{Type:"server-removed", ServerID:id})` **then** evicts. Ordering is guaranteed
(both hub-ops are serialized on the hub goroutine, SendToUser first), and drain-on-close
delivers the notice before the socket closes. Event gains `ServerID int64`.

**Client:** on `server-removed`, remove that server from `servers` + `serverChannels`
state; if the active channel belonged to it, navigate to `#general`; show a notice. The
user lands cleanly instead of looping. (Security is unchanged — eviction still closes
the socket regardless of whether the client cooperates.)

**Tests:** ws integration — kick → the kicked socket receives `server-removed` (carrying
the serverId) and THEN closes (proves ordering + drain-on-close); existing eviction +
"owner unaffected" still hold; -race clean. Browser QA — A kicks B → B drops the server
and lands on `#general` live (no manual refresh).

**Follow-ups this unlocks (separate ticks):** live presence (scoped to co-members),
live member-joined, cross-channel unread/mention badges — all need exactly this push.

## Unread indicators (parity, iter 95)

**Goal:** channels in the sidebar (global, server, DM) show an **unread** indicator
(bold + a dot) when they hold messages newer than what you've read; it clears when you
open the channel. The most-requested Discord-parity gap. MVP = unread *dots*; mention
*counts* are a follow-up.

**Read state:** new table `channel_reads(user_id, channel_id, last_read_id, updated_at,
PK(user_id,channel_id))` — CREATE TABLE (no ALTER on hot tables). `last_read_id` = the
highest message id the user has read in that channel.

**Store:**
- `MarkChannelRead(user, channel)` — upsert `last_read_id = max(message id in channel)`.
- `UnreadChannelIDs(user) []int64` — accessible channels (same predicate as
  `CanAccessChannel`, set-based: public-global OR server-member OR dm-member) that have a
  non-deleted message with `id > last_read_id` authored by someone else (your own sends
  never self-unread).

**REST:** `GET /api/unreads` → `{channelIds:[...]}` for the caller; `POST
/api/channels/{id}/read` → 204 (access-checked via `CanAccessChannel`; 403 otherwise,
400 bad id). Auth-gated.

**Client:** an `unread: Set<channelId>` synced on load + a ~10s poll. The sidebar bolds +
dots any channel in the set **except the active one** (the channel you're viewing is never
"unread"). Opening a channel removes it optimistically; leaving a channel marks it read
server-side (WS-effect cleanup) so the next poll won't re-flag messages you saw.

**Threat model (Rule B/15 — tested):** mark-read and unread are derived from the JWT user
(Rule C); `POST /read` on an inaccessible channel → 403 (no read-marker leak); unread
results are access-scoped (you never learn a channel exists via unread).

**Follow-ups:** mention **counts** (red badge — scan unread bodies for @you/@everyone/
@here); **instant** unread via the iter-94 per-user push (push channel-activity to
co-members so dots appear without waiting for the poll); per-channel/server mute.

## Mention-count badges (parity, iter 96)

**Goal:** complete the notification story from iter-95 — a channel with **unread mentions**
shows Discord's red badge with a count, alongside the plain unread dot. The signal users
act on most.

**Detection (server-side, matches the client's mention rendering):** the client highlights
`@([A-Za-z0-9_]{2,32})` case-insensitively as a mention-of-you when the token equals your
username, plus `@everyone`/`@here`. Server-side, a message mentions user U if
`body ~* '@(U|everyone|here)([^a-z0-9_]|$)'`: the trailing boundary stops `@alice` from
matching `@alice2` (the client captures the longer token too). Usernames are validated
`[a-zA-Z0-9_]{3,32}`, so the interpolated regex has no metacharacters; the pattern is also
passed as a **bind parameter** (no SQL injection) — double safety (Rule B).

**Store:** `UnreadChannelIDs` → `Unreads(user, username) []ChannelUnread{ChannelID,
Mentions}`. One query: INNER JOIN accessible channels to their unread, non-deleted,
not-mine messages (id > last_read), `GROUP BY` channel, with `Mentions = COUNT(*) FILTER
(WHERE body ~* pattern)`. A channel in the result is unread; Mentions ≥ 1 means it has
mention(s).

**REST:** `GET /api/unreads` now returns `{channels:[{id, mentions}]}` (was `{channelIds}`).

**Client:** the unread state becomes `Map<channelId, mentionCount>`. The sidebar shows, for
a non-active channel: a **red mention badge** with the count (capped "9+") when mentions > 0,
else the grey unread dot. Mark-read on leave clears both.

**Threat model (Rule B/15 — tested):** `@alice2` does not count as a mention of `alice`;
`@everyone`/`@here` do; your *own* message mentioning someone doesn't badge you; deleted
messages don't count; mentions in an inaccessible channel never surface (access scoping).

**Follow-ups:** code-span exclusion (``@you`` in inline code shouldn't count — rare),
@role mentions, a global mention inbox.

## Configurable STUN + optional TURN for mesh voice (audio/reliability, iter 99)

**Goal:** advance the audio north star toward "no-drops": let a self-hoster point mesh
WebRTC at their own STUN and (for hostile/symmetric NATs) a TURN relay, instead of the
hardcoded public Google STUN. All optional — Rule A: the one-command stack needs none.
**Not adopting a service** (no stack-guardian): this is config plumbing for a TURN the
self-hoster runs (e.g. coturn, free); the Cloud tier running managed TURN is future.

**Config (env, all optional):** `OPENCORD_STUN_URL` (default `stun:stun.l.google.com:19302`
— current behavior; override with your own, e.g. your coturn's STUN), `OPENCORD_TURN_URL`,
`OPENCORD_TURN_USERNAME`, `OPENCORD_TURN_PASSWORD`. `Config.ICEServers()` builds the
`[{urls},{urls,username,credential}]` list (STUN if set + TURN if set).

**Serve:** the existing authed `POST /api/voice/token` round-trip now also returns
`iceServers` (both mesh and SFU responses). Authed so TURN creds never reach anonymous
callers (Rule C). The SFU path ignores them (LiveKit manages its own ICE); the mesh path
uses them.

**Client:** `VoiceSession` (mesh) takes the server's `iceServers` and uses them for every
`RTCPeerConnection`, falling back to the built-in public-STUN default only if the
voice-token call failed. The hostile-NAT relay path now exists end-to-end.

**Verification (Rule 14, explicit):** config + endpoint plumbing is unit/integration
tested (env set → `iceServers` includes the TURN entry; unset → STUN only). **Real NAT
traversal requires a deployed TURN server (coturn) + a symmetric-NAT client**, which
can't be exercised in-context — stated, not claimed. The mesh two-browser voice E2E still
passes on loopback (host candidates).

**Follow-ups:** ephemeral/time-limited TURN credentials (coturn REST/HMAC) instead of
static long-term creds; a way to fully disable the default public STUN (offline LAN);
TURN for the SFU deployment.

## Search operators — from: / has: (parity, iter 100)

**Goal:** extend in-channel search with Discord-style filter operators alongside free
text: `from:<username>` (author), `has:link`, `has:image`, `has:file`. Advances the
explicit Messaging backlog item "Message search (from/in/has/before/after)".

**Parsing (`parseSearchQuery`):** tokenize the query; `from:X` / `has:link|image|file`
become filters, everything else is the free-text match (so `from:alice deploy` searches
alice's messages containing "deploy"). Unknown `has:` values fall through to free text
(graceful). Operator-only queries (e.g. `has:link`) are valid — no free-text clause.

**Query (`SearchMessages`, dynamic but fully parameterized — Rule B):** conditions are
appended per filter; every value (channel, text, from, limit) is a bind parameter (no SQL
injection); operator fragments are fixed SQL. `has:link` = `body ~* 'https?://'`;
`has:image`/`has:file` = `EXISTS` on `attachments` by `content_type LIKE 'image/%'` (or
NOT). `from:` is a case-insensitive exact username match. Free text keeps the
LIKE-wildcard-escaped ILIKE. Access-gating + the ≤200-char bound are unchanged (handler).

**Client:** no required change (operators work through the existing search box); the search
placeholder gains a discoverability hint.

**Threat model (Rule B/15 — tested):** `from:` value and free text are bind params (an
injection attempt in either is inert); LIKE wildcards in free text stay escaped; the
attachment EXISTS subqueries are channel-scoped via the outer `m.channel_id`. Access
scoping unchanged — search still only returns messages from a channel you can access.

**Follow-ups:** `in:#channel` (cross-channel search), `before:`/`after:` date filters,
`has:video`, combining with the planned global search.

## Invite expiry (security hygiene, iter 101)

**Goal:** invites currently never expire — a leaked code works forever. New invites now
auto-expire after 7 days (Discord's default), enforced at redeem. Hardening of an existing
surface; complete + testable, no UI churn.

**Schema:** `ALTER TABLE server_invites ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ`
(nullable). Existing invites have NULL → stay permanent (backward-compatible); new ones
get `now() + 7 days`.

**Create:** `CreateInvite` stamps `expires_at = now() + inviteTTL` (7d). No API/UI change.

**Redeem:** `RedeemInvite` looks up the invite incl. `expires_at`; a not-found code →
`ErrInvalidInvite` (404, unchanged), an existing-but-expired code → `ErrInviteExpired`
(404 with "this invite has expired"). The expiry is checked server-side, atomically with
the lookup — a stale client can't bypass it (Rule B/C).

**Threat model (Rule B/15 — tested):** an expired invite is rejected (store: stamp the
row's expires_at into the past → redeem → ErrInviteExpired; router: 404 + message); a
valid (within-window) invite still admits; the not-found path is unchanged.

**Follow-ups (need a small invite-options form, deferred):** customizable expiry
(`0 = never`), **max-uses** (`uses`/`max_uses` columns, atomic check-and-increment at
redeem, already designed), invite links + revoke-invite UI.

---

## Ban members (v0.4 — server moderation parity, 2026-06-16)

**Goal:** server owners/admins can **ban** a member — remove them like a kick AND
block them from rejoining (an invite redeem rejects a banned user) until **unbanned**.
Extends the existing kick path (`RemoveServerMember`) and the invite-redeem path.
Discord-parity moderation: ban / unban / a bans list. Pre-existing `kick` stays; ban is
the stronger action (kick lets them rejoin with a fresh invite, ban does not).

**Schema:** a new `server_bans` table — `(server_id, user_id, banned_by, reason,
created_at)`, PK `(server_id, user_id)`, both FKs `ON DELETE CASCADE` (a deleted user or
server drops its bans). Reason is bounded (`maxBanReasonLen`, server-trimmed).

**Store (`internal/chat`):**
- `BanServerMember(serverID, actorID, targetID, reason)` — SAME authz as the kick
  (`RemoveServerMember`): actor must be owner/admin, can't ban self, the owner, and an
  admin can't ban a fellow admin; target must currently be a member (`ErrUserNotFound`
  otherwise). Atomic (one tx): delete the membership row AND upsert the ban row. Caller
  evicts the banned user's live sockets (reuses `Hub.EvictUserFromChannels`).
- `UnbanServerMember(serverID, actorID, targetID)` — owner/admin only; `ErrUserNotFound`
  if not banned.
- `IsServerBanned(serverID, userID)` / `ListServerBans(serverID)`.
- `RedeemInvite` now rejects a banned user with a new `ErrBanned` **before** adding the
  membership — enforced server-side, a valid code can't bypass a ban (Rule B/C).

**REST (`internal/httpapi`):** under the existing `/servers/{id}` group —
`POST /servers/{id}/bans` `{userId, reason}`, `DELETE /servers/{id}/bans/{userId}`,
`GET /servers/{id}/bans` (admin-gated). Redeem maps `ErrBanned` → **403**. On a
successful ban the handler sends `server-removed` to the target + evicts its sockets
(identical to kick), so a banned user's client drops the server live.

**Web:** a **ban** button beside **kick** in the members panel (same admin-gated
visibility), with a confirm + optional reason prompt; a **Banned (N)** section in the
panel listing banned users with an **unban** button each. `api.ts`:
`banServerMember` / `unbanServerMember` / `fetchServerBans`; `ServerBan` type.

**Threat model (Rule B/15 — tested):** unauth → 401; a stranger/plain-member can't ban
(403); can't ban the owner or yourself (403); an admin can't ban a fellow admin (403);
a banned user redeeming a still-valid invite → 403 (`ErrBanned`); after unban they may
rejoin. Reason is bounded + escaped. Authz mirrors the kick matrix exactly.

---

## Member timeout (v0.4 — moderation parity, 2026-06-16)

**Goal:** completes the kick/ban/**timeout** moderation triad. An owner/admin can
**timeout** (temporarily mute) a member for a duration; while the timeout is active the
member can read but **cannot post** in the server's channels (server-enforced, like
slowmode). It auto-expires; an owner/admin can clear it early. Discord's "timeout".

**Schema:** `ALTER TABLE server_members ADD COLUMN IF NOT EXISTS timeout_until TIMESTAMPTZ`
(NULL/past = not timed out; future = muted until then).

**Store (`internal/chat`):**
- `TimeoutServerMember(serverID, actorID, targetID, until)` — SAME authz as ban/kick
  (owner/admin; can't timeout yourself, the owner, and an admin can't timeout a fellow
  admin; target must be a member). `until` is clamped to `(now, now+maxTimeoutDuration]`
  (28 days, Discord's max). Returns the effective `until`.
- `ClearTimeout(serverID, actorID, targetID)` — owner/admin sets `timeout_until = NULL`.
- `timeoutBlocked(channelID, userID)` — resolves the channel's server and reports whether
  that member's `timeout_until` is in the future. Enforced server-side in `SaveReply` +
  `SaveWithAttachments` (after the read-only + slowmode guards) → `ErrTimedOut`; a client
  can't bypass it. `ListServerMembers` now returns `timeout_until` so the UI can badge a
  muted member and disable the composer for a timed-out viewer.

**REST (`internal/httpapi`):** `POST /servers/{id}/timeouts` `{userId, durationSeconds}`
and `DELETE /servers/{id}/timeouts/{userId}` (both admin-gated, same matrix as ban). The
WS send path (`client.go`) and the HTTP attachment send path map `ErrTimedOut` → a
"you're timed out" error/`429` instead of silently dropping.

**Web:** a **timeout** button in the members panel (prompts for minutes) beside kick/ban;
a ⏳ **muted** badge + a **clear** button on a timed-out member's row; the composer is
disabled with a "you're timed out…" notice when the viewer is currently timed out in the
active server channel (derived from the polled member list — the server still enforces it
regardless of client state). `timeoutServerMember` / `clearMemberTimeout` in `api.ts`;
`timeoutUntil?` on `ServerMember`.

**Threat model (Rule B/15 — tested):** unauth → 401; stranger/plain-member can't timeout
(403); can't timeout the owner or yourself (403); an admin can't timeout a fellow admin
(403); a timed-out member's post is rejected server-side (`ErrTimedOut`) over BOTH the WS
and the HTTP attachment path; after the timeout is cleared (or expires) they can post
again. Duration is clamped server-side (a client can't request a 100-year mute or a
negative one). Reactions/edits while timed out are a documented follow-up.

---

## Channel categories (v0.4 — server structure parity, 2026-06-16)

**Goal:** Discord-style **categories** — a server groups its channels under named,
collapsible category headers in the sidebar. A channel optionally belongs to a category;
`category_id NULL` = uncategorized (renders at the top, today's behaviour). MVP scope:
create category + create a channel under a category + collapsible sidebar groups. Out of
scope (follow-ups): drag-reorder, moving an existing channel between categories,
per-category permission overrides.

**Schema:** new `channel_categories (id, server_id→servers ON DELETE CASCADE, name,
created_at)` + `ALTER TABLE channels ADD COLUMN category_id BIGINT REFERENCES
channel_categories(id) ON DELETE SET NULL` — deleting a category leaves its channels
uncategorized (never deletes channels). Indexed by server_id.

**Store (`internal/chat`):**
- `ChannelCategory` struct + `CreateChannelCategory(serverID, name)` /
  `ListChannelCategories(serverID)`. Name trimmed + bounded (1..32 runes; unlike channel
  names, categories allow spaces/caps — they're display labels).
- `CreateServerChannel` now delegates to `CreateServerChannelInCategory(serverID, name,
  categoryID *int64)` (old callers unchanged, pass nil). When categoryID is non-nil it is
  validated to belong to serverID (Rule B — a client can't attach a channel to another
  server's category); a bad/cross-server id is rejected (`ErrCategoryNotFound`).
- `Channel.CategoryID *int64`; `ListServerChannels` now selects `category_id`.

**REST (`internal/httpapi`):** `POST /servers/{id}/categories` `{name}` (admin-gated) +
`GET /servers/{id}/categories` (member-gated). `POST /servers/{id}/channels` accepts an
optional `categoryId`.

**Web:** `serverCategories: Record<serverId, ChannelCategory[]>` loaded alongside
`serverChannels`. The sidebar renders uncategorized channels first, then each category as
a collapsible header (▾/▸, per-category client collapse state) with its channels nested.
A `+ category` action (admin) creates a category; each category header (admin) has a `+`
to create a channel inside it. `fetchChannelCategories` / `createChannelCategory` in
`api.ts`; `createServerChannel` takes an optional categoryId; `ChannelCategory` type +
`Channel.categoryId`.

**Threat model (Rule B/15 — tested):** category create/list authz mirrors channel
create/list (admin to create, member to list; stranger → 403). A channel can't be
attached to a category from another server (`ErrCategoryNotFound`). Category name is
bounded; empty/whitespace rejected.

---

## Invite management — list active invites + revoke (v0.4)

**Why:** invites were write-only — a member minted a code via a prompt and that was it.
A leaked/oversharing code stayed valid for its full 7-day TTL with no way to kill it, and
nobody could see how many codes a server had outstanding. This adds the "revoke UI"
backlog item: admins can list a server's active invites and revoke a leaked one
immediately. (max-uses is a clean follow-up; this tick is list + revoke.)

**Store (`internal/chat`):**
- `Invite{Code, CreatedBy, CreatorName, CreatedAt, ExpiresAt *time.Time}`.
- `ListInvites(serverID)` — active (unexpired) invites joined with the creator's
  username, newest first. Expired invites are filtered out (they're dead anyway and
  pruning them is a separate concern).
- `RevokeInvite(serverID, code)` — `DELETE ... WHERE code=$1 AND server_id=$2` (Rule B
  cross-server guard: a code from another server can't be revoked via this server's path);
  `ErrInvalidInvite` when no row matched.

**REST (`internal/httpapi`):** `GET /servers/{id}/invites` (admin-gated, like bans — a
non-admin gets 403, not the list) and `DELETE /servers/{id}/invites/{code}` (admin-gated).
`POST /servers/{id}/invites` (mint) stays members-only, unchanged.

**Web:** the **members panel** (already the admin management surface, next to Bans) gains
an **Invites** section for admins: each active invite shows its code, expiry ("expires in
Nd" / "never"), creator, a **copy** button, and a **revoke** button; a **+ New invite**
button mints one and refreshes the list. Loaded via `loadInvites` (mirrors `loadBans`,
swallows 403 for non-admins). A plain member's `invite` sidebar button keeps the existing
quick-mint prompt; an admin's opens the members panel. `Invite` type + `fetchServerInvites`
/ `revokeServerInvite` in `api.ts`.

**Threat model (Rule B/15 — tested):** list + revoke require admin (stranger/member →
403). Revoke is scoped by `server_id`, so an admin of server A can't revoke server B's
code via A's path (cross-server guard). Revoking an unknown/already-gone code → 404. A
revoked code no longer redeems (404).

---

## Invite max-uses (v0.4)

**Why:** invites had only a time cap (7-day expiry). A max-uses cap lets an admin mint a
code that admits a fixed number of people (e.g. a one-shot link), the other half of
Discord's invite controls.

**Store (`internal/chat`):** `server_invites` gains `max_uses INT` (NULL = unlimited) +
`uses INT NOT NULL DEFAULT 0`. `CreateInviteWithMaxUses(serverID, userID, maxUses *int)`
(CreateInvite stays the unlimited shorthand → no churn to its 8 existing callers).
`RedeemInvite` now: already-a-member → no-op, **no use consumed** (idempotent rejoin);
else a **transaction** does a guarded `UPDATE ... SET uses = uses + 1 WHERE max_uses IS
NULL OR uses < max_uses` then the member INSERT — so a failed join rolls back the use and
concurrent redeems of the last slot can't overshoot (one wins, the rest get
`ErrInviteExhausted`). `Invite` exposes `MaxUses`/`Uses`; `ListInvites` filters out
exhausted codes (active = redeemable, same as expired).

**REST:** `POST /servers/{id}/invites` accepts an optional `{maxUses}` (1–1000; empty body
still = unlimited, legacy-safe). Redeeming an exhausted code → 404
(`this invite has reached its maximum uses`).

**Web:** the panel's **+ New invite** prompts for a max-uses cap (blank = unlimited); each
invite row shows `N/M uses` (capped) or a running `N uses`.

**Threat model (Rule B/15 — tested):** the cap is enforced + counted server-side (a stale
client can't bypass it); maxUses is bounded 1–1000; the atomic guarded UPDATE makes the
limit race-safe; a member re-redeeming never burns a use.

## v0.3 — Server settings: rename + delete a server (owner/admin)

**Why:** a server, once created, could never be renamed or removed — the one missing
half of basic server management (Discord's Server Settings → Overview + Delete Server).
Closes the unchecked "Create/join servers + **server settings**" parity item.

**Store (`internal/chat`):**
- `RenameServer(serverID, actorID, name) (Server, error)` — **admin+** (owner or admin,
  Discord's "Manage Server"). `UPDATE servers SET name=$1 WHERE id=$2 RETURNING …`; not an
  admin → `ErrForbidden`; unknown server → `ErrServerNotFound`. Returns the updated row
  with the actor's role.
- `DeleteServer(serverID, actorID) error` — **owner-only** (destructive, Discord parity).
  Non-owner (incl. admin + non-member) → `ErrForbidden`; unknown → `ErrServerNotFound`.
  One **transaction**: `DELETE FROM messages WHERE channel_id IN (server's channels)`
  first — `messages.channel_id` has no `ON DELETE CASCADE`, so the channel cascade would
  otherwise hit an FK violation — then `DELETE FROM servers WHERE id=$1`, whose
  `ON DELETE CASCADE` FKs remove the server's `server_members`, `channels` (→ their
  `channel_reads`/pins via cascade), `server_invites`, `channel_categories`, and
  `server_bans`. The global `#general` (`server_id IS NULL`) is untouched.

**REST:**
- `PATCH /servers/{id}` `{name}` (1–64 chars, trimmed — same validation as create) →
  200 with the updated server; 403 non-admin; 404 unknown.
- `DELETE /servers/{id}` → 204; 403 non-owner; 404 unknown.

**WS (live):**
- Rename broadcasts `server-renamed` (`serverId` + new `name`) to **every member** via
  `Hub.SendToUser`, so each member's sidebar relabels live without a refetch. `Event`
  gains a `Name` field for this.
- Delete reuses the kick/ban path: each member gets a `server-removed` push then
  `Hub.EvictUserFromChannels` on the server's (now-deleted) channel ids, so every
  member's client drops the server and falls back to `#general` live.

**Web:** an owner/admin **⚙ Server Settings** affordance in the members panel: an inline
**rename** field (admin+) and, for the **owner only**, a **Delete server** button behind a
typed/confirmed prompt. Handles `server-renamed` (relabel in place) and the existing
`server-removed` (drop the server) live.

**Threat model (Rule B/15 — tested):** rename is admin-gated and the name is bounded
(1–64, trimmed) server-side; delete is strictly owner-only — an admin, a plain member, and
a non-member are all rejected (403), proven by an authz-matrix integration test; the delete
is transactional (messages-then-cascade) so a server is never left half-deleted, and the
cascade is verified to remove members/channels/messages while leaving the global `#general`
intact.

## Message search — `before:` / `after:` date operators (v0.3 polish, 2026-06-16)

Extends the existing Discord-style search-operator parser (`from:` / `has:link|image|file`)
with date-range filters, closing the GOAL.md "Message search … `before:`/`after:` TODO".

**Operators:** `before:<YYYY-MM-DD>` and `after:<YYYY-MM-DD>`, both **day-exclusive**
(matching Discord): `before:2024-01-15` ⇒ `created_at < 2024-01-15 00:00 UTC` (the named day
and everything after are excluded); `after:2024-01-15` ⇒ `created_at >= 2024-01-16 00:00 UTC`
(the named day and everything before are excluded). They combine into a window
(`after:A before:B`) and compose with `from:`/`has:`/free text.

**Parsing (`parseSearchQuery` + new `parseSearchDate`):** the date is parsed strictly via
`time.ParseInLocation("2006-01-02", …, UTC)`. A malformed or hostile value (bad format,
impossible date, an injection string) fails the layout and the token **falls through to free
text** — a bad `before:`/`after:` never errors the search and never reaches SQL as a date.

**SQL (`SearchMessages`):** each bound adds one `m.created_at < $N` / `>= $N` condition with
the parsed `time.Time` passed as a **bind parameter** (Rule B — no SQL injection; operator
fragments are fixed SQL). The pre-existing `messages_created_at_idx` makes the range
index-supported.

**Web:** search-box placeholder + tooltip updated to advertise `before:`/`after:`.

**Threat model (Rule B/15 — tested):** `TestSearchDateOperatorsIntegration` proves the
day-exclusive bounds, windowing, `before:`+free-text composition, that a malformed date is
inert free text, that an injection (`after:'; DROP TABLE messages;--`) matches nothing AND
leaves the table intact (all 3 messages still searchable afterward). Browser QA (`07c3`)
drives `before:2099-01-01` (finds the recent message) / `after:2099-01-01` (finds nothing)
through the real search box.

## Status emoji (v0.4 profiles, 2026-06-16)

Extends the existing custom-status feature (`PUT /me/status`) with an optional **status
emoji** shown before the status line — closing the GOAL.md "Custom status DONE … status
emoji TODO" item.

**Storage:** `users.status_emoji TEXT` (idempotent `ALTER TABLE … ADD COLUMN IF NOT EXISTS`;
NULL = none). `ServerMember` carries `statusEmoji`; `ListServerMembers` selects
`COALESCE(u.status_emoji,'')`.

**API:** `PUT /me/status` now accepts `{status, statusEmoji}`. `SetUserStatus(ctx, userID,
status, emoji)` trims + caps each (status ≤128 runes, emoji ≤16 runes — generous enough for a
ZWJ sequence, tiny enough to bound abuse, Rule B); an empty/whitespace value clears that
field (NULL). Identity is the JWT-derived caller only (Rule C) — no target-user param.

**Web:** the header "set status" affordance prompts for an emoji (optional) then the status
text; the emoji renders (React element — escaped, no innerHTML) before the status line in the
header button and in both member-list panels via a `.status-emoji` span. An emoji can be set
alone (no text).

**Threat model (Rule B/C — tested):** `TestUserStatusIntegration` proves set/clear, the
128-rune status cap AND the 16-rune emoji cap on over-long input, and emoji-only set. Browser
QA `07d2` sets `🚀` + a status line and asserts the emoji renders before the text in the
member list AND the header. The value is bounded server-side and React-escaped on render, so a
hostile "emoji" string can neither overflow storage nor inject markup.

## Presence states — idle / dnd / invisible (v0.4 profiles, 2026-06-16)

Extends presence from online/offline to the four Discord states — **online · idle · dnd ·
invisible** — as a manual user-chosen availability (auto-idle-on-inactivity deferred).

**Storage:** `users.presence_state TEXT` (idempotent ADD COLUMN; NULL reads as 'online').

**Effective-presence rule (the core logic, pure + unit-tested):**
`EffectivePresence(connected, raw)` — what OTHER viewers see: a disconnected member, or one
who chose **invisible** (even while connected), reads as **offline**; otherwise their chosen
online/idle/dnd shows through. `NormalizePresence` coerces any empty/unknown/hostile value to
"online" (Rule B). The member-list HTTP annotation applies this per row, EXCEPT the viewer's
own row, which always shows their true chosen state (so the picker reflects invisible/idle/dnd).

**API:** `PUT /me/presence {presence}` → `SetUserPresence` (JWT caller only, Rule C; normalized,
unknown → online). `ServerMember.presence` is the effective state the client renders; the raw
`PresenceState` is internal (`json:"-"`). `ServerMember.online` is now also correct for
invisible (false to others).

**Web:** a header presence picker (native `<select>` + a colored pip: green/amber/red/grey);
the member-list presence dot is colored by `mb.presence` (online green · idle amber · dnd red ·
invisible/offline grey) in BOTH the right-sidebar member list and the members-management panel.
Optimistic update + member-list refresh on change.

**Threat model / tests (Rule B/C):** `TestNormalizePresence` + `TestEffectivePresence` (pure,
no DB) cover the mapping incl. invisible-while-connected → offline and hostile input → online.
`TestPresenceEndpointIntegration` drives the real `PUT /me/presence` + member annotation: the
caller sees their own dnd/idle/invisible, a disconnected member reads offline, a bogus value
normalizes to online, and an unauthenticated set is 401. Browser QA `07d2b` sets DnD via the
picker and asserts the self dot turns red in the member list AND the header pip recolors.

## User Settings + UI polish (v0.5 — UX Discord parity, owner-set 2026-06-17)

**Why:** owner directive — bring the UI to Discord's polish bar. Today the login page is a bare
card (`Auth.tsx`), and all account/voice controls are scattered in the 2604-LOC `Chat.tsx` header +
voice bar; there's no settings surface. Tracked as the GOAL.md "TOP PRIORITY — UI/UX Discord parity"
list, advanced one slice per tick alongside the loop's normal health/QA work.

**Scope (one shippable slice per tick; each browser-QA + AI-vision verified, Rule 14):**

1. **Login / register redesign** (`Auth.tsx`, `styles.css`) — Discord-quality: branded layout,
   visual hierarchy, inline validation + error/loading states, password show/hide, mobile-responsive.
   No backend change (same `login`/`register` calls). Browser QA: register → land in app; bad creds →
   inline error; AI-vision the polished card.

2. **User Settings modal — "My Account" tab** (new `Settings.tsx`, `styles.css`, wire from
   `Chat.tsx`) — a Discord-style overlay with a left tab-nav. Move avatar upload/preview, username,
   custom status + emoji, and the presence picker out of the header and into it; a ⚙ entry point by
   the user footer opens it, Esc/overlay-click closes. No new endpoints (reuses `PUT /me/status`,
   `PUT /me/presence`, `POST /avatar`). Browser QA: open settings → change status → see it reflect.

3. **Voice & Video settings tab** — input/output device pickers (lift the existing in-call ones into
   settings), **input + output volume sliders**, a **mic-test input-sensitivity meter** (Web Audio
   level from the selected input), **noise-suppression / echo-cancellation / auto-gain toggles**
   (applied to `getUserMedia` constraints), **camera device + live `<video>` preview**. Persist
   per-user in `localStorage` (device ids + toggles are client/device-specific; no backend). Wire the
   chosen constraints into `voice.ts`/`sfu.ts` capture. Browser QA: open tab → pick device →
   mic-test meter moves → toggles persist across reload; AI-vision the panel.

4. **Appearance / general polish pass** — spacing, hover/active states, visible focus rings (keep the
   axe-core a11y scan green), consistent component styling across sidebar/header/chat/member list.

5. **(stretch) Video calling** — camera on/off + per-participant video tiles in a voice channel
   (mesh first, SFU after), reusing the screen-share tile plumbing.

**Non-negotiables:** every slice stays self-hostable (Rule A — settings persist locally, no new paid
service); inputs validated (Rule B); browser-QA + AI-vision verify the rendered result before "done".

### Slice 2 — executable build plan (User Settings modal · My Account tab) — SHIPPED iter 129

Built as specced: `web/src/components/Settings.tsx` (overlay + modal + tab rail), header `.meta`
collapsed to a `self-chip` ⚙ entry point, `editMyStatus` prompt-flow → plain `saveStatus(status,
emoji)`, CSS for `.settings-*` + `.self-chip*`. QA selectors migrated (status/presence/avatar moved
header→modal; new `07d0` ⚙-open + Esc-close). browser QA green; AI-vision verified. Next: slice 3
(Voice & Video settings tab).

### Slice 3a — executable build plan (Voice & Video tab · audio: devices + DSP + mic test)

Scope this tick to the AUDIO half so it's shippable + cleanly verifiable without disturbing the
heavily-QA'd voice-bar/session flow; **defer** input/output **volume sliders** + **camera device +
live preview** to slice 3b/3c. No backend (device ids + DSP toggles are device-specific → localStorage,
Rule A).

**New `web/src/voiceSettings.ts`** — the single localStorage source of truth for persistent voice
prefs: `getInputDeviceId()/setInputDeviceId(id)`, `getOutputDeviceId()/setOutputDeviceId(id)`, and 3
DSP toggles `noiseSuppression`/`echoCancellation`/`autoGainControl` (`getAudioProcessing()` →
`{ns,ec,agc}`, all default **true**; `setAudioProcessing(partial)`). Keys namespaced `opencord.voice.*`.

**`voice.ts`** — `audioConstraints(deviceId)` reads the 3 DSP flags from `voiceSettings.getAudioProcessing()`
instead of the hardcoded `true`s (default true ⇒ no behavior change until the user toggles). The
screen-share constraints (line ~106, all `false`) stay hardcoded — those are deliberate, not user prefs.

**`Chat.tsx`** — seed `inputDevice`/`outputDevice` `useState` from `voiceSettings.get*DeviceId()`;
`changeInputDevice`/`changeOutputDevice` also persist via `voiceSettings.set*DeviceId`. Pass to
`<Settings>`: `audioInputs`, `audioOutputs`, `inputDevice`, `outputDevice`, `onChangeInputDevice`,
`onChangeOutputDevice`, `onRefreshDevices` (= existing `refreshDevices`). Single source of truth in
Chat → the in-call voice-bar pickers and the settings pickers stay in sync via shared state + localStorage.

**`Settings.tsx`** — enable the **Voice & Video** tab (drop the disabled "SOON" placeholder). Tab body:
input-device `<select>` (Auto + `audioInputs`), output-device `<select>` (Auto + `audioOutputs`), 3 DSP
toggle checkboxes (read/write `voiceSettings`), and a **Mic Test** button → live input-sensitivity
**meter**: self-contained `getUserMedia({audio:{deviceId}})` → `AudioContext` → `AnalyserNode` → rAF loop
sets a 0–1 level → meter-fill width. Start/Stop; **clean up** (stop tracks, close ctx, cancel rAF) on
Stop + tab-switch + unmount. Call `onRefreshDevices()` on entering the tab (the mic test grants the
permission that populates device labels).

**`styles.css`** — `.settings-voice-row`, `.settings-toggle` (checkbox row), `.mic-test`, `.mic-meter`
+ `.mic-meter-fill` (animated width), reuse `--accent`/`--online`. Focus rings on every control.

**QA (`qa/browser.mjs`) — new `07k` Voice & Video flow** (Chromium already launches with
`--use-fake-device-for-media-stream` + `--use-fake-ui-for-media-stream`, so getUserMedia/enumerate
resolve + the fake mic emits a tone): open settings → click the **Voice & Video** tab → assert the 3
DSP toggles render; flip **noise suppression** off → reload → reopen → assert it's still off
(localStorage persistence); click **Mic Test** → assert `.mic-meter-fill` width grows > 0 within ~3s
(fake tone) → Stop. AI-vision the panel. Verify: `npm run build`, full browser QA green, ship +
`railway up` + rollout-verify.

**Shipped iter 130** as specced, with two QA refinements landed iter 131: (a) the persistence check
uses a direct `localStorage.getItem` assertion + modal remount instead of a `page.reload()` (a reload
mid-suite resets the app to `#general` and breaks the downstream server-channel steps); (b) the
mic-test meter assertion polls for the PEAK level over a ~4s window (the fake mic *pulses*, so a single
read after a fixed wait flakes to 0).

### Slice 3b — Voice & Video tab · camera device + live preview — SHIPPED iter 131

`voiceSettings.ts` gains `getCameraDeviceId()/setCameraDeviceId()`. `Settings.tsx` Voice & Video tab
adds a **Camera** section: a videoinput `<select>` (enumerated locally in Settings — video isn't in the
voice pipeline yet) + a mirrored 16:9 `<video>` preview driven by `getUserMedia({video:{deviceId}})`
("Test Camera"/"Stop Camera"); `startCameraTest(deviceId=camera)` takes an explicit id so a live swap
doesn't read a stale closure; teardown (stop tracks, detach `srcObject`) on Stop + tab-switch + unmount.
`styles.css` `.cam-preview`/`.cam-preview-video` (`object-fit:cover`, `transform:scaleX(-1)` mirror).
QA `07d3c`: picker renders (`getByLabel('camera',{exact:true})` — the video's `aria-label="camera
preview"` would otherwise collide on substring), `waitForFunction(videoWidth>0)` proves frames decode,
`.cam-preview.live` toggles, Stop tears it down. AI-vision verified the fake-device preview. **Slice 3c
TODO:** input/output volume sliders.

### Slice 3c — Voice & Video tab · output (master) volume slider — SHIPPED iter 132

`voiceSettings.ts` gains `getOutputVolume()/setOutputVolume()` (0..1, default 1). A pure
`effectiveVolume(peerVol, master)` (exported from `voice.ts`, clamped to [0,1], vitest-tested) is the
master-scaling math. Both transports gain `setMasterVolume(v)` (added to the `VoiceTransport`
interface): `VoiceSession` + `SfuSession` re-apply `el.volume = effectiveVolume(peerVol, master)` to
every attached peer `<audio>` immediately, and apply it at peer-attach + in `setPeerVolume`; the field
seeds from `getOutputVolume()`. `Settings.tsx` adds an Output Volume `<input type=range>` →
`setOutputVolume` + `onSetMasterVolume` (Chat routes that to `voiceRef.current.setMasterVolume`).
Playback-only ⇒ never touches the race-sensitive sender/negotiation code. QA: browser `07d3` (slider
renders + persists `outputVolume==='0.5'`); voice.mjs two-client (master 50% × per-user 40% → peer
audio ~0.2, proving live composition); vitest `voice.test.ts` (4 cases incl. clamp + master-0 mute).
**Slice 3d TODO:** input volume / mic-gain — needs a `GainNode` spliced into capture (mute/PTT/hot-swap
interaction), so it's its own careful tick.

Concrete plan captured at commit 1b97700 so a fresh-context tick builds it without re-discovery.
Reuses existing handlers — NO new endpoints (`PUT /me/status`, `PUT /me/presence`, `POST /avatar`).

**Current header `.meta` block (`Chat.tsx` ~1675–1726)** holds, in order: online-count dot · hidden
avatar `<input ref={avatarInputRef}>` + `self-avatar-btn` (avatar + username, `onClick` → file picker
via `onAvatarPicked`) · `presence-pill` (`presence-pip` + `presence-select` `<select>` → `changePresence`,
aria-label "set your presence") · `status-edit` link (→ `editMyStatus()`, which uses TWO `window.prompt`s
for emoji+status) · `log out`. Handlers: `onAvatarPicked` (~529), `editMyStatus` (~1130), `changePresence`
(~1154). State: `myPresence`/`setMyPresence_`, `myStatus`/`setMyStatus_`, `myStatusEmoji`/`setMyStatusEmoji_`,
`avatarVersion`.

**Build:**
1. **`web/src/components/Settings.tsx`** (new) — fixed overlay + modal, left `settings-nav` tab list
   (My Account active; a disabled/"coming soon" Voice & Video tab placeholder for slice 3), `settings-body`.
   Props: `{ token, user, myPresence, myStatus, myStatusEmoji, avatarVersion, onAvatarPicked,
   onSaveStatus(status,emoji), onChangePresence(next), onClose }`. **My Account tab:** avatar preview
   (`<Avatar bust={avatarVersion}>`) + "Change avatar" button (triggers a file input → `onAvatarPicked`);
   username (read-only — no rename endpoint yet); **custom status** = a text `<input>` (value myStatus) +
   an emoji `<input>` (value myStatusEmoji) + a **Save** button → `onSaveStatus` (replaces the
   `window.prompt` flow); **presence** = the 4-option `<select>` → `onChangePresence`. Close on Esc
   (keydown listener) + overlay click; stop propagation on the modal.
2. **`Chat.tsx`** — add `settingsOpen` state. In `.meta`, REPLACE the avatar-btn + presence-pill +
   status-edit cluster with: a compact user chip (avatar + username) + a **⚙ "User settings"** button
   (`aria-label="user settings"`, opens the modal) + keep online-count + `log out`. Refactor
   `editMyStatus` into a plain `saveStatus(status, emoji)` (no prompts) passed to Settings; keep
   `changePresence`/`onAvatarPicked` as-is. Render `<Settings/>` when `settingsOpen`.
3. **`styles.css`** — `.settings-overlay` (fixed, dim backdrop, grid center), `.settings-modal`
   (elevated card, max-width ~740px, flex row), `.settings-nav`/`.settings-tab` (left rail),
   `.settings-body`, `.settings-row` (label + control), reuse `--bg-elevated`/`--accent`. Focus rings.

**QA migration (`qa/browser.mjs`) — selectors MOVE from header into settings; update in lockstep:**
- New: click ⚙ (`getByRole('button',{name:'user settings'})`) → modal visible; Esc closes it.
- `07d2` status: open settings → fill the status text input → Save → assert it shows in the member list
  (drop the `window.prompt` dialog handler for status).
- `07d2b` presence: open settings → set the presence `<select>` to dnd → assert the member dot recolors.
- `07i` avatar: open settings → change avatar there.
- `08b` mobile header: header now has fewer controls; keep the ≤640px no-overflow assertion.
Verify: `npm run build`, full browser QA green, **AI-vision the modal** (My Account tab — avatar, status
fields, presence, tab rail; clean/Discord-like), then ship + `railway up` + rollout-verify the new bundle.

## Video calling (camera in a voice call) — slice 1 (mesh, camera ⊻ screen)

**Why:** the #1 remaining Discord-parity feature — voice + screen-share exist, but you can't turn
your camera on. **Design reuses the proven mesh screen-share pipeline** (capture → `addScreenTracksToPeer`
→ `voice-screen` frame → `ontrack` → `screenStream` tile) so the receive path is UNCHANGED (lowest risk
to the heavily-QA'd screen flow). Slice 1: camera is **mutually exclusive with screen-share** (both use
the single `screenStream` video slot); simultaneous screen+camera is slice 2 (a parallel video stream).

**The only new concept is a `kind: 'screen' | 'camera'` tag** so both ends label/mirror correctly — the
video pixels render in the existing tile regardless.

- **Server (`internal/ws`):** `Event` gains `Kind string json:"kind,omitempty"`; the inbound parse
  struct gains `Kind`; the `voice-screen` relay validates `kind ∈ {"", "screen", "camera"}` (Rule B —
  unknown → drop the frame) and carries it only when `On`. Bounded like `StreamID`.
- **`voice.ts`:** new `startCamera(deviceId?)` mirrors `startScreenShare` but `getUserMedia({video})`
  (no audio, `contentHint:'motion'`), sets the same `screenStream`/`screenVideoTrack`, publishes via the
  same `addScreenTracksToPeer`, sends `{voice-screen, on, streamId, kind:'camera'}`. A `localVideoKind`
  field; `startScreenShare` sends `kind:'screen'`. `onLocalScreen(stream, kind)` gains the kind. The
  join re-announce (handle/voice-join) carries the current kind. `VoicePeer` gains
  `videoKind?: 'screen'|'camera'`; `onScreenAnnounce` records `ev.kind`; `emitRoster` includes it.
  `ontrack`/`addScreenTracksToPeer`/`stopScreenShare` UNCHANGED (camera reuses them). `VoiceTransport`
  gains `startCamera`. `getCameraDeviceId()` from voiceSettings supplies the device.
- **`sfu.ts`:** `startCamera` stub rejects ("not on SFU yet"), like `startScreenShare`.
- **`Chat.tsx`:** a 📹 Camera toggle in the voice bar (`toggleCamera` — starts camera, stopping screen
  first if active; mutual exclusion). Track `localVideoKind` from the `onLocalScreen` callback; the local
  self-tile + the remote tile **mirror** (`scaleX(-1)`) + label "camera" when `videoKind==='camera'`.
- **QA (`qa/voice.mjs`):** mirror the screen two-client test — A clicks 📹 Camera → A sees a mirrored
  `[data-screen-self]` camera tile → B receives a live inbound video track in `[data-screen-peer]` →
  A stops → both tiles clear. The existing screen-share test must stay green (proves no regression).
- **Verify:** `go test ./...` (the voice-screen relay integration test still passes + a `kind` assertion),
  `npm run build`, full browser+voice QA green, AI-vision the camera tile, ship + `railway up` + rollout-verify.
- **Slice 2 (next):** a parallel `cameraStream`/`voice-camera` path so screen + camera coexist; SFU video.

## Per-channel notification mute (v0.5 — notifications)

**Why:** Discord lets you mute a noisy channel so it stops nagging you. Reuses the existing
unread pipeline — a muted channel simply drops out of the unread results, so the sidebar dots,
mention badges, AND the browser-tab badge all auto-respect it (one server-side change, no client
re-modelling). Per-user, derived from the JWT (Rule B/C); you can only mute a channel you can access.

- **Schema (`internal/db/schema.sql`):** `channel_mutes(user_id, channel_id, PRIMARY KEY(user_id,
  channel_id))` with `ON DELETE CASCADE` FKs — mirrors `channel_reads`. Idempotent (self-applies).
- **Store (`internal/chat/chat.go`):** `MuteChannel`/`UnmuteChannel` (INSERT ON CONFLICT DO NOTHING /
  DELETE), `MutedChannelIDs(userID)`; `Unreads` gains `AND NOT EXISTS (SELECT 1 FROM channel_mutes cm
  WHERE cm.channel_id=c.id AND cm.user_id=$1)` so muted channels never surface as unread.
- **Router:** `POST`/`DELETE /channels/{id}/mute` (access-gated via `CanAccessChannel` — a non-member
  gets 403, can't mute someone else's private channel) and `GET /me/muted-channels` → `[]int64`.
- **Web:** `api.ts` `muteChannel`/`unmuteChannel`/`fetchMutedChannels`; `Chat.tsx` tracks a `muted`
  Set, fetches it on load, a header **mute/unmute** toggle for the current channel, and a 🔕 dim on
  muted sidebar channels. The unread/tab badges need NO change — the server already excludes muted.
- **QA:** store/integration test (mute excludes from Unreads; access-gated mute is 403 for a non-member;
  unmute restores) + browser `mute → header toggle flips` + realtime two-client (B mutes the server
  channel → A posts → B gets NO unread dot/tab badge; unmute → it returns).
- **Verify:** go test + full browser/realtime/voice QA green, ship + railway up + rollout-verify.

## User profiles: about-me + pronouns + a profile card (v0.5 — profiles)

**Why:** the Profiles component has avatars/status/presence but no bio. Discord lets you set an
"About Me" + pronouns and view anyone's profile card. Slice 1: set them in settings, view them by
clicking a member (a centered card overlay — reuses the Settings overlay pattern, no fragile popover
positioning). The card reads the already-loaded member-list data (no extra fetch); message-author
trigger is a follow-up.

- **Schema:** `ALTER TABLE users ADD COLUMN IF NOT EXISTS about TEXT; ... pronouns TEXT;` (idempotent).
- **Store:** `ServerMember` gains `About`/`Pronouns` (member query selects them); `SetUserProfile(userID,
  about, pronouns)` — trimmed + capped (about ≤ 190 runes like Discord, pronouns ≤ 40), empty→NULL,
  JWT-derived caller only (Rule B/C).
- **Router:** `PUT /me/profile` `{about, pronouns}`.
- **Web:** `api.ts` `setMyProfile` + `ServerMember` type gains about/pronouns; Settings My Account adds
  an **About Me** textarea + **Pronouns** input (+ Save → refresh member list). New `ProfileCard`
  component (centered overlay, Esc/overlay-close) shows avatar, name, pronouns, presence dot, custom
  status, and about (all React-escaped). Clicking a member row opens it.
- **QA:** store/integration (SetUserProfile caps + clears; ListServerMembers returns them) + browser
  (settings → set about/pronouns → click my member row → the card shows them) + AI-vision the card.
- **Verify:** go test + full QA green, ship + railway up + rollout-verify.

## Profile card from message authors (v0.5 — profiles, slice 2)

**Why:** the iter-140 profile card only opened from the member-list (server channels only). You usually
want to click someone IN the chat. Slice 2: clicking a message's avatar/name opens the card anywhere
(incl. #general / DMs) by fetching the author's public profile.

- **Store:** `GetUserProfile(userID) → UserProfile{UserID, Username, About, Pronouns, Status,
  StatusEmoji, PresenceState}`; `ErrUserNotFound` for a missing id.
- **Router:** `GET /users/{id}/profile` (authed group): returns ONLY public fields (no hash/email —
  none exist anyway, but assert the shape); annotates presence via `EffectivePresence` (others see
  invisible/disconnected as offline; you see your own true state) using `hub.OnlineUserIDs()`. 404 for
  a missing user. **Rule 15:** any authed user can view any user's PUBLIC profile (like Discord) — never
  more than the card needs.
- **Web:** `api.ts` `fetchUserProfile` (→ `ServerMember`-shaped); `Chat.tsx` `openUserProfile(id)` fetches
  + opens the existing `ProfileCard`; the main message list's avatar + author become clickable.
- **QA:** browser (click a message author in #general → card shows their profile) + **Rule-15 XSS-inert**
  (set `about`/`pronouns` to `<img onerror>` / `<script>` → the card renders the LITERAL text, no exec)
  + integration test (`GET /users/{id}/profile` returns public fields, 404 for missing, auth-required).
- **Verify:** go test + full QA green, ship + railway up + rollout-verify.

## Voice & Video — input (mic) volume slider (TOP PRIORITY slice 3d)

**Why:** the Voice & Video tab has an Output Volume slider (how loud you hear others) but no **Input
Volume** — the Discord control for how loud *you* sound to everyone. Deferred from slice 3c because,
unlike playback-only output volume, mic gain must be spliced into the live capture chain (interacts
with mute / push-to-talk / device hot-swap). This is its dedicated tick.

- **`voiceSettings.ts`:** `getInputVolume()` / `setInputVolume(v)` — 0..1, default 1 (=unchanged),
  clamped, corrupt→1. Mirrors the output-volume pair; localStorage key `opencord.voice.inputVolume`.
- **`voice.ts` (capture chain):** a `GainNode` spliced AFTER capture — `raw mic → MediaStreamSource →
  micSendGain → MediaStreamDestination → micSendTrack`, exactly the proven `buildScreenSendAudio`
  pattern. Peers receive `micSendTrack` (gain-scaled), not the raw track. `setInputVolume(v)` sets
  `micSendGain.gain.value` live (mid-call). Degrades safely: if Web Audio is unavailable
  `buildMicSend` returns the raw track so the mic still works. **Mute/PTT/deafen unchanged** — they
  still toggle `track.enabled` on the RAW source track (`localStream`); a disabled source feeds
  silence through the gain node, so peers get silence exactly as before. **Hot-swap** (`setInputDevice`)
  rebuilds the gain chain from the new mic and `replaceTrack`s the new `micSendTrack` on every mic
  sender (no renegotiation). `stop()` tears down the gain node + send track. Seeded from
  `getInputVolume()` so a new call starts at the persisted level.
- **`Settings.tsx` / `Chat.tsx`:** an Input Volume `<input type=range>` (aria-label "input volume")
  above Mic Test, persisting via `setInputVolume` and applying live to the active call through a new
  `onSetInputVolume` prop → `voiceRef.current.setInputVolume(v)`.
- **QA:** vitest (voiceSettings clamp/default/corrupt) + browser (slider renders + persists across a
  modal remount) + **two-client voice** (set sender A's input volume to 100% → B's inbound mic RMS is
  audible; set it to 0% → B's inbound RMS drops to ~silence → the gain is really wired through the
  send path, not just the UI; the mic sender + call survive the change; restore to 100%).
- **Verify:** go build/vet/test + vitest + full browser+voice QA green, AI-vision the slider, ship +
  `railway up` + rollout-verify.

## Date dividers in the message list (v0.5 — messaging parity)

**Why:** Discord separates messages by calendar day with a centered date divider ("Today" /
"Yesterday" / "June 17, 2026"). Opencord's message list ran days together with no separator — a
visible parity + readability gap. Client-only, no backend.

- **`web/src/dates.ts` (new):** `dayLabel(d, now=new Date())` → "Today" / "Yesterday" / full local
  date. `now` injectable for tests. Unit-tested (`dates.test.ts`): Today/Yesterday boundaries, older
  dates, and the month-boundary case (yesterday across the 1st).
- **`Chat.tsx`:** in the message map, `newDay` = first message whose `createdAt.toDateString()` differs
  from the previous (and the very first message). When true, render a `.day-divider` before the message
  via a keyed `<Fragment>`. A new day ALSO breaks same-author grouping (Discord-faithful).
- **`styles.css`:** `.day-divider` — the date centered on a hairline rule (`::before/::after` flex
  lines), muted text, matching the dark theme.
- **QA:** browser asserts a `.day-divider` renders and reads "Today" (all QA messages are sent now);
  AI-vision verified the divider. Full browser+voice QA green.
- **Verify:** tsc + vitest (27/27) + full QA green + AI-vision; ship + `railway up` + rollout-verify.

## Hover timestamp on grouped messages (v0.5 — messaging parity)

**Why:** grouped continuation rows hide the avatar/name/time (Discord-style), which loses *when* each
line was sent. Discord reveals a compact timestamp in the gutter on hover. Client-only.

- **`dates.ts`:** `shortTime(d)` → compact "9:41 AM" / "21:41" (hour:minute, no seconds, viewer locale).
  Unit-tested (HH:MM present, seconds absent).
- **`Chat.tsx`:** the grouped branch's `.avatar-spacer` now holds `<span class="hover-time">{shortTime}</span>`.
- **`styles.css`:** `.hover-time` is muted 10px, right-aligned in the 38px gutter, `opacity:0` →
  `opacity:1` on `.message.grouped:hover` (matches the existing `.msg-actions` hover reveal).
- **QA:** browser asserts the gutter time exists, reads HH:MM (no seconds), and reveals on hover
  (computed opacity 0 → 1); AI-vision verified the revealed "8:53 AM" gutter time.
- **Verify:** tsc + vitest (29/29) + full QA green + AI-vision; ship + `railway up` + rollout-verify.

## Discord-style message header timestamp (v0.5 — messaging parity)

**Why:** message headers showed the raw `toLocaleTimeString()` ("8:53:23 AM") — Discord never shows
seconds; it shows "Today at 8:53 AM". Now consistent with the compact gutter `shortTime`.

- **`dates.ts`:** `messageTimestamp(d, now=new Date())` = `${dayLabel} at ${shortTime}` → "Today at
  9:41 AM" / "Yesterday at 9:41 AM" / "June 15, 2026 at 9:41 AM". Unit-tested (relative-day prefix,
  no seconds).
- **`Chat.tsx`:** the three message-header `.time` spans (main list, search results, pins) use it; the
  timeout-deadline string is left as-is (different semantic).
- **QA:** browser asserts the header reads `^Today at H:MM` with no `H:MM:SS`; AI-vision verified.
- **Verify:** tsc + vitest (31/31) + full QA green + AI-vision; ship + `railway up` + rollout-verify.

## "Start of channel" intro (v0.5 — messaging parity)

**Why:** Discord shows a welcome block at the top of every channel/DM scrollback ("Welcome to
#general! · This is the start of the #general channel."). Opencord's list started cold at the first
message. Most visible on a freshly created channel/DM (empty → it's all you see). Client-only.

- **`Chat.tsx`:** above the message map (same `membersOf/searchResults/pins === null` guard), a
  `.channel-intro` block — a round `#`/`@` icon + title + subtitle. DM variant: "@username" +
  "This is the beginning of your direct message history with @username."
- **`styles.css`:** `.channel-intro*` — 68px round elevated icon, 28px bold title, muted subtitle.
- **QA:** browser asserts `.channel-intro` renders with "Welcome to #general" + "start of the #general
  channel"; AI-vision verified the block (icon + title + subtitle, sits above the Today divider).
- **Verify:** tsc + full QA green + AI-vision; ship + `railway up` + rollout-verify.

## Custom server emoji — backend (v0.5, slice 1)

**Why:** Discord lets a server upload named image emoji (`:name:`). Slice 1 is the backend
(self-hostable, local-disk like avatars — Rule A); slices 2/3 add client `:name:` rendering + a
picker/upload UI.

- **Schema** (`internal/db/schema.sql`): `server_emoji(id, server_id→servers ON DELETE CASCADE, name,
  emoji_key, content_type, created_by, created_at)` + `server_id` index + `(server_id, name)` unique.
- **Store** (`internal/chat/emoji.go`): `ServerEmoji`, `ValidEmojiName` (`^[a-z0-9_]{2,32}$`),
  `CreateServerEmoji` (23505→`ErrEmojiExists`), `ListServerEmoji`, `ServerEmojiForServe`,
  `DeleteServerEmoji` (scoped by server_id → can't delete a foreign server's emoji).
- **Handlers** (`internal/httpapi/emoji.go`): mirror the avatar pattern. Upload bounds the body
  (`MaxBytesReader`, 256 KiB), validates the name, **sniffs** the content-type and rejects anything
  not in the image allowlist (SVG sniffs as text → rejected; no script/XSS vector), stores under an
  opaque key (no path traversal), cleans up the file on every error path, 409 on dup. Serve sets
  nosniff + inline + `filepath.Base` defense-in-depth. Delete is best-effort file cleanup + 204.
- **Routes** (authed group): `POST /servers/{id}/emoji` (admin), `GET /servers/{id}/emoji` (member),
  `DELETE /servers/{id}/emoji/{emojiId}` (admin), `GET /emoji/{id}` (any authed user). README synced.
- **Rule 15 (verified):** `TestServerEmojiIntegration` — non-admin can't upload/delete (403),
  non-member can't list (403), invalid name 400, oversized rejected (no row), non-image rejected,
  dup-same-server 409 vs same-name-different-server allowed, wrong-server delete 404 (survives), all
  unauth 401. Witnessed passing on a real Postgres + the real router; build/vet/gofmt/full-suite green.
- **Next:** slice 2 — client renders `:name:` as the emoji image (markdown + a server-emoji fetch);
  slice 3 — emoji picker + server-settings upload/delete UI.

## Custom server emoji — client `:name:` rendering (v0.5, slice 2)

**Why:** slice 1 shipped the backend; slice 2 makes `:name:` render as the emoji image in messages.
Server-scoped: only renders for names in the CURRENT server's emoji set (literal text in #general/DMs).

- **`api.ts`:** `listServerEmoji(token, serverId)` → `ServerEmoji[]`.
- **`Chat.tsx`:** per-server emoji cache (`Record<serverId, Map<name,id>>`), fetched once per server,
  empty for non-server views; the active map is threaded into all 3 `renderMarkdown` call sites.
- **`markdown.tsx`:** new inline rule `:([a-z0-9_]{2,32}):` (matches backend `ValidEmojiName`) →
  `React.createElement('img', {className:'emoji-inline', src:`/api/emoji/${id}`, alt/title:`:name:`})`
  ONLY when `opts.emoji.get(name)` returns an id; unknown names stay literal (so a later known emoji
  still resolves). **XSS-safe:** React elements only (no innerHTML), numeric-id src, strict charset.
- **`styles.css`:** `.emoji-inline` 22×22 inline, baseline-aligned.
- **Tests:** `markdown.test.tsx` (8 cases: known→img, unknown/empty/no-map→literal, bad-name/UPPER
  literal, doesn't corrupt URLs/`::`, composes with bold). browser.mjs E2E: owner uploads an emoji via
  the API → sends `:qa_emoji:` → asserts `img.emoji-inline` src `/api/emoji/{id}` + alt; screenshot.
- **Verify (done):** tsc + vitest 39/39 + full QA green (no markdown regression across all message
  types) + AI-vision (shortcode renders as an inline image); ship + `railway up` + rollout-verify.
- **Next:** slice 3 — emoji picker (insert `:name:`) + server-settings upload/delete UI + live refetch.

## Custom server emoji — manager UI (v0.5, slice 3a) — feature complete

**Why:** slices 1+2 shipped the backend + `:name:` rendering, but emoji could only be added via raw API.
Slice 3a is the admin UI to upload/list/delete emoji, with live cache invalidation.

- **`api.ts`:** `uploadServerEmoji(token, serverId, name, file)` (multipart; surfaces 409/400/413),
  `deleteServerEmoji(token, serverId, emojiId)`.
- **`Chat.tsx`:** an **Emoji** section in the members/server-settings panel (admin-gated, mirroring the
  Invites/Bans pattern; `.emoji-manager-*` namespace so it can't collide with existing selectors):
  list (image + `:name:` + delete), upload form (name input client-validated `[a-z0-9_]{2,32}` + file
  picker), and `refreshEmojiCache` which rebuilds the per-server name→id map after every upload/delete
  so `:name:` resolves **live, no page reload** — the slice-3a UX win.
- **`styles.css`:** dark-theme `.emoji-manager-*` consistent with invites/bans.
- **QA:** browser E2E — open manager → upload via the UI → row appears → send `:ui_emoji:` → renders
  `img.emoji-inline` WITHOUT a reload (proves live invalidation) → delete → row removed. Screenshot.
- **Verify (done):** tsc + vitest 39/39 + full QA green + AI-vision (manager Discord-like, integrated).
  Ship + `railway up` + rollout-verify. Backend enforces admin regardless (client gate is UX).
- **Custom emoji is now complete end-to-end.** Optional later: slice 3b emoji PICKER (insert `:name:`
  from a palette in the composer); stickers/GIF much later.

## Custom emoji — composer picker (v0.5, slice 3b)

**Why:** convenience over typing `:name:` — a picker to insert a server's custom emoji into the composer.

- **`Chat.tsx`:** a 🙂 toggle in the composer (shown only when the active server has custom emoji),
  a popover listing `activeEmoji` (each an `img.emoji-inline` + `:name:` title). `insertEmojiShortcode`
  splices `:name:` at the caret (mirrors the mention-accept pattern: `onMouseDown`+`preventDefault` to
  keep focus, `requestAnimationFrame` to restore caret), closes on select / outside-click / Esc / channel
  switch. Reuses the per-server emoji map (no api.ts change). Separate state from the reaction palette.
- **`styles.css`:** `.emoji-picker-*` (dark grid popover above the composer).
- **QA:** browser E2E — open picker → lists `:ui_emoji:` → click inserts `:ui_emoji:` into the draft →
  send → renders `img.emoji-inline`. Reaction palette (`pickerFor`) untouched. Screenshot.
- **Verify (done):** tsc + vitest 39/39 + full QA green + AI-vision (picker + popover render). Ship.
- **Note:** the QA emoji fixture is a degenerate 1px PNG → inline emoji look tiny in screenshots
  (picker tiles render visibly). A clearly-visible fixture is the next QA improvement so AI-vision of the
  rendered emoji is unambiguous. **Custom emoji feature is now fully complete (backend+render+manager+picker).**

## Video slice 2 — screen + camera coexist (PLAN, investigated iter 154)

**Goal:** let a participant share their screen AND camera at once (today they're mutually exclusive —
one mesh video slot). NICHE + HIGH-BLAST-RADIUS (touches the core voice mesh, the product's
differentiator), so spec-first + implement as its own dedicated effort with full two-client voice QA.

**Current model (voice.ts):** one shared `screenStream`/`screenVideoTrack` (camera reuses the same
field); `startCamera` bails with `if (this.screenStream) return`. One video transceiver per peer; the
receiver holds one `inboundVideoStream`/`screenStream` per peer + one tile. The `voice-screen` announce
carries `{on, streamId, kind}` and the Go relay (`internal/ws/client.go`) validates `kind ∈ {screen,
camera}` + bounds streamId — already rich enough for two streams, EXCEPT stop frames drop `kind`.

**Changes to coexist (sub-slices, in safe order):**
- **2a sender (low risk):** add independent `cameraStream`/`cameraVideoTrack` fields; drop the mutual-
  exclusion guard; `addCameraTrackToPeer` (own transceiver, own streamId, CAMERA_BITRATE < screen);
  `stopCamera` independent of `stopScreenShare`; `ensurePeer` adds BOTH if active; announce each stream
  separately with its `kind`.
- **2d relay (minimal):** carry `kind` on the stop frame too, so the receiver knows WHICH stream ended
  (`internal/ws/client.go` lines ~208-210). Backward compatible.
- **2b receiver (low):** per-peer state keyed by kind ({screen?, camera?} streams); `ontrack` stores the
  inbound stream, `onScreenAnnounce` classifies it by kind; preserve the transceiver-reuse re-attach
  path for screen-only/camera-only backward compat.
- **2c UI (medium):** render up to two tiles per peer (+ self), labelled screen/camera, own-camera
  mirrored, screen-audio volume only on the screen tile; keep the ≤640px no-overflow guard.

**Regression risks (two-client voice QA MUST re-verify):** screen-only, camera-only, screen↔camera
switch (transceiver reuse — no duplicate tiles), mute/PTT/deafen (deafen still silences screen audio),
peer join while both active (joiner sees both), peer leave. Extend `qa/voice.mjs`: A shares screen +
camera → B sees 2 tiles → A stops camera → B sees 1 → A stops screen → B sees 0; each tile's controls
work independently.

**Value/risk note (for prioritization):** simultaneous screen+camera is an advanced/niche case; the
change is the highest-blast-radius in the codebase (core mesh). Recommend implementing as a focused
dedicated tick (delegate per the plan + careful review + the full two-client QA above) OR deferring in
favour of higher-ROI broadly-used parity (custom-emoji reactions, role colors) — owner's call. Spec is
ready either way.

## Custom-emoji reactions + P1 fix: emoji images now actually load (iter 155)

**Two things shipped together (the fix is the headline):**

**P1 FIX — emoji images never loaded in a browser.** Custom-emoji code rendered raw
`<img src="/api/emoji/{id}">`, but that endpoint is auth-gated (401 without a bearer token) and an
`<img>` tag can't send the Authorization header → every emoji image 401'd → broken image. This
affected the whole emoji feature (slices 2-3) and was MISSED for 4 ticks because the QA only asserted
the `<img>` element EXISTED (+ src), never that it LOADED; AI-vision of the 1px-looking result was
ambiguous. Caught this tick by a new `naturalWidth>0` QA assertion. Fix mirrors the existing
`Avatar.tsx` pattern: new `web/src/components/EmojiImg.tsx` fetches the bytes WITH the token
(`fetchAttachment`) → `URL.createObjectURL(blob)` → blob-URL `<img>`, cached by id. Wired into every
emoji render site (markdown inline, manager, picker, reaction chip). Verified: the `naturalWidth>0`
checks now PASS + AI-vision shows the emoji rendering as a visible image (was broken before).
**QA lesson:** for any auth-gated image, assert it LOADS (`naturalWidth>0`), not just that the element
exists — and prefer fetch+blob over `<img src>` for token-auth'd images.

**FEATURE — custom-emoji reactions.** React to a message with a server's custom emoji, stored as a
`custom:{id}` marker in the existing `reactions.emoji` TEXT column (no schema/WS change). Backend
`validEmoji` accepts unicode (≤16) OR `custom:{1-16 digits}` (format-validated, bounded ≤24, XSS-safe —
numeric id). Client: reaction palette lists the server's custom emoji after the unicode quick-set;
chips render via `EmojiImg` for `custom:` markers. Go integration test (accept/reject cases) +
browser E2E (react with a custom emoji → chip loads → toggle off). Verified: go test, vitest 39/39,
full QA `browser=0 realtime=0 voice=0 search=0`.

## Desktop notifications + image-bug-class audit (iter 156)

**Audit (no code change — bug class CONTAINED):** audited every auth-gated image surface for the
tick-155 `401-on-<img>-src` bug. Avatars (`Avatar.tsx`) and attachments (`Attachment.tsx`) BOTH already
use the auth'd fetch+blob pattern AND already have `naturalWidth>0` decode-check QA (avatar QA line
~1338, attachment QA line ~445). Emoji was the only offender (fixed iter 155). So all three image
surfaces now use fetch+blob + have load-guards. Refined lesson: the decode-check rigor already existed
for avatars/attachments — the emoji slices just failed to mirror it when adding a new image surface.

**Feature — desktop notifications (Web Notifications API), Rule-A graceful:** Discord's low-noise
default — when the tab is UNFOCUSED and a new message in the active channel is a DM (any) or @-mentions
you (`@you`/`@everyone`/`@here`), show a desktop notification (author + snippet); click focuses the
window. Off by default; opt-in via a new Settings → **Notifications** tab whose toggle requests OS
permission on click (a user gesture) and only persists ON if granted (denied → stays off + hint).
- `web/src/notify.ts`: pure `shouldNotify({enabled,permission,hidden,isMine,isDM,mentionsMe})` +
  `mentionsMe(body,username)` (regex-escaped username, word-bounded, @everyone/@here) +
  `getDesktopNotify`/`setDesktopNotify` (localStorage, default off) + `requestNotifyPermission` +
  `showNotification` (no-op unless granted; try/catch so it never throws into the WS handler; `tag`
  collapses spam). Every entry point degrades to no-op when unsupported/denied.
- `Chat.tsx`: the `message` WS handler feeds `shouldNotify` + fires `showNotification`.
- Tests: vitest `notify.test.ts` (21 cases — full `shouldNotify` truth table + `mentionsMe` incl.
  `@meelsewhere` rejection + regex-escape). browser E2E: stub `window.Notification`, grant permission,
  force `document.hidden`, a 2nd user posts an @mention → assert a notification was constructed with
  the author + body. Verified: build clean, vitest 60/60, full QA `browser=0 realtime=0 voice=0 search=0`.

## User blocking — backend (v0.5, slice 1)

**Why:** Discord lets you block a user (privacy/safety). Slice 1 = the block API + DM-only enforcement;
hiding a blocked user's messages in SERVER channels is slice 2.

**Semantics:** a block is enforced SYMMETRICALLY for DMs — if A blocked B OR B blocked A, neither can
open or use their DM (create/open, send, react, read history all denied).

- **Schema:** `user_blocks(blocker_id, blocked_id, created_at, PK(blocker_id,blocked_id))` + index on
  blocked_id.
- **Store:** `BlockUser` (self→ErrForbidden, target must exist, idempotent), `UnblockUser` (0 rows→
  ErrUserNotFound), `IsBlocked(a,b)` (symmetric EXISTS), `ListBlocked` (directed, newest first);
  `ErrBlocked`.
- **Enforcement (3 points):** (1) `CanAccessChannel` — the central gate (send/react/history) — the
  `kind='dm'` branch ALSO denies if the caller is in a block relationship with the other DM member
  (server/global branches UNCHANGED, regression-tested); (2) `CreateOrGetDM` → `ErrBlocked` (can't
  open/reopen); (3) `ListDMs` filters out blocked DMs so the sidebar never shows an un-openable one.
- **Routes:** `POST /users/{id}/block` (400 self / 404 unknown / 204), `DELETE /users/{id}/block`
  (404 / 204), `GET /me/blocks`. `HandleCreateDM` maps `ErrBlocked`→403. README synced.
- **Rule 15 (verified):** integration tests — symmetric block → `CreateOrGetDM` both directions
  ErrBlocked; a PRE-EXISTING DM's `CanAccessChannel` false for BOTH after block; server-channel +
  #general access UNAFFECTED (explicit regression guard); unblock restores; endpoints 401/400/404/204.
  Full suite green (existing DM/server tests still pass). Build/vet/gofmt clean.
- **Next (slice 2 — client + hide msgs):** block/unblock button (profile card / member row), a
  blocked-users list in Settings, and hide/collapse a blocked user's messages in server channels.

## User blocking — client (v0.5, slice 2) — feature complete

**Why:** slice 1 shipped the block API + DM enforcement; slice 2 is the client — block control, blocked
list, and hiding blocked users' messages in channels.

- **`api.ts`:** `blockUser`/`unblockUser`/`listBlocked`.
- **`blocking.ts` (new):** pure `visibleMessages(messages, blocked)` — drops messages from blocked
  authors BEFORE the date-divider/grouping pass (so hiding never orphans a divider or breaks a group);
  live WS messages from a blocked user are auto-hidden (render filters on the set). Unit-tested.
- **`Chat.tsx`:** `blocked: Set<number>` fetched on load; `toggleBlock` (optimistic+revert, never self,
  closes the card on block); message render uses `visibleMessages`.
- **`ProfileCard.tsx`:** a Block/Unblock button (danger-styled; only on another user's card).
- **`Settings.tsx`:** a new **Privacy** tab listing blocked users (avatar + name + Unblock) + a hint;
  unblocking refreshes Chat's hide set so the messages reappear.
- **QA:** two-author browser E2E — 2nd user posts → main user blocks via the card → message disappears →
  Settings → Privacy lists them → Unblock → message reappears. vitest for `visibleMessages`.
- **Verify (done):** build clean, vitest 65/65, full QA `browser=0 realtime=0 voice=0 search=0`,
  AI-vision (block button + Privacy list + message-hide). **User blocking complete (backend + client).**

## Group DMs — backend (v0.6, slice 1)

**Why:** the highest-ROI parity item after blocking (decided tick 159). DMs are already channels of
`kind='dm'` with an N-member `channel_members` join table, and the WS hub fans out per channel id — so
N-member group DMs are a clean generalization of the existing 2-member model. Backend-first slice
(mirrors the blocking epic); the client UI (create-group flow, group avatar/title) is slice 2.

**Model (no schema change — `channel_members` already N-member, Rule A).** A group DM is the same
`kind='dm'` channel with 3+ members. 1:1 DMs stay exactly as they are (2 members). Group naming is
deferred (channels.name carries a UNIQUE constraint, unsuitable for free-form group names) — slice-1
groups are unnamed and the client will render them as a comma-joined member list (Discord's default).

- **Generalize the DM shape (`chat.go`):** `DMChannel` gains `Users []DMUser` (ALL other members,
  username-sorted). `User DMUser` is RETAINED (= `Users[0]`) so the existing 2-member client keeps
  working unchanged after this slice deploys; slice 2 switches the client to `Users`.
- **`CreateGroupDM(ctx, creator, otherIDs)` (new store fn):** dedupe + drop self; resolve each id
  (`ErrUserNotFound` if any missing); reject if creator is in a block relationship with ANY member
  (`ErrBlocked`, symmetric — you can't form a group with someone you've blocked / who blocked you —
  conservative, avoids the "hide a whole group for one block" problem at the source). 0 others →
  `ErrCannotDMSelf`-style guard; exactly 1 other → delegate to `CreateOrGetDM` (idempotent 1:1);
  2..9 others → create a NEW channel (groups are NOT deduped, like Discord). Cap total at 10 members
  (`ErrGroupTooLarge`).
- **`ListDMs` generalized:** aggregate ALL non-blocked others per channel into `Users` (group consecutive
  rows by channel id). A channel with zero non-blocked others is dropped — this preserves the existing
  2-member behavior (a 1:1 whose sole other is blocked disappears) AND, for a group, simply omits a
  later-blocked member from `Users` without hiding the whole group.
- **`CanAccessChannel` DM branch generalized:** the block-deny is now gated on `member count = 2`, so a
  1:1 with a block stays denied (unchanged, tested) but a group member is never denied access just
  because one co-member is blocked (message-level hiding from blocking slice-2 covers their messages).
- **`POST /api/dms/group` (new route + `HandleCreateGroupDM`):** body `{"identifiers":[...]}` (usernames
  or numeric ids), 64 KiB bounded (Rule B). Maps `ErrUserNotFound`→404, `ErrBlocked`→403,
  `ErrGroupTooLarge`/too-few→400. Returns the `DMChannel` (with `Users`) as seen by the creator.
- **`types.ts`:** `DMChannel` gains an optional `users?: {id,username}[]` (additive, documents the new
  contract for slice 2; no rendering change this slice).

**Verify (Rule 14):** `go build`/`vet`/`test` green incl. a new `TestGroupDMIntegration` (3-member create
+ access for all members + non-member denied; per-viewer `ListDMs` aggregates the right others; blocked
member rejected at create; group survives a later block while the 1:1 with that user is hidden; cap +
too-few guards; 1-other delegates to the idempotent 1:1). WS fanout to all N members is already covered
by the per-channel hub (verified manually next slice with the create UI). No client UI yet → no browser
QA delta this slice; slice 2 adds the create-group flow + group rendering + its QA.

## Group DMs — client (v0.6, slice 2)

**Why:** slice 1 shipped the N-member backend + `POST /api/dms/group`; slice 2 is the client so a
user can actually create and see group DMs. Also closes the slice-1 QA gap: a 3-client WS fanout test.

- **`dm.ts` (new, pure + vitest):** `dmOthers(dm)` (the `users[]` array, falling back to legacy singular
  `user`), `dmIsGroup(dm)` (>1 other), `dmTitle(dm)` (the other usernames comma-joined — Discord's
  default title for an *unnamed* group), `parseIdentifiers(raw)` (split a typed string on
  whitespace/commas, dedupe, drop blanks). Unit-tested incl. the legacy-payload fallback.
- **`api.ts`:** `createGroupDM(token, identifiers[])` → `POST /api/dms/group`.
- **`NewGroupModal.tsx` (new):** a compact create dialog (reuses the `.settings-overlay` backdrop):
  a chips input (type a username/id, Enter/comma adds a chip; ✕ or Backspace-on-empty removes;
  ≤9 chips), inline error from the server, a Create button that adapts its label (Create DM for one
  person / Create Group for 2+) and is disabled until ≥1 chip. Esc + overlay-click + ✕ close.
- **`Chat.tsx`:** the `+ New DM` button now opens this modal (replacing the old double `window.prompt`
  1:1 flow — a polish win; a single chip still creates the idempotent 1:1 via the group endpoint).
  DM list, header, and welcome render via `dmTitle`/`dmIsGroup`; a group shows a group-glyph avatar
  (`.dm-group-avatar`) instead of a single user Avatar, with the full member list in the `title`.
  On create, the new channel is added to `dms` (de-duped) and selected.
- **`styles.css`:** `.dm-group-avatar` (accent-tinted circle + people glyph) and the chips modal
  (`.group-modal`, `.group-chips`, `.group-chip`) — compact, dark-theme-consistent.
- **`types.ts`:** `users` already added in slice 1.

**Verify (Rule 14):** vitest for `dm.ts`; `go build/vet/test` green incl. a NEW
`TestServeWSGroupDMFanoutIntegration` (3 ws clients on a group channel — A sends → B AND C both
receive; a 4th non-member's handshake to the group channel is refused 403). Browser QA: a new
create-group flow (open modal → add two members → create → the group appears in the DM list titled by
its members → open it) added to `qa/browser.mjs`; AI-vision review the modal + the rendered group row.
Then ship + `railway up` + rollout-verify the live bundle carries the new flow.

Later sub-slices: group naming (needs a non-UNIQUE name column), add/remove member, leave group,
stacked member avatars.

## Custom colored roles — backend (v0.7, slice 1)

**Why:** tick-159 ROI plan #2 — "every Discord server uses them." Today roles are the fixed
owner/admin/member PERMISSION tier (`server_members.role`). This adds Discord-style **cosmetic** roles:
a server admin creates named, colored roles, assigns them to members, and a member's name renders in
their **top** (highest-position) role's color. Cosmetic only — the permission tier is untouched
(low blast radius); the member-list Admins/Members grouping stays.

**Model (additive, no change to the existing role column).**
- `server_roles (id, server_id→servers ON DELETE CASCADE, name, color, position, created_at)` —
  server-scoped; `position` orders them (higher = wins the color tie).
- `member_roles (user_id→users, role_id→server_roles ON DELETE CASCADE, PK(user_id, role_id))` — the
  assignment; deleting a role or user cascades. A member's display color = their assigned role with the
  highest `position` (Discord's top-role rule).

- **`Role` struct** `{id, serverId, name, color, position}`. **`ServerMember` gains `color`** (the top
  role color, "" = none) surfaced via a correlated subquery in `ListServerMembers`.
- **Store (all mutations admin-gated via `IsServerAdmin`, Rule C):** `CreateServerRole` (validate name
  1–32 chars + color `#RGB`/`#RRGGBB` → `ErrInvalidColor`; position = max+1), `ListServerRoles`
  (position desc), `UpdateServerRole` (rename/recolor, role must belong to the server),
  `DeleteServerRole` (cascades assignments), `AssignServerRole`/`UnassignServerRole` (target must be a
  member; role must belong to the server).
- **Routes** (cosmetic roles live under `custom-roles` since `POST .../roles` is the permission
  setter): `GET /api/servers/{id}/custom-roles` (member-gated), `POST` (create), `PATCH/DELETE
  …/custom-roles/{roleId}`, `PUT/DELETE …/members/{userId}/custom-roles/{roleId}` (assign/unassign).
  Bodies 4 KiB-bounded (Rule B); admin-gating returns 403, bad color/name 400, unknown role/member 404.

**Verify (Rule 14):** `go build/vet/test` green incl. a new `TestServerCustomRolesIntegration` (admin
creates/edits/deletes a role; non-admin is forbidden; assign → the member's `color` in `ListServerMembers`
becomes the top role's; two roles → highest position wins; bad color rejected; delete cascades the
assignment). **Live E2E on a real server** (curl the full CRUD + assignment + member-list color; adversarial:
non-admin 403, bad hex 400, cross-server role 404). No client UI yet → slice 2 adds the role-manager UI +
colored names + its QA.

## Custom colored roles — client (v0.7, slice 2a)

**Why:** slice 1 shipped the role backend (CRUD + assignment + top-role `color` on members). Slice 2a is
the client so admins can actually create/assign roles and members' names render in their color.

- **Backend tweak (needed for the assignment UI to show state):** `ServerMember` gains `roleIds []int64`
  — the cosmetic role ids the member holds in this server (highest-position first), aggregated in
  `ListServerMembers` alongside the existing `color`. No new endpoint.
- **`types.ts`:** new `Role {id, serverId, name, color, position}`; `ServerMember` gains `color?` +
  `roleIds?: number[]`.
- **`api.ts`:** `listServerRoles`, `createServerRole`, `updateServerRole`, `deleteServerRole`,
  `assignServerRole`, `unassignServerRole` (mirror `setServerMemberRole`).
- **`RolesManagerModal.tsx` (new):** opened from the members-panel server-settings (admin only). Lists
  existing roles (color swatch + name + delete), and a create row (name input + `<input type="color">`
  + Discord-style preset swatches + Add). Inline server errors. Esc/overlay/✕ close. Reuses the
  `.settings-overlay` + group-modal scaffold.
- **`ProfileCard.tsx`:** when an admin views a server member, a new **Roles** section shows every server
  role as a toggle chip (filled in its color when assigned, per `member.roleIds`); clicking assigns/
  unassigns live. New props: `activeServerId?`, `canManageRoles?`, `serverRoles?`, `onAssignRole?`,
  `onUnassignRole?` (all optional → the card stays usable in non-server/non-admin contexts).
- **`Chat.tsx`:** fetch the active server's roles into state; wire the manager modal + the ProfileCard
  props/handlers (assign/unassign → refresh members + roles so colors update live); render each
  member's name in `style={{ color: mb.color }}` in the sidebar member list + the members panel.
- **`styles.css`:** the roles manager (swatches, role rows), the profile roles chips.

**Deferred to slice 2b:** message-author coloring (needs the author's role color joined into the message
history + WS payload — a backend change); role reordering (drag); per-role permissions.

**Verify (Rule 14):** vitest stays green; `go build/vet/test` green incl. an extended
`TestServerCustomRolesIntegration` asserting `roleIds` on members; full browser QA — a new flow: admin
opens Manage Roles → creates a colored role → opens a member's profile → assigns it → the member's name
in the list renders in the role color; AI-vision the manager + colored name. Ship + `railway up` +
rollout-verify the live bundle.

## Custom colored roles — message-author coloring (v0.7, slice 2b)

**Why:** slice 2a colored names in the member list/profile but NOT on chat messages. Slice 2b completes
the feature: a message author's name renders in their top role color.

- **Backend:** `Message` gains `authorColor` — the author's top custom-role color in the channel's server
  (correlated subquery `authorColorSQL`, or the `authorColor()` helper for the live `Save*`/`EditMessage`
  paths). "" for DM/global channels or uncolored authors. Added to `Recent`, `PinnedMessages`,
  `SearchMessages` (read), and `SaveReply`/`SaveWithAttachments`/`EditMessage` (live broadcast). Reflects
  CURRENT role assignment (computed at query time, not persisted).
- **Client:** `Message.authorColor`; message author render sites (main head button + search + pins) apply
  `style={{ color: m.authorColor }}`.

**Verify:** `TestMessageAuthorColorIntegration` (server-channel message carries the role color via both
Save and Recent; DM message carries none); full QA green incl. a new flow that re-enters a server channel
after a role assignment and asserts the message author name is tinted; AI-vision verified the colored
message author. Ship + `railway up` + rollout-verify. **Colored roles now fully complete.**

## Threads — backend (v0.8, slice 1)

**Why:** the biggest remaining Discord parity gap (owner's TOP PRIORITY = UI/UX parity). A thread is a
sub-conversation spawned from a parent channel. Modeled as a `kind='thread'` channel with a `parent_id`
(mirrors how group DMs reused `channel_members`) — so messages, history, WS fanout, post-policy, and
slowmode all work UNCHANGED (channel-id-scoped already).

**Model (reuse the channel infra; minimal new surface).**
- Schema: `channels.parent_id BIGINT REFERENCES channels(id) ON DELETE CASCADE` (+ index). A thread is
  `kind='thread'`, `parent_id=<parent>`, **`server_id` copied from the parent**, `name` set.
- **Access is inherited for free:** because the thread copies the parent's `server_id`, the existing
  `CanAccessChannel` server-membership branch (or `ELSE TRUE` for a public parent) already gates it —
  NO change to `CanAccessChannel`/`CanPostInChannel`. A thread member = the parent's server member.
- **Channel struct** gains `ParentID *int64` + `Kind string` (omitempty) so the client can identify threads.
- Threads are kept OUT of the regular channel lists (`ListChannels`, `ListServerChannels`, and the unreads
  query) via `AND kind <> 'thread'` — they're fetched separately, like DMs stay out.
- `CreateThread(ctx, parentID, name)`: validate the parent exists, is NOT a DM and NOT itself a thread
  (no nesting in slice 1), copy its `server_id`, validate name (1–100 chars), insert `kind='thread'`.
  `ListThreads(ctx, parentID)`: the parent's threads, newest first.
- Routes: `POST /api/channels/{id}/threads {name}` (gate: `CanAccessChannel`+`CanPostInChannel`) and
  `GET /api/channels/{id}/threads` (gate: `CanAccessChannel`). 4 KiB-bounded (Rule B).

**Verify (Rule 14):** `go build/vet/test` green incl. `TestThreadsIntegration` (create a thread on a
server channel; a parent member accesses it, a non-member is denied; the thread is absent from
`ListServerChannels`/`ListChannels`; posting + `Recent` work in the thread; a thread on a DM is rejected;
nesting rejected; name validation). Live E2E on a real server (create/list threads, post in a thread,
member-200/non-member-403, thread not in the channel list). No client yet — slice 2 adds the thread UI
("start thread" on a message, a thread list/panel, the thread view) + a 3-client WS thread-fanout check.

## Threads — client (v0.8, slice 2)

**Why:** slice 1 shipped the thread backend (a thread = a `kind='thread'` channel with `parentId`,
reusing the message/WS infra). Slice 2 is the client so users can create/open threads.

- **`types.ts`:** `Channel` gains `kind?` + `parentId?`.
- **`api.ts`:** `fetchThreads(token, channelId)`, `createThread(token, channelId, name)`.
- **`Chat.tsx`:**
  - State `threads: Channel[] | null` (the panel, mirrors `pins`) + `activeThread: Channel | null` (the
    thread being viewed — its name/back-link, since threads aren't in any channel list).
  - Header gets a **🧵 Threads** button (shown for a normal channel, not a DM, not while in a thread) →
    `openThreads()` fetches + opens a panel (mirrors the pins panel) listing the channel's threads + a
    **+ New thread** create (prompt name → `createThread` → open it).
  - A **thread** action in the message hover row (`createThreadFromMessage` → prompt name → create+open) —
    Discord-faithful discoverability.
  - `selectThread(t)` opens the thread channel (reuses `selectChannel`'s WS reconnect) + sets
    `activeThread`; `selectChannel` clears `activeThread`. `activeChannelName` falls back to
    `activeThread?.name`; in a thread the header shows `🧵 name` + a **← back** link to the parent.
  - The composer placeholder reflects the thread name.
- **`styles.css`:** a small `.thread-row` (reuses the pins-panel/`.search-results` container).

**Verify (Rule 14):** vitest stays green; `go build/vet/test` green incl. a NEW
`TestServeWSThreadFanoutIntegration` (3 ws clients on a thread channel — A sends → B AND C receive; a
non-member's handshake to the thread channel is refused 403 — proving thread realtime + access inherit
the parent). Browser QA: a new flow — open a server channel → 🧵 Threads → + New thread → the thread
appears → open it → post a message → the thread view shows it; AI-vision the thread panel + thread view.
Then ship + `railway up` + rollout-verify the live bundle.

Later (slice 3+): message-anchored threads ("X started a thread" system message), thread unread counts,
archive/auto-archive, a thread indicator on the source message.

## Threads — message-anchored + discoverable (v0.8, slice 3)

**Why:** the threads MVP (slices 1-2) only surfaces threads via the header panel — a real
discoverability gap. Discord anchors a thread to the message it was started from and shows a clickable
"🧵 thread" reference on that message. This closes the gap and makes threads usable in practice.

- **Schema:** `channels.source_message_id BIGINT REFERENCES messages(id) ON DELETE SET NULL` — the message
  a thread was started from (nullable; only threads set it).
- **`CreateThread`** gains an optional `fromMessageID *int64`, validated to be a non-deleted message IN
  the parent channel (Rule B/C, mirrors reply validation — a client can't anchor to a message it can't
  see); stored on the thread.
- **`Message`** gains `ThreadID *int64` + `ThreadName string` — the thread anchored to this message (if
  any), surfaced in the read paths (`Recent`/`SearchMessages`/`PinnedMessages`) via a LEFT JOIN
  `channels t ON t.source_message_id = m.id AND t.kind='thread'`.
- **Route:** `POST /api/channels/{id}/threads` accepts `{name, fromMessageId?}`.
- **Client:** the message-hover **thread** action passes the message id; if the message already anchors a
  thread (`m.threadId`), it OPENS that thread instead of creating a duplicate (Discord behavior). A
  **🧵 {name}** chip renders under a message that started a thread → clicking opens it. `selectThread`
  works from `{id, name}` (a minimal Channel).

**Verify (Rule 14):** `go build/vet/test` green incl. extending `TestThreadsIntegration` (a thread created
`fromMessageID` records the anchor; the source message's `ThreadID`/`ThreadName` surface in `Recent`; an
anchor to a cross-channel/bogus/deleted message is rejected or dropped). Full QA green incl. a new flow:
start a thread from a message → a 🧵 chip appears on that message → click it → the thread opens; AI-vision
the chip. Ship + `railway up` + rollout-verify.

## Custom colored roles — hoisting (v0.7, slice 3)

**Why:** Discord groups the member list by "hoisted" roles (a role displayed as its own section). This
completes the colored-roles feature and is the owner's "feels like Discord" polish bar.

**Design (ADDITIVE, low-risk).** A role gains a `hoist` flag. In the member list, for each hoisted role
(highest position first) a section lists the members whose TOP hoisted role is that one (header = role
name in its color); the remaining members fall into the EXISTING Admins/Members grouping unchanged. With
no role hoisted (the default + every existing server) the member list renders exactly as today — so the
existing member-list QA stays green and the feature only activates when an admin opts in.

- **Schema:** `server_roles.hoist BOOLEAN NOT NULL DEFAULT false`.
- **`chat.go`:** `Role.Hoist`; `CreateServerRole`/`UpdateServerRole` accept `hoist`; `ListServerRoles`
  returns it. No `ListServerMembers` change — the client already has `member.roleIds` + the roles list, so
  it computes each member's top hoisted role itself.
- **`api.ts`/`types.ts`:** `hoist` on create/update + the `Role` type.
- **`RolesManagerModal`:** a "Display separately" checkbox per role + on the create row.
- **`Chat.tsx` member list:** group hoisted-role members into role sections above Admins/Members.

**Verify (Rule 14):** `go build/vet/test` green incl. extending `TestServerCustomRolesIntegration`
(`hoist` round-trips through create/update + list). Full QA green incl. a new flow: hoist a role → the
assigned member appears under a role-named section in the member list; AI-vision the hoisted section.
Ship + `railway up` + rollout-verify.

## Voice presence (v0.9, slice 1)

**Why:** the audio component is furthest from its north star and has stagnated. Voice presence — knowing
WHO is in a voice call — is the foundation for joinable/discoverable calls and (later) dedicated voice
channels. First visible win: a live "🔊 N in voice" indicator on the active channel header.

**Design (hub-owned, lock-free).** The hub already relays `voice-join`/`voice-leave` through its single
goroutine (with the sender's `From` + the channel). Track presence there:
- **`Hub.voiceMembers map[int64]map[int64]bool`** (channelID → set of userIDs), mutated ONLY on the hub
  goroutine. On a `voice-join`/`voice-leave` event (in the `h.events` case) add/remove the user; on
  `unregister`/`evict` (disconnect) drop the user from their channel's voice set. Each change emits a new
  `voice-presence` event to the channel; on register, if a call is already in progress, the new client
  gets the current set.
- **`Event.VoiceMembers []int64`** + `Type:"voice-presence"` carries the channel's current voice user ids.
- No new HTTP endpoint this slice — the WS event powers the active-channel indicator (cross-channel
  sidebar presence is slice 2, via a `Hub.VoiceMembers(channelID)` query mirroring `OnlineUserIDs`).

- **Client:** consume `voice-presence` → track the active channel's voice user ids; render a live
  **"🔊 N in voice"** chip in the channel header when N>0 (you're included once you join).

**Verify (Rule 14):** `go build/vet/test` green incl. extending `TestServeWSVoiceSignalingIntegration`
(A joins → B receives a `voice-presence` listing A; A leaves/disconnects → it drops). Full QA green incl.
a browser assertion that a second client joining voice surfaces "in voice" for the first; AI-vision the
header chip. Ship + `railway up` + rollout-verify.

## Voice presence — cross-channel sidebar (v0.9, slice 2)

**Why:** slice 1 showed "🔊 N in voice" for the ACTIVE channel (live via WS). Discord shows voice
participants per channel in the sidebar — so you see a call on ANY channel. The client's WS is per-channel
(can't get live presence for other channels), so this uses an HTTP query.

- **`Hub.VoiceMembersFor(channelIDs) map[int64][]int64`** — HTTP-safe query (request/reply channel,
  mirrors `OnlineUserIDs`; runs on the hub goroutine, no lock). Returns each channel's in-voice user ids.
- **`GET /api/servers/{id}/voice-presence`** (members only) → `{channelId: [userIds]}` for the server's
  channels (only non-empty). The handler lists the server's channels + asks the hub.
- **Client:** `fetchServerVoicePresence(token, serverId)`; state `serverVoice` (channelId → userIds). The
  server sidebar renders a small **🔊 N** badge on each channel with voice members. Refreshed on
  server-select, when any `voice-presence` WS event fires, and on a modest poll (~15 s) while a server is
  open (the only way to learn a call started on a channel you're not viewing).

**Verify (Rule 14):** `go build/vet/test` green incl. a route + hub test (two members; one joins voice on
a channel via WS → `GET /voice-presence` lists them on that channel; a non-member is 403). Full QA green
incl. a browser assertion that a 🔊 badge appears on a server channel when someone is in its call;
AI-vision the sidebar badge. Ship + `railway up` + rollout-verify.

## Voice channels as entities (v0.9, slice 3a — backend)

**Why:** slices 1–2 added voice *presence* (who's in a call, per channel + cross-channel sidebar
badges) as the foundation for **dedicated voice channels** — Discord's core 🔊 channel you click to
join. Slice 3a makes a voice channel a first-class entity server-side; slice 3b wires the visible
🔊 channel + click-to-join + participants-beneath in the client. The voice infra (per-channel
`voiceMembers`, voice-join WS path, `VoiceMembersFor` query) is already keyed by channel id and is
kind-agnostic, so a `kind='voice'` channel reuses it unchanged.

- **Store:** new `CreateServerChannelOfKind(ctx, serverID, name, categoryID, kind)` — the existing
  `CreateServerChannel`/`CreateServerChannelInCategory` keep their signatures and delegate with
  `kind="public"` (zero blast radius). `kind` is validated to `'public'|'voice'` (Rule B — defense in
  depth even though the route validates); the INSERT sets the `kind` column (schema already has it).
- **List:** `ListServerChannels` also SELECTs `kind` and sets `Channel.Kind`, but **normalizes
  `'public'`→`""`** so existing (text) channels' JSON stays byte-identical (omitempty) — only
  `kind='voice'` channels carry `"kind":"voice"` on the wire.
- **Route:** `POST /api/servers/{id}/channels` accepts an optional `kind` in the body (default
  `'public'`, validated to `'public'|'voice'`, 400 otherwise), admin-gated as today, and calls
  `CreateServerChannelOfKind`. The client's create-channel UI does NOT yet send `kind`, so no voice
  channels appear in the UI until slice 3b — backend is dormant-but-ready, zero user-visible change.

**Verify (Rule 14/15):** `go build/vet/test` green incl. `TestServerVoiceChannelIntegration` (admin
creates a `voice` channel → `ListServerChannels` returns it with `kind='voice'`; a text channel returns
`kind=''`; adversarial: a plain member can't create — route 403; an invalid kind → store rejects /
route 400). Live E2E on the running stack: create a voice channel via the API, list it, confirm
`kind='voice'`; bad kind → 400; non-admin → 403. (Browser/visual verification lands with slice 3b's UI.)

## Voice channels in the client (v0.9, slice 3b — UI)

**Why:** slice 3a made `kind='voice'` a first-class server channel server-side; 3b makes it visible
and usable — Discord's core 🔊 channel you click to join, with participants listed beneath it.
Architectural constraint: mesh voice signaling rides the per-channel WS, so being "in" a voice channel
== having it as your active channel (background voice while reading another text channel needs a
separate-WS refactor — a later slice).

- **Create:** `addServerChannel(serverId, categoryId?, kind='public')` + a new **"+ voice"** button in
  the server group create the channel with `kind:'voice'` (api `createServerChannel` gains an optional
  `kind`, omitted for text so the body is unchanged).
- **Sidebar:** `channelButton` renders a 🔊 glyph (not #) for a voice channel and lists its current
  participants beneath the row (`voice-channel-group` → `voice-participants`), resolving ids→names via
  the active server's `memberList`. Reuses the existing `serverVoice` cross-channel presence.
- **Main view:** `activeChannelIsVoice` (active server channel `kind==='voice'`) swaps the text chat +
  composer for a centered **voice-channel-view**: 🔊 icon, name, a Join/Disconnect control
  (reusing `joinVoice`/`leaveVoice` + the existing in-call voice bar), and a roster. The text composer
  and message list are hidden for voice channels; DMs/threads/text are untouched (flag is false).

**Verify (Rule 14):** tsc clean, vitest 77/77, go build/vet/test green; browser QA grows a new flow —
create via "+ voice" → 🔊 row in the sidebar → opens the join view (composer absent) → Join → in-call
voice bar + Disconnect → back to Join → return to a text channel — all green; AI-vision verified the
join view, the in-call view, and the sidebar participants-beneath. Ship + `railway up` + rollout-verify.
**P1 follow-ups (slice 3c polish):** the channel *header* still shows `#` + text-channel actions
(pins/threads/edit-topic/make-read-only/slowmode/search) for a voice channel — swap to 🔊 and hide the
inapplicable actions. **3d:** auto-join on click; background voice while viewing a text channel.

## Voice channel header polish (v0.9, slice 3c)

**Why:** slice 3b's AI-vision pass surfaced a P1 — a voice channel's header still showed `#` + the
text-channel actions (make-read-only, slowmode, edit-topic, pins, threads, message search) that don't
apply to a call. 3c makes the header read as a voice channel: the brand shows `🔊 <name>` (not `#`),
and the inapplicable actions are hidden (`!activeChannelIsVoice`). The header's own Join-voice /
N-in-voice buttons are hidden too (the main voice view carries its own Join + roster — no duplicate).
The `🔔 mute` notification toggle stays (harmless). Frontend-only; text/DM/thread headers unchanged.

**Verify (Rule 14):** tsc clean, vitest 77/77, go build/vet/test green; browser QA grew a regression
guard — on the open voice channel it asserts the header brand contains 🔊 and that
`.readonly-toggle/.slowmode-edit/.topic-edit/.pins-open/.threads-open/.search-form` are all absent;
AI-vision confirmed the cleaned-up header. Ship + `railway up` + rollout-verify.

## Voice channel roster — live source + presence assertion (v0.9, slice 3d)

**Why:** the slice-3b voice view listed participants from `serverVoice[channelId]` (the ~15s polled
cross-channel map), so a join/leave lagged up to 15s and the QA in-call screenshot non-deterministically
showed/omitted the roster. Fix at the root: the open voice channel IS the active channel, so its roster
now prefers the **live** `voicePresence` (updated on every WS voice-presence event), falling back to the
polled map only until the live list arrives. Roster updates are now instant.

**Verify (Rule 14):** tsc clean, vitest 77/77, go build/vet/test green; browser QA now asserts the
voice-channel view roster lists SELF after joining (a real end-to-end presence assertion inside the view,
`waitFor` the roster item → the in-call screenshot is deterministic); AI-vision confirmed the roster +
sidebar participant both show. Ship + `railway up` + rollout-verify.

## Voice channels are voice-only — reject text posts + threads (v0.9, slice 3e, Rule-15 hardening)

**Finding (reproduced):** slices 3a–3d hid the composer for a voice channel in the UI, but the BACKEND
still accepted text. `CanPostInChannel` checked only post-policy + membership, not `kind`, so a hostile
client could POST to a `kind='voice'` channel via the raw WS `message` frame or the REST attachment path
— the messages were stored + broadcast to others in the call but never displayed (data-integrity
inconsistency + an unbounded-write abuse vector with no moderation UI). Separately, `CreateThread`
rejected `dm`/`thread` parents but not `voice`, so a thread could be started under a voice channel.

**Fix (defense in depth, Rule B/15):** `CanPostInChannel` returns false for `kind='voice'` (the single
chokepoint both the WS send path `SaveReply` and the REST path `SaveWithAttachments` call, so both are
closed at once); `CreateThread` adds `voice` to the not-threadable kinds. A voice channel is now
voice-only at the data layer, consistent with the UI.

**Verify (Rule 14/15):** reproduced the break first (`Save` to a voice channel succeeded → test RED),
applied the fix, re-attacked (`Save`/`SaveWithAttachments`/`CreateThread` → `ErrForbidden`/
`ErrNotThreadable`, test GREEN), proved legit use intact (text channels still post + thread).
`TestVoiceChannelRejectsMessages` encodes the exploit; go build/vet/test green. Ship + `railway up` +
live re-attack on the deploy (raw WS `message` frame to a voice channel → not persisted).

## Group DM — leave group (v0.2 DM sub-slice)

**Why:** group DMs (kind='dm', ≥3 members) can be created + rendered (tick-160/161) but a member can't
LEAVE one — a Discord parity + usability gap (a group DM you can't leave is a trap).

- **Store `LeaveGroupDM(ctx, channelID, userID)`:** verify the channel is a DM (`kind='dm'`), the user is
  a member, and it's a GROUP (≥3 members — a 1:1 DM can't be "left", that's a future "close DM"); then
  `DELETE` the membership row. Errors: `ErrNotGroupDM` (not a DM / a 1:1), `ErrChannelNotFound`,
  `ErrUserNotFound` (not a member — no existence leak, Rule B). Messages are preserved (history intact,
  like a server leave); the group stays a group for the rest even if it drops to 2 members.
- **Route `POST /api/dms/{id}/leave`:** auth from JWT (Rule C), call the store, then notify realtime —
  `BroadcastToChannel(id, {type:"dm-membership", channelId})` so the REMAINING members refetch their DM
  list (updated member roster) live, and `EvictUserFromChannels(me, [id])` to drop the leaver's now-invalid
  socket. 204. Maps `ErrNotGroupDM`→400, `ErrChannelNotFound`/`ErrUserNotFound`→404.
- **WS `Event`:** add a `ChannelID` field (parallel to `ServerID`) for channel-scoped user events.
- **Client:** `leaveGroupDM(token, id)`; a **"Leave Group"** button in the group DM header (only when
  `dmIsGroup`); on 204 remove the DM from the sidebar + fall back to #general if it was active; a
  `dm-membership` WS handler refetches DMs so remaining members see the leaver gone live.

**Verify (Rule 14/15):** `TestLeaveGroupDMIntegration` (group leave removes the row + ListDMs drops it
for the leaver but keeps it for others; adversarial: a 1:1 DM can't be left → 400-class, a non-member →
404-class, a non-existent channel → 404); route test (member leaves → 204, non-member → 404, 1:1 → 400);
go build/vet/test green; browser QA asserts the Leave-Group flow; AI-vision the button + post-leave
sidebar. Ship + `railway up` + rollout-verify.

## Group DM stacked member avatars (v0.2 DM sub-slice — UI polish)

**Why:** group DM rows showed a single generic people-glyph; Discord shows a STACK of the first two
members' avatars so a group reads as "several people" at a glance.

- **Client:** the DM-list group branch renders the first two `dmOthers(d)` via the `Avatar` component
  inside a `.dm-group-stack` (two 15px avatars offset top-left / bottom-right, each ringed in the sidebar
  bg so the front reads on top). 1:1 DMs unchanged (single avatar). Frontend-only, reuses `Avatar`
  (img-or-initials, so each member is a distinct coloured circle).

**Verify (Rule 14):** tsc clean, vitest 77/77, go build green; browser QA asserts the group row shows
`.dm-group-stack` with exactly 2 `.dm-stack-avatar`; AI-vision confirmed the stacked avatars in the
sidebar. (Follow-up: the welcome-intro big icon still uses the 👥 emoji — optionally stack it too.)
Ship + `railway up` + rollout-verify.

## Group DM — add member (v0.2 DM sub-slice)

**Why:** mirrors leave-group to complete group-DM membership management. In Discord you ADD people to a
group (and only ever leave yourself — there's no "remove other"), so add+leave is the full set.

- **Store `AddGroupDMMember(ctx, channelID, actorID, targetID)`:** verify the actor is a member (checked
  FIRST, no existence leak — Rule B), the channel is a GROUP DM (`kind='dm'`, ≥3 members; a 1:1 can't be
  added to — start a new group instead → `ErrNotGroupDM`), the group is under the 10-member cap
  (`ErrGroupTooLarge`), the target isn't already in it (`ErrAlreadyMember`), and there's no block either
  way (`ErrBlocked`, symmetric). Then INSERT the membership row. The route resolves the
  username/id identifier via `LookupUserByIdentifier` (→ `ErrUserNotFound`/404) before calling.
- **Route `POST /api/dms/{id}/members {identifier}`:** auth (Rule C), resolve identifier, add, then
  notify realtime — `BroadcastToChannel(id, dm-membership)` (existing members + the actor refetch their
  DM list → updated roster) AND `SendToUser(targetID, dm-membership)` (the NEW member's client refetches
  → the group appears in their sidebar live, even though they weren't connected to the channel). 204;
  maps `ErrNotGroupDM`→400, `ErrForbidden`→403, `ErrAlreadyMember`→409, `ErrBlocked`→403,
  `ErrGroupTooLarge`→400, not-found→404.
- **Client:** `addGroupDMMember(token, id, identifier)`; an **"➕ add"** header button (only when
  `dmIsGroup`) → prompts for a username → on success refetches DMs (immediate roster/stack update).

**Verify (Rule 14/15):** `TestAddGroupDMMemberIntegration` (add → the new member's ListDMs includes the
group, roster grows; adversarial: non-member actor→ErrForbidden, 1:1→ErrNotGroupDM, already-member→
ErrAlreadyMember, blocked→ErrBlocked, cap→ErrGroupTooLarge); browser flow adds a member + asserts the
group title/stack updates; live E2E (add→204, the new member's /api/dms now lists the group). Ship +
`railway up` + rollout-verify. (Two-client realtime "added member sees the group appear LIVE" → next
tick, mirroring the leave-group coverage cadence.)

## Group DM — naming (v0.2 DM sub-slice, backend slice 1)

**Why:** the last group-DM membership/identity gap. Discord lets you name a group DM (else it's titled by
its members). Backend slice 1 makes a group nameable + returns the name; slice 2 wires the client (a
rename action + `dmTitle` using the name). Additive — an unnamed group renders exactly as today.

- **Schema:** group DM names are NOT unique (many groups can share a name), so recreate the global
  channel-name partial unique index to ALSO exclude `kind='dm'` (the exact precedent threads used) —
  `kind NOT IN ('thread','dm')`. Idempotent (DROP IF EXISTS + CREATE IF NOT EXISTS), self-applied on boot.
  Reuses the existing `channels.name` column (NULL for unnamed DMs).
- **Store `RenameGroupDM(ctx, channelID, actorID, name)`:** actor must be a member (checked first, Rule B),
  channel must be a GROUP DM (`kind='dm'`, ≥3 members; a 1:1 can't be named → `ErrNotGroupDM`); the name is
  trimmed + bounded ≤100 (`ErrInvalidGroupName` if longer), and an EMPTY name clears it (NULL → back to the
  member-list title). `DMChannel` gains `Name` (`json:"name,omitempty"`); `ListDMs` returns it (NULL→"").
- **Route `PATCH /api/dms/{id}` `{name}`:** auth (Rule C), rename, then `BroadcastToChannel(dm-membership)`
  so every member relabels live (reusing the leave/add realtime path). 204; maps `ErrForbidden`→403,
  `ErrNotGroupDM`→400, `ErrInvalidGroupName`→400, not-found→404.

**Verify (Rule 14/15):** `TestRenameGroupDMIntegration` (set → ListDMs returns the name; clear → NULL;
adversarial: non-member→ErrForbidden, 1:1→ErrNotGroupDM, >100→ErrInvalidGroupName); live E2E (rename→204,
ListDMs shows it, clear→"", non-member→403). go build/vet/test green. Slice 2 (client rename + dmTitle)
next tick — the client ignores `name` until then, so this is dormant-but-ready (no UI change, no deploy
needed unless I choose to ship the backend ahead).

## Chat header — compact action bar (icon buttons + collapsible search + overflow) — DEFERRED, spec-only

**Why (vision-found iter 187/195, logged GOAL P2):** in a server channel with the member-list panel
open — the most common view — the chat header wraps its actions onto **two or three rows**
(`make read-only / slowmode / edit topic / pins / threads`, then `mute / Join voice / Search…`, then
`N online / username / log out`). iter-188 capped the TITLE so it can't be the cause, but the controls
themselves are too wide: ~6–10 text-label buttons + a ~240px search box + the meta cluster overflow the
narrowed column. Discord keeps this single-row with **icon-only buttons** (tooltip on hover) and a
**search icon that expands**. This is the real single-row fix; the title cap was only half.

**Why DEFERRED / spec-only (NOT a one-tick job — blast-radius check, iter 190):** the browser/realtime/
voice/sfu QA matches these buttons by **visible text** via `getByRole('button',{name})` in ~10 call
sites across `qa/browser.mjs`, `qa/voice.mjs`, `qa/sfu.mjs` (`Join voice`, `make read-only`,
`allow everyone`, `edit topic`, `slowmode`, `pins`), PLUS a header-line-count assertion. Re-skinning to
icon-only would break all of those at once. Doing it on a long session risks a multi-file regression.

**Staged plan (a dedicated, fresh-context tick):**
1. **Migrate QA selectors to CLASSES first, no UI change. — DONE (iter 196).** All 10 text-based
   header selectors migrated to scoped `.chat-header .<class>` locators (voice-join ×5 across
   browser/sfu/voice, readonly-toggle + its state read via textContent, topic-edit, slowmode-edit,
   pins-open); the message-hover `.msg-actions` pin stayed text-based (not a header button). Full QA
   green, no header-button name-selectors remain — the tests are decoupled from the label text.
2. **Icon-ify the buttons. — DONE (iter 197).** Each text label replaced with a glyph + `aria-label` +
   `title` (📌 pins · 🔔/🔕 mute · 🧵 threads · 🔒/🔓 read-only · 🐌 slowmode · 📝 edit-topic · 🎙 Join
   voice · 🔊 N presence · ➕/✏️/🚪 group add/rename/leave); `.chat-header .icon-btn` CSS (16px glyph,
   hover pill). readonly STATE moved text→aria-label; voice-presence count asserted via data-count. Cut
   the header from THREE rows → TWO (AI-vision verified); shipped + rollout-verified. Slice 3 (search)
   completes single-row.
3. **Collapsible search. — DONE (iter 198).** The ~240px search box is now a 🔍 icon that expands the
   input on click and collapses on Esc / empty blur / channel-switch / clear. The action bar is a
   single icon row; with the on-demand member-list panel CLOSED (default) the header is single-row.
   Remaining: with the panel OPEN the meta cluster (online/username/logout) still wraps — slice 4 below.
   QA uses an idempotent `openSearch()` (click 🔍 then fill). Shipped + rollout-verified.

   *(original slice-3 plan kept for reference below.)* Replace the always-open ~240px search box with a 🔍 icon that expands the
   input on click (Esc/blur collapses) — reclaims the single biggest chunk of width. QA: click 🔍 →
   `.search-input` appears → type → results; collapse afterwards.
4. **(Optional) overflow "⋯" menu** for the least-used actions (edit-topic, slowmode) if still tight.
5. **Verify (Rule 14):** full QA green at every slice; AI-vision confirm the header is **single-row** in
   a server channel with the member list open (the exact failing view); deploy + rollout-verify.

**Acceptance:** the server-channel header (member list open) is one row at ≥1100px; every action stays
reachable with an accessible name; no QA regression. Until built, the iter-188 title cap stands and this
stays a P2 in GOAL.md.

## Scroll-up history pagination (load older messages)

**Why:** history is a fixed window — `Recent` returns the newest 50 with no cursor, so a channel with
>50 messages can never show older history (scroll-up hits a hard wall). Discord loads older history as
you scroll up. **Backend slice 1 DONE (iter 203):** `RecentBefore(ctx, channelID, viewerID, beforeID,
limit)` (beforeID>0 = page strictly older than that id, oldest-first; 0 = newest page == Recent, which is
now a thin wrapper so its 15+ callers are untouched); `GET /messages?before=<id>` parses the cursor
(absent/0/invalid = newest). Test `TestRecentBeforePaginationIntegration`; shipped + live-verified
(?before=0/huge/invalid all 200). *(This slice also surfaced + fixed a latent migration boot bug — the
intermediate channels_global_name_uniq recreate didn't exclude group DMs; see `fix(db)` iter 203.)*

**Frontend slice 2 DONE (iter 204):** a "↑ Load older messages" pill at the top pages older history in (prepend + scroll-anchor via useLayoutEffect; the iter-191 auto-scroll skips the prepend via justPrependedRef; intro replaces the button at the channel start). De-risked from auto-load-on-scroll to a button (no stale-closure handler); auto-load is a future enhancement. QA seeds a global pgseed channel (60 msgs) so the button + 50→60 paging + no-yank are E2E-verified; shipped + rollout-verified. ORIGINAL PLAN: on scroll near the TOP of `.messages`, fetch the page before the oldest
loaded message (`?before=<oldest.id>`), PREPEND it, and PRESERVE the scroll position (anchor on the
previously-top message so the view doesn't jump). Key interaction: the iter-191 smart-auto-scroll keys
on `messages` changing — prepending older messages must NOT trigger auto-scroll-to-bottom (it only fires
when atBottom or channelChanged or own-send, none true while reading history up top, so it should be
safe, but VERIFY). Stop paging when a returned page is shorter than the limit (reached channel start).
Browser-QA: post >50 messages, open the channel, scroll to top, assert older messages load + the view
doesn't jump.

## Bottom-left sidebar user panel (Discord layout parity) — iter 206

**Why:** Discord's iconic layout puts the user panel (avatar + presence + name + settings) at the
BOTTOM-LEFT of the sidebar, and keeps the channel header to just title + action icons. Opencord put
the user identity cluster (`.meta`: online count + self-chip + log out) in the chat HEADER's top-right,
which (a) is non-Discord and (b) wraps to a 2nd header row in group DMs / when the member-list panel
narrows the column (the iter-188/195 "appearance slice-4" P2; see 07l-group-header.png). Moving the
cluster out of the header into a sidebar footer fixes the wrap permanently AND matches Discord.

**Change (minimal, mostly a move):**
- Cut the `.meta` cluster from `<header className="chat-header">` and render it as a
  `<div className="sidebar-user">` footer at the BOTTOM of `<aside className="sidebar">` (after the
  server actions). Children unchanged (online dot + count, `.self-chip` settings button, `log out`),
  so every class/aria selector (`user settings`, `log out`, `.self-chip-pip.presence-*`,
  `.self-chip .avatar-self`, `N online`) keeps resolving — only the parent location moves.
- CSS: replace the header-anchored `.meta` rule with a `.sidebar-user` footer bar (border-top,
  bg, flex, the self-chip flex-grows); drop the two mobile `.meta` header overrides (now moot).
- The header keeps `flex-wrap` (harmless) but nothing forces a wrap anymore → single-row header.

**QA delta:** the header-wrap line-count test no longer measures `.meta` in the header (it's gone);
repurpose it to assert the header has NO `.sidebar-user` and no h-overflow, and that the panel +
its self-chip/log-out are reachable in the sidebar. Mobile: the panel lives in the drawer, so the
phone test opens the drawer to reach settings/log out (Discord-mobile behavior). Desktop tests that
click `user settings`/`log out` are unaffected (the 220px sidebar is always visible).

**Verify:** tsc + vitest + full browser/realtime/voice/search QA green; AI-vision confirms the
bottom-left panel + clean single-row header (incl. the group-DM header that used to wrap); deploy +
rollout-verify.

## Voice mute/deafen in the user panel (Discord parity) — SPEC for iter 212+ (implement from a fresh context)

**Why:** Discord's bottom-left user panel always shows mic-mute + deafen toggles — the canonical place
to mute yourself, including BEFORE joining a call ("join already muted"). Opencord has mute/deafen only
in the in-call voice bar (`toggleMute`/`toggleDeafen` in Chat.tsx, functional only while `voiceRef.current`
exists; `muted`/`deafened` reset on join/leave). The iter-206 sidebar user panel (`.sidebar-user`) is now
the natural home for always-available mute/deafen.

**Current state (verified iter 211/212):**
- `Chat.tsx`: `muted`/`setMuted` (~323), `deafened`/`setDeafened` (~325); `toggleMute` (~1008) does
  `if (voiceRef.current) setMuted(voiceRef.current.toggleMute())`; `toggleDeafen` (~1073) sets state +
  `voiceRef.current?.setDeafened(next)`. Both no-op the live session when not in a call. State resets in
  `joinVoice`/leave (584-585, 986, 999-1000).
- `web/src/voiceSettings.ts` is the localStorage source of truth (device ids + DSP flags, default-on).
- The in-call voice bar already renders mute/deafen buttons (mirror their icons/aria for consistency).

**Slices (build minimal, verify each — Rule 14):**
1. **Persisted self-mute/deafen defaults.** Add `selfMute:boolean` + `selfDeafen:boolean` to
   `voiceSettings.ts` (default false) with the existing get/set + a vitest (default/persist/corrupt-input,
   mirroring the other flags). Pure, no UI — ships green on its own.
2. **Panel toggles.** In `.sidebar-user` (new `.sidebar-user-voice` row, or extend `.sidebar-user-foot`)
   add a 🎤 mute button and a 🎧 deafen button (aria-label "toggle mute" / "toggle deafen", a struck/red
   state when active). They read/write the slice-1 state AND, when `voiceRef.current` exists, call the
   existing `toggleMute`/`toggleDeafen` so in-call behavior is unchanged (the panel becomes a 2nd control
   point). Deafen-implies-muted stays (existing logic). CSS only; mirror the voice-bar button styling.
3. **Apply-on-join.** In `joinVoice`, instead of unconditionally `setMuted(false)`, seed `muted`/`deafened`
   from the persisted slice-1 defaults and apply them to the new session (mute the mic / deafen) right
   after connect — so "join already muted" works. Keep the in-call toggles authoritative thereafter.
4. **QA (grow the suite):** browser QA — panel mute/deafen toggle + persist across a modal/remount + the
   struck visual (AI-vision). Voice QA (`qa/voice.mjs`, 2-client) — set self-mute in the panel BEFORE
   joining, then A joins a call with B: assert B measures ~silence for A's mic immediately on join (the
   pre-set mute applied), then A un-mutes in the panel → B hears A. This proves slice 3 end-to-end (the
   send-path RMS pattern already used by the input-volume test).
5. **Verify + ship:** tsc + vitest + full browser/realtime/voice/search QA green; AI-vision the panel
   buttons (idle + active states); deploy + rollout-verify (bundle carries the new control). Blast-radius
   guard: the change touches `joinVoice` + `muted`/`deafened` + voiceSettings — check the voice-bar
   mute/deafen + the two-client voice tests still pass (they share `toggleMute`/`toggleDeafen`).

**Acceptance:** mute/deafen reachable from the user panel always; pre-call mute applies on the next join
(B hears silence); in-call behavior + the voice-bar controls unchanged; full QA + AI-vision green.
**Blast radius:** `joinVoice`, `muted`/`deafened`, `voiceSettings.ts`, the voice-bar buttons, the
two-client voice tests. SPEC-first because it spans UI + state + the live voice session (>3 surfaces).

## Bundled optional coturn (self-host TURN relay — audio/scale, iter 218)

**Why:** the audio north star is thousands-scale, free to self-host, with no drops across real
networks. Mesh + the opt-in LiveKit SFU + the HMAC ephemeral-TURN credential scheme
(`OPENCORD_TURN_SECRET`, `ICEServersForUser`, iter 137) are all built — but the credential scheme
was never paired with an actual relay: a self-hoster behind a symmetric/hostile NAT still had to
*bring their own* coturn (the hard part). This slice ships the relay itself as an OPTIONAL bundled
service so "self-host voice that works across NATs" is one command, not a research project.

**stack-guardian: APPROVE (iter 218)** — coturn is BSD OSS, self-hosted on the operator's own box,
zero recurring cost to the maintainer; it's a component of the OSS product, not a second host we run.
Gated off-by-default so it can never silently start on the Railway/prod path (Rule 16) and the
one-command `docker compose up` stays byte-identical + TURN-free (Rule A). Same opt-in pattern as the
existing `OPENCORD_SFU_URL` LiveKit path.

**Design (minimal):**
- `docker-compose.yml`: a `coturn` service under `profiles: ["turn"]` (OFF by default — absent from
  plain `docker compose up`/`config`; present only under `docker compose --profile turn up`).
  `network_mode: host` (a TURN relay allocates many UDP ports; host networking is the reliable,
  documented coturn setup). Configured via flags for the `use-auth-secret` REST scheme so it validates
  the SAME HMAC credentials the server already mints: `--use-auth-secret`,
  `--static-auth-secret=${OPENCORD_TURN_SECRET}`, `--realm=opencord`, a bounded relay port range,
  `--fingerprint`, `--no-cli`, no TLS in the basic profile (TLS/turns is a later slice).
- `server` service: surface `OPENCORD_STUN_URL` / `OPENCORD_TURN_URL` / `OPENCORD_TURN_SECRET` as env,
  each defaulting to today's behavior (`OPENCORD_TURN_URL` empty ⇒ STUN-only mesh, unchanged). When the
  operator sets the URL + secret AND enables the profile, the server hands out creds the bundled coturn
  accepts — a matched pair from one `OPENCORD_TURN_SECRET`.
- `.env.example` (new): documents `JWT_SECRET` + the full voice env set (STUN/TURN/SFU incl. the
  previously-undocumented `OPENCORD_TURN_SECRET` + `OPENCORD_TURN_TTL`) with the `--profile turn` recipe.
- `README.md`: a short "Voice across NATs (optional TURN)" note + the new env rows.

**Verify (Rule 14, honest scope):** `docker compose config` parses; `docker compose config --services`
(no profile) does NOT list `coturn` and the server still shows empty `OPENCORD_TURN_URL` (default stack
unchanged); `docker compose --profile turn config --services` DOES list `coturn`. The server side
(env → `iceServers` carries the TURN entry only when set) is already covered by `TestICEServers` /
`TestEphemeralTurnCredentials`. **A real relay through a symmetric NAT can't be exercised in the loop
(needs a hostile-NAT client + the host-net relay running) — stated, not faked;** the config-gating +
credential-pairing are what this slice verifies.

**Blast radius:** `docker-compose.yml`, `.env.example` (new), `README.md`. No Go code changes (the
server already reads the env). The Railway deploy is untouched (it never runs the `turn` profile).

## Markdown headers + subtext (chat parity, iter 221)

**Why:** Discord supports line headers (`# H1`, `## H2`, `### H3`) and subtext (`-# small`) in
messages — a common formatting primitive Opencord's markdown subset lacked (it had bold/italic/
strike/spoiler/code/quote/lists/autolink/emoji). Pure FORMATTING — no new security surface: the
header's inline content still flows through the existing XSS-safe `renderInline` (no
dangerouslySetInnerHTML; raw HTML stays literal, Rule B).

**Design (minimal, fits the existing line-oriented renderer):** in `renderBlocks`, before the
quote/list/para run logic, match two single-line forms — `HEADER = /^(#{1,3})\s+(.+)$/` (level =
hash count) and `SUBTEXT = /^-#\s+(.+)$/` — and emit a styled `<div class="md-h md-h{1..3}">` /
`<div class="md-subtext">` with the inline-rendered content. Visual weight only (chat text, not
document headings): CSS gives a controlled hierarchy (h1 1.5em → h3 1.07em, bold; subtext 0.8em
muted) so a header never dominates the message list. `#`-with-a-space is required, so `#channel`
(no space) is unaffected; `-#` doesn't collide with the `- ` bullet (which needs a space after the
dash). Empty-content (`# ` alone) stays a paragraph.

**Verify:** vitest (each level → the right class + inline children still render, e.g. `# **x**` keeps
the `<strong>`; raw HTML in a header stays inert); browser QA sends a `#`/`##`/`-#` message and
asserts the rendered classes + readable text; AI-vision the hierarchy. **Blast radius:** markdown.tsx,
styles.css, markdown.test.tsx, qa/browser.mjs. No backend (bodies already store + broadcast verbatim,
now bidi-sanitized).

## Markdown masked links [text](url) (chat parity + Rule 15, iter 222)

**Why:** Discord's `[text](url)` (show custom text for a link) is a high-usage markdown primitive
Opencord lacked (it had only bare-URL autolinks). It carries a PHISHING surface (display text ≠
destination), so it's a Rule-15 feature.

**Design (secure by construction):** add ONE inline rule
`/\[([^\]\n]+)\]\((https?:\/\/[^\s)]+)\)/` — the URL group is `https?://` in the REGEX, so a
`[x](javascript:…)` / `[x](data:…)` simply does NOT match and the whole token renders as inert
literal text (same safety as the bare-URL autolinker; never a scheme check that could be loosened by
accident). The `<a>` carries `target=_blank` + `rel=noopener noreferrer` (reverse-tabnabbing) and
**`title`=the real URL** as a lightweight anti-spoof (hover reveals the true destination when the text
lies). The display text is inline-rendered so `[**bold**](url)` works. Placed before the autolink rule
(earliest-index would pick it anyway since `[` precedes the inner `http`).

**Verify (Rule 15):** vitest — valid http/https → `<a href=url title=url target=_blank rel=…>text</a>`;
`href !== text` (the phishing shape) with `title===url`; `[x](javascript:alert(1))` + `data:` → NO `<a>`,
literal text; a quote inside the URL stays in `href` (no `on*` prop extracted); `[**b**](url)` keeps the
`<strong>`. Browser QA sends a masked link and asserts the rendered `<a>` (href/text/title). AI-vision.
**Blast radius:** markdown.tsx, markdown.test.tsx, qa/browser.mjs (the existing `.body a` CSS covers it).

## "New messages" unread divider (chat parity, iter 223)

**Why:** Discord renders a "New" line in the message list at the read→unread boundary when you open
a channel with unreads — the "where did I leave off" marker. Opencord tracks per-channel unread
(`channel_reads.last_read_id`, sidebar dot/badge) but never showed the in-list divider.

**Design (minimal; reuses the existing read marker + the day-divider render path):**
- Server: a `Store.LastReadID(channelID, userID) (*int64, error)` — the user's `channel_reads.last_read_id`
  for the channel, or nil when there's no row (a genuine first visit → no divider). The WS history event
  (built at connect in `client.go`, BEFORE the client POSTs `/read`) carries it as `lastReadId` on the
  `Event` (`*int64`, omitempty → nil omitted). So the boundary is the PRE-open read position.
- Client: capture `lastReadId` from the history event into a frozen `readBoundaryId` (reset on channel
  switch); the live `message` path never updates it, so the divider stays put as you read (Discord
  behavior — it clears only when you leave + return). Before the first VISIBLE message whose `id >
  readBoundaryId` (only when readBoundaryId != null and such a message exists), render a `.new-divider`
  ("New", red line) — alongside the day-divider. All-read (boundary == latest) ⇒ no divider; first visit
  (nil) ⇒ no divider.

**Verify:** store test (nil with no row; the value after `MarkChannelRead`; advances on a later read);
WS-serve test (history event includes `lastReadId` for a member with a read marker); two-client realtime
E2E (B reads #general → switches away → A posts 2 → B returns → a "New" divider sits before A's 2
messages); AI-vision. **Blast radius:** chat.go (+ store test), ws/hub.go (Event field), ws/client.go
(history wiring + serve test), types.ts, Chat.tsx, styles.css, qa/realtime.mjs. No schema change
(`channel_reads` already exists).

## Search-match highlighting (UI polish, iter 229)

**Why:** Discord bolds the matched term inside each search result so a long list is scannable.
Opencord's results render the message body (markdown) but never highlight the query — found via
the iter-229 AI-vision sweep.

**Design (minimal, post-process the markdown output — stays XSS-safe):** a `highlightMatches(node,
query)` helper in `markdown.tsx` walks the React tree `renderMarkdown` already produced and, in each
STRING text node, wraps case-insensitive occurrences of the (trimmed) query in `<mark
class="search-match">`. It only re-wraps EXISTING text (no new markup from the message; the query is
the viewer's own input, React-escaped), so the XSS invariant is untouched. Applied ONLY to the
search-results body (blast radius = the search panel); custom components (EmojiImg) and childless nodes
are passed through. Empty query → unchanged.

**Verify:** vitest (a plain match → one `<mark>`; case-insensitive; a match INSIDE markdown like
`**bold**` keeps the `<strong>` and wraps its text; empty query → no marks; a raw-HTML body still inert);
browser QA (search → the result contains a `.search-match`); AI-vision. **Blast radius:** markdown.tsx,
Chat.tsx (search-results render), styles.css, markdown.test.tsx, qa/browser.mjs.
