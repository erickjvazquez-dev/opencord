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

- [~] **Group DMs (Discord parity, highest-ROI per tick-159 plan)** — **slice 1 backend DONE (iter
  160):** generalized the 2-member DM model (`kind='dm'` channels) to N members with **no schema change**
  (the `channel_members` join table + per-channel WS hub are already N-member). `DMChannel` gains
  `Users []DMUser` (all others, username-sorted; `User` kept = `Users[0]` so the 1:1 client is unbroken
  until slice 2); `CreateGroupDM` (dedupe/drop-self, resolve every member, reject a block with any member,
  1-other→idempotent 1:1, 2..9-others→fresh non-deduped group, 10-cap); `ListDMs` aggregates non-blocked
  others (a group with one blocked co-member just omits them, never hidden); `CanAccessChannel` block-deny
  now gated on member-count=2 (a group member is never denied for a blocked co-member). New
  `POST /api/dms/group {identifiers:[...]}` (64 KiB bounded). `go build/vet/test` green incl.
  `TestGroupDMIntegration`; **live E2E on a real server** (sorted users, per-viewer ListDMs, member-200/
  non-member-403, 404/400 guards, 1-other==1:1); shipped + `railway up` + rollout-verified (new route 401
  not 404). **Slice 2 client DONE (iter 161):** new `NewGroupModal.tsx` — the Discord-style "New Direct
  Message" dialog (chips input, Enter/comma adds, ✕/Backspace removes, ≤9; button reads "Create DM" for
  one / "Create Group" for 2+; Esc/overlay/✕ close). The `+ New DM` button now opens it (replacing the
  old double `window.prompt` — a polish win; one chip → idempotent 1:1). New pure `dm.ts`
  (`dmOthers`/`dmIsGroup`/`dmTitle`/`parseIdentifiers`, vitest 12, legacy `user` fallback); DM list,
  header, welcome + composer render via it; a group shows an accent group-glyph avatar with the full
  member list in the title. Also closed the slice-1 QA gap: a **3-client WS fanout test** (A→B+C; a
  non-member's group handshake refused 403). Verify: tsc clean, vitest 77/77, **full QA green
  (browser=0 realtime=0 voice=0 search=0** — new create-group browser flow + realtime migrated to the
  modal), AI-vision verified (modal + sidebar row + header + welcome + composer), go test green;
  shipped + `railway up` + rollout-verified (live bundle byte-identical, carries "New Direct Message"/
  "Create Group"). **Core group DMs complete (create + render + realtime).**
  **Leave-group DONE (iter 177):** a "🚪 leave group" header action (groups only) removes you via
  `POST /api/dms/{id}/leave` (store `LeaveGroupDM`: group-only ≥3 members, membership-first so a
  non-member never leaks kind/size, Rule B); remaining members refresh live via a `dm-membership` WS
  broadcast, the leaver's socket is evicted. `TestLeaveGroupDMIntegration` + browser create→leave→gone
  flow + AI-vision; blast-radius guard caught+fixed 2 pre-gate breaks (tsc event union + README
  docs-sync); shipped + rollout-verified (live: leave→204, leaver's list drops it / others keep it,
  re-leave→404), AND a two-client realtime test (iter 178: A leaves a group → B, viewing it, sees A drop
  from the member list LIVE via `dm-membership`, no reload — proving the remaining-members refresh edge).
  **Stacked member avatars DONE (iter 179):** the DM-list group row shows a Discord-style STACK of the
  first two members' avatars (`.dm-group-stack`, two 15px Avatars offset + ringed in the sidebar bg)
  instead of a generic glyph; browser QA asserts the 2-avatar stack + AI-vision verified; shipped +
  rollout-verified. **Group naming backend DONE (iter 184, slice 1):** a group DM can be named (Discord
  parity) — schema recreates the global channel-name index to exclude `kind='dm'` (group names NOT
  unique, the thread precedent), store `RenameGroupDM` (actor-member-first, group-only, ≤100, empty
  clears → member-list title), `DMChannel.Name` + `ListDMs` returns it, `PATCH /api/dms/{id} {name}` +
  dm-membership relabel broadcast. `TestRenameGroupDMIntegration` (incl. two-groups-same-name) + **live
  E2E** (rename→204, all members see the name, clear→"", non-member→403); shipped + rollout-verified
  (migration applied on boot). **Group naming slice 2 client DONE (iter 185):** a "✏️ rename" header
  action (groups only) prompts (pre-filled with the current name) → `renameGroupDM` PATCHes it; empty
  clears. `DMChannel.name` on the client; `dmTitle` shows a named group's custom name (group-only —
  a 1:1 ignores any stray name) and falls back to the comma-joined members when blank; new
  `dmMembersLabel` always lists members, used for the named-group sidebar tooltip + the welcome
  subtitle so "who's in here" stays visible. The realtime relabel works via the existing
  dm-membership refetch (set/clear propagates to all members live). dm.test.ts +4 (named title,
  blank fallback, 1:1-ignores-name, members label); browser QA rename→clear flow + 07k2 screenshot;
  tsc clean, vitest 81/81, full QA green (browser=0 realtime=0 voice=0 search=0), AI-vision verified
  the renamed header; shipped + `railway up` + rollout-verified (live bundle carries the rename prompt).
  **✅ Group DM naming COMPLETE (backend + client + realtime).**
  **add-member DONE (iter 181):** a "➕ add" header action (groups only) adds a member via
  `POST /api/dms/{id}/members {identifier}` (store `AddGroupDMMember`: actor-member-first, group-only,
  10-cap, already-member/blocked guards, Rule B/15); the new member's client gets a `SendToUser`
  dm-membership push so the group appears in their sidebar live, existing members refresh via the channel
  broadcast. `TestAddGroupDMMemberIntegration` (full adversarial matrix) + browser add-flow + **live E2E**
  (add→204, new member's /api/dms lists it, re-add→409, non-member→403); blast-radius guard PASS; shipped +
  rollout-verified, AND a two-client realtime test (iter 182, realtime.mjs §15: A adds D → B viewing sees D
  in the title live AND D on #general sees the group appear in their sidebar live via the SendToUser push,
  no reload — proving the not-on-channel member is pushed into the new DM). In Discord you add people but only ever leave yourself, so **group-DM membership mgmt
  is now complete (add + leave)**. **Intro-icon consistency DONE (iter 180):** the 68px group-DM welcome icon now
  stacks the same two member avatars as the sidebar (was a 👥 emoji) — browser QA asserts it + AI-vision;
  shipped + rollout-verified. Group-DM rendering is now consistent (sidebar + welcome).
- [~] **Custom colored roles (Discord parity, tick-159 ROI #2)** — **slice 1 backend DONE (iter 162):**
  Discord-style COSMETIC colored roles, additive to and separate from the owner/admin/member PERMISSION
  tier (untouched — low blast radius). New `server_roles` (id, server_id, name, color, position) +
  `member_roles` (user_id, role_id) tables (no change to `server_members.role`). Store (all mutations
  admin-gated, Rule C): Create (validate name 1-32 + color `#RGB`/`#RRGGBB`, position=max+1), List
  (position desc), Update (rename/recolor), Delete (cascades assignments), Assign/Unassign (target must
  be a member, role must belong to the server). `ServerMember` gains `color` = the member's **top**
  (highest-position) role color via a correlated subquery in `ListServerMembers` (Discord's top-role
  rule). Routes under `/custom-roles` (since `POST .../roles` is the permission setter). `go build/vet/
  test` green incl. `TestServerCustomRolesIntegration`; **live E2E on a real server** (full CRUD +
  assignment + member-list color resolution + recolor + cascade-on-delete; adversarial: non-admin 403,
  bad hex 400, non-member 404, bogus role 404); shipped + `railway up` + rollout-verified (new route 401
  not 404). **Slice 2a client DONE (iter 163):** `RolesManagerModal` (opened from the members-panel
  server settings, admin) — list/recolor/delete roles + a create row (name + `<input type=color>` +
  Discord preset swatches); `ProfileCard` gains a **Roles** section (admin) showing the server's roles as
  toggle chips filled in their color (assigned via `member.roleIds`), and the name tinted by
  `member.color`; member names in the sidebar + members panel render in their color; assign/unassign
  refresh members+roles so colors update live. Backend tweak: `ServerMember.roleIds` (aggregated in
  `ListServerMembers`) so the client shows assignment state. tsc clean, vitest 77/77, go test green incl.
  `roleIds`; **full QA green (browser=0 realtime=0 voice=0 search=0** — new flow creates a role → assigns
  via the profile → asserts the member name is color-tinted); AI-vision verified the manager, the colored
  name (member panel + admins sidebar + profile header), and the assigned chip; shipped + `railway up` +
  rollout-verified (live bundle carries "Create colored roles"). **Slice 2b DONE (iter 164):
  message-author coloring** — `Message.authorColor` (the author's top role color in the channel's server)
  joined into the read paths (`Recent`/`PinnedMessages`/`SearchMessages` via a shared `authorColorSQL`
  subquery) + set on the live paths (`SaveReply`/`SaveWithAttachments`/`EditMessage` via an `authorColor()`
  helper); "" for DM/global channels; reflects current role assignment. Client renders the message author
  (head + search + pins) in `m.authorColor`. `TestMessageAuthorColorIntegration` (server message colored
  via Save + Recent; DM none); full QA green incl. a new flow that re-enters a server channel after a role
  assignment (forced history refetch via a channel hop) and asserts the message author is tinted;
  AI-vision verified. Shipped + `railway up` + rollout-verified. **Slice 3 DONE (iter 169): role hoisting**
  — `server_roles.hoist`; Create/Update/List round-trip it; the member list shows each hoisted role
  (position desc) as its own role-named, role-colored section above Admins/Members. **ADDITIVE** — with no
  role hoisted (default), the member list is unchanged; the client groups using `roleIds` + the roles list
  (no `ListServerMembers` change). RolesManagerModal gained a "Display separately (hoist)" toggle. go test
  green (hoist round-trips); full QA green incl. a hoist→section flow; AI-vision verified the "QA-MOD"
  section; shipped + rollout-verified. **✅ Custom colored roles FULLY COMPLETE (create + assign + colored
  names everywhere + hoisting).** Later (optional): role reordering (drag), per-role permissions.
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
- [x] **Voice & Video settings tab** — slice 3a DONE (iter 130): the **Voice & Video** tab is live
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
  verified. **Slice 3d DONE (iter 143): input (mic) volume slider** — the Discord "Input Volume"
  control (how loud peers hear YOU), spliced as a `GainNode` into the live CAPTURE chain (raw mic →
  source → `micSendGain` → destination → `micSendTrack`), reusing the proven screen-audio send-gain
  pattern. Peers receive the gain-scaled track; `setInputVolume` sets the gain live mid-call.
  Mute/PTT/deafen unchanged (toggle the raw source `.enabled` — a disabled source feeds silence
  through the node); a mic hot-swap rebuilds the chain + `replaceTrack`s; degrades to the raw track if
  Web Audio is absent. vitest (clamp/default/corrupt/independence); browser QA (slider renders +
  persists + re-reads on remount); **two-client voice QA measures B's DECODED inbound RMS for A's mic:
  input 0% drops it to ~silence while C stays audible → the gain is really wired through the send
  path** (the mute/PTT/screen/camera flows all stayed green = blast radius intact). Shipped + `railway
  up` + rollout-verified (live bundle carries the slider). **Tab complete (3a–3d).**
- [~] **Appearance / general polish pass** — **focus-ring a11y baseline DONE (iter 133):** one global
  `:focus-visible` rule gives EVERY interactive element (buttons/links/selects/`[role]`/`[tabindex]`) a
  consistent, theme-matched 2px accent ring under **keyboard** focus (never on a mouse click), replacing
  the inconsistent/near-invisible browser default across sidebar/header/chat/member list. CSS-only,
  axe-core stays 0-violations; new browser-QA `3f3` Tabs to a button and asserts a visible outline;
  AI-vision verified the ring. **Press (:active) feedback DONE (iter 134):** the app had 31 `:hover`
  rules but ZERO `:active` ones — added a global press dim (`opacity:.8`, + `brightness(.9)` on filled
  accent buttons) so every button/link gives tactile feedback on press; deliberately NOT a `transform`
  (a positional nudge moves the element on mousedown and breaks pointer/Playwright click-stability —
  caught by QA). New browser-QA `3f4` holds a button and asserts it dims. **Header title truncation
  DONE (iter 188):** the chat header title (`.chat-header .channel`) now ellipsis-truncates
  (`max-width:min(36ch,55vw)` + `overflow:hidden` + `text-overflow:ellipsis`, the `.channel-topic`
  pattern) with the full title on a hover `title` attr — a long custom group name or many-member
  member-list title no longer grows unbounded; browser QA `07k4` sets an ~82-char name and asserts
  the title element is clipped (scrollW > clientW), AI-vision verified the ellipsis. **Still TODO:**
  consistent spacing sweep; broader "feels like Discord" pass; **P2 (follow-up from iter 188): the
  DM header's many TEXT-label controls still wrap** (pins/mute/add/rename/leave/voice + a wide search
  box exceed the row even with the title capped) — Discord keeps these single-row by using icon-only
  buttons / an overflow "⋯" menu. That's the real single-row fix; the title cap was the first half.
  **Scope note (blast-radius check, iter 190):** this is NOT a one-tick job — the QA matches header
  buttons by TEXT via `getByRole('button',{name})` in ~10 call sites across browser.mjs, voice.mjs,
  AND sfu.mjs (Join voice, make read-only, edit topic, slowmode, pins) plus a header-line-count test.
  An icon-only redesign must update all of those. Do it as a dedicated SPEC'd tick (Rule 6): spec the
  icon set + aria-labels + the collapsible-search, migrate the QA selectors to classes first, then
  re-skin — so the redesign and the QA rework land together and nothing regresses. **SPEC WRITTEN (iter
  195): see SPEC.md "Chat header — compact action bar"** — 5-slice staged plan (migrate QA selectors to
  classes → icon-ify → collapsible search → optional overflow → vision-verify single-row). **Slice 1
  DONE (iter 196):** QA selectors → classes. **Slice 2 DONE (iter 197):** action buttons icon-ified
  (aria-label + title); header cut from THREE rows → TWO (AI-vision verified), shipped + rollout-verified.
  **Slice 3 DONE (iter 198):** collapsible 🔍 search — the action bar is now a single icon row; the
  header is single-row with the on-demand member panel CLOSED (default). **Slice 4 DONE (iter 206) —
  P2 FULLY RESOLVED:** the meta cluster (avatar + presence + username + ⚙ + log out + online count)
  was RELOCATED out of the header entirely into a Discord-style **bottom-left sidebar user panel**
  (`.sidebar-user`, pinned via `margin-top:auto`). The header is now a clean single row of title +
  action icons in EVERY case — group DMs and member-panel-open included (verified by AI-vision on
  07l-group-header.png, which used to wrap). Mostly a move (all self-chip/log-out/presence classes +
  aria kept, so QA selectors resolved unchanged); on mobile the panel lives in the off-canvas drawer.
  Full QA green, blast-radius guard PASS, rollout-verified (live bundle carries `sidebar-user`). Spec:
  SPEC.md "Bottom-left sidebar user panel". **✅ Appearance header-compaction COMPLETE (slices 1–4).**
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
  **Slice 2 PLANNED (iter 154):** investigated the mesh video path + wrote a concrete sub-sliced plan
  in SPEC.md ("Video slice 2 — screen + camera coexist") — 2a sender dual-track, 2d relay kind-on-stop,
  2b receiver two-stream state, 2c UI two tiles, with the two-client-QA regression matrix. It's NICHE +
  highest-blast-radius (core mesh), so flagged for a dedicated implementation tick OR deferral in favour
  of higher-ROI parity (custom-emoji reactions, role colors) — owner's call. SFU video: later.

- [x] **Voice mute/deafen in the user panel (Discord parity)** — DONE (iter 213). Always-available 🎤
  mic-mute + 🎧 deafen toggles in the bottom-left `.sidebar-user` panel (aria-labelled, with a red
  diagonal slash + danger tint when active, mirroring the in-call voice-bar). Persisted self-mute/deafen
  flags added to `voiceSettings.ts` (`getSelfMute`/`getSelfDeafen`, opt-in default OFF, vitest-covered);
  `muted`/`deafened` now seed from them so the panel reflects the intent out of a call, and `joinVoice`
  applies them on connect (an explicit `setMuted(on)` added to the VoiceTransport — mirrors `setDeafened`)
  so **"join already muted"** works. `toggleMute`/`toggleDeafen` are shared by the panel + voice bar and
  persist the new state. Browser QA asserts the panel toggles + localStorage persistence + reload-survival
  + struck visual; **2-client voice QA proves B hears ~silence on A's pre-muted join (RMS 0.0000) then
  hears A after a panel un-mute (0.3043)**. tsc + vitest 91/91 + full QA (browser=0 realtime=0 voice=0
  search=0) + AI-vision verified; shipped + railway + rollout-verified.
- [x] **QA: prove pre-call DEAFEN apply-on-join end-to-end — DONE (iter 214).** Added a 2-client
  pre-call-deafen scenario to `qa/voice.mjs`: A sets self-deafen in the panel BEFORE rejoining (B stays
  in the call), then on join it asserts both effects deafen must have — (a) B's decoded RMS for A's mic
  is ~0 (deafen forces the mic off, 0.0000) AND (b) A's inbound `<audio>` for B is `.muted` (deafen
  silences incoming) — then un-deafen restores both (mic 0.0000→0.3143, incoming un-muted). Full QA green
  (browser=0 realtime=0 voice=0 search=0) + AI-vision. The apply-on-join matrix now covers BOTH flags.
