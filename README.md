# Opencord

**A fully open-source, self-hostable Discord alternative that anyone can run locally at little to no cost.**

Opencord is built to be owned by the people who run it: one Go binary, a static
web client, and Postgres. No SaaS, no telemetry, no per-seat pricing — clone it,
run one command, and you have your own real-time chat server.

> **Status:** v0.1 — *minimal realtime MVP*. Accounts + one global `#general`
> channel over WebSocket, to prove the realtime loop end-to-end. Servers,
> multiple channels, DMs, roles, and voice are on the roadmap (see
> [`GOAL.md`](./GOAL.md)).

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

## API surface (v0.1)

| Method | Path                  | Auth   | Description                          |
| ------ | --------------------- | ------ | ------------------------------------ |
| `GET`  | `/healthz`            | —      | Liveness + version                   |
| `POST` | `/api/auth/register`  | —      | Create account → `{token, user}`     |
| `POST` | `/api/auth/login`     | —      | Log in → `{token, user}`             |
| `GET`  | `/api/auth/me`        | bearer | Current user                         |
| `GET`  | `/api/messages`       | bearer | Recent history (REST)                |
| `WS`   | `/ws?token=<jwt>`     | token  | Real-time channel (history + send)   |

WebSocket frames are JSON envelopes: `{type: "history"|"message"|"presence"}`.
Clients send `{ "body": "hello" }`.

## Project layout

```
cmd/server/        main — wires config, db, auth, ws, http
internal/
  config/          env-driven settings
  db/              pgx pool + embedded idempotent schema
  auth/            bcrypt + JWT + middleware + handlers
  chat/            message persistence + history
  ws/              hub + per-connection client pumps
  httpapi/         chi router
web/               React + Vite client
docker/            server + web (nginx) Dockerfiles + nginx.conf
docker-compose.yml one-command stack
```

## License

[MIT](./LICENSE). (If you'd prefer copyleft to keep network forks open, AGPL-3.0
is a drop-in swap — open an issue.)
