# Opencord — Goals & Roadmap

**North Star:** A fully open-source Discord alternative anyone can run **for free**,
owned by the people who use it. Two ways to run it, same app:

1. **Local / self-hosted (always free, nothing behind a paywall).** `git clone` +
   one command and you have your own server. To let friends on other computers join
   without manual port-forwarding, Opencord ships **built-in secure tunneling** (an
   optional, free, self-hostable relay/tunnel so a local server is reachable over the
   internet securely). Every feature works here — no feature gating, ever.
2. **Cloud Opencord (the only paid option).** For a small fee, a managed 24/7 hosted
   account with all services always-on, so you don't run anything yourself. Same app,
   same features — you're paying for uptime/hosting, not features.

**Your data is yours.** We keep nothing from users; self-host owns its DB outright,
and Cloud is hosting-only (no mining, no telemetry, no phone-home — Rule A).

**Product bar — match Discord exactly, then exceed it.** The **layout, design, and
features must mirror Discord's** (our own from-scratch implementation of the same UX —
not Discord's proprietary assets/code/trademarks), PLUS the improvements we've already
added (e.g. screen-share audio mixing: sharer→viewers gain, sharer self-monitor, and
per-viewer volume; a rebindable global PTT hotkey). Every tick advances Discord parity
— one feature/surface at a time — toward "indistinguishable from Discord in feel,
better in the details, free and self-ownable." The exhaustive feature list lives in
"## Discord Feature Parity" below; the **business model is fixed: free local + secure
tunnel, paid cloud only for 24/7 hosting.**

---

## Blockers
<!-- P0 items added here by /qa and /self-improve when critical bugs are found -->

- [x] **P1 (UI polish, found+fixed iter 90 via AI-vision QA): the channel header wrapped its text in
  server channels.** With the member-list sidebar present (~220px, shown >900px) the chat column is
  narrow; `.chat-header` had no `flex-wrap`, so its flex items shrank to min-content and wrapped their
  *text* across lines — "Join voice" → 2 lines, "1 online ·" → 3 lines, "log out" → 2 lines, brand →
  2 lines — a cramped, broken-looking bar (the plain `#general` view has the full width and never
  triggered it). Fix: `.chat-header` now wraps with a `row-gap` (same pattern as the voice bar), the
  brand can shrink (`min-width:0`, its topic already ellipsizes), and the action/meta labels stay
  single-line (`white-space:nowrap`); `.meta` is right-anchored (`margin-left:auto`). Header now flows
  into two tidy rows. Regression: `qa/browser.mjs` measures rendered text-line count per control in a
  server channel and asserts each is single-line — **proven to catch it** (reverting the CSS makes
  brand/Join-voice/log-out report 2 lines + horizontal overflow). Full browser+realtime+voice QA green;
  AI-vision verified clean in both single- and two-user server views.

- [x] **P1 (QA gap, found iter 77, closed iter 80): voice-bar ≤640px overflow check skipped the
  PTT-on state.** `qa/voice.mjs` now re-checks overflow at 390px WITH PTT on (widest controls:
  `Hold to talk` + `key:` rebind), asserting `scrollWidth <= clientWidth`, Talk + key reachable,
  + `voice-06-ptt-mobile.png` AI-vision verified (clean wrap, no clip). 3 new checks, QA green.

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
- [x] Profiles: initials avatars + **uploaded avatars** (local-disk, access-gated
  serve, Avatar component renders the image or falls back to initials everywhere;
  header click-to-upload; Rule-15 hardened + vision-verified). Banners/status TODO
- [x] Servers/guilds — servers + members + server-scoped channels (per-server names, members-only access adversarially verified at HTTP+WS) + sidebar Servers accordion (create/join server, create channel, chat) — backend + UI, E2E verified
- [x] Direct messages — DM channels + membership + per-channel access control (WS/REST 403 for non-members, adversarially verified) + sidebar DM list, new-DM flow, DM-aware header/composer (two-user E2E verified)

## Next (v0.3 — Roles & Polish)