- [x] **P1 (UI parity, found iter 214) — DONE (iter 215): deafen visually strikes the mic icon too.** The
  panel 🎤 now shows the red slash when `muted || deafened` (deafen silences your mic), so a deafened user
  sees BOTH icons struck — Discord parity. Display-only: `aria-pressed`/`data-muted` stay the real mute
  toggle so the click still flips `muted` alone and un-deafen restores your prior mute state; a
  `data-mic-silenced` attribute exposes the derived state. Browser QA asserts deafen-alone strikes the 🎤
  (with `aria-pressed` still false) and clears on un-deafen; AI-vision confirmed both icons struck; shipped
  + railway + rollout-verified.

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
- [x] **Trojan-Source / bidi-spoofing hardening (Rule 15, iter 216).** Message bodies could carry
  Unicode bidirectional override/embedding/isolate controls (U+202A–U+202E, U+2066–U+2069 — CVE-2021-42574),
  which reorder how a message renders vs. its logical content (spoofing a URL/quoted line). The server now
  strips them on EVERY write path at the store chokepoint (`Save`/`SaveReply`/`SaveWithAttachments` + an
  edit can't re-inject via `EditMessage`) — Rule B, server-side, protecting all clients + stored history.
  Legit Unicode (emoji incl. ZWJ sequences, Arabic/Hebrew RTL, CJK, combining marks, LRM/RLM marks) is
  preserved. Reproduced (test failed pre-fix: 9 controls stored verbatim) → fixed → re-attacked (clean) →
  proved no collateral. Regression tests: unit (`stripBidiControls`, exact char set) + integration through
  the real store + through the **real WS ingest path** (`TestServeWSHostileFrameHandling`). Live-probed
  the deploy (WS sent U+202E/U+2066 on the wire → persisted clean "wire-live-clean").
- [x] **Bidi stripping extended to all rendered names — DONE (iter 217).** Applied `stripBidiControls` at
  every display-name write path: server names (`CreateServer`/`RenameServer`), server channels
  (`CreateServerChannelOfKind`, covering the create-channel funnel), global channels (`CreateChannel`),
  threads (`CreateThread`), categories (`CreateChannelCategory`), group-DM names (`RenameGroupDM`), custom
  status + status emoji (`SetUserStatus`), and custom role names (`validateRole`, covering create + update).
  **Usernames were already safe** — auth's `^[a-zA-Z0-9_]{3,32}$` rejects these chars at registration (no
  change needed). Rule-15 cycle: reproduced (server name stored 9 controls verbatim) → fixed → re-attacked
  (clean) → proved no collateral (Arabic + emoji name preserved). Regression: `TestBidiControlStrippingNamesIntegration`
  (server/channel/thread/status/role). go build/vet/test green; live-probed the deploy.
