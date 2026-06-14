# Opencord — Goals & Roadmap

**North Star:** A fully open-source, self-hostable Discord alternative anyone can
run locally at little to no cost — built toward **full feature parity with
Discord**: every feature Discord has, eventually, owned by the people who run it.
The exhaustive target list lives in "## Discord Feature Parity" below.

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
- [x] Verify end-to-end in a browser (two users, live message) — Rule 14 (qa/realtime.mjs: 2 contexts, live message + reaction + presence)
- [x] Push to GitHub + CI (build + `go test`)

## Next (v0.2 — Structure)

- [x] Multiple channels — table, REST list + create, per-channel WS routing, sidebar UI (E2E + DB integration tests in CI)
- [x] Message edit/delete (owner-only, live WS, soft delete)
- [x] Typing indicators — read state still TODO
- [x] Profiles: initials avatars — uploaded avatars/banners/status still TODO
- [x] Servers/guilds — servers + members + server-scoped channels (per-server names, members-only access adversarially verified at HTTP+WS) + sidebar Servers accordion (create/join server, create channel, chat) — backend + UI, E2E verified
- [x] Direct messages — DM channels + membership + per-channel access control (WS/REST 403 for non-members, adversarially verified) + sidebar DM list, new-DM flow, DM-aware header/composer (two-user E2E verified)

## Next (v0.3 — Roles & Polish)

- [x] Rate limiting + abuse protection — per-connection WS token bucket (Rule 15)
- [~] Roles & permissions — server roles (owner/admin/member); roles UI (members panel + owner promote/demote); admin-gated channel creation; **message moderation** (admins delete others' messages) with a delete-button UI shown to admins in server channels — all two-user E2E verified; **read-only / announcement channels** (per-channel posting policy: only admins post, WS-enforced) with an admin toggle, a 🔒 badge, and a disabled composer for non-admins — all E2E verified. (A full per-role permission matrix is future polish beyond MVP parity.)
- [x] Invites — invite-code join (replaces the open join-by-id gap): members mint codes, redeeming admits you; non-member can't mint/guess (403/404), adversarially verified. (Membership mgmt: kick/roles still TODO)
- [x] Search — in-channel message search (case-insensitive, access-gated, LIKE-wildcards escaped per Rule B), header search box + results panel (channel-spanning search later)
- [x] Message grouping (collapse consecutive same-author messages within 5 min) — flagged then closed by browser AI-vision QA 2026-06-13
- [x] Mobile-responsive layout — sidebar collapses into an off-canvas drawer behind a header menu toggle (≤640px); chat goes full-width; backdrop + close-on-select (E2E + vision verified)

## Later

- [ ] Voice/video channels (WebRTC + SFU, e.g. mediasoup)
- [ ] Screen share
- [ ] File/image uploads + media proxy
- [ ] Federation / multi-instance
- [ ] Plugin/bot API

---

## Discord Feature Parity — the full target

The north star is **every feature Discord has**, open-source and self-hostable.
This is the exhaustive backlog (✓ = shipped); the loop pulls the next highest-value
item from here as the structural milestones above land.

### Servers / Guilds
- [ ] Create/join servers (guilds) + server settings
- [ ] Channel categories + ordering
- [ ] Invites (links, max-uses, expiry, temporary membership)
- [ ] Server discovery / community servers · vanity invite URLs
- [ ] Welcome screen + rules screening · server templates
- [ ] Server boosts / tiers (cosmetic) · member list
- [ ] Audit log · scheduled events

### Channels
- [x] Text channels (create, list, switch)
- [ ] Voice channels · stage channels · forum channels
- [ ] Categories · per-channel permissions · channel topic
- [ ] Slowmode · NSFW gating · announcement channels + following
- [ ] Threads + archived threads · pinned messages

### Messaging
- [x] Send / receive in real time · edit / delete (owner-only) · typing indicators
- [x] Reactions (emoji) — add/remove, per-viewer counts, live WS, client UI (quick palette + chips)
- [ ] Custom emoji · stickers · GIF picker
- [x] Markdown — bold/italic/strikethrough, inline & fenced code, `> ` blockquotes,
  and `||spoilers||` (click to reveal); XSS-safe (React elements, no innerHTML);
  E2E + AI-vision verified.
- [ ] Mentions @user/@role/@everyone/@here (+ notifications) · replies · threads
- [ ] File / image / video attachments · link embeds + previews
- [ ] Pinned + bookmarked messages · read state / unread / mention badges
- [ ] Message search (from/in/has/before/after) · polls · timestamp grouping

### Voice / Video
- [ ] Voice channels (WebRTC + SFU, e.g. mediasoup) · video calls
- [ ] Screen share / Go Live · soundboard
- [ ] Push-to-talk · voice activity · noise suppression · per-user volume/mute/deafen

### Direct messages
- [ ] 1:1 DMs · group DMs · friends / friend requests / blocking

### Users / Profiles
- [x] Avatars (initials)
- [ ] Uploaded avatars + banners · custom status + activity ("playing X")
- [ ] About me / pronouns / connections · per-server nicknames
- [ ] Presence: online / idle / DnD / offline / invisible

### Roles & Permissions
- [ ] Roles (hierarchy, colors, icons, mentionable)
- [ ] Granular permissions (server + per-channel overrides)

### Moderation
- [ ] Kick / ban / timeout · bulk delete · AutoMod (keyword/spam) · reporting

### Notifications
- [ ] Per-channel/server settings + mute · @mention + DM notifications · web push

### Platform / Integrations
- [ ] Bot/API + webhooks + slash commands · OAuth2 app authorization
- [ ] Theme (dark/light) · accessibility · i18n · keyboard shortcuts
- [ ] Desktop + mobile clients (PWA first)

---

## Non-negotiables (carry every milestone)

- Self-hostable with one command; no required paid service.
- Treat every inbound payload as hostile (validate, bound, reject).
- Every fix/feature verified end-to-end in the live app before "done" (Rule 14).
