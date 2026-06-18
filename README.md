# Opencord

**A fully open-source, self-hostable Discord alternative that anyone can run locally at little to no cost.**

Opencord is built to be owned by the people who run it: one Go binary, a static
web client, and Postgres. No SaaS, no telemetry, no per-seat pricing — clone it,
run one command, and you have your own real-time chat server.

> **Status:** v0.4 — *in progress*. The realtime core is solid and growing, with
> servers, DMs, roles, and voice (incl. screen share) all working. More Discord
> parity is on the roadmap (see [`GOAL.md`](./GOAL.md)).
>
> **Working today:** accounts (bcrypt + JWT) · multiple channels with per-channel
> real-time routing (create + switch in a sidebar) · **servers/guilds** (group
> channels under a members-only server; rename, delete, leave, transfer ownership) ·
> **channel categories** (collapsible groups) · **direct messages** (private,
> members-only) · live messaging with **edit & delete** (owner-only) · **emoji
> reactions** (live counts) · **replies** + **jump-to-message** · **@mentions** with
> autocomplete · **markdown** · **file & image attachments** (local-disk, access-gated,
> inline images) · **grouped messages** (consecutive same-author) · **pinned messages** ·
> **message search** (in-channel, with `from:` / `has:` / `before:` / `after:`
> operators) · **typing indicators** · **unread indicators + @mention badges** ·
> **presence** (online / idle / DnD / invisible) + **custom status** (text + emoji) ·
> **avatars** (uploaded images, falling back to generated initials) · **roles**
> (owner / admin / member, promote-demote) · **moderation** (kick · ban · timeout ·
> admins delete others' messages · read-only/announcement channels · per-channel
> slowmode) · **invites** (codes with optional expiry + max-uses, list/revoke) ·
> **voice channels** (WebRTC — mute, deafen, push-to-talk + global hotkey,
> per-user volume, device picker, active-speaker indicator; opt-in LiveKit SFU path
> for scale) · **screen share** (up to 4K@60, with system audio + per-side audio-level
> controls) · **mobile-responsive** (sidebar drawer) · per-connection **rate limiting** ·
> one-command Docker stack · CI with a Postgres service running DB integration tests.

---

## Stack

| Layer      | Tech                                                        |
| ---------- | ---------------------------------------------------------- |
| Backend    | Go — `chi` router, `gorilla/websocket` gateway, `pgx`      |
| Frontend   | React + TypeScript + Vite                                  |
| Database   | PostgreSQL                                                 |
| Auth       | Local accounts, bcrypt hashing, JWT sessions              |
| Deploy     | Docker Compose (one command), or run each piece by hand    |

## Quick start (Docker — recommended)

```bash
git clone <your-fork-url> opencord && cd opencord
docker compose up --build
```

Open **http://localhost:3000**, create an account, and start chatting. Open a
second browser/incognito window, register another user, and watch messages and
the online count update live.

## Quick start (local dev, hot reload)

Requires **Go ≥ 1.22** and **Node ≥ 20** on your `PATH`.

```bash
# 1. Postgres in the background
make dev-db

# 2. Go backend (terminal A) — http://localhost:8080
cp .env.example .env      # optional; sane defaults exist
make dev-server

# 3. Vite web client (terminal B) — http://localhost:5173
make dev-web
```

The Vite dev server proxies `/api` and `/ws` to the Go backend, so you only ever
hit one origin in the browser.

## Configuration

All config is environment-driven (see [`.env.example`](./.env.example)):

| Variable             | Default                  | Purpose                                                       |
| -------------------- | ------------------------ | ------------------------------------------------------------ |
| `OPENCORD_ADDR`      | `:8080`                  | Listen address (a platform `$PORT` overrides it)             |
| `DATABASE_URL`       | local Postgres DSN       | Postgres connection string                                   |
| `JWT_SECRET`         | `dev-insecure-change-me` | **Change in production** — signs JWTs. The server logs a loud warning at boot while the insecure default is in use (anyone can forge tokens). |
| `CORS_ORIGIN`        | `*`                      | Allowed REST origin                                          |
| `OPENCORD_SFU_URL`   | _(empty → mesh)_         | Optional LiveKit SFU URL for large voice calls (opt-in)      |
| `OPENCORD_SFU_KEY`   | _(empty)_                | LiveKit API key (only with `OPENCORD_SFU_URL`)              |
| `OPENCORD_SFU_SECRET`| _(empty)_                | LiveKit API secret (only with `OPENCORD_SFU_URL`)          |
| `OPENCORD_STUN_URL`  | _(empty)_                | Optional STUN server for WebRTC NAT discovery (e.g. `stun:stun.l.google.com:19302`) |
| `OPENCORD_TURN_URL`  | _(empty)_                | Optional self-hostable TURN relay for hostile NATs (free, Rule A) |
| `OPENCORD_TURN_USERNAME` | _(empty)_            | TURN credential username (only with `OPENCORD_TURN_URL`)     |
| `OPENCORD_TURN_PASSWORD` | _(empty)_            | TURN credential password (only with `OPENCORD_TURN_URL`)     |
| `OPENCORD_UPLOAD_DIR`| `data/uploads`           | Where message attachments are stored on local disk (Rule A — no object store; mount a volume here to persist across restarts) |