- [~] Roles & permissions — server roles (owner/admin/member); roles UI (members panel + owner promote/demote); admin-gated channel creation; **message moderation** (admins delete others' messages) with a delete-button UI shown to admins in server channels — all two-user E2E verified; **read-only / announcement channels** (per-channel posting policy: only admins post, WS-enforced) with an admin toggle, a 🔒 badge, and a disabled composer for non-admins — all E2E verified. (A full per-role permission matrix is future polish beyond MVP parity.)
- [x] Invites — invite-code join (replaces the open join-by-id gap): members mint codes, redeeming admits you; non-member can't mint/guess (403/404), adversarially verified. (Membership mgmt: roles done; **kick + ban + timeout done** — owner/admin, with live WS eviction; ban blocks rejoining until unban; timeout temporarily mutes a member server-side)
- [x] Search — in-channel message search (case-insensitive, access-gated, LIKE-wildcards escaped per Rule B), header search box + results panel (channel-spanning search later)
- [x] Message grouping (collapse consecutive same-author messages within 5 min) — flagged then closed by browser AI-vision QA 2026-06-13
- [x] Mobile-responsive layout — sidebar collapses into an off-canvas drawer behind a header menu toggle (≤640px); chat goes full-width; backdrop + close-on-select (E2E + vision verified)
- [x] Jump-to-message — clicking a quoted reply preview, a pinned message, or a search
  result scrolls to + briefly flashes the original message in the channel (closing the
  panel first when needed). Frontend-only; reply-jump (inline) + search-jump (close-panel
  path) E2E + AI-vision verified. (No-op when the target is older than the loaded window —
  fetch-older-on-jump is a follow-up.) **History pagination backend DONE (iter 203):** `RecentBefore` + `GET /messages?before=<id>` (the fixed-50 window now has a scroll-up cursor); **frontend DONE (iter 204):** a "↑ Load older messages" pill pages older history in (scroll-anchored, no view-jump); E2E-tested via a seeded pgseed channel + AI-vision verified. **Auto-load-on-scroll DONE (iter 205, Discord parity):** scrolling near the TOP now pages older history in automatically (the SPEC's "future enhancement") — a stale-closure-safe handler (a `loadOlderRef`/`hasMoreHistoryRef` mirror feeds the once-created native scroll listener, guarded by `loadingOlderRef` + `hasMoreHistory` so repeated fires are cheap no-ops and it self-stops at the channel start). The "↑ Load older" pill stays as a visible fallback (and reaching it scrolls to the top → same `loadOlder` path). pgseed bumped 60→120 msgs (3 pages) so the browser QA exercises scroll-to-top auto-load (50→120, no click) + scroll-anchored no-yank; AI-vision verified the anchored mid-history view. **History pagination COMPLETE (backend + button + auto-load-on-scroll).**
- [x] Smart auto-scroll + jump-to-present DONE (iter 191) — the message list no longer yanks a
  reader who has scrolled up into history to the bottom on every new message: it auto-follows
  only when already pinned to the bottom (80px slack) or the new message is the reader's own
  send; a channel switch still lands instantly at the newest. A Discord-style "↓ Jump to present"
  pill appears while scrolled up and returns to the latest on click. Native scroll listener via a
  callback ref (React 18's delegated onScroll didn't fire here); browser-QA `3f1` (wheel down→pin,
  up→pill, click→bottom) + AI-vision verified; realtime QA stays green; shipped + rollout-verified.
- [x] ArrowUp-edits-last-message DONE (iter 194) — pressing ↑ in an EMPTY composer opens the
  reader's most recent (non-deleted) message in this channel for inline editing (Discord shortcut),
  reusing the existing startEdit/edit-row UI. Guarded on `draft === '' && editingId === null` so
  ArrowUp otherwise moves the caret, and the @mention dropdown still owns ArrowUp while open.
  Browser-QA `4b` (send → ArrowUp → edit input pre-filled → Escape) + AI-vision verified; shipped +
  rollout-verified.
- [x] Reconnecting banner + faster offline detection DONE (iter 202) — a debounced "Reconnecting…"
  amber bar (under the header, Discord-style) shows only when the socket stays down past a 1.5s grace
  window (a channel-switch reconnect never flashes it); clears on recovery. Plus: the browser `offline`
  event proactively closes the dead socket so the reconnect backoff + banner start immediately instead
  of waiting ~60s for the ping timeout (a silent drop rarely sends a close frame). realtime §1e asserts
  the banner appears offline + clears on recovery; AI-vision verified; shipped + rollout-verified.
- [x] Esc-closes-panel DONE (iter 201) — pressing Esc closes the open right-side panel (pins → search
  → members) or exits a thread to its parent (Discord standard). Global keydown handler, skipped while
  typing and deferred whenever a modal/picker owns Esc (settings/new-DM/roles/profile/emoji/reaction/
  PTT-rebind) so one Esc closes the topmost thing and the next the panel beneath. Browser-QA closes the
  pins panel via Esc; the existing settings/profile Esc tests still pass (no conflict); shipped +
  rollout-verified.
- [x] Sidebar long-name truncation DONE — server/channel/DM names ellipsis-truncate
  (`.item-name`/`.server-name-text` get `min-width:0` + `overflow:hidden` + `text-overflow:
  ellipsis`; badge/avatar/#id/unread stay `flex-shrink:0`) with the full name in a `title`
  tooltip, so a long name never overflows the fixed 220px sidebar. Browser QA creates a
  long-named server and asserts the name element clips + stays within the sidebar; AI-vision
  verified (tick-114, closing the tick-113 finding).
- [ ] **P1 (UI parity design pass, found iter 219 via AI-vision): header action icons are colorful
  emoji, not monochrome line icons.** The chat-header controls (pin 📌, threads 🧵, notify 🔔, search 🔍,
  plus the server row 🔒 read-only, 🐌 slowmode, 📝 topic, …) render as mismatched colorful emoji, which
  reads less "native chat app" than Discord's clean monochrome line-icon row. A genuine parity gap, but a
  whole-app visual decision (sourcing/creating ~10 consistent SVG line icons + a shared `<Icon>`) that the
  owner should steer — NOT an autonomous mid-loop redesign. Deferred until owner-confirmed; if greenlit,
  do it as one focused design pass with a shared icon set so the look stays consistent.

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
  pure fn, vitest-tested) so a huge room never mixes every stream. ✅ **self-host TURN
  for hostile/symmetric NATs**: config + ephemeral HMAC creds (iter 137) NOW paired with a
  **bundled coturn** (iter 218) — an optional `docker compose --profile turn up` service
  (OFF by default, Rule A; `stack-guardian` APPROVE; free self-hosted OSS), validating the
  same `OPENCORD_TURN_SECRET` the server mints. README + `.env.example` document the full
  voice env (incl. the previously-undocumented `OPENCORD_TURN_SECRET`/`TTL`). Verified: profile
  gating (coturn absent from the default stack) + coturn 4.6.2 boots with our flags; a real
  symmetric-NAT relay needs a hostile-NAT client (not loop-testable, stated). Next: cascaded
  SFUs for true thousands-scale. Railway
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
  **Voice presence shipped (iter 170, v0.9 slice 1):** the hub tracks who's in the voice
  call per channel (a `voiceMembers` map mutated lock-free on the single Run goroutine;
  voice-join/leave/disconnect update it) and broadcasts a `voice-presence` event; the client
  shows a live **"🔊 N in voice"** header chip so you see a call WITHOUT joining (foundation
  for discoverable calls + dedicated voice channels). WS test + 3-client browser E2E + AI-vision.
  **Slice 2 DONE (iter 171): cross-channel sidebar presence** — a lock-free `Hub.VoiceMembersFor`
  query + `GET /api/servers/{id}/voice-presence` (members only); the client polls per server (15s +
  an immediate refresh on any voice-presence WS event) and shows a **🔊 N** badge on each server
  channel with an active call. Hub query + route-auth tests; full QA green (a raw-WS voice-join shows
  the badge during the call + clears on disconnect); AI-vision.
  **Slice 3a DONE (iter 172): voice channels as entities (backend).** A dedicated voice channel is now
  a first-class `kind='voice'` server channel. Store `CreateServerChannelOfKind` (validates kind ∈
  {public,voice}, Rule B; `CreateServerChannel`/`InCategory` keep their signatures and delegate with
  "public" — zero blast radius); `ListServerChannels` surfaces `kind` but normalizes `'public'→""` so a
  text channel's JSON stays byte-identical (only voice carries `"kind":"voice"`). `POST .../channels`
  accepts an optional `kind` (default public, 400 on anything but public|voice), admin-gated as before.
  The voice infra (per-channel `voiceMembers`, voice-join WS path, `VoiceMembersFor`) is kind-agnostic,
  so a voice channel reuses it unchanged. Store + route integration tests (create voice + list kind +
  invalid-kind rejected; route 403 non-admin / 400 bad kind / 201 kind='voice'); go build/vet/test green;
  shipped + `railway up` + **live E2E verified** (201 voice / unchanged text / 400 bad-kind / 403
  non-admin on the deploy). The client create-channel UI doesn't send `kind` yet → no UI change until 3b.
  **Slice 3b DONE (iter 173): voice channels in the client.** A 🔊 voice channel you create (new
  "+ voice" button), click to open a centered **join-to-talk view** (text composer + message list
  hidden), Join → the in-call voice bar, Disconnect → back. Participants list **beneath** the channel
  row in the sidebar (Discord-style), resolving ids→names via the active server's `memberList`, reusing
  `serverVoice` presence. Architectural note: mesh voice rides the per-channel WS, so being "in" a voice
  channel == it being your active channel. tsc clean, vitest 77/77, go build/vet/test green; browser QA
  grew a create→🔊 row→join-view (no composer)→Join→in-call bar→Disconnect flow (all green); AI-vision
  verified the join view, in-call view, and sidebar participants-beneath; blast-radius guard PASS;
  shipped + `railway up` + **rollout-verified** (live JS/CSS bundles carry the new code).
  **Slice 3c DONE (iter 174): voice-channel header polish.** The header now reads as a call, not a text
  room — the brand shows `🔊 <name>` (not `#`) and the inapplicable text-channel actions (make-read-only,
  slowmode, edit-topic, pins, threads, message search) + the header's own Join-voice/N-in-voice buttons
  (redundant with the main view) are hidden; the 🔔 mute toggle stays. All gated on `!activeChannelIsVoice`
  so text/DM/thread headers are byte-identical. tsc/vitest/go green; browser QA grew a regression guard
  (header shows 🔊 + hides readonly/slowmode/topic/pins/threads/search); AI-vision confirmed; shipped +
  rollout-verified. **✅ Voice channels FULLY USABLE + POLISHED (3a backend · 3b UI · 3c header).**
  **Slice 3d DONE (iter 175): live roster + presence assertion.** The voice view's "who's here" roster
  now reads the LIVE `voicePresence` (updated on every WS voice-presence event — the open voice channel
  IS the active channel) instead of the ~15s polled `serverVoice`, so a join/leave shows instantly with
  no poll lag (falls back to the polled map only until the live list arrives). Browser QA now asserts the
  view roster lists SELF after joining (a real end-to-end presence assertion + a deterministic in-call
  screenshot); AI-vision confirmed roster + sidebar participant both show; shipped + rollout-verified.
  **Slice 3e DONE (iter 176): Rule-15 hardening — voice channels are voice-only.** An adversarial pass
  found the UI hid the composer but the BACKEND still accepted text: `CanPostInChannel` checked
  post-policy + membership but not `kind`, so a hostile client could POST to a `kind='voice'` channel via
  the raw WS `message` frame or the REST attachment path (stored + broadcast to the call, never shown —
  data-integrity + unbounded-write abuse). `CreateThread` also didn't reject a voice parent. Fixed at the
  single chokepoint (`CanPostInChannel` → false for voice, closing both WS + REST paths; `CreateThread`
  adds voice to not-threadable). Reproduced the break (test RED), fixed, **re-attacked on the live
  deploy** (raw-WS post → `error` frame, not broadcast, not persisted = BLOCKED ✅), proved text channels
  still post + thread; `TestVoiceChannelRejectsMessages` encodes the exploit. **✅✅✅ Voice channels
  COMPLETE + HARDENED (3a–3e).** Next voice (optional, later): auto-join on click; background voice
  (separate WS); screen share, video, soundboard.
  **Next tick should ROTATE component** (voice had 5 ticks) — candidates: group-DM sub-slices, appearance
  polish, or a Rule-15 pass on another surface.
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
- [~] **Threads** — **slice 1 backend DONE (iter 166):** a thread = `kind='thread'` channel with
  `parent_id`, copying the parent's `server_id` so access/posting/history/WS-fanout all work UNCHANGED
  (the same channel-id-scoped reuse group DMs used). `channels.parent_id` (+ index); per-scope name
  unique indexes recreated to exclude threads (many can share a name). `CreateThread` (no DM/no nesting,
  name 1-100, copies server_id) + `ListThreads`; threads kept out of `ListChannels`/`ListServerChannels`/
  unreads. Routes `GET/POST /api/channels/{id}/threads` (gated on parent access; POST also CanPost).
  `TestThreadsIntegration` + live E2E (access inherited, absent from channel list, full adversarial set);
  shipped + `railway up` + rollout-verified (new route 401 not 404). **Slice 2 client DONE (iter 167):**
  a **🧵 threads panel** (mirrors the pins panel) opened from the channel header — lists the channel's
  threads + a "+ New thread" create; a **thread** message-hover action; `selectThread` reuses the message
  view + WS reconnect and tracks `activeThread` so the header shows "← 🧵 name" with a back-link, and the
  intro + composer reflect the thread. New `TestServeWSThreadFanoutIntegration` (3 clients on a thread →
  A sends, B+C receive; non-member handshake 403). tsc/vitest/go green; full QA green (browser=0 realtime=0
  voice=0 search=0 — new flow creates/opens/posts in a thread + confirms it's listed); AI-vision verified
  the thread view + panel; shipped + `railway up` + rollout-verified (live bundle carries "🧵 threads").
  **✅ Threads MVP COMPLETE (create + open + chat + realtime).** **Slice 3 DONE (iter 168):
  message-anchored + discoverable threads** — `channels.source_message_id`; `CreateThread(…, fromMessageID)`
  validated to a non-deleted message in the parent (Rule B/C, dropped otherwise); `Message.threadId/
  threadName` surfaced via a LEFT JOIN in the read paths; the message-hover **thread** action anchors to
  the message (or opens the existing one — one thread per message), and a clickable **🧵 {name}** chip
  renders under the source message. `TestThreadsIntegration` extended (anchor recorded + surfaced; cross-
  channel anchor dropped); full QA green incl. a start-from-message→chip→open flow; AI-vision verified the
  chip; shipped + `railway up` + rollout-verified. Later (optional): "X started a thread" system message,
  thread unread counts, archive/auto-archive.

### Messaging
- [x] Send / receive in real time · edit / delete (owner-only) · typing indicators
- [x] Reactions (emoji) — add/remove, per-viewer counts, live WS, client UI (quick palette + chips)
- [x] Date dividers — Discord-style "Today" / "Yesterday" / full-date separators between calendar
  days in the message list (iter 146): client-only `dayLabel` (unit-tested incl. month boundary), a
  new day breaks same-author grouping; browser QA + AI-vision verified ("Today" divider renders).
- [x] Hover timestamp on grouped messages (iter 147) — grouped continuation rows reveal a compact
  gutter time ("8:53 AM", no seconds) on hover (`shortTime` unit-tested; opacity 0→1); browser QA
  asserts the reveal + AI-vision verified the rendered gutter time.
- [x] Discord-style header timestamp (iter 148) — message headers now read "Today at 9:31 AM"
  (`messageTimestamp` = relative day + compact time, no seconds) across the main list, search results,
  and pins; unit-tested + browser QA asserts the format + AI-vision verified.
- [x] "Start of channel" intro (iter 149) — Discord-style welcome block atop every channel/DM
  scrollback (round #/@ icon + "Welcome to #general!" + "This is the start of the #general channel.";
  DM variant); browser QA asserts it + AI-vision verified (composes with the Today divider).
- [~] Custom emoji (server-uploaded `:name:`) — **backend DONE (iter 150, slice 1):** `server_emoji`
  table + store CRUD + admin-gated upload/delete, member-gated list, public serve (local-disk like
  avatars, Rule A); Rule-15 hardened (sniffed image allowlist → no SVG/XSS, opaque keys → no traversal,
  size cap, scoped delete) and verified by `TestServerEmojiIntegration` (9 cases incl. all adversarial).
  **Client `:name:` render DONE (iter 151, slice 2):** `markdown.tsx` renders `:slug:` as an inline
  `<img>` from `/api/emoji/{id}` for names in the active server's emoji map (per-server cached;
  literal in #general/DMs), XSS-safe (React img, numeric src); 8 vitest cases + a browser E2E
  (upload via API → `:qa_emoji:` → `img.emoji-inline`) + AI-vision verified, no markdown regression.
  **Manager UI DONE (iter 152, slice 3a):** an admin **Emoji** section in the members/server-settings
  panel — list + upload (name + image) + delete, with `refreshEmojiCache` so `:name:` resolves LIVE
  (no reload); browser E2E (UI upload → live render → delete) + AI-vision verified. **Custom emoji is
  now complete end-to-end** (backend + render + manager). **Picker DONE (iter 153, slice 3b):** a
  composer 🙂 popover lists the server's emoji and inserts `:name:` at the caret (browser E2E +
  AI-vision; reaction palette untouched). **Custom emoji is FULLY complete (backend+render+manager+picker).**
  · stickers · GIF picker (later)
  **+ custom-emoji REACTIONS (iter 155):** react with a server's custom emoji (`custom:{id}` marker in
  the reactions emoji column; palette lists them; chips render the image). **+ P1 FIX (iter 155):** emoji
  images NEVER loaded in-browser (raw `<img src=/api/emoji/{id}>` 401'd — img tags can't send the bearer
  token); fixed via a new `EmojiImg` fetch+blob component (mirrors `Avatar`), wired into every emoji
  render site. Caught by a new `naturalWidth>0` QA assertion (element-existence checks had missed it for
  4 ticks). QA lesson logged: assert auth-gated images LOAD, not just exist.
- [x] Markdown — bold/italic/strikethrough, inline & fenced code, `> ` blockquotes,
  `||spoilers||` (click to reveal), `- `/`1. ` lists, autolinked URLs, **and `#`/`##`/`###`
  headers + `-#` subtext (Discord parity, iter 221)** — styled visual hierarchy (h1 1.5em → h3,
  bold; subtext small+muted), inline markdown still works inside a header, `#channel`/`####`/bare
  `# ` correctly NOT headers; XSS-safe (React elements, no innerHTML — header content flows through
  the same `renderInline`); vitest + browser QA + AI-vision verified.
- [ ] **Markdown follow-ups (found iter 221): masked links `[text](url)` + underline `__`.** Masked
  links are higher-usage than headers but carry a PHISHING surface (display text ≠ destination) — do it
  with a Rule-15 pass: validate the URL to http(s)-only (reuse the autolink scheme guard, NEVER
  javascript:/data:), add `title`=the real URL on hover (anti-spoof), target=_blank + rel=noopener. Add
  the exact adversarial vitest cases the autolinker has (javascript:/data: in the `(url)` slot stays
  inert). Underline `__text__` is lower-value and tricky (collides with `_italic_` parsing) — defer.
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
- [~] 1:1 DMs (done) · **user blocking backend DONE (iter 157, slice 1)** — `user_blocks` table +
  block/unblock/list API + SYMMETRIC DM enforcement (block → can't open/send/read the DM, either
  direction) via the central `CanAccessChannel` gate (+ CreateOrGetDM + ListDMs filter); server-channel
  access unaffected (regression-tested); Rule-15 adversarial integration tests. **Slice 2:** block
  button (profile card/member row) + blocked-list in Settings + hide blocked users' messages in
  channels. **Client slice 2 DONE (iter 158):** Block/Unblock button on the profile card + a Settings
  → Privacy blocked-users list (+ hint), and blocked authors' messages are HIDDEN in every channel
  (pure `visibleMessages` filter; live WS auto-hidden). Two-author browser E2E (block → message
  disappears → unblock → reappears) + vitest. **User blocking COMPLETE (backend + client).**
  · group DMs · friends / friend requests (later)

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
- [~] **About Me + pronouns + a profile card DONE (iter 140)** — set an About Me (≤190, Discord's
  cap) + pronouns (≤40) in Settings → My Account (`PUT /me/profile`, trimmed/capped/clears,
  JWT-derived, Rule B/C); they're carried on the member list (`users.about`/`pronouns` columns,
  `ListServerMembers` selects them) and shown on a **profile card** — click a member → a centered card
  overlay (Esc/overlay close) with avatar + presence pip, name, pronouns, custom status, and the About
  Me bio (all React-escaped). Store integration test (set/trim/cap/clear + surfaces via members) +
  browser QA (set in settings → click my row → card shows them) + AI-vision verified. **Profile card
  from a message author DONE (iter 141)** — clicking a message's avatar/name fetches the author's
  public profile (`GET /users/{id}/profile`, any authed user, public fields only, effective presence,
  404 for missing — Rule 15) and opens the same card, so it works in #general / DMs (no member list).
  Store test (GetUserProfile + not-found) + browser QA (click a message author → card) + **a Rule-15
  XSS-inert assertion** (an `<img onerror>` in About Me renders as literal text, no element/script
  injected — AI-vision-confirmed). Connections · per-server nicknames TODO
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
  **Browser tab badge DONE (iter 138)** — `document.title` reflects activity so a
  backgrounded tab signals it (Discord-style): `(N) • Opencord` when you have unread
  @mentions (N = the count), `● Opencord` for plain unreads, plain `Opencord` when all
  read; excludes the channel you're viewing; resets on logout. Reuses the existing unread
  map (no new state/endpoint); two-client realtime QA asserts the three title states.
  **Per-channel mute DONE (iter 139)** — mute a noisy channel and it stops surfacing as unread: a
  `channel_mutes(user_id, channel_id)` table + `Unreads` gains `NOT EXISTS (channel_mutes)`, so the
  sidebar dots, mention badges, AND the tab badge ALL vanish from one server-side change.
  `POST`/`DELETE /channels/{id}/mute` (access-gated, Rule B/C) + `GET /me/muted-channels`; a header
  🔔/🔕 toggle + a sidebar dim. Store integration test (excludes from Unreads, per-user, idempotent,
  reversible) + browser toggle + two-client realtime (B mutes → A posts → B gets NO dot/tab badge).
  **Desktop notifications DONE (iter 156)** — Web Notifications API (Rule-A graceful): when the tab is
  UNFOCUSED and a new active-channel message is a DM or @-mentions you, show a desktop notification
  (author + snippet; click focuses). Off by default; opt-in via a new Settings → Notifications tab
  (requests OS permission on the click, persists only if granted). Pure `shouldNotify`/`mentionsMe`
  (`notify.ts`, 21 vitest cases) + browser E2E (stub Notification, force hidden, 2nd user @mentions →
  notification constructed w/ author+body). Server-level mute · web push TODO

