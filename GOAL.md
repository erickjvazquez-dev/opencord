# Opencord — Goals & Roadmap

**North Star:** A fully open-source, self-hostable Discord alternative anyone can
run locally at little to no cost.

---

## Blockers
<!-- P0 items added here by /qa and /self-improve when critical bugs are found -->

---

## Now (v0.1 — Minimal Realtime MVP)

- [x] Accounts — register/login, bcrypt, JWT sessions
- [x] One global `#general` channel
- [x] WebSocket gateway — history on connect + live broadcast + presence
- [x] Postgres persistence (self-migrating schema)
- [x] `docker compose up` one-command stack
- [ ] Verify end-to-end in a browser (two users, live message) — Rule 14
- [x] Push to GitHub + CI (build + `go test`)

## Next (v0.2 — Structure)

- [x] Multiple channels — table, REST list + create, per-channel WS routing, sidebar UI (v0.2; E2E-verified + DB integration tests in CI)
- [ ] Servers/guilds (group channels under a server; membership)
- [ ] Direct messages
- [ ] Message edit/delete + timestamps grouping
- [ ] Typing indicators + read state
- [ ] Profiles + avatars

## Next (v0.3 — Roles & Polish)

- [ ] Roles & permissions (owner/admin/member, per-channel)
- [ ] Invites + membership management
- [ ] Search
- [ ] Rate limiting + abuse protection (Rule 15 hardening pass)
- [ ] Mobile-responsive layout

## Later

- [ ] Voice/video channels (WebRTC + SFU, e.g. mediasoup)
- [ ] Screen share
- [ ] File/image uploads + media proxy
- [ ] Federation / multi-instance
- [ ] Plugin/bot API

---

## Non-negotiables (carry every milestone)

- Self-hostable with one command; no required paid service.
- Treat every inbound payload as hostile (validate, bound, reject).
- Every fix/feature verified end-to-end in the live app before "done" (Rule 14).
