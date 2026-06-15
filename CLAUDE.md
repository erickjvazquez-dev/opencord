# Opencord — Project Instructions

Open-source, self-hostable Discord alternative. Go backend + React/Vite client +
Postgres. This file is the project-specific rule set; it sits **on top of** the
workspace-level Claude Code Framework rules (`~/CLAUDE.md`) — those still apply.

**North Star:** a free, open-source Discord alternative owned by its users, run two
ways from one app: **local/self-hosted (always free, no paywall)** — `git clone` +
one command, with **built-in free secure tunneling** so friends on other computers
can join without manual port-forwarding — or **Cloud Opencord (the only paid tier)**,
a managed 24/7 hosted account (paying for uptime, never for features). **Match Discord
exactly** in layout, design, and features (our own from-scratch implementation of the
same UX — not Discord's assets/code/trademarks), PLUS the improvements we've added
(screen-share audio mixing, global PTT hotkey, …). **Users' data is theirs — we keep
nothing** (no telemetry/phone-home, Rule A). Each tick advances Discord parity. Full
target list + business model in [`GOAL.md`](./GOAL.md).

## Architecture (keep this shape)

- **One Go binary** (`cmd/server`) serves REST (`/api/*`, `/healthz`) and the
  WebSocket gateway (`/ws`). Schema is embedded and self-applied on boot.
- `internal/` is split by concern: `config`, `db`, `auth`, `chat`, `ws`,
  `httpapi`. Cross-cutting realtime fan-out lives in `ws.Hub` (single goroutine
  owns the client set — no locks). Per-channel routing will extend the hub.
- **Web** is a thin SPA; it talks to one origin (nginx proxy in prod, Vite proxy
  in dev). No backend logic in the client.

## Project rules

### Rule A — Self-hostable, zero required cost
Every feature must work on the one-command local stack with no paid third-party
service. If something needs an external service, it must be optional and degrade
gracefully when absent. No telemetry, no phone-home.

### Rule B — Treat every inbound payload as hostile
Every WS frame and HTTP body is attacker-controlled: validate types, bound sizes
(bodies 64 KiB, WS msgs 4 KiB), reject early. Never trust client-supplied user
identity — always derive the user from the verified JWT, never from the payload.

### Rule C — Secrets and auth hygiene
Passwords only ever stored as bcrypt hashes; never logged. `JWT_SECRET` is
required to be overridden for anything but local dev. Auth derives identity from
the token, server-side, on every request and every socket.

### Rule D — Spec before big changes; verify end-to-end before "done"
Anything touching >3 files or adding a user-facing flow gets a note in `SPEC.md`
first. Never claim a fix/feature done without verifying it in the running app
(two browsers for realtime), not just green tests (workspace Rule 14).

### Rule E — Keep the build green and the stack one-command
`go build ./...`, `go vet ./...`, and `go test ./...` must pass. `docker compose
up --build` must serve the app at http://localhost:3000. Don't merge changes that
break either.

## Toolchain note (this machine)

The default `go` (1.11) and `node` (v10, nvm) are too old. Use Homebrew:
`PATH="/opt/homebrew/bin:$PATH"` gives Go 1.26 + Node 23 + npm 10. The project's
`.ccf/project.env` already bakes this into `CCF_TEST_CMD`.

## Roadmap

See [`GOAL.md`](./GOAL.md). Current milestone: **v0.1 minimal realtime MVP**.
