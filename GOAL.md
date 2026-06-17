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

## TOP PRIORITY — UI/UX Discord parity (owner-set 2026-06-17)

**Owner directive (2026-06-17): drive the UI to Discord's polish bar.** Each tick, advance the
highest unchecked item BELOW before pulling from `## Now` / `## Next` / the parity backlog — but
keep doing the loop's normal health/QA/security work *alongside* it (don't drop the gates). Every
UI slice ships the usual way: spec-first (`SPEC.md`), build the minimal Discord-faithful thing,
then **browser QA + AI-vision verify** the rendered result before "done" (Rule 14). Today the
account/voice controls are scattered in the 2604-LOC `Chat.tsx` header + voice bar; there is no
settings surface and the login page is a bare card. Spec: `SPEC.md` "User Settings + UI polish".

- [x] **Login / register page redesign** DONE (iter 128) — `Auth.tsx` + `styles.css`: branded
  "OPENCORD" wordmark, mode-aware heading/subtitle ("Welcome back!" / "Create an account"),
  uppercase field labels, **password show/hide toggle**, inline min-length hint, loading
  **spinner** on submit, disabled-until-valid submit, accessible labels + `role="alert"` error.
  Selectors preserved so realtime/voice QA stays green; new browser-QA assertion exercises the
  toggle. Browser QA `browser=0 realtime=0 voice=0 search=0`; AI-vision verified both modes;
  shipped + `railway up` + rollout verified.
- [x] **User Settings surface (Discord-style)** DONE (iter 129) — new `Settings.tsx`: a fixed
  overlay + modal with a left `settings-nav` tab rail (**My Account** active, **Voice & Video**
  disabled "SOON" placeholder for slice 3). My Account consolidates what was scattered in the
  header: avatar preview (with presence pip) + **Change Avatar**, read-only username, **custom
  status + emoji** (text + emoji inputs + a Save button with a "Saved" confirm — replaces the old
  double `window.prompt` flow), and the presence `<select>`. Header `.meta` cluster collapsed to a
  compact `self-chip` (avatar + presence pip + username + ⚙) that opens it; **Esc + overlay-click +
  ✕** all close. No new endpoints (reuses `PUT /me/status`, `PUT /me/presence`, `POST /avatar`).
  QA migrated in lockstep (selectors moved header→modal; new ⚙-open/Esc-close assertion `07d0`);
  browser QA `browser=0 realtime=0 voice=0 search=0`; AI-vision verified the modal (My Account),
  the status-save flow, the avatar upload, and the mobile header (no overflow). Shipped + `railway
  up` + rollout-verified.
- [~] **Voice & Video settings tab** — slice 3a DONE (iter 130): the **Voice & Video** tab is live
  (no longer a "SOON" placeholder) with **input + output device pickers**, a **mic test /
  input-sensitivity meter** (Web Audio RMS from the selected device — verified responding to the
  fake-mic tone in QA), and **noise-suppression + echo-cancellation + auto-gain toggles**. New
  `web/src/voiceSettings.ts` is the single localStorage source of truth (device ids + DSP flags,
  default-on); `voice.ts audioConstraints` reads the DSP flags (no behavior change until the user
  opts out); the in-call voice-bar pickers + the settings pickers share Chat state + localStorage.
  Browser QA `07d3` (toggles render, persist across a modal remount, meter moves); AI-vision verified
  the panel; shipped + `railway up` + rollout-verified. **Slice 3b DONE (iter 131): camera device +
  live preview** — a videoinput picker (persisted in `voiceSettings`) + a mirrored 16:9 `<video>`
  preview driven by `getUserMedia({video})` ("Test Camera" / "Stop Camera"), full teardown on
  stop/tab-switch/unmount, live device hot-swap. Browser QA `07d3c` (picker renders, preview decodes
  the fake-device frames `videoWidth>0`, container goes `.live`, stop tears it down); AI-vision verified.
  **Slice 3c DONE (iter 132): output (master) volume slider** — a 0–100% slider (persisted in
  `voiceSettings`) wired LIVE through both transports (`VoiceSession` + `SfuSession`
  `setMasterVolume`): a pure `effectiveVolume(peerVol, master)` (vitest-tested, clamped) scales every
  peer's `<audio>` playback on top of their personal volume. Two-client voice QA proves it composes
  live (master 50% × peer 40% → 0.2); browser QA proves the slider renders + persists; AI-vision
  verified. **Still TODO (slice 3d):** input **volume / mic-gain** slider — deferred because it needs a
  `GainNode` spliced into the capture chain (interacts with mute/PTT/hot-swap), unlike playback-only
  output volume; do it carefully on its own tick.
