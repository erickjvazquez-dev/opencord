# Opencord — Loop Improvements & QA Reflections

One entry per self-improve tick (newest first): what the loop learned about its own
QA, coverage, or process. Appended by `/self-improve-opencord` step 6.5 ("Reflect").

## 2026-06-14 (iter 59) — Voice slice 2 shipped (mesh WebRTC client + device intelligence); the WS-race the E2E nearly hid

Built voice **slice 2**: `web/src/voice.ts` (`VoiceSession`, one RTCPeerConnection per peer,
**perfect-negotiation** glare handling, crisp DSP + ~96 kbps Opus, **device auto-detect** following the
OS default with a manual mic/output picker — the owner's mid-tick directive), wired into `Chat.tsx`, and
a two-context fake-media E2E (`qa/voice.mjs`) proving a real `connectionState === "connected"` mesh link
+ live remote audio on both sides. Full harness green: `browser=0 realtime=0 voice=0`.

**The lesson — a green-looking E2E that was actually a silent product/test race.** The *first* full-harness
run failed at "A joins voice" (`.voice-bar` never appeared). Root cause wasn't the WebRTC code: `joinVoice`
guards on `wsRef.current?.readyState === WebSocket.OPEN`, and the test clicked "Join voice" the instant
after register — before the channel WS finished opening — so the guard silently returned and nothing
happened. **Two process gaps this exposes:**
1. **QA gap (fixed this tick):** any action that depends on the WS being OPEN must first wait for the
   connected signal (`.dot.online`). I added that wait; voice.mjs is now deterministic. The browser/realtime
   suites happen to avoid this because their first WS action is slower-arriving, but the pattern is latent
   there too — a good audit target.
2. **Product polish (fixed this tick):** `joinVoice` *silently no-ops* when the WS isn't open yet — a real
   user clicking Join voice in the first second after load got nothing, no feedback. Fixed: the Join-voice
   button is now `disabled` until `connected` (with a "Connecting…" title + a `.link:disabled` style).
   Silent guards that eat a user action are a UX bug, not just a test race — worth a broader sweep for
   other controls that fire before the WS is ready.

**Highest-value next-tick item:** the biggest remaining voice coverage gap is that **`qa/voice.mjs` only
covers the happy 2-peer path** — add a **3rd peer** (proves the mesh is genuinely N-way, not just pairwise,
and exercises the simultaneous-join glare path that perfect-negotiation exists to handle) and a
mute-audibility assertion. Then start voice slice 3 (active-speaker / SFU) groundwork.

## 2026-06-14 (iter 58) — Voice signaling shipped; clearing context for the WebRTC client build

Shipped voice **slice 1** (WS signaling relay: `voice-join`/`voice-leave`/`voice-signal`, dumb relay,
16 KiB frames for SDP, two-client test) and recorded the owner's **audio north star** (thousands /
HD / free, via OSS SFU later) + the **per-component excellence** directive (GOAL + loop skill + memory).

**This tick: clearing context (judgment trigger).** Slice 2 — the mesh WebRTC **client** (getUserMedia,
RTCPeerConnection-per-peer, STUN, offer/answer/ICE, join/leave UI, audio playback) plus a fake-media
two-context E2E — is a major new subsystem, and this session is ~58 ticks deep. The disciplined call
(framework rule for sustained deep work; I flagged it last turn) is to build it in a **fresh context**
off the now-complete SPEC, not in a long degrading one. Durable handoff is all on disk:
- SPEC "Voice channels — MVP" has the full design (mesh, the 3 signal frames, STUN, **client peer
  discovery = answer incoming offers, existing-members-offer-to-joiner, ties by user id**, trickle ICE).
- GOAL voice item `[~]` with the north star; memory `project_opencord` voice section.
- Signaling relay is committed + green; client is the only remaining slice for a testable call.

