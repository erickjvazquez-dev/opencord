# Opencord — Loop Improvements & QA Reflections

One entry per self-improve tick (newest first): what the loop learned about its own
QA, coverage, or process. Appended by `/self-improve-opencord` step 6.5 ("Reflect").

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