- [~] **Appearance / general polish pass** — **focus-ring a11y baseline DONE (iter 133):** one global
  `:focus-visible` rule gives EVERY interactive element (buttons/links/selects/`[role]`/`[tabindex]`) a
  consistent, theme-matched 2px accent ring under **keyboard** focus (never on a mouse click), replacing
  the inconsistent/near-invisible browser default across sidebar/header/chat/member list. CSS-only,
  axe-core stays 0-violations; new browser-QA `3f3` Tabs to a button and asserts a visible outline;
  AI-vision verified the ring. **Press (:active) feedback DONE (iter 134):** the app had 31 `:hover`
  rules but ZERO `:active` ones — added a global press dim (`opacity:.8`, + `brightness(.9)` on filled
  accent buttons) so every button/link gives tactile feedback on press; deliberately NOT a `transform`
  (a positional nudge moves the element on mousedown and breaks pointer/Playwright click-stability —
  caught by QA). New browser-QA `3f4` holds a button and asserts it dims. **Still TODO:** consistent
  spacing sweep; broader "feels like Discord" pass.
- [~] **Video calling** — slice 1 DONE (iter 135): **camera on/off in a mesh voice call** with a live
  video tile for each participant (the #1 missing Discord feature). Reuses the proven screen-share
  publish/render pipeline + an additive `kind: 'screen'|'camera'` tag on the `voice-screen` frame (Go
  relay validates the kind, Rule B — unknown → dropped) so tiles label/mirror correctly (your own
  camera is mirrored; remote isn't; camera carries no audio). Mutually exclusive with screen-share for
  now (one mesh video slot). **Fixed a real WebRTC bug found by the two-client QA:** switching video
  source screen↔camera REUSES the transceiver so `ontrack` doesn't re-fire — the receiver now retains
  the inbound video stream and re-attaches it on the announce. `go test` (relay + hostile-kind), vitest,
  full browser+voice QA all green (screen-share stays green = no regression); AI-vision verified both
  the "Your camera" self-view and the remote "X's camera" tile; shipped + `railway up` + rollout-verified.
  **Slice 2 (next):** a parallel `cameraStream` path so screen + camera coexist; SFU video.

## Blockers
<!-- P0 items added here by /qa and /self-improve when critical bugs are found -->

- [x] **P2 (security/robustness, found+fixed iter 97 via Rule-15 pass): register with an
  over-long password returned 500, not 400.** Passwords had a min (≥6) but no max; bcrypt
  rejects inputs >72 bytes, so a 73–65536-byte password (within the 64 KiB body cap)
  surfaced as a generic 500 from inside Register. Fixed: `HandleRegister` now bounds
  password to 6–72 bytes (`maxPasswordLen`) → clean 400. Reproduced (500) → fixed →
  re-attacked (400) → happy path intact; regression test added. Login unaffected (over-long
  password → normal 401). Also de-flaked `TestServeWSRateLimitIntegration` (a load-sensitive
  fixed-sleep → poll) so the health gate can't false-red.

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
- [x] Typing indicators · **read state DONE** — per-channel unread indicators (bold + pip
  on global/server/DM channels; `channel_reads` table, access-scoped `UnreadChannelIDs`,
  mark-read on open/leave, ~10s poll; store+router+browser tested, AI-vision verified)
- [x] Profiles: initials avatars + **uploaded avatars** (local-disk, access-gated
  serve, Avatar component renders the image or falls back to initials everywhere;
  header click-to-upload; Rule-15 hardened + vision-verified). Banners/status TODO
- [x] Servers/guilds — servers + members + server-scoped channels (per-server names, members-only access adversarially verified at HTTP+WS) + sidebar Servers accordion (create/join server, create channel, chat) — backend + UI, E2E verified
- [x] Direct messages — DM channels + membership + per-channel access control (WS/REST 403 for non-members, adversarially verified) + sidebar DM list, new-DM flow, DM-aware header/composer (two-user E2E verified)

## Next (v0.3 — Roles & Polish)

- [x] Rate limiting + abuse protection — per-connection WS token bucket (Rule 15)
- [~] Roles & permissions — server roles (owner/admin/member); roles UI (members panel + owner promote/demote); admin-gated channel creation; **message moderation** (admins delete others' messages) with a delete-button UI shown to admins in server channels — all two-user E2E verified; **read-only / announcement channels** (per-channel posting policy: only admins post, WS-enforced) with an admin toggle, a 🔒 badge, and a disabled composer for non-admins — all E2E verified. (A full per-role permission matrix is future polish beyond MVP parity.)
- [x] Invites — invite-code join (replaces the open join-by-id gap): members mint codes, redeeming admits you; non-member can't mint/guess (403/404), adversarially verified. (Membership mgmt: roles done; **kick + ban + timeout done** — owner/admin, with live WS eviction; ban blocks rejoining until unban; timeout temporarily mutes a member server-side)
- [x] Search — in-channel message search (case-insensitive, access-gated, LIKE-wildcards escaped per Rule B), header search box + results panel (channel-spanning search later)
- [x] Message grouping (collapse consecutive same-author messages within 5 min) — flagged then closed by browser AI-vision QA 2026-06-13
- [x] Mobile-responsive layout — sidebar collapses into an off-canvas drawer behind a header menu toggle (≤640px); chat goes full-width; backdrop + close-on-select (E2E + vision verified)
- [x] Jump-to-message — clicking a quoted reply preview, a pinned message, or a search
  result scrolls to + briefly flashes the original message in the channel (closing the
  panel first when needed). Frontend-only; reply-jump (inline) + search-jump (close-panel
  path) E2E + AI-vision verified. (No-op when the target is older than the loaded window —
  fetch-older-on-jump is a follow-up.)
- [x] Sidebar long-name truncation DONE — server/channel/DM names ellipsis-truncate
  (`.item-name`/`.server-name-text` get `min-width:0` + `overflow:hidden` + `text-overflow:
  ellipsis`; badge/avatar/#id/unread stay `flex-shrink:0`) with the full name in a `title`
  tooltip, so a long name never overflows the fixed 220px sidebar. Browser QA creates a
  long-named server and asserts the name element clips + stays within the sidebar; AI-vision
  verified (tick-114, closing the tick-113 finding).

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
  screen-share signaling survive socket drops; real offline→online E2E). **Configurable
  STUN + optional self-hostable TURN SHIPPED** (`OPENCORD_STUN_URL` / `OPENCORD_TURN_*`
  env → `iceServers` served via the authed voice/token round-trip → mesh
  RTCPeerConnections; degrades to STUN; free, Rule A). Config+endpoint tested, mesh E2E
  intact; **real symmetric-NAT traversal needs a deployed coturn** (not exercised
  in-context). **Ephemeral/HMAC TURN creds DONE (iter 137)** — setting `OPENCORD_TURN_SECRET`
  switches `/voice/token` from static TURN username/password to **short-lived per-user HMAC
  credentials** (coturn's `use-auth-secret` / TURN REST scheme): `username = "<expiry-unix>:<userID>"`,
  `credential = base64(HMAC-SHA1(secret, username))`, TTL `OPENCORD_TURN_TTL` (default 12h). A leaked
  cred self-expires and can't be forged without the secret (Rule C/15). Unit-tested (deterministic,
  user-scoped, time-bounded, unforgeable-without-secret) + live E2E (`POST /voice/token` returns
  `username:"…:9", credential:"<hmac>"`); the static path + the no-TURN default are untouched (mesh QA
  green). TODO: TURN for higher-bitrate screen video + SFU; deploy a real coturn for symmetric-NAT E2E.
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
- [~] Create/join servers (guilds) DONE + **server settings: rename + delete DONE**
  (rename = owner/admin "Manage Server", live `server-renamed` sidebar relabel; delete =
  owner-only, destructive — one tx removes the server's messages then cascades its members/
  channels/invites/categories/bans, global `#general` untouched, members live-evicted via
  `server-removed`. Authz matrix + cascade adversarially tested at store+HTTP, browser E2E:
  create → rename → delete. ⚙ Server settings section in the members panel.) · **leave-server
  DONE** (any non-owner member voluntarily leaves via `POST /servers/{id}/leave`; the owner
  can't — must delete/transfer; leaver's live sockets evicted; authz + access-loss tested at
  store+HTTP, realtime browser E2E: B rejoins → leaves → server drops from B's sidebar; the
  panel shows "leave server" to non-owners, "delete server" to the owner) · **transfer-
  ownership DONE** (owner hands the server to another member via `POST /servers/{id}/transfer`;
  one tx promotes the target to owner, demotes the old owner to admin, updates `servers.owner_id`;
  owner-only, can't transfer to self/non-member; "make owner" button on member rows; authz +
  role-swap tested at store+HTTP, realtime browser E2E round-trip A→B→A) · server description /
  icon / vanity URL TODO
- [~] **Channel categories DONE** (collapsible groups; create + nest a channel + optional
  categoryId on channel create, Rule-B cross-server guard) · ordering/drag TODO
- [~] Invites — code-join + **7-day expiry DONE** (enforced server-side at redeem;
  expired → 404, adversarially tested; legacy invites stay permanent) + **list + revoke
  DONE** (admin-gated Invites section in the members panel: lists active codes with
  expiry/creator, a + New invite mint, copy, and a revoke that kills a leaked code so it
  stops redeeming immediately; `GET`/`DELETE /servers/{id}/invites[/{code}]`, revoke
  scoped by `server_id` so no cross-server revoke — Rule B; adversarial integration test +
  browser E2E + AI-vision). **max-uses DONE** (optional 1–1000 join cap, enforced + counted atomically server-side; exhausted → 404, adversarially tested; panel shows N/M uses) · invite links · temporary membership TODO
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
- [~] **Categories DONE** — a server groups channels under named, collapsible category
  headers (Discord-style); `channel_categories` table + nullable `channels.category_id`
  (ON DELETE SET NULL → deleting a category leaves channels uncategorized). Admin-gated
  create (`POST /servers/{id}/categories`), member list, optional `categoryId` on channel
  create (cross-server category attach rejected, Rule B). Sidebar renders uncategorized
  channels first, then collapsible groups with a per-category "+" to add a channel and a
  "✕" to **delete** the category (admin; channels survive as uncategorized via the FK's
  ON DELETE SET NULL, cross-server delete rejected). Adversarial integration test
  (create/list/delete authz + cross-server guards) + browser E2E (create → nest →
  collapse/expand → delete → channel survives uncategorized) + AI-vision verified.
  (Reorder/drag + move-existing-channel + per-channel permissions + channel topic[done]
  still TODO)
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
  click-to-insert) — E2E + AI-vision verified. **Mention notifications DONE** — unread
  @mentions surface as a red count badge on the channel (see Notifications). (@role,
  threads still TODO)
- [~] File / image attachments — DONE (composer 📎, multipart upload, local-disk
  store, access-gated serve, inline images + download chips; Rule-15 hardened +
  vision-verified incl. **two-client live propagation** — B sees A's upload render
  live, fetched with B's own token). Video transcode · link embeds + previews TODO
- [~] Pinned messages — pin/unpin (admin-gated in server channels), 📌 badge, live
  WS update, and a "pins" panel listing all of a channel's pins; E2E + AI-vision
  verified. (**unread DONE** — sidebar unread dots, see Messaging "read state"; bookmarks
  + **mention-count badges DONE** — red count badge for unread @mentions, see Notifications;
  bookmarks TODO)
- [~] Message search — **operators DONE** (`from:<user>`, `has:link`, `has:image`,
  `has:file`, **`before:<YYYY-MM-DD>` / `after:<YYYY-MM-DD>` (day-exclusive date bounds,
  combine into a window)** + free text; parameterized dynamic SQL, store+browser tested,
  injection-inert — a malformed/hostile date falls through to inert free text).
  `in:#channel` · polls · timestamp grouping TODO

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
  via the Avatar component, header upload entry). **Custom status DONE** — a short
  status line by the name (member list + panel + header; set via `PUT /me/status`,
  Rule-C/own-only, trimmed+capped 128, React-escaped; store+router+browser tested,
  AI-vision verified). **Status emoji DONE** — an optional emoji shown before the status
  (`users.status_emoji`, capped 16 runes, same `PUT /me/status` payload, React-escaped;
  renders in header + both member panels; store+browser tested, AI-vision verified — `🚀`
  before the line). Banners · activity ("playing X") still TODO
- [ ] About me / pronouns / connections · per-server nicknames
- [~] Presence: **online/offline + idle/DnD/invisible DONE** — manual presence picker
  (header `<select>`); `users.presence_state`, effective-presence rule (others see
  invisible/disconnected as offline, you see your own true state — pure unit-tested),
  `PUT /me/presence`; member-list dot colored green/amber/red/grey in both panels.
  Store+http+browser tested, AI-vision verified (red DnD dot). **Auto-idle-on-inactivity DONE
  (iter 136)** — a frontend inactivity timer (mousemove/key/wheel/touch; ~10min, like Discord)
  drops `online → idle` after a quiet stretch and restores `online` on the next activity; it ONLY
  transitions a presence the timer itself set (a manual idle/dnd/invisible is never overridden — a
  manual change cancels auto-restore). No backend (reuses `PUT /me/presence`). The threshold is
  live-tunable via `window.__ocIdleMs` so browser QA `07d4` shortens it: quiet → the self pip turns
  amber (idle), activity → online; AI-vision verified the amber pip on the header chip + member list.

### Roles & Permissions
- [ ] Roles (hierarchy, colors, icons, mentionable)
- [ ] Granular permissions (server + per-channel overrides)

### Moderation
- [~] **Kick DONE** — owner/admin removes a member (`DELETE /servers/{id}/members/{userId}`);
  owner kicks any non-owner, admin kicks members only, nobody kicks the owner/self
  (authz matrix tested). Security: the kicked user's live WS sockets are evicted
  (`Hub.EvictUserFromChannels`) so they stop receiving immediately — proven by a
  `-race` ws integration test + browser QA (online count drops on kick). Red kick
  button in the members panel; two-user E2E + AI-vision verified. **Live kick-notice
  DONE** — `Hub.SendToUser` pushes a `server-removed` event so the kicked user's client
  drops the server + falls back to #general live (no broken reconnect loop); E2E +
  AI-vision verified. **Ban DONE** — the stronger form of kick: removes the member AND
  blocks rejoining (`server_bans` table; `RedeemInvite` rejects a banned user with
  `ErrBanned`→403 even on a valid code) until an owner/admin unbans. Same authz matrix
  as kick; atomic remove+ban tx; reuses the live WS eviction + `server-removed` push.
  `POST`/`DELETE`/`GET /servers/{id}/bans`; members-panel **ban** button + admin
  **Banned (N)** section with **unban** + reason. Adversarially integration-tested
  (rejoin-blocked then unban-restores) + two-user browser E2E + AI-vision verified.
  **Timeout DONE** — temporarily mute a member: `server_members.timeout_until` +
  a server-side post-guard in `SaveReply`/`SaveWithAttachments` (`ErrTimedOut`,
  enforced on BOTH the WS and HTTP send paths) so a muted member can't post until it
  expires or is cleared; same authz matrix as ban/kick; duration clamped to ≤28d.
  `POST`/`DELETE /servers/{id}/timeouts`; members-panel **timeout/unmute** button +
  ⏳ muted badge; the muted viewer's composer is disabled with a notice. Adversarially
  integration-tested (mute enforced then lifted, duration clamp) + two-user browser
  E2E + AI-vision verified. · bulk delete · AutoMod (keyword/spam) · reporting TODO
- [x] Message moderation (admins delete others' messages) — shipped (v0.3)

### Notifications
- [~] **Unread indicators + @mention-count badges DONE** (per-channel sidebar grey dots
  for unread; red count badge for unread @mentions incl. @everyone/@here, server-side
  match mirrors the client highlight; store+router+browser tested, AI-vision verified).
  Per-channel/server mute · DM notifications · web push TODO

### Platform / Integrations
- [ ] Bot/API + webhooks + slash commands · OAuth2 app authorization
- [~] **Accessibility** — WCAG-AA color contrast fixed (links/mentions/green labels/
  avatar initials) + axe-core WCAG scan in the browser QA (0 serious/critical, regression-
  guarded). Keyboard-nav · screen-reader · i18n · theme (dark/light) still TODO
- [ ] Desktop + mobile clients (PWA first)

### QA / loop tooling
- [x] `qa/search-smoke.sh` DONE — read-only post-deploy search smoke (register → bounded
  `before:`/`after:`/free-text searches on `#general` → assert the full window strictly
  exceeds a tightened/contradictory window and an unmatchable token returns 0). Verifies
  query/filter features on prod by **discrimination against existing history**, not
  write-then-find (plain text posts go via WS, not REST). Run via `make qa-search`
  (defaults to `CCF_LIVE_URL`, `OPENCORD_BASE_URL=` to point elsewhere); **also wired
  into `qa/run.sh` as a gated step (`search=$RC4`)** so it runs against the local stack
  on every browser-QA run (iter 123). On an empty `#general` it reports INCONCLUSIVE
  (exit 0) rather than false-red. Proven to catch a "match-all" regression (operators
  silently ignored → 5 of 7 assertions trip); green on prod (7 msgs) and on the local
  gate (14 msgs).

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