### Platform / Integrations
- [ ] Bot/API + webhooks + slash commands · OAuth2 app authorization
- [~] **Accessibility** — WCAG-AA color contrast fixed (links/mentions/green labels/
  avatar initials) + axe-core WCAG scan in the browser QA (0 serious/critical, regression-
  guarded). Keyboard-nav · screen-reader · i18n · theme (dark/light) still TODO
- [~] Desktop + mobile clients (PWA first) — **slice 1 DONE (iter 190):** installable PWA
  foundation — branded SVG favicon (accent squircle + chat bubble), `manifest.json`
  (display:standalone, theme/bg #1e1f22, maskable icon), theme-color + description + text-only
  Open Graph in the head. Served by the embedded Go binary (favicon→image/svg+xml,
  manifest→application/json), browser-QA `0b` + rollout-verified on prod. **Next slice:** a
  service worker for an offline app-shell (deferred — caching a realtime app needs care: never
  cache `/api`/`/ws`, only the static shell) and PNG icons (192/512) for broader install support.

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
- [x] **Prove mute/PTT/deafen by RECEIVER silence (not just the UI label)** — DONE (iters 144–145):
  `qa/voice.mjs` proves all three audio-gating guarantees two-client via the `measureRms(page, audioId)`
  helper (decoded inbound RMS through an AnalyserNode), each measured at B for A's track:
  **mute** 0.31 → **0.0000** → 0.24 (unmute); **deafen** ("also mutes your mic") 0.31 → **0.0000** →
  0.30 (undeafen); **PTT** idle (on, not held) **0.0000** → held 0.31. The silenced state is exactly
  0.0000 (true silence at the peer), so "the UI says muted" is now "the peer provably hears nothing" —
  the most safety-critical voice guarantee. Also reconfirms slice-3d's capture gain node didn't break
  any of them. The receiver-RMS primitive is now reusable for future audio features (noise gate, per-peer input).

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