**Next tick (fresh context):** build `web/src/voice.ts` (`useVoice` hook) + wire into `Chat.tsx`
(join/leave + roster + route WS voice events) + a Playwright test launched with
`--use-fake-device-for-media-stream --use-fake-ui-for-media-stream` asserting two contexts reach an
`RTCPeerConnection` `connected` state. Then iterate audio toward the north star (OSS SFU via
`stack-guardian`).

## 2026-06-14 (iter 57) — Markdown lists; the delimiter ambiguity that needed care

Added `- `/`* ` and `1. ` lists to the renderer. The interesting bit is the `*` ambiguity: `* item`
(bullet) vs `*italic*` (emphasis). Requiring a trailing space on the bullet rule (`/^[-*]\s+/`)
disambiguates them, and I probed both to confirm `*italic*` still renders as `<em>`, not a one-item
list.

Reflections:
- **When a new rule shares a delimiter with an existing one, prove the OLD behavior still holds.** The
  risk in adding `*`-bullets wasn't the new feature — it was silently breaking `*italic*`. The probe's
  `italic-not-a-bullet` case is the regression guard for that interaction; I wrote it before trusting
  the rule.
- **block vs inline is the right axis for this parser.** Lists/blockquotes are line-grouped in
  `renderBlocks`; bold/italic/code/links/mentions are span-level in `renderInline`. Keeping that split
  clean meant lists slotted in without touching any inline logic.
- **This likely closes the markdown arc.** bold/italic/strike/code/fence/blockquote/spoiler/lists/
  mentions/links is a complete-enough set; further markdown (tables, headers) would be diminishing
  returns. Next ticks should move to a *different* surface (e.g. moderation, presence, account) rather
  than keep enriching one renderer — watch for that drift.

## 2026-06-14 (iter 56) — Pins panel UI; reused the panel pattern, kept panels exclusive

