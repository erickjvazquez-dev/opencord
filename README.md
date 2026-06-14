# Opencord

**A fully open-source, self-hostable Discord alternative that anyone can run locally at little to no cost.**

Opencord is built to be owned by the people who run it: one Go binary, a static
web client, and Postgres. No SaaS, no telemetry, no per-seat pricing — clone it,
run one command, and you have your own real-time chat server.

> **Status:** v0.2 — *in progress*. The realtime core is solid and growing.
> DMs, servers/guilds, roles, and voice are on the roadmap (see
> [`GOAL.md`](./GOAL.md)).
>
> **Working today:** accounts (bcrypt + JWT) · multiple channels with per-channel
> real-time routing (create + switch in a sidebar) · **servers/guilds** (group
> channels under a members-only server) · **direct messages** (private, members-only) ·
> live messaging with **edit & delete** (owner-only) · **emoji reactions** (live
> counts) · **grouped messages** (consecutive same-author) · **typing indicators** ·
> online presence · **initials avatars** · **mobile-responsive** (sidebar drawer) ·
> per-connection **rate limiting** · one-command Docker stack · CI with a Postgres
> service running DB integration tests.

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

| Variable        | Default                          | Purpose                              |
| --------------- | -------------------------------- | ------------------------------------ |
| `OPENCORD_ADDR` | `:8080`                          | Listen address                       |
| `DATABASE_URL`  | local Postgres DSN               | Postgres connection string           |
| `JWT_SECRET`    | `dev-insecure-change-me`         | **Change in production.** Signs JWTs |
| `CORS_ORIGIN`   | `*`                              | Allowed REST origin                  |

## API surface

| Method   | Path                           | Auth   | Description                                    |
| -------- | ------------------------------ | ------ | ---------------------------------------------- |
| `GET`    | `/healthz`                     | —      | Liveness + version                             |
| `POST`   | `/api/auth/register`           | —      | Create account → `{token, user}`               |
| `POST`   | `/api/auth/login`              | —      | Log in → `{token, user}`                       |
| `GET`    | `/api/auth/me`                 | bearer | Current user                                   |
| `GET`    | `/api/channels`                | bearer | List public channels                           |
| `POST`   | `/api/channels`                | bearer | Create a channel `{name}` (2–32 `[a-z0-9_-]`)  |
| `GET`    | `/api/dms`                     | bearer | List your direct-message channels              |
| `POST`   | `/api/dms`                     | bearer | Open/get a DM with `{username}`                |
| `GET`    | `/api/servers`                 | bearer | List servers you belong to                     |
| `POST`   | `/api/servers`                 | bearer | Create a server `{name}` (you auto-join)       |
| `POST`   | `/api/servers/{id}/invites`    | bearer | Mint an invite code (members only) → `{code}`  |
| `POST`   | `/api/invites/{code}`          | bearer | Redeem an invite → join the server             |
| `GET`    | `/api/servers/{id}/channels`   | bearer | A server's channels (members only)             |
| `POST`   | `/api/servers/{id}/channels`   | bearer | Create a channel in a server (members only)    |
| `GET`    | `/api/messages?channel=<id>`   | bearer | Recent history (DM channels: members only)     |
| `PATCH`  | `/api/messages/{id}`           | bearer | Edit your own message `{body}`                 |
| `DELETE` | `/api/messages/{id}`           | bearer | Delete your own message (soft)                 |
| `PUT`    | `/api/messages/{id}/reactions` | bearer | React with an emoji `{emoji}`                   |
| `DELETE` | `/api/messages/{id}/reactions/{emoji}` | bearer | Remove your reaction                   |
| `WS`     | `/ws?token=<jwt>&channel=<id>` | token  | Real-time channel (history · send · typing)    |

**WebSocket protocol.** Server→client frames are JSON envelopes keyed by `type`:
`history`, `message`, `message-edited`, `message-deleted`, `reaction`, `typing`,
`presence`. Connecting to a **DM** channel (or any future private channel) requires
membership — a non-member's upgrade and REST history requests are refused with `403`.
Client→server: `{ "body": "hello" }` to send, or `{ "type": "typing" }` to signal
typing. Inbound frames are rate-limited per connection (burst 5, 2/s).

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
