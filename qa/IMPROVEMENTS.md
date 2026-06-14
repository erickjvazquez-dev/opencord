# Opencord — Loop Improvements & QA Reflections

One entry per self-improve tick (newest first): what the loop learned about its own
QA, coverage, or process. Appended by `/self-improve-opencord` step 6.5 ("Reflect").

## 2026-06-14 (iter 37) — Read-only channels: per-channel posting policy (last roadmap slice)

The final roadmap item: `channels.post_policy` ('everyone'|'admins') — an admins-only
channel is a read-only announcement channel. Gated the **WS send path** (the new attack
surface) by folding the check into `store.Save` (every caller covered, defense in depth),
returning `ErrForbidden`, which `readPump` turns into an `error` frame to the sender.
`PATCH /api/channels/{id}` sets the policy (server admins only). Backend-only; the
set-read-only UI is the follow-up.

Reflections:
- **Gate the write at the store, not the handler, when there are multiple entry points or a
  hot path.** Messages only flow through one WS handler today, but putting the policy check
  in `Save` means a future REST-post, an import tool, or a bot can't bypass it. The cost is
  one indexed lookup per message — acceptable, and correctness on a permission check beats a
  micro-optimization.
- **A dropped WS frame needs an out-of-band "no" or it reads as a bug.** The send path used
  to `continue` on any Save error (rate limit, etc.) — fine for those, but a *forbidden*
  post silently vanishing looks broken. Mapped `ErrForbidden` → an `error` frame so the
  sender learns why. (The client doesn't surface it yet — that's the UI slice, and no user
  can reach a read-only channel until the set-policy UI ships anyway.)
- **Verifying a WS *rejection* needs a listening client, not just a status code.** The live
  check connected a member socket, sent, and asserted both the `error` frame AND that the
  message never persisted (GET /messages) — a 2xx/4xx wouldn't exist for a WS frame.
- **The roadmap is essentially complete.** v0.2 + v0.3 are built and verified end-to-end.
  Remaining are UI polish (set-read-only control, error toasts) and net-new features beyond
  the original parity list (threads, attachments, voice) — all good fresh-context work.

## 2026-06-14 (iter 36) — Moderation UI: the admin delete button (role-aware client)

Surfaced last tick's moderation backend in the UI. The client now needs to know its role
per server, so `ListServers` (and CreateServer/RedeemInvite) carry the requesting user's
`role`; the chat computes `canModerate` for the active server channel and shows the delete
button on *others'* messages when I'm owner/admin (edit stays author-only). Verified E2E in
the two-client realtime QA: B posts → A (owner) clicks delete → B sees `[deleted]` live.

Reflections:
- **A backend permission only becomes a feature once the client can ask "what am I here?"**
  Moderation was enforced server-side last tick, but invisible until the client knew its
  role. Cheapest answer: fold the viewer's role into the resource it already fetches
  (`/servers`), rather than a separate "my permissions" call per channel.
- **Authorize on the server, *hint* on the client.** The delete button is shown via
  `canModerate`, but the actual permission is still enforced in `DeleteMessage` (iter 35).
  The UI flag is a convenience/affordance, never the gate — a hand-crafted DELETE still
  hits the server check. Keep that split explicit so a UI bug can't become a security bug.
- **No new pixels, no new screenshot — but say so.** This tick added a *conditional* on an
  existing, already-vision-graded button (delete) and reused the verified `[deleted]`
  render. The assertion (B sees `[deleted]` live) covers behaviour; I logged that there's no
  new visual surface rather than silently skipping the vision step.
- **Roles & permissions is now feature-complete bar per-channel overrides** — the last
  remaining slice is channel-level permission overrides (e.g. a read-only announcement
  channel), which is a bigger model change and a good fresh-context candidate.

## 2026-06-14 (iter 35) — Message moderation: server admins can delete others' messages

`DeleteMessage` now allows the author OR an admin of the message's channel's server (the
moderation core). Backend-only this tick — the admin delete *button* is the next UI slice.
Full Rule-15 cycle live: a plain member deleting another's server message → 404; the owner
(and a promoted admin) → 204; public/serverless channels are unaffected (non-author → 404,
the original behaviour).