- [x] Rate limiting + abuse protection — per-connection WS token bucket (Rule 15)
- [~] Roles & permissions — server roles (owner/admin/member); roles UI (members panel + owner promote/demote); admin-gated channel creation; **message moderation** (admins delete others' messages) with a delete-button UI shown to admins in server channels — all two-user E2E verified; **read-only / announcement channels** (per-channel posting policy: only admins post, WS-enforced) with an admin toggle, a 🔒 badge, and a disabled composer for non-admins — all E2E verified. (A full per-role permission matrix is future polish beyond MVP parity.)
- [x] Invites — invite-code join (replaces the open join-by-id gap): members mint codes, redeeming admits you; non-member can't mint/guess (403/404), adversarially verified. (Membership mgmt: roles done; **kick done** — owner/admin, with live WS eviction; ban/timeout TODO)
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
  `track.enabled`, supersedes mute; E2E + AI-vision verified). **Deafen shipped**
  (silences all incoming audio + forces the mic off; E2E verified). **Global PTT
  hotkey shipped** (hold a bound key — default `` ` ``, rebindable + persisted —
  anywhere to talk; stands down while typing; E2E + AI-vision verified).
  *Next voice polish: screen share, video, soundboard.*
- [~] Screen share — mesh: share screen (getDisplayMedia, configured for up to 4K@60 —
  contentHint detail, 8 Mbps, maintain-resolution) with optional system/tab audio; live
  video tiles for every viewer; **screen-audio mixing** — sharer scales the level sent to all
  viewers (Web Audio gain) + a local self-monitor (default off), and each viewer has a
  per-share playback volume independent of voice. `voice-screen` WS frame (Rule-B bounded).
  Two-client E2E (B receives a live video track) + AI-vision verified. SFU path + true
  thousands-scale screen share still TODO.
- [~] File/image uploads — **DONE for messages**: attach files/images to a message
  (composer 📎 → multi-file staging → send). Stored on **local disk** under
  `OPENCORD_UPLOAD_DIR` (Rule A, no object store), opaque random keys (no path
  traversal), sniffed content type + nosniff + attachment-disposition for non-images
  (no inline script), access-gated serve (`CanAccessChannel`, non-member 403). Images
  render inline (fetched via authed blob so the JWT never hits an `<img src>`), other
  files as a download chip. Limits: ≤10 files, ≤8 MiB each, ≤40 MiB/request.
  Adversarial-tested (Rule 15) + browser-QA + AI-vision verified. TODO: link
  embeds/previews, video transcode, durable/managed media store (Cloud tier).
- [ ] Federation / multi-instance
- [ ] Plugin/bot API

---

## Platform & hosting (business model — see North Star)

- [~] **Voice/screen reliability across networks** — WS auto-reconnect SHIPPED (chat +
  screen-share signaling survive socket drops; real offline→online E2E). TODO: optional
  **self-hostable TURN** (`OPENCORD_TURN_*` env → RTC iceServers, degrades to STUN; free,
  Rule A) for hostile NATs / higher-bitrate screen video.
- [ ] **Built-in secure tunneling (free, local)** — let friends on other computers reach a
  self-hosted server without manual port-forwarding: an optional, free, self-hostable
  relay/tunnel (e.g. bundled reverse-tunnel) — "creating local servers for you and your
  friends with secure tunneling, nothing behind a paywall." Must stay free + self-hostable.
- [ ] **Real accounts with email** — add an email to accounts (register/login, unique,
  bcrypt unchanged). Unlocks **invite/DM by email** (the lookup is already identifier-based:
  username + user id work today; email is the one-line `WHERE email=$1` branch once stored)
  and is the basis for password reset + optional SSO.
- [ ] **Cloud Opencord (the only paid tier) — charge for OPERATIONS, never features.** A
  fully-local user pays nothing and loses no features; the paid tier sells "we host it, secure
  it, back it up, keep it 24/7" — recurring ops + bandwidth, not locked software. The managed
  bundle: always-on hosting (we patch/restart); **managed secure connectivity** (TURN + tunnel
  across any NAT, TLS + DDoS protection — also where egress cost lives, so cost tracks revenue);
  **encrypted backups + point-in-time restore**; **security ops** (auto-patching, abuse/spam
  protection, monitoring, audit logs); managed TLS + custom domain. Teams/orgs: SSO/SAML/2FA/
  SCIM, managed SFU at scale, hosted media+CDN, compliance/retention/SLA. Dividing line is fixed:
  **local = every feature free + your own TURN/SFU/tunnel/backups on your box; cloud = we run +
  secure + keep it online.** Data stays the user's either way; no mining/telemetry (Rule A).

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
- [~] Server boosts / tiers (cosmetic) TODO · **member list DONE** — Discord-style
  right sidebar in server channels, grouped by role (Admins/Members) with avatars +
  role badges + **per-member online presence** (green dot on online members via
  `Hub.OnlineUserIDs`, offline dimmed; -race tested + AI-vision verified), polled for
  join/leave/promote (live member-joined + live presence broadcast are follow-ups;
  idle/DnD states TODO); hidden ≤900px. E2E + AI-vision verified.
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
  highlighted as all-mentions; **`@`-autocomplete** (typing `@`+partial offers
  channel-active usernames; ↑/↓ to move, Enter/Tab to accept, Esc to dismiss,
  click-to-insert) — E2E + AI-vision verified. (@role, delivery/notifications,
  threads still TODO)
- [~] File / image attachments — DONE (composer 📎, multipart upload, local-disk
  store, access-gated serve, inline images + download chips; Rule-15 hardened +
  vision-verified incl. **two-client live propagation** — B sees A's upload render
  live, fetched with B's own token). Video transcode · link embeds + previews TODO
- [~] Pinned messages — pin/unpin (admin-gated in server channels), 📌 badge, live
  WS update, and a "pins" panel listing all of a channel's pins; E2E + AI-vision
  verified. (bookmarks, read state / unread / mention badges TODO)
- [ ] Message search (from/in/has/before/after) · polls · timestamp grouping

### Voice / Video
- [~] Voice channels — mesh WebRTC audio for small calls (2–4): join/leave, live
  roster with per-peer connection state, crisp DSP, device auto-detect + picker,
  mute (E2E verified). SFU for scale + video calls still TODO.
- [~] Screen share (mesh, up to 4K@60, with system audio + per-side audio-level
  controls) — done; Go Live · soundboard still TODO
- [x] Voice controls — noise suppression, mute, voice-activity/speaking indicator,
  per-user volume, push-to-talk, deafen, and a rebindable global PTT hotkey all shipped

### Direct messages
- [ ] 1:1 DMs · group DMs · friends / friend requests / blocking

### Users / Profiles
- [x] Avatars (initials)
- [~] Uploaded avatars — DONE (image upload, access-gated serve, renders everywhere
  via the Avatar component, header upload entry). Banners · custom status + activity
  ("playing X") still TODO
- [ ] About me / pronouns / connections · per-server nicknames
- [~] Presence: **online/offline DONE** (green dot in the member list, hub-backed; -race
  tested + AI-vision verified) · idle / DnD / invisible TODO

### Roles & Permissions
- [ ] Roles (hierarchy, colors, icons, mentionable)
- [ ] Granular permissions (server + per-channel overrides)

### Moderation
- [~] **Kick DONE** — owner/admin removes a member (`DELETE /servers/{id}/members/{userId}`);
  owner kicks any non-owner, admin kicks members only, nobody kicks the owner/self
  (authz matrix tested). Security: the kicked user's live WS sockets are evicted
  (`Hub.EvictUserFromChannels`) so they stop receiving immediately — proven by a
  `-race` ws integration test + browser QA (online count drops on kick). Red kick
  button in the members panel; two-user E2E + AI-vision verified. · ban / timeout ·
  bulk delete · AutoMod (keyword/spam) · reporting TODO
- [x] Message moderation (admins delete others' messages) — shipped (v0.3)

### Notifications
- [ ] Per-channel/server settings + mute · @mention + DM notifications · web push

### Platform / Integrations
- [ ] Bot/API + webhooks + slash commands · OAuth2 app authorization
- [~] **Accessibility** — WCAG-AA color contrast fixed (links/mentions/green labels/
  avatar initials) + axe-core WCAG scan in the browser QA (0 serious/critical, regression-
  guarded). Keyboard-nav · screen-reader · i18n · theme (dark/light) still TODO
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