Shipped the pins panel (header "pins" button → list of the channel's pins), completing pinned
messages end to end.

Reflections:
- **Reusing the search-results panel made this a tiny diff** — same container/markup, just a different
  data source (`fetchPins`) and header. The expensive part of a panel (layout/CSS/message rendering)
  was already built; the new feature is mostly state + a fetch. Recognizing "this is the search panel
  with different data" is what kept it small.
- **Three mutually-exclusive overlays need explicit exclusivity, or they fight.** members / search /
  pins all render over the message list. I made opening any one clear the others (and a channel switch
  clear pins), and ordered the render conditions by priority — so you never get two panels stacked or a
  stale one re-appearing when you close another. Enumerated the interactions rather than hoping.
- **Pinned messages is now a complete vertical feature** across 4 ticks (iter 53 backend → 54 UI → 55
  list endpoint → 56 panel), each slice independently shipped, tested, and (for UI) AI-vision-checked —
  a good template for medium features in a long session: small, verifiable increments over one big PR.

## 2026-06-14 (iter 55) — Pins list endpoint; correctness over the cheap in-memory filter

Added `GET /messages/pins` so a future pins panel sees ALL pins, not just those among the loaded
last-50 messages. Backend slice (panel UI next).

Reflections:
- **Resisted the cheap-but-wrong shortcut.** A pins panel could just filter the in-memory `messages`
  (`m.pinned`), which needs no backend — but it would silently miss any pin older than the loaded
  window. Spent the extra ~30 lines on a dedicated query/endpoint so the feature is *correct*, and
  logged the reasoning. "It mostly works for recent data" is a bug waiting to confuse someone.
- **New read endpoints are nearly free when you mirror the existing one.** `HandlePins` is `HandleRecent`
  with a different store call — same `ChannelIDFromQuery`, same `CanAccessChannel` gate, same shape —
  so the access-control story is identical and obviously-correct, and the test is a near-copy.
- **Still backend-first** at iter 55 to keep long-context risk low; the panel UI (reusing the existing
  search-results-panel pattern) is the next clean tick.

## 2026-06-14 (iter 54) — Pin UI; mirrored the server authz rule in the client gate

Completed pinned messages: pin/unpin hover action, 📌 badge, live `message-pinned` WS handler.
Two-tick slice done (backend iter 53 → UI iter 54).

Reflections:
- **The client's "can I do this?" gate should mirror the server's authz, not guess.** `canPin =
  !activeServerChannel || canModerate` is the exact client-side reflection of the store rule (admins
  in server channels, any member elsewhere). The server still enforces it (the UI gate is just UX), but
  matching them means the button only shows when the action will actually succeed — no misleading
  controls that 403.
- **Lean on the existing live-update path instead of optimistic state.** Pin calls the API and lets the
  `message-pinned` broadcast update the flag for everyone — same pattern as delete. One source of truth
  (the broadcast), and the sender sees the same update as everyone else, so no optimistic/rollback code.
- **The pin badge sits outside the (group-collapsed) message head** so a pinned message still shows it
  even when grouped under the same author — a small correctness check the screenshot confirmed.

## 2026-06-14 (iter 53) — Pinned messages (backend); reused the moderation authz model

Shipped pin/unpin backend: `messages.pinned`, `SetMessagePinned`, `PUT/DELETE /messages/{id}/pin`
with a broadcast, flag surfaced in `Recent`. Backend-first (UI next), matching the channel-topics
two-tick pattern.

Reflections:
- **A new action should reuse the project's *existing* authorization shape, not invent a new one.**
  Pinning's gate mirrors moderation-delete: resolve the message's `server_id`, require server-admin
  for server channels, fall back to channel-access for serverless ones. Same query, same error
  sentinels (`ErrMessageNotFound`/`ErrForbidden`), same 404-no-leak behavior. Consistency = fewer
  surprises for clients and a smaller test surface.
- **Backend-first keeps long-context risk low and the slice independently verifiable** — schema +
  tested endpoint now; the UI (pin action + pinned panel/badge, handle the `message-pinned` WS event)
  is a clean separate tick. The `message-pinned` event ships now but is harmlessly ignored until the
  client handles it.
- **Round-trip the new flag through a real read path in the test** — I asserted pinned=true/false via
  `store.Recent` (the same query the app serves history with), not just the UPDATE's return, so the
  new SELECT column is proven wired, not assumed.

## 2026-06-14 (iter 52) — Autolink URLs; security-by-construction over sanitization

Clickable `http(s)` links in messages (a chat table-stakes gap — pasted URLs were dead text). One
new inline rule; XSS-safe because only `https?://` is matched (href can't be `javascript:`/`data:`)
and React escapes the attribute — plus `rel="noopener noreferrer"` for tab-nabbing.

Reflections:
- **The allowlist regex *is* the security control — no separate sanitizer needed.** Rather than match
  any `scheme:` and then filter dangerous schemes (a blocklist that rots), the rule only ever matches
  `https?://`, so an unsafe href is unrepresentable. Probed the `javascript:`/`data:` cases explicitly
  to prove they fall through to plain text. Prefer "can't express the unsafe thing" to "remember to
  strip it."
- **Edge cases are where autolink earns trust:** trailing `).` punctuation, underscores inside the URL
  (must not become italics), and URLs inside inline code (must stay literal). The earliest-match parser
  handled the last two for free; the punctuation trim was the one deliberate touch. Probed all three.
- **Verification cost stayed ~zero:** folded the URL into the existing markdown QA message (no extra WS
  send → no rate-limit pacing) and reused the react-dom/server probe. The ladder keeps paying off.

## 2026-06-14 (iter 51) — Channel-topic UI; completed the two-tick vertical slice

Shipped the frontend half from last tick: the header shows a server channel's topic and admins
get an "edit topic" control. Channel topics is now a complete, E2E-verified feature.

Reflections:
- **Backend-first, then UI in the next tick, is a clean way to size risk in a long session.** The
  backend slice (iter 50) was pure data + a tested endpoint; this tick was a contained UI change
  against an already-proven API. Each half was small and independently verifiable — better than one
  big cross-layer change deep in a long context.
- **Reused the project's own UI patterns instead of inventing.** The "edit topic" admin control mirrors
  the existing read-only toggle (same `canModerate` gate, same optimistic `setServerChannels` update,
  same `window.prompt` affordance as channel creation). Matching local conventions kept the diff tiny
  and the QA wiring trivial (the harness already drives prompts via `promptAnswer`).
- **The QA harness's existing seams made the new test almost free:** set `promptAnswer`, click, assert
  `.channel-topic`. Investments in test infrastructure compound — the Nth UI feature costs almost no
  incremental QA effort.

## 2026-06-14 (iter 50) — Channel topics (backend slice); a schema change done safely

Shipped a real parity item's backend: server channels get a `topic` (admin-set, ≤1024 chars,
returned in the API). New schema column, `SetChannelTopic`, and `PATCH /channels/{id}` extended
to take optional `postPolicy` and/or `topic`. UI follows next tick (backend-first pattern).

Reflections:
- **Extend an endpoint without breaking its existing clients: make new fields optional pointers.**
  PATCH used to require `postPolicy`; switching both fields to `*string` means each is applied only
  when present, so the read-only-channel UI's `{postPolicy}` requests still work unchanged. I added a
  backward-compat test asserting exactly that — the cheapest insurance against a silent break.
- **A schema change is safe here because two earlier invariants hold:** the ALTER is idempotent
  (`ADD COLUMN IF NOT EXISTS … DEFAULT ''`) and `Migrate` is serialized by the advisory lock (iter 40),
  so the new column adds cleanly even under concurrent boots/test packages. Past hardening paid off.
- **Verified on a *fresh* DB** (`docker compose down -v`) so the new column actually exercises the
  first-creation path CI will hit — the iter-40 lesson, applied by habit now.
- **Sized to the context:** still backend-only (no frontend) on a long session; the visible header UI
  is a clean separate tick.

## 2026-06-14 (iter 49) — @everyone/@here highlighting; a feature tick (not more tests)

After several test/QA ticks, deliberately advanced a user-visible parity item instead of adding
the Nth test: `@everyone`/`@here` now render as highlighted all-mention chips (they ping you too),
with a `mention-all` DOM hook. Pure renderer + React elements (no new CSS, XSS-safe). GOAL Mentions
TODO shrank.

Reflections:
- **Watch for safe-busywork drift.** I'd done many low-risk test ticks; another would have been
  borderline churn (Rule 10). The honest move when the product is green is to *advance* it — a small,
  fully-verifiable feature — not manufacture coverage to look busy. Picked a bounded renderer change
  with the same verification rigor (react-dom/server probe → browser QA → AI-vision).
- **Reuse the verification ladder you built.** Probe the pure unit (1s) → fold the assertion into an
  existing QA message (no extra WS send, dodges the rate limiter) → eyeball one screenshot. Each new
  renderer feature now costs almost no incremental QA effort because the rungs are in place.
- **Keep `[~]` honest:** this is rendering only — no delivery/notifications — so Mentions stays `[~]`
  with the remaining work spelled out, not optimistically closed.

## 2026-06-14 (iter 48) — Auth input-hardening audit: confirmed bounded, then guarded it

Rule-15 audit of the auth input surface: register/login already bound the body
(`MaxBytesReader` 64 KiB) and validate username/password — so no vuln, but the handler
rejection paths had no test (only the regex was unit-tested). Added DB-free guards: malformed
/non-JSON bodies, bad username, short password, and a >64 KiB body all → 400 without a DB call.

Reflections:
- **An audit that finds the code is *already* safe still earns a regression test.** "It's
  bounded" was true but unguarded — a future refactor of `decodeCreds` could drop the
  `MaxBytesReader` and nothing would fail. Pin the safety property, not just fix the unsafe ones.
- **Test at the cheapest layer that still proves the property.** These run with a nil DB pool
  because every rejection happens before the DB call — so they're fast, always-on in CI, and
  don't need the Postgres service. Find the layer where the invariant lives and test there.
- **Context discipline:** 10th tick in one session — kept it backend-only + DB-free again
  (lowest blast radius). Strongly recommending a `/clear` before the next feature-sized tick;
  all loop state is durable (GOAL/IMPROVEMENTS/heartbeat/wakeup/memory) so it resumes clean.

## 2026-06-14 (iter 47) — Deployed to Railway; then guarded the deploy-critical routing

Out-of-band this session: shipped a single-binary prod build (Go serves the embedded SPA) and
**deployed Opencord to Railway** — live at opencord-production-1b00.up.railway.app, verified
in-cloud (register/login/SPA/asset/401 + a browser screenshot of the login screen). This tick
added the missing tests for that new code.

Reflections:
- **Ship the guard for what you just shipped — especially deploy-path code.** The `/*` SPA
  catch-all and the embed handler went out with the deploy but had no tests; a refactor could
  silently make `/api/bogus` serve index.html (masking real 404s) or break the SPA. Added a
  webui unit test + an httpapi guard asserting the catch-all serves client routes but never
  shadows the API. New code that reaches production gets a test in the very next tick.
- **A good test taught me real `net/http` behavior:** `http.FileServer` 301-canonicalizes
  `/index.html`→`/`. My first assertion was wrong (expected 200); the failure was the test
  being naive, not the handler. Fixed the test, kept the meaningful cases. Tests that fail on
  framework semantics are still teaching you something — read the failure, don't paper over it.
- **Deploy gotchas logged in memory, not just here:** Railway's managed-DB plugin returns
  "Unauthorized" (CLI token lacks DB-create scope; `railway login` can't run in the session's
  non-interactive `!` shell). Worked around with a Postgres Docker-image service. CAVEAT: no
  volume yet → data is ephemeral. (See [[project_opencord]].)
- **Next:** add a persistent volume to the cloud DB (offered to owner); a CI `docker build` of
  the root Dockerfile would guard the image build itself (the one deploy-path step CI doesn't
  cover — go+web are built separately but not the combined image).

## 2026-06-14 (iter 46) — Tested the rate limiter (the abuse defense I only knew worked by accident)

Added `TestServeWSRateLimitIntegration`: one connection floods 30 messages; the test asserts the
per-connection token bucket throttles most (saved < 30) while allowing the burst (observed 5/30,
matching `rateBurst`). Backend-only tick (deliberately low-risk given a long session).

Reflections:
- **A security feature you only confirmed by accident has no guard — write it.** I learned the rate
  limiter existed in iter 44 because it silently dropped the QA bot's sends. "It clearly works" was
  true but untested; one refactor of the bucket math could have weakened it invisibly. Convert every
  *observed* behavior into an *asserted* one. The flood test now pins burst+throttle behavior.
- **Make adversarial assertions timing-robust.** The exact pass-count depends on send speed vs refill,
  so the test asserts a wide band (`>=3 && <30`), not `==5` — it proves the invariant (throttling
  happens, burst survives) without being flaky on a slow CI runner.
- **Match the tick's risk to the context's age.** Eight ticks deep in one session, I chose a
  self-contained backend test over a cross-cutting frontend change — less blast radius if my context
  is fraying. (Offered the user a `/clear`; they kept going, so I kept the work conservative.)

## 2026-06-14 (iter 45) — @mention rendering; applied last tick's lesson proactively

Shipped the first @mention slice: `@user` chips with the viewer's own mention highlighted
distinctly (amber `mention-me` vs accent). Client-only, XSS-safe, GOAL Mentions now `[~]`.

Reflections:
- **Applied iter-44's "isolate the unit" lesson *before* the slow path, not after.** Rather than
  build → run the ~2-min full browser QA → debug, I first proved the renderer via react-dom/server
  (`@alice`→mention-me, `@bob`→mention, `foo@bar`→`@bar` quirk) in ~1s, *then* ran the E2E once with
  confidence. A debugging lesson is only banked when it changes the *next* tick's order of operations.
- **Paced the new send by default.** Knowing the rate-limiter trap (iter 44), the mention step waits
  for the bucket before sending — no flaky discovery this time. The meta-QA gap noted last tick (steps
  accumulate WS frames) is now handled per-step; a future cleanup could factor a `sendPaced()` helper.
- **Logged the scope honestly:** rendering only (no resolution/notifications/autocomplete) and the
  `foo@bar` email quirk are in SPEC + the commit, so the `[~]` is truthful, not optimistic.

## 2026-06-14 (iter 44) — Markdown blockquote + spoiler; isolate the unit when E2E fails

Completed the Markdown subset: `> ` blockquotes (block-level line grouping) and `||spoiler||`
(stateful click-to-reveal component), still React-elements-only (XSS-safe). GOAL's Markdown item
is now `[x]`. But the real lesson was the debugging.

Reflections:
- **When an E2E test fails, isolate the unit before touching it.** The browser QA timed out waiting
  for a blockquote, which *looked* like a renderer bug. Instead of poking the renderer blind, I
  transpiled `markdown.tsx` with esbuild and rendered it through `react-dom/server`:
  `"> q\n||s||"` → `<blockquote>q</blockquote><span class="spoiler">s</span>`. The renderer was
  **provably correct**, which redirected the hunt to the test/runtime — saving a wrong "fix" to good
  code. **Keep this technique: prove the pure unit in isolation to bisect unit-vs-integration.**
- **The actual cause was the product working as designed: the rate limiter.** Diagnostics (compose
  value correct, draft cleared on Enter, yet no message) showed the WS frame was *sent* but *dropped*
  server-side — the per-connection token bucket (`rateBurst=5`, `+2/s`, every message AND typing
  frame costs a token) had emptied under the bot's rapid sends. An automated client sends far faster
  than a human and trips real abuse limits. **Fix the test (pace it), not the limit.**
- **Layered diagnostics beat guessing:** body-HTML dump → "message never sent"; compose-value
  before/after Enter → "sent but dropped"; each step removed half the search space. Add the
  diagnostic, don't theorize.
- **Meta-QA gap to watch:** the browser QA accumulates WS frames across steps; future message-send
  steps should pace by default (or the suite should reset the connection) so they don't randomly trip
  the limiter as more steps are added.

## 2026-06-14 (iter 43) — Multi-line composer; "check the input path can type the syntax"

Set out to finish Markdown (blockquote/spoiler) and discovered the composer was a single-line
`<input>` — so blockquotes and multi-line code blocks were **untypeable**, and there were no
multi-line messages at all. Fixed the foundation first: the composer is now an auto-growing
`<textarea>` with Enter=send / Shift+Enter=newline (the Discord convention). Verified E2E
(browser QA types two lines via Shift+Enter, asserts they land in one message and that the first
line wasn't sent alone) + AI-vision of `03d-multiline.png`.

Reflections:
- **A renderer for syntax users can't enter is dead code — check the INPUT path before adding
  output features.** Last tick's "next" list had blockquote/spoiler; building them onto a
  single-line input would have shipped Markdown nobody could trigger. The fix was the composer,
  not more parse rules. **Rule: verify the whole user path (type → send → render), not just the
  render half, before extending a feature.**
- **Foundational UX unblocks several backlog items at once:** multi-line input now enables
  blockquote, multi-line code blocks, and just plain paragraph messages — one change, several
  features unblocked. Prefer the enabling fix over a cosmetic one.
- **Next (now genuinely unblocked):** Markdown blockquote (`> `) + spoiler (`||x||`, click to
  reveal); both are typeable now and worth a focused tick.

## 2026-06-14 (iter 42) — Closed the iter-39 edit/reaction HTTP gap; caught a self-made phantom gap

Two things this tick. (1) Closed a *real* logged gap: the message **edit** and **reaction** REST
endpoints had no HTTP-layer test despite carrying authz — added cases to `router_integration_test.go`
(edit author-only → 404 for others; empty body 400; reactions channel-access gated → non-member 403;
invalid emoji 400; add/remove 200). (2) The item I'd queued as "next" — *add a web CI job* — turned
out to be **already done**: `ci.yml` has gated `web (typecheck · build)` since the first CI commit.

Reflections:
- **A "gap" found by a partial grep is not a gap — verify against the whole source before logging
  it.** Last tick I grepped `ci.yml` for `DATABASE_URL|go test|postgres`, saw only the server job,
  and declared the web ungated. Reading the full file this tick showed a `web` job that passes every
  run. **Rule: confirm a missing-thing claim by reading the file / `git show HEAD:<file>` and the
  live CI job list, not by a keyword search that can miss a sibling block.** Cheaper than a tick spent
  "fixing" what exists. (Corrected the false note in the iter-41 entry above.)
- **Honesty over tidiness:** struck the wrong claim in place rather than deleting it, so the loop's
  record shows what it learned, not a clean rewrite.
- **Next real gaps (verified by reading, not grepping):** message edit/reaction over **WS** (not just
  REST); finishing Markdown (blockquote/spoiler, still `[~]` in GOAL); a new parity feature
  (mentions / pinned messages) for a Track-2 tick.



After two backend test-coverage ticks, advanced the *product*: a Discord-like Markdown
subset in messages (**bold**, *italic*, ~~strike~~, `inline`/```fenced``` code). Built it
security-first — `renderMarkdown` returns **React elements, never an HTML string**, so it's
XSS-safe by construction (raw `<script>` renders as literal text). Ran the **full browser QA
stack** for the first time this session and reviewed `03c-markdown.png` by eye: bold/code
render, the `<script>` is escaped. browser + realtime QA both green.

Reflections:
- **CI does not build/typecheck the web client — a real coverage gap.** ~~`ci.yml` only runs
  the Go job…~~ **[CORRECTED iter 42 — this was WRONG.]** `ci.yml` has had a `web (typecheck ·
  build)` job (`npm ci && npm run build`) since the first CI commit (`aca1f80`), and it passes
  every run. I concluded "no web job" from a partial `grep` that only matched the server job's
  lines instead of reading the whole file. The frontend IS gated. See iter 42's lesson.
- **Rendering user content is an attack surface — pick a structurally-safe design, not a
  filter.** Returning React elements (no `dangerouslySetInnerHTML`) means there's no HTML sink
  to sanitize and no blocklist to keep current; the safety is in the shape of the code. Prefer
  that over "escape then inject" every time.
- **Grow the QA with the feature (Step 5b):** added a markdown+XSS assertion to `browser.mjs`
  in the same commit, so the guard ships with the feature, not later.
- **Balance the tracks:** three straight QA ticks would drift from the North Star (Discord
  parity); a feature tick that *still* ships its own QA + vision check keeps both moving.

## 2026-06-14 (iter 40) — WS access-control suite + a CI race adding tests exposed

Shipped the WebSocket gateway's first access-control test (`internal/ws/serve_integration_test.go`):
the realtime path is the most attacker-exposed surface (Rule B), and `ServeWS` rejects
401/403/400 *before* the upgrade — now proven, including a **real gorilla/websocket
handshake** (member connects via the `?token=` browser path and receives history; a
non-member is refused 403, no leak before upgrade).

But adding integration tests to a *third* package turned CI red — and the lesson is the
bigger deliverable:
- **A new test package can change the concurrency profile of `go test ./...`.** Go runs
  package binaries in parallel, so chat + httpapi + ws all called `db.Migrate` at once on
  CI's fresh DB. `CREATE … IF NOT EXISTS` is **not** atomic against simultaneous creation
  → catalog duplicate-key (SQLSTATE 23505 on pg_class/pg_type). Fixed at the root with a
  `pg_advisory_lock` in Migrate (+ `TestMigrateConcurrent` regression guard).
- **A green local `go test ./...` ≠ green CI when the local DB is pre-migrated.** The race
  only fires on *first* concurrent creation; my warm dev DB already had the schema, so it
  hid the bug. **Process fix (apply every DB tick): verify on a FRESH DB** — `docker
  compose down -v` before the run — to match CI, not the warm dev DB.
- **Rule 15 done right even when the first repro misses:** a single `go test ./...` didn't
  reproduce the timing race, so I forced it deterministically (8 goroutines from a barrier,
  fresh schema each run) — 3/3 FAIL without the lock, 3/3 PASS with it, then full suite +
  CI green. Don't conclude "can't reproduce" from one non-overlapping run.

## 2026-06-14 (iter 39) — HTTP-layer authz coverage: testing router.go, not just the store

`internal/httpapi/router.go` (the entire REST surface) had **zero** test files while
`auth`/`chat`/`ws` all had suites. The store layer's authorization was well covered by
`chat/store_integration_test.go`, but the *HTTP wiring* — JWT middleware, the inline
`IsServerMember`/`IsServerAdmin` gates in `mountServerRoutes`, and the `error → HTTP
status` mapping — was untested. A bug there (wrong code, a missing gate) is a security
hole the store tests can't see. Added `router_integration_test.go`: a `DATABASE_URL`-gated
suite that drives the *real* chi router via `httptest` and walks an attacker through every
boundary (401 unauth · 403 non-member/non-admin · 404 no-existence-leak on moderation ·
400 bad role/policy/id · 2xx for the legitimate owner/admin). Verified against a local
postgres (7/7 subtests) and runs in CI's postgres service.

Reflections:
- **"Tested" must name the layer.** The store being green hid that the HTTP layer mapping
  it to status codes had no guard at all. Coverage gaps live at seams between packages —
  the next audit target is the **WS layer's** REST-adjacent access checks (`ServeWS`
  channel-access gating) tested through a real upgrade, not just `ws` unit tests.
- **Reuse the established pattern, don't reinvent.** Mirrored the store suite's
  skip-without-`DATABASE_URL` convention so `go test ./...` stays green locally and the
  test is a real CI guard — not a second bespoke harness.
- **Next QA growth:** extend this suite to the message edit/reaction endpoints (PUT/DELETE
  reactions, PATCH body) and assert the read-only `Save`→403 path through HTTP once a
  message-send REST route exists (today messages are sent over WS).

## 2026-06-14 (iter 38) — Read-only channel UI: finishing the last feature's front end

Completed the read-only-channel feature shipped backend-only last tick. The client now
gets each channel's `postPolicy` (added to the Channel JSON), so: an admin sees a
"make read-only / allow everyone" header toggle + a 🔒 badge; a non-admin in a read-only
channel gets a disabled composer ("read-only — only admins can post"); and the WS `error`
frame is finally surfaced (alert). Single-client QA toggles it and grades the badge by eye.

Reflections:
- **A backend permission needs its state on the resource for the UI to reflect it.** Just
  like roles needed `Server.role`, read-only needed `Channel.postPolicy` on the channel
  list — the UI can't disable a composer for a policy it can't see. Pattern across this
  whole arc: enforce server-side, then expose the deciding field on the thing the client
  already fetches.
- **Known limitation, logged not hidden:** toggling read-only doesn't live-propagate to
  other connected clients (no `channel-updated` broadcast) — they see it on next load. The
  enforcement is still correct (server-side); only the *composer-disable hint* lags. A
  `channel-updated` WS event is the fix when it matters; noted, not silently shipped.
- **UI-hint vs server-gate, again:** the composer disable + Send-disable are affordances;
  `Save`'s `ErrForbidden` (iter 37) is the real gate. Three features now follow this split
  (moderation, read-only, post-policy) — it's the house pattern for permissions here.
- **Roadmap is now 100% built AND fully wired through the UI.** Every parity item from
  GOAL.md is shipped, verified, and usable in-app. Next work is genuinely net-new (threads,
  attachments, voice, read state) — bigger, fresh-context features, not roadmap cleanup.

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