Reflections:
- **Widening a permission means re-checking the rows that relied on it being narrow.** The
  old delete was a one-line `UPDATE ... WHERE id AND user_id`. Loosening it (author OR
  admin) required a SELECT-then-authorize-then-UPDATE — and the existing
  `TestDeleteMessageIntegration` (non-author → not-found) had to keep passing. It did,
  because for a *public* channel there's no server admin path; I verified that explicitly
  rather than assuming the refactor preserved it.
- **Return the same error for "not allowed" as for "not found" on a delete** — a member
  probing message ids shouldn't learn which exist in a channel they can moderate-but-not.
  Kept `ErrMessageNotFound` for the unauthorized path (→ 404) rather than a distinct 403,
  matching the existing non-leak behaviour.
- **Edit deliberately left author-only.** Moderation is about *removing* abuse, not
  rewriting someone's words — admins can delete but not edit others' messages. Scoping a
  permission tightly is itself a security decision.
- **Next:** the moderation UI (show the delete button on others' messages when I'm an admin
  of the active channel's server — needs the client to know its role per server, via a
  `role` on the servers list), then per-channel permission overrides.

## 2026-06-14 (iter 34) — Roles UI: members panel + the owner's promote control

Made last tick's roles backend usable in-app: `GET /servers/{id}/members` + a members
panel (avatar · name · role badge), with an owner-only make-admin/demote toggle per
non-owner member. Verified two ways: single-client browser QA (owner sees the OWNER
badge) and a **two-client** realtime test — A (owner) opens the panel and promotes B
(member) → B's badge flips to ADMIN live (`rt-07-roles.png`).

Reflections:
- **The panel pattern paid off a third time.** Search, then... actually the members panel
  reuses the exact "replace the message area + ✕ close" structure as search — same
  `.search-results` shell. Three overlays (search, members) now share it; a shared
  `<Panel title onClose>` component is the obvious next refactor if a fourth appears.
- **CI-coverage gap caught:** the members list is exercised by the browser/realtime QA,
  which DON'T run in CI (they need a live browser). So `ListServerMembers` would have had
  zero CI coverage — extended `TestServerRolesIntegration` to assert it (owner-first
  ordering + the promoted admin). Rule: anything only the Playwright QA touches still needs
  a Go integration test, or CI is blind to it.
- **Roles is now usable end-to-end** (create server → invite → join → owner promotes in the
  UI → admin can create channels). Remaining roles slices: message moderation (admins delete
  others' messages — needs the client to know its role in the active channel's server) and
  per-channel permission overrides.

## 2026-06-14 (iter 33) — Server roles, first slice (the big v0.3 feature, bounded)

