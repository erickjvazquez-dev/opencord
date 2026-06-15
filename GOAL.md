# Opencord — Goals & Roadmap

**North Star:** A fully open-source, self-hostable Discord alternative anyone can
run locally at little to no cost — built toward **full feature parity with
Discord**: every feature Discord has, eventually, owned by the people who run it.
The exhaustive target list lives in "## Discord Feature Parity" below.

---

## Blockers
<!-- P0 items added here by /qa and /self-improve when critical bugs are found -->

- [x] **P1 (loop-process): the health-gate `go test ./...` silently skipped every DB/WS
  integration test** (no `DATABASE_URL`) — a false-green gate. Fixed iter 61: `scripts/test.sh`
  boots the compose Postgres, runs the full suite (integration tests now execute), and tears
  it down; `make test` + `CCF_TEST_CMD` point at it. CI already ran them (sets `DATABASE_URL`);
  this brings the local/loop gate to parity. Verified: 20+ `*Integration` tests now RUN, not skip.
- [x] **P1 (UI/QA): the in-voice bar had no mobile-viewport check.** Verified iter 64: at 390px the bar
  wraps cleanly onto stacked rows — every control readable + tappable, **zero horizontal overflow** (AI-vision
  + an objective `scrollWidth <= clientWidth` assertion). Flex-wrap handles it; **no redesign needed**
  (anti-churn). Added a durable ≤640px check to `qa/voice.mjs` (+ `voice-04-mobile.png`) so it stays covered.

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

- [~] Voice channels (audio) — **NORTH STAR (owner-set 2026-06-14): thousands of
  participants per call with no fidelity loss, disconnects, or choppiness, using the
  best open-source tech, free to self-host.** Path: (1) ✅ WS signaling relay; (2) ✅
  **mesh WebRTC client for small calls (2–4)** — one RTCPeerConnection per peer with
  perfect-negotiation glare handling, crisp capture DSP (echo cancellation + noise
  suppression + auto-gain, 48 kHz, ~96 kbps Opus, above Discord's default), **device
  auto-detect** (defaults to the OS's active mic/headset and auto-follows on
  plug/unplug) plus a **manual mic + output picker** (hot-swaps the track with no
  renegotiation; `setSinkId` routes output); two-browser E2E proves connectionState
  `connected` + live remote audio (qa/voice.mjs); (3) **open-source SFU — DECIDED:
  LiveKit** (Apache-2, Go + first-class server SDK, free single-node self-host;
  `stack-guardian` APPROVE 2026-06-14 over mediasoup/Janus). Strictly **opt-in**
  (`OPENCORD_SFU_URL` empty ⇒ mesh; one-command stack stays SFU-free — Rule A);
  server mints room=channel tokens from the verified JWT, gated by `CanAccessChannel`
  (Rule B/C). Plan in SPEC "mesh → OSS SFU (LiveKit) scale path". Built: ✅ token
  endpoint; ✅ real-LiveKit acceptance proven; ✅ **client SFU path** (`web/src/sfu.ts`
  `SfuSession` over lazy-imported `livekit-client`; `joinVoice` picks SFU when the
  server offers a token, else mesh; transport-agnostic voice-bar; two-browser SFU E2E
  via `qa/sfu-run.sh`). **Mesh ↔ SFU both work E2E.** ✅ **active-speaker selection**
  (`autoSubscribe:false` + top-N loudest audio subscription, `selectAudioSubscriptions`
  pure fn, vitest-tested) so a huge room never mixes every stream. Next: optional
  self-host TURN (hostile NATs), cascaded SFUs for true thousands-scale. Railway
  demo instance DEFERRED (Railway is TCP-only + egress-unbounded). With
  **active-speaker selection** (forward only the top-N loudest) this reaches
  thousands-scale audio; (4) distributed/cascaded SFUs + optional self-hosted TURN.
  Signaling is hardened: a dedicated voice rate bucket bounds a flood (Rule 15),
  and the WS send channel is close-race-proof (`done`-channel, `-race` clean).
  **Active-speaker indicator shipped** (client-side Web Audio VAD → green speaking
  ring on local + remote chips; E2E + AI-vision verified). **Per-user volume shipped**
  (a local-only slider per peer → `HTMLAudioElement.volume`; E2E verified).
  **Push-to-talk shipped** (press-and-hold Talk button gates the mic via
  `track.enabled`, supersedes mute; E2E + AI-vision verified).
  *Next voice polish: deafen, global PTT hotkey.*
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
- [~] **Slowmode** (per-channel post cooldown, admin-set, server-enforced, 🐌 badge —
  E2E + adversarial test) · NSFW gating · announcement channels (done: read-only
  policy) + following · channel topic (done)
- [ ] Threads + archived threads · pinned messages

### Messaging
- [x] Send / receive in real time · edit / delete (owner-only) · typing indicators
- [x] Reactions (emoji) — add/remove, per-viewer counts, live WS, client UI (quick palette + chips)
- [ ] Custom emoji · stickers · GIF picker
- [x] Markdown — bold/italic/strikethrough, inline & fenced code, `> ` blockquotes,
  `||spoilers||` (click to reveal), `- `/`1. ` lists, and autolinked URLs; XSS-safe
  (React elements, no innerHTML); E2E + AI-vision verified.
- [~] Mentions — `@user` chips (your own highlighted), plus `@everyone`/`@here`
  highlighted as all-mentions; E2E + AI-vision verified. (@role, autocomplete,
  delivery/notifications, replies, threads still TODO)
- [ ] File / image / video attachments · link embeds + previews
- [~] Pinned messages — pin/unpin (admin-gated in server channels), 📌 badge, live
  WS update, and a "pins" panel listing all of a channel's pins; E2E + AI-vision
  verified. (bookmarks, read state / unread / mention badges TODO)
- [ ] Message search (from/in/has/before/after) · polls · timestamp grouping

### Voice / Video
- [~] Voice channels — mesh WebRTC audio for small calls (2–4): join/leave, live
  roster with per-peer connection state, crisp DSP, device auto-detect + picker,
  mute (E2E verified). SFU for scale + video calls still TODO.
- [ ] Screen share / Go Live · soundboard
- [~] Voice controls — noise suppression, mute, voice-activity/speaking indicator,
  per-user volume, and push-to-talk shipped; deafen · global PTT hotkey still TODO

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
- **Per-component excellence (owner-set 2026-06-14):** every component has its own
  north star and the loop drives each toward it continuously — not just "feature
  exists" but "best-in-class." Audio → thousands/HD/no-drops/free (above); chat →
  instant + lossless realtime; infra → one-command + scales; UI → polished, fast,
  accessible; security → hostile-input-proof. Use the best open-source tech for each;
  everything must stay free to self-host.
