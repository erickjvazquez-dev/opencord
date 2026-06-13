# Opencord — Loop Improvements & QA Reflections

One entry per self-improve tick (newest first): what the loop learned about its own
QA, coverage, or process. Appended by `/self-improve-opencord` step 6.5 ("Reflect").

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