Began roles & permissions with a deliberately small, Discord-shaped slice instead of the
whole thing: `server_members.role` (owner|admin|member), channel creation gated to admin+
(members can't — the Discord default), and owner-only promote/demote
(`POST /servers/{id}/roles`). Backend only; per-channel overrides, message moderation, and
a UI are later slices. Full Rule-15 cycle live: member create-channel → 403 until the owner
promotes them → admin → 201; member self-promote → 403; invalid role → 400.

Reflections:
- **A huge feature is a sequence of small verified slices, not one commit.** "Roles &
  permissions" sounds like a monolith; the first useful, shippable unit is one role enum +
  one gated action + one management endpoint. Each slice ships green and de-risks the next —
  better than a giant branch, especially deep in a long session.
- **Tightening an existing gate is a regression risk — check who relied on the old rule.**
  Channel-create went member→admin. The QA flows create channels only as the server *owner*
  (an admin), so they stayed green — but I verified that explicitly rather than assuming.
  When you narrow a permission, grep every caller/test that exercised the looser one.
- **Roles QA is a two-actor story** — the next slice's QA should be: owner promotes B in the
  UI, B (now admin) can create a channel; before promotion B has no "+ channel". That needs
  the roles UI first, so it's coupled to the UI slice.
- **Next:** roles UI (show role, owner's promote control) + message moderation (admins delete
  others' messages); then per-channel permission overrides.

## 2026-06-14 (iter 32) — In-channel search, with LIKE-wildcard hardening

Added message search within a channel: `GET /api/messages/search?channel&q`, a header
search box, and a results panel (count + matches, ✕ clear). Access-gated like every read
path. Verified at three layers: store test, browser-QA (find + clear) + vision, and a live
HTTP adversarial run (member 200, empty q 400, non-member 403).

Reflections:
- **A LIKE/ILIKE search is an injection surface even when parameterized.** The query is a
  bound parameter (no SQL injection), but `%`/`_` are LIKE *wildcards* — a user typing `%`
  would otherwise match-all. Escaped them (`\ % _` → `\\ \% \_`) so the term matches
  literally; encoded the exact case as a test (`%` → only the literal-`%` message) and a
  live check (`%25` → 0 matches). Rule B isn't just "bound params"; it's "what does each
  metacharacter mean in the sink?"
- **Self-inflicted process slip (caught, no harm):** the standalone live-search probe failed
  ECONNREFUSED because the *previous* qa/run.sh had torn the DB down and I didn't re-boot it.
  Reminder for ad-hoc live checks: they don't inherit the harness's stack — boot db + server
  + assert /healthz first (the same fail-loud lesson from the iter-25 harness fix, applied to
  one-off probes).
- **Next:** roles & permissions (the big v0.3 item — spec-first, fresh context); later,
  channel-spanning search (search all channels you can access, grouped by channel).

## 2026-06-14 (iter 31) — Two-client server flow: invite → redeem → live chat (the deferred QA)

Closed the two-client server QA I'd logged pending for three ticks. realtime.mjs now,
after the DM flow: A creates a server + channel, mints an invite (code captured from the
shown prompt), posts a message; B redeems the code, auto-lands in the server channel,
reads A's history, and then receives A's *next* message live (`rt-06-bob-server.png`).
This is the realtime, through-the-UI proof of the whole servers+invites stack — the
backend/HTTP checks couldn't show the cross-user live path. Pure QA (only realtime.mjs)
— no production risk.

Reflections:
- **A flow that prompts more than once per page needs per-page mutable answers.** The DM
  step had hard-wired `a.on('dialog', d => d.accept(userB))`, which would have answered
  the server-name prompt with a username. Refactored to an `ans = {a, b}` holder set
  before each action, plus `aDefault` to capture the invite code the prompt pre-fills.
  Same pattern as browser.mjs — now both harnesses share it.
- **Redeem auto-navigates, so the QA didn't need to click the channel** — `joinServerPrompt`
  selects the server's first channel after redeem, so B just lands there. Testing the
  real handler's side effects (not re-implementing navigation in the test) kept it short.
- **Coverage milestone:** every members-only surface (DM, server channel) now has BOTH a
  backend adversarial "outsider is blocked" test AND a two-client "member gets in and
  chats live" UI test. That pairing is the template for the next private feature.
- **Next:** roles & permissions (the big v0.3 item — spec-first, backend then UI) and search.

## 2026-06-13 (iter 30) — Server invites: a feature that closes a gap I shipped

The first servers slice (iter 27) shipped an **open** `POST /servers/{id}/join` — anyone
could join any server by guessing its sequential id, defeating the members-only access
control. Replaced it with **invite codes**: a member mints an unguessable 8-char code
(crypto/rand), redeeming it admits you; the open join endpoint is gone. Full Rule-15
cycle, all live-verified: old join → 404 (gap closed), non-member read → 403, non-member
mint → 403, bogus code → 404, real code → 200 then read → 200. Plus a store integration
test and a browser-QA invite-button check.

Reflections:
- **A feature can be the fix for a gap you shipped.** The "members-only" servers weren't
  actually private until joining was gated. When you add access control, audit *every*
  way in — I gated reads/WS but left an open join door for three ticks. New private
  resources need a "how does someone get IN, and is THAT gated?" check, not just "can an
  outsider read it?".
- **QA pattern — capturing a prompt's value:** the invite code is shown via
  `window.prompt(msg, code)`. To assert it in browser QA, the dialog handler records
  `d.defaultValue()` into a node var, and the test polls it (the prompt fires after an
  async round-trip, so a poll, not a bare check). Reusable for any "we showed the user a
  generated value" flow.
- **Still pending (now 3 ticks):** the two-client server flow (A invites → B redeems →
  B chats live) — the realtime analogue of today's single-client + HTTP checks.

## 2026-06-13 (iter 29) — Mobile-responsive layout; a transition-vs-screenshot QA lesson

First v0.3 item: at ≤640px the sidebar collapses into an off-canvas drawer behind a
header `☰` toggle, the chat goes full-width, a backdrop dims it, and selecting a channel
closes it (`selectChannel` helper). Pure CSS media query + a little drawer state — no
data/access changes. Verified E2E (toggle opens/closes the drawer) + by eye (`08-mobile-
closed` full-width chat; `08-mobile-open` drawer over a dimmed chat).

**QA lesson — screenshots can catch an animation mid-flight.** The first mobile shots
looked broken: "closed" showed the sidebar half-on, "open" showed it half-off. Not a CSS
bug — the 0.2s slide `transition` was still animating when the screenshot fired (the QA
resizes desktop→mobile mid-session, which animates the transform; a real mobile load
renders hidden from the start, no flash). Fix: `waitForTimeout(350)` to let the transition
settle before the shot. Lesson: **when vision-checking anything with a CSS transition,
wait for it to settle first**, or the artifact lies. Don't "fix" a transition artifact by
changing correct CSS.

Reflections:
- **The AI-vision pass earned its keep again** — the assertions were green (the drawer
  *did* open/close), but only the screenshot revealed the mid-transition capture. A
  text/DOM-only QA would have shipped misleading artifacts.
- **Close-on-select via one helper:** routing all three sidebar lists' clicks through
  `selectChannel(id)` (set channel + close drawer) kept the mobile behaviour in one place
  instead of sprinkling `setSidebarOpen(false)` across handlers.
- **Next:** the two-client server flow (still pending from iter 28) and a mobile
  vision-check of the *open DM/serverlist* drawer states, not just channels.

## 2026-06-13 (iter 28) — Servers UI: a sidebar accordion over the existing chat machinery

Shipped the servers UI: a "Servers" sidebar section listing each server (name + id badge)
with its channels indented (accordion), plus create-server / join-by-id / create-channel.
Selecting a server channel reuses the existing chat machinery unchanged — the header,
composer, history, send, reactions, and grouping all "just work" because a server channel
is still a channel. Verified single-client E2E (create server → add channel → post) and
by eye (`07-server.png`: three clean sidebar sections, accordion, active server channel).

Reflections:
- **A multi-source dialog QA needs answer routing.** browser.mjs had one fixed prompt
  answer; the server flow has two prompts (server name, channel name) with the *same*
  message text as the existing channel prompt, so message-text routing wouldn't work. Made
  the dialog handler read a mutable `promptAnswer` set before each action — the clean
  pattern for a flow that prompts more than once. Reuse it for future multi-prompt flows.
- **"It's still a channel" kept the UI tiny.** Because DMs and server channels are both
  just channels with access control, three different sidebar sources (global, DM, server)
  all feed the *same* `channelId` → one chat view. Resolving the active channel's display
  name across all three sources (`current ?? activeServerChannel`, plus `activeDM`) was the
  only header/composer change. Modeling the access layer in the backend paid off in the UI.
- **Next QA growth:** a two-client server flow — A creates a server, shares its id, B joins
  and sees the channel + a live message (the realtime analogue of the single-client test);
  and a non-member-can't-see-it isolation check through the UI.
- **Polish (P1, logged):** the Servers section is dense at the bottom of a tall sidebar; a
  Discord-style left server rail is the eventual shape. Fine for the MVP; not a P0.

## 2026-06-13 (iter 27) — Servers/guilds backend; an idempotent-migration bug the suite caught

Shipped the servers/guilds backend foundation (servers + members + server-scoped
channels, per-server channel names, REST create/list/join, members-only access). Built
additively so global channels still work (a channel with `server_id IS NULL` stays a
global room). Verified at all three layers: store (`TestServersIntegration`), live HTTP
(owner 200 / outsider 403 on a server's channels + message read), and live WS (outsider
403, owner 101, join→101); global list excludes server channels.

**The bug the test suite caught — a non-idempotent migration.** Allowing each server its
own `#general` meant replacing the table-wide `UNIQUE(channels.name)` with partial unique
indexes. But the default-channel seed used `INSERT ... ON CONFLICT (name)`, which has no
arbiter once the constraint is dropped — so the *second* `db.Migrate` (every test re-runs
it) failed with `42P10`. Caught because the integration tests each migrate a shared db, so
the 2nd test failed loudly. Fix: a constraint-independent guarded seed
(`INSERT ... SELECT ... WHERE NOT EXISTS`). Re-verified by an 8×-migrate run on a fresh db.

Reflections:
- **A migration must survive running twice — test that explicitly.** The suite caught it by
  luck (shared db, sequential migrates). A dedicated "migrate twice, assert no error" test
  would catch this class deterministically; worth adding for any schema that drops/renames.
- **Blast radius of a uniqueness change:** dropping `UNIQUE(name)` silently broke
  `DefaultChannelID` (its `WHERE name='general'` became ambiguous across per-server
  generals). Grepped every `name =` lookup and scoped it to `server_id IS NULL`. When you
  change a constraint, grep every query that relied on it.
- **Node 23 `fetch` can't set `Connection: Upgrade`** (undici rejects it) — for WS-status
  probes use `curl` (it worked for both the DM and server WS 403 checks). Node's built-in
  `WebSocket` is for real WS sessions, not status-only probes.
- **Next:** the servers **UI** (server rail + per-server channel list + create/join), then
  a two-client server-isolation browser flow.

## 2026-06-13 (iter 26) — Closed the DM reaction-access gap (full Rule-15 cycle)

Closed the security follow-up logged when DMs shipped: reactions weren't access-checked,
so a non-member could react to a private DM message by guessing its id (ids are
sequential). Ran the complete Rule-15 loop: **reproduced** (a new test failed —
`non-member AddReaction err = <nil>`), **fixed** (`requireChannelAccess` gate in
`AddReaction`/`RemoveReaction` → `ErrForbidden` → HTTP 403), **re-attacked at both layers**
(store test passes; live server: carol PUT *and* DELETE → 403, member → 200), proved no
regression (members + public reactions still work, full suite green), and **encoded** it as
`TestDMReactionAccessControlIntegration`.

Reflections:
- **Every guessable-id endpoint on a private resource needs its own gate.** The read/connect
  gate (iter 24) wasn't enough — *write* paths that take an id (react, and later: pin, edit
  others', report) each need the same `requireChannelAccess` check. New private surfaces
  should get an access-control test per mutating verb, not just per read.
- **Node 23's built-in `WebSocket` unblocked live WS-layer adversarial checks** with zero
  deps — used it to send a real DM message, then curl-react as a non-member. Worth keeping in
  the toolkit for any WS-only flow the curl/HTTP harness can't reach.
- **Verified at two layers on purpose:** the store test proves the authorization logic; the
  live HTTP run proves the handler maps `ErrForbidden`→403. A thin mapping is still a layer
  that can be wrong.

## 2026-06-13 (iter 25) — DM UI + a QA-harness P0: the harness was testing STALE code

Shipped the DM UI (sidebar "Direct Messages" section, "+ New DM" flow, DM-aware
header/composer) — a DM just selects its channel, so the existing chat machinery
(history/send/edit/react/typing) works unchanged. Verified two-user E2E: A opens a DM
with B, sends; B reloads, opens it from the sidebar, reads it (`rt-04`/`rt-05`, mirror
views, both titled with the *other* user).

**The real find — a QA-harness P0 that had been silently lying.** The DM browser QA
failed at first; the server log showed `:8080 bind: address already in use`. Root cause:
`qa/run.sh` started the server with `go run ./cmd/server`, which spawns a **temp child
binary**; the cleanup `pkill -f "cmd/server"` killed the `go run` parent but not the
child, so an **orphaned server held :8080 across runs**. Every subsequent `run.sh` failed
to bind, and the QA silently tested the *stale* backend. It went unnoticed for ticks 20–23
only because those changes were frontend/QA-only; the DM UI was the first to need *new
backend*, which the stale server lacked.

Fixes (Track 0, high value): `run.sh` now (a) frees :8080/:5173 before starting, (b)
**builds a real binary and runs that** (killable by PID, no orphan), and (c) **fails loud**
if `/healthz` isn't up — never again silently runs against a stale server.

Reflections:
- **"Tests pass" can mean "tests ran against the wrong build."** A health check on the
  thing-under-test (is OUR server actually up?) belongs in every harness, not just checks
  on the feature. The loop's iter-14-style "verify on the live thing" must include "verify
  it's the *current* live thing."
- **`go run` is a QA footgun** — its orphan child outlives naive `pkill`. Build+exec, or
  kill by port, in any throwaway-server harness.
- **Next:** live DM-list updates (today B must reload to see a new DM — push a WS event on
  DM creation); a three-user UI isolation check once a 3rd context is worth the cost.

## 2026-06-13 (iter 24) — Direct messages: backend slice + the first access-control surface

First big v0.2 feature, built backend-first (reactions pattern). A DM is a new channel
**kind** with a `channel_members` table; the genuinely new piece is **per-channel access
control** (`CanAccessChannel`) gating both the WS upgrade and REST history. Verified two
ways before "done": DB integration test (`TestDirectMessagesIntegration`) on real Postgres,
AND a live-server adversarial pass — carol (non-member) hitting the DM's REST history and
WS both returned **403**, while alice (member) got 200 (Rule 14 + Rule 15).

Reflections:
- **The first private surface changes the QA shape.** Until now every channel was public,
  so QA never tested *authorization*. From here, every read/write path needs a
  "non-member is denied" probe — I added one at the HTTP/WS layer this tick; the browser QA
  should grow a two-client DM-isolation flow when the UI lands (A and B DM; C must not see
  it). This is the access-control analogue of the per-channel-isolation gap from iter 21.
- **Test helper as shared infrastructure:** extending `setup()` to also return the pool (so
  tests can register multiple users) unlocked the multi-user tests DMs/roles/membership all
  need. The 5 call-site updates were mechanical; worth it — future membership tests reuse
  `regUser(t, pool)`.
- **Known follow-ups (logged in SPEC):** (a) react/edit/delete on DM messages aren't yet
  membership-gated (read path is, and IDs aren't exposed to non-members); (b) CreateOrGetDM
  has no cross-process lock — a concurrent double-open could duplicate a DM channel.
- **Next:** the DM **UI** slice (sidebar DM list + "message @user" + open a DM), then the
  two-client DM-isolation browser QA.

## 2026-06-13 (iter 23) — Guard the grouping *break* cases (the likely regression site)

Last tick shipped grouping with a single "it groups" assertion. The more likely
regression is the *inverse* — grouping failing to BREAK — so this tick added both
break cases: (a) browser QA, after deleting the message above a grouped follow-up,
asserts the follow-up un-groups and its avatar returns; (b) realtime QA, B replies
after A and asserts a different author is NOT grouped (keeps its avatar). No product
code changed — pure Track-0 coverage.

Reflections:
- **One screenshot proved every rule at once** (`rt-01b-different-authors.png`): a
  `[deleted]` row, a same-author message that un-grouped *because the row above it was
  deleted*, then two different-author rows — i.e. the exact same "second line" body that
  GROUPS in the single-client test correctly UN-groups here under different context. That
  is the strongest kind of QA evidence: the same code, opposite-but-correct outcomes.
- **Test the inverse of every new conditional.** A feature that hides UI under a
  condition needs a test that the UI *returns* when the condition flips — assert both
  edges, not just the one you built toward.
- **Next QA growth (still open from iter 21):** per-channel isolation across two clients
  — A in #general, B in a second channel; a message in one must NOT appear in the other.

## 2026-06-13 (iter 22) — Closed the message-grouping P1 the loop's own QA raised

The loop's AI-vision QA flagged (iter ~19) that consecutive same-author messages
repeat the avatar + name — un-Discord-like. This tick closed it: consecutive
same-author messages within 5 min collapse into a tight block (avatar gutter kept via
a spacer so bodies align; name/time/avatar hidden). A deleted message breaks the run.
Verified by eye in `03a-grouped.png` — clean, aligned, tight.

Reflections:
- **The find→fix loop worked as designed:** a P1 *surfaced by AI-vision* one tick became
  a *closed + regression-guarded* item a few ticks later. The screenshot review is
  earning its keep — it catches polish gaps no assertion would.
- **Blast-radius win:** relocating the hover action toolbar out of the (now-conditional)
  message header into a floating absolute element could have broken edit/delete/react.
  The existing browser-QA flow caught that risk for free — all three still PASS — which
  is exactly why the harness drives real clicks, not DOM assertions. New assertion added:
  "consecutive same-author message is grouped / hides the repeated avatar."
- **Next QA growth:** assert a group *breaks* correctly — a different author, or a
  >5-min gap, or a deleted message in the middle must start a fresh (avatar-bearing) row.
  That's the inverse of what I tested and the more likely regression site.

## 2026-06-13 (iter 21) — Two-client realtime QA (the fan-out the single client can't prove)

The whole point of Opencord is *realtime* — yet every QA so far drove a **single**
browser, so live fan-out between users was never actually proven (the unchecked
"## Now: two users, live message" item sat open). Added `qa/realtime.mjs`: two
independent browser contexts (alice + bob), wired into `run.sh` after the single-client
suite (combined exit code). It asserts, with no reload:
- A sees **2 online** after B joins (presence fan-out).
- B receives A's message live; B sees A's reaction chip live.
- Cross-client `mine` correctness — A's chip is accent/"mine", B's identical chip is the
  neutral non-mine style (the count-only broadcast carries `mine=false`). Confirmed by
  **eye** in `rt-02` (B, neutral) vs `rt-03` (A, accent, count 2).
- B reacts too → the count climbs to **2 for both, live**, and A's "mine" survives the
  count-only update.

Reflections:
- **A single-client harness structurally cannot test a realtime app.** This is the
  highest-leverage QA shape for Opencord; future realtime features (typing seen by the
  *other* user, read state, DMs) should get a two-context assertion here, not a solo one.
- **Bonus catch (vision):** `rt-02` also shows B's live "alice is typing…" — typing
  fan-out works across clients, now captured by a screenshot for future regression eyes.
- **Next QA growth:** per-channel isolation across two clients (A in #general, B in a
  second channel — a message in one must NOT appear in the other); reconnect/replay
  after a dropped socket.

## 2026-06-13 (iter 20) — Reactions UI shipped for the orphaned backend

The reactions **backend** shipped last tick (`7e8ecfd`: add/remove, per-viewer counts,
live WS) but had **zero web UI** — the feature was only reachable via curl. Wired it
into the client (chips + quick-emoji palette + optimistic toggle) and grew the browser
QA to drive it (react → assert highlighted chip + count → survives edit).

Reflections:
- **Real bug the new QA caught (AI-vision + assertion):** the `message-edited` WS
  handler replaced the whole message object, so editing a message **wiped its
  reactions**. The "reaction survives the edit" assertion + `04-edited.png` proved the
  fix (merge `reactions` instead of clobbering). A static render alone wouldn't have
  found this — it took *driving* the edit after a reaction.
- **Loop-process gap → rule:** the loop should scan for "**shipped backend, missing
  frontend**" gaps every tick (a whole feature was usable only via curl). Cheap check:
  `grep` a backend route family in `web/src/` and flag any with no client caller.
- **`mine` is client-owned:** the live `reaction` broadcast is count-only (viewerID 0 →
  `mine=false`), so the UI seeds "mine" from the viewer-scoped history and overlays it
  on count updates. Document this so a future refactor doesn't trust broadcast `mine`.
- **Next QA growth:** two-client reaction propagation (A reacts → B sees the count tick
  up live, the realtime path the single-client QA can't prove); per-channel reaction
  isolation in the UI.

## 2026-06-13 — Browser QA + AI manual test introduced (owner ask)

The loop had only ever tested the **backend** (curl/WS scripts) — never the real
rendered UI. Added:
- `qa/browser.mjs` (Playwright) — drives the actual UI like a user: register → send
  → avatar → edit → create/switch channel → delete, **logging every click** and
  screenshotting each step into `qa/qa-screenshots/`.
- `qa/run.sh` / `make qa-browser` — boots a dev stack, runs the QA, tears down.
- **AI manual test** = the loop `Read`s the screenshots and grades them by eye.

Reflections from the first run:
- **P1 (AI-vision finding):** consecutive messages from the same author repeat the
  avatar + name; Discord groups them into one block. → tracked under GOAL.md
  "timestamp grouping".
- **Process fix (applied):** QA must start from a **fresh DB** (`docker compose
  down -v`), or stale data breaks assertions (two `(edited)` elements). Done.
- **Next QA growth:** add reaction-chip assertions when the reactions UI ships; add
  a two-context typing-indicator check; assert per-channel message isolation in the UI.