## API surface

| Method   | Path                           | Auth   | Description                                    |
| -------- | ------------------------------ | ------ | ---------------------------------------------- |
| `GET`    | `/healthz`                     | —      | Liveness + version                             |
| `POST`   | `/api/auth/register`           | —      | Create account → `{token, user}`               |
| `POST`   | `/api/auth/login`              | —      | Log in → `{token, user}`                       |
| `GET`    | `/api/auth/me`                 | bearer | Current user                                   |
| `PUT`    | `/api/me/status`               | bearer | Set your custom status `{status, statusEmoji}` (empty clears) |
| `PUT`    | `/api/me/profile`              | bearer | Set your profile `{about, pronouns}` (empty clears)  |
| `GET`    | `/api/users/{id}/profile`      | bearer | A user's public profile (about, pronouns, status, presence) |
| `POST`   | `/api/users/{id}/block`        | bearer | Block a user (symmetric — neither can DM the other) |
| `DELETE` | `/api/users/{id}/block`        | bearer | Unblock a user                                 |
| `GET`    | `/api/me/blocks`               | bearer | Users you've blocked                           |
| `PUT`    | `/api/me/presence`             | bearer | Set your presence `{presence}` (`online`/`idle`/`dnd`/`invisible`) |
| `GET`    | `/api/channels`                | bearer | List public channels                           |
| `POST`   | `/api/channels`                | bearer | Create a channel `{name}` (2–32 `[a-z0-9_-]`)  |
| `PATCH`  | `/api/channels/{id}`           | bearer | Channel settings `{postPolicy?, topic?, slowmodeSeconds?}` (admin) |
| `GET`    | `/api/channels/{id}/threads`   | bearer | List the channel's threads                     |
| `POST`   | `/api/channels/{id}/threads`   | bearer | Start a thread `{name}` off the channel        |
| `POST`   | `/api/channels/{id}/read`      | bearer | Mark a channel read (clears its unread/mention badge) |
| `GET`    | `/api/unreads`                 | bearer | Your channels with unread + @mention counts    |
| `POST`   | `/api/channels/{id}/mute`      | bearer | Mute a channel (stops its unread/mention/tab badge) |
| `DELETE` | `/api/channels/{id}/mute`      | bearer | Unmute a channel                               |
| `GET`    | `/api/muted-channels`          | bearer | Ids of the channels you've muted               |
| `GET`    | `/api/dms`                     | bearer | List your direct-message channels              |
| `POST`   | `/api/dms`                     | bearer | Open/get a DM with `{username}`                |
| `POST`   | `/api/dms/group`               | bearer | Start a group DM with `{identifiers:[…]}`       |
| `GET`    | `/api/servers`                 | bearer | List servers you belong to                     |
| `POST`   | `/api/servers`                 | bearer | Create a server `{name}` (you auto-join as owner) |
| `PATCH`  | `/api/servers/{id}`            | bearer | Rename a server `{name}` (admin)               |
| `DELETE` | `/api/servers/{id}`            | bearer | Delete a server + its content (owner)          |
| `POST`   | `/api/servers/{id}/leave`      | bearer | Leave a server (non-owner members)             |
| `POST`   | `/api/servers/{id}/transfer`   | bearer | Transfer ownership `{userId}` (owner)          |
| `GET`    | `/api/servers/{id}/channels`   | bearer | A server's channels (members only)             |
| `POST`   | `/api/servers/{id}/channels`   | bearer | Create a channel `{name, categoryId?}` (admin) |
| `GET`    | `/api/servers/{id}/categories` | bearer | A server's channel categories (members only)   |
| `POST`   | `/api/servers/{id}/categories` | bearer | Create a category `{name}` (admin)             |
| `DELETE` | `/api/servers/{id}/categories/{catId}` | bearer | Delete a category; channels survive uncategorized (admin) |
| `GET`    | `/api/servers/{id}/members`    | bearer | List members with roles + presence (members only) |
| `POST`   | `/api/servers/{id}/roles`      | bearer | Set a member's role `{userId, role}` (owner)   |
| `GET`    | `/api/servers/{id}/custom-roles` | bearer | List the server's colored roles (members)    |
| `POST`   | `/api/servers/{id}/custom-roles` | bearer | Create a colored role `{name, color}` (admin) |
| `PATCH`  | `/api/servers/{id}/custom-roles/{roleId}` | bearer | Edit a colored role `{name, color}` (admin) |
| `DELETE` | `/api/servers/{id}/custom-roles/{roleId}` | bearer | Delete a colored role (admin)        |
| `PUT`    | `/api/servers/{id}/members/{userId}/custom-roles/{roleId}` | bearer | Assign a role to a member (admin) |
| `DELETE` | `/api/servers/{id}/members/{userId}/custom-roles/{roleId}` | bearer | Unassign a role (admin)           |
| `DELETE` | `/api/servers/{id}/members/{userId}` | bearer | Kick a member (admin; live-evicted)      |
| `GET`    | `/api/servers/{id}/bans`       | bearer | List bans (admin)                              |
| `POST`   | `/api/servers/{id}/bans`       | bearer | Ban a member `{userId, reason?}` (admin)       |
| `DELETE` | `/api/servers/{id}/bans/{userId}` | bearer | Unban (admin)                              |
| `POST`   | `/api/servers/{id}/timeouts`   | bearer | Timeout/mute a member `{userId, durationSeconds}` (admin) |
| `DELETE` | `/api/servers/{id}/timeouts/{userId}` | bearer | Clear a timeout (admin)                 |
| `GET`    | `/api/servers/{id}/invites`    | bearer | List active invite codes (admin)               |
| `POST`   | `/api/servers/{id}/invites`    | bearer | Mint an invite `{maxUses?}` (members; 7-day expiry) → `{code}` |
| `DELETE` | `/api/servers/{id}/invites/{code}` | bearer | Revoke an invite code (admin)              |
| `POST`   | `/api/invites/{code}`          | bearer | Redeem an invite → join the server             |
| `GET`    | `/api/messages?channel=<id>`   | bearer | Recent history (DM/server channels: members only) |
| `POST`   | `/api/messages`                | bearer | Send a message with file/image attachments (multipart: `channelId`, `body?`, `files`) |
| `GET`    | `/api/messages/search?channel=<id>&q=` | bearer | Search a channel (`from:`/`has:`/`before:`/`after:` operators) |
| `GET`    | `/api/messages/pins?channel=<id>` | bearer | List a channel's pinned messages            |
| `PATCH`  | `/api/messages/{id}`           | bearer | Edit your own message `{body}`                 |
| `DELETE` | `/api/messages/{id}`           | bearer | Delete a message (author, or a server admin)   |
| `PUT`    | `/api/messages/{id}/pin`       | bearer | Pin a message (admin in a server channel)      |
| `DELETE` | `/api/messages/{id}/pin`       | bearer | Unpin a message (admin)                        |
| `PUT`    | `/api/messages/{id}/reactions` | bearer | React with an emoji `{emoji}`                   |
| `DELETE` | `/api/messages/{id}/reactions/{emoji}` | bearer | Remove your reaction                   |
| `GET`    | `/api/attachments/{id}`        | bearer | Download an attachment (access-gated to the channel's members) |
| `POST`   | `/api/avatar`                  | bearer | Set your own avatar (multipart `file`, image ≤2 MiB)            |
| `GET`    | `/api/users/{id}/avatar`       | bearer | A user's avatar image (404 → client shows initials)            |
| `POST`   | `/api/servers/{id}/emoji`      | bearer | Upload a custom server emoji (multipart `name`, `file`, image ≤256 KiB) (admin) |
| `GET`    | `/api/servers/{id}/emoji`      | bearer | A server's custom emoji (members only)                         |
| `DELETE` | `/api/servers/{id}/emoji/{emojiId}` | bearer | Delete a custom server emoji (admin)                      |
| `GET`    | `/api/emoji/{id}`              | bearer | A custom emoji's image (any authed user)                       |
| `POST`   | `/api/voice/token`             | bearer | Mint a LiveKit SFU token for a voice channel (when `OPENCORD_SFU_URL` is set) |
| `WS`     | `/ws?token=<jwt>&channel=<id>` | token  | Real-time channel (history · send · typing · voice signaling) |

**WebSocket protocol.** Server→client frames are JSON envelopes keyed by `type`:
`history`, `message`, `message-edited`, `message-deleted`, `reaction`, `typing`,
`presence`, `error`, the server-membership events `server-removed` / `server-renamed`,
and voice signaling (`voice-join` / `voice-leave` / `voice-signal` / `voice-screen`).
Connecting to a **DM** or **server** channel requires membership — a non-member's
upgrade and REST history requests are refused with `403`. Client→server:
`{ "body": "hello", "replyTo"?: <id> }` to send, `{ "type": "typing" }` to signal
typing, or a `voice-*` frame for WebRTC signaling. Inbound frames are size-bounded
(Rule B) and rate-limited per connection (a text bucket, burst 5 @ 2/s, plus a
separate, more generous bucket for bursty voice signaling).

## Project layout

```
cmd/server/        main — wires config, db, auth, ws, http
internal/
  config/          env-driven settings
  db/              pgx pool + embedded idempotent schema
  auth/            bcrypt + JWT + middleware + handlers
  chat/            channels + messages: persistence, edit/delete, history
  ws/              per-channel hub + client pumps (typing, rate limit)
  httpapi/         chi router
web/               React + Vite client
docker/            server + web (nginx) Dockerfiles + nginx.conf
docker-compose.yml one-command stack
```

## License

[MIT](./LICENSE). (If you'd prefer copyleft to keep network forks open, AGPL-3.0
is a drop-in swap — open an issue.)
