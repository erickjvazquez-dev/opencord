---
description: Gap detection and multi-agent technical-lead orchestrator — audits every layer of the project, specs each gap it finds, and fans the fixes out to sub-agents.
---
# /gap — Gap Detection + Multi-Agent Technical Lead Orchestrator

Audits every layer of the project, finds what's missing or broken, specs each gap
via spec-kit, then spawns focused sub-agents to fix them in parallel.

Config is read from `.ccf/project.env` (e.g. `$CCF_PROJECT_DIR`, `$CCF_GITHUB_REPO`,
`$CCF_TEST_CMD`, `$CCF_LIVE_URL`, `$CCF_HEALTH_PATH`, `$CCF_NORTH_STAR`). Source it at
the top of any bash block: `source .ccf/project.env 2>/dev/null || true`. Project-specific
checks below route to optional `.ccf/hooks/*.sh` — run a hook only if it exists.

Acts like a Technical Lead / PM: reads current state, identifies gaps, prioritizes,
delegates to specialists, and synthesizes results.

This loop runs until no gaps remain. It never stops finding improvements.

## Usage
```
/gap                   # full audit + spawn agents for top 3 gaps
/gap --audit-only      # just find gaps, don't implement
/gap --area frontend   # audit only frontend
/gap --area backend    # audit only backend/API
/gap --area tests      # audit test coverage gaps
/gap --area security   # audit security posture
/gap --area ux         # audit user experience gaps
/gap --area ai         # audit AI integration gaps
```

## Instructions for Claude (Technical Lead mode)

### Step 0 — Read current state (parallel)

Read all of these simultaneously:
```bash
cd "${CCF_PROJECT_DIR:-$(pwd)}"
source .ccf/project.env 2>/dev/null || true

# What's been built
git log --oneline -20

# What's planned
grep '^- \[ \]' GOAL.md | head -20

# What's broken (skip if no remote CI configured)
[ -n "${CCF_GITHUB_REPO}" ] && gh run list --repo ${CCF_GITHUB_REPO} --limit 3 --workflow ci.yml \
  --json conclusion --jq '[.[].conclusion]' 2>/dev/null

# QA findings
ls reports/qa/ 2>/dev/null | sort -r | head -1 | xargs -I{} cat reports/qa/{} 2>/dev/null | head -50

# Design-system compliance (token names defined by the project's DESIGN_TOKENS.md)
[ -f DESIGN_TOKENS.md ] && head -40 DESIGN_TOKENS.md
```

Read (if present): `${CCF_PROJECT_DIR}/DESIGN_TOKENS.md`, `${CCF_PROJECT_DIR}/GOAL.md`
(north star section), and recall `$CCF_NORTH_STAR` from config.

### Step 1 — Audit each layer

For each area, check for gaps vs the north star:

**Frontend gaps (if the project has a UI):**
- All UI surfaces follow the project's design system? (DESIGN_TOKENS.md compliance)
- Iconography / visual style consistent across surfaces?
- Light/dark toggle on all surfaces (if applicable)?
- Mobile layout passes at 375px?
- Accessibility: WCAG AA contrast, keyboard nav, ARIA labels?
- Interactive elements sized/styled per the design system's constraints?
- Does the UI look native in each context it's embedded in (if it embeds anywhere)?

**Backend gaps (if the project has a backend/API):**
- All endpoints have auth/authorization where needed?
- Core integrations / routing complete per `$CCF_NORTH_STAR`?
- Error tracking configured and capturing errors (if one is used)?
- Limits/quotas consistent and sane (common-sense check)?
- New endpoints/handlers have tests? (no change without a test)
- No duplicate operation IDs / route collisions in the API spec? (silent framework bug — see GAP-009)

**Test gaps:**
- E2E tests cover all critical user flows?
- A11y tests in place (if there's a UI)?
- Security tests passing (0 CRITICAL/HIGH)?
- Test count declining? (regression)
- Tests for new features?
- **Product testing** (most important — see below):
  - The actual product exercised end-to-end, not just unit tests?
  - Each primary user journey driven to completion and real output asserted?
  - Every UI surface verified in its rendered/live state?

**PRODUCT TESTING GAPS (spawn sub-agents per surface):**
This is the highest-priority audit area. Missing product tests are invisible bugs.
Enumerate the project's user-facing surfaces and primary journeys, then spawn one
sub-agent per surface to drive the full user flow end-to-end (no human) and assert
real output — not just a 200. If the project ships a QA hook, use it as the entry point:

```
# If present, this is the canonical product-test entry point:
[ -x .ccf/hooks/qa.sh ] && .ccf/hooks/qa.sh

Sub-agent per surface (examples — adapt to THIS project's surfaces):
  - Primary UI / web app: open it, complete the main journey, assert the result is correct
  - Each context the product embeds into: verify it appears and behaves natively there
  - API / CLI: run smoke + correctness checks (mirror the /qa skill's phases)
  - Settings / onboarding: complete the setup flow, verify it persists and works
Each sub-agent reports the surface, the journey it drove, and PASS/FAIL with evidence.
```

**CI/CD gaps (if a CI is configured):**
- All jobs green? (test, security, deploy, e2e)
- Dependency/security scanners using their current (non-deprecated) invocation?
- CI runner / action versions current (no deprecated tooling)?
- No duplicate-operation-id / route-collision warnings in CI output?

**UX gaps (from QA reports):**
- Any CRITICAL findings unresolved?
- Onboarding / first-run flow friction?
- Error messages clear and non-technical (no raw tracebacks leaked to the user)?
- The product's core value visible and real on every surface (per `$CCF_NORTH_STAR`)?
- Auth/error states include actionable next-step guidance?

**Core-integration gaps (per `$CCF_NORTH_STAR`):**
- Are the project's headline capabilities all implemented and wired end-to-end?
- Do any feature flags / capability lists agree across all the files that maintain them? (see GAP-019)
- Does any "status"/"capabilities" surface reflect what's actually configured?

### Step 2 — Prioritize gaps

Rank by: North Star alignment × User impact × Implementation effort.

Format each gap:
```
GAP-[N] [LAYER] [PRIORITY] — <one sentence description>
Root cause: <why it exists>
Impact: <what breaks for users>
Fix: <concrete implementation step>
Effort: [XS/S/M/L/XL]
Requires spec-kit: [yes/no]
```

### Step 3 — Spec each gap (for M/L/XL items)

For any gap rated M, L, or XL:
1. Write a concise internal spec (not a file — just in context)
2. The spec defines: what to build, acceptance criteria, files to change
3. Follow speckit.specify principles: one thing, verifiable, no scope creep

For XS/S items: go directly to implementation (already well-defined).

### Step 4 — Spawn sub-agents

For each gap, spawn a focused sub-agent. Each sub-agent:
- Gets a single, specific task
- Has access to the right tools (Bash, Read, Write, Edit)
- Reports back: DONE / BLOCKED / NEEDS_REVIEW

**Sub-agent assignments:**
- Frontend gaps → frontend-design skill
- Backend/API gaps → direct implementation
- Test gaps → write test + verify it passes
- Security gaps → `/security-checklist` (the framework's 20-item SSOT; grade the diff/posture against it, file findings as `SEC-*`). Escalate a deep-dive to the CLI-built-in `/security-review` (diff pass) when a finding needs manual analysis.
- UX gaps → fix and re-run QA Lead

**Orchestrator tracks:**
- Which agents are running
- Which completed successfully
- Which need follow-up
- Merge conflicts (resolve by keeping most recent feature, not breaking tests)

### Step 5 — Synthesize and report

After all sub-agents complete:

```
GAP REPORT — [timestamp]
Orchestrator: Technical Lead

GAPS FOUND: N
GAPS FIXED THIS RUN: N
GAPS QUEUED IN GOAL.MD: N
GAPS NEEDING MORE SPEC: N

FIXED:
- GAP-1: [what was fixed]
- GAP-2: [what was fixed]

QUEUED:
- GAP-3: [added to GOAL.md as <ITEM_ID>]

BLOCKED:
- GAP-4: [reason] → [suggested next action]

TOP UNRESOLVED GAP: [one sentence — the most important thing still missing]
```

### Step 6 — Update GOAL.md and loop

- Add unfixed gaps as new backlog items (with proper wave labeling)
- Update meta-improve counter if > 5 new items added
- Schedule next gap run via /self-improve loop

## Orchestrator principles (from spec-kit best practices)

1. **One agent, one gap** — never assign multiple unrelated tasks to one agent
2. **Specify before implement** — M/L/XL gaps get a spec; XS/S go direct
3. **Verify don't trust** — always check that the sub-agent actually fixed the gap
4. **No scope creep** — if fixing gap A uncovers gap B, add B to the queue, don't fix it now
5. **Karpathy commits** — each fix is one focused commit with one clear purpose
6. **All tests before merge** — sub-agent must verify `${CCF_TEST_CMD}` passes
7. **Design rules apply** — any frontend fix must comply with the project's design system (DESIGN_TOKENS.md if present)
8. **Security gates** — never merge if security check fails

## Integration with /self-improve loop

The /self-improve loop calls /gap automatically when:
- All health checks are green
- No P0/P1 issues open
- Backlog has < 5 unchecked items

This ensures the product never stagnates — there's always a gap to close,
always a direction toward the North Star.

## Gap Rules Registry (the orchestra's memory)

When a gap is found — by `/gap`, by `/self-improve` Track-0, or by the QA agent — **record it
here as a permanent detection rule** so it's checked every future run and never silently returns.
This is how the loop compounds: gap → rule → checked forever → triggers `/self-improve` when it
reappears. Format: `GAP-NNN — <what to check> — <how> — <severity>`.

- **GAP-001 — Every tool/endpoint/service has automated E2E coverage** — for each public
  tool/endpoint and each UI surface, an automated test drives it end-to-end (no human) and asserts
  real output, not just a 200. Missing coverage = P1. *(How: enumerate the public surface — e.g. an
  API spec / route list / CLI commands — and diff against the tests.)*
- **GAP-002 — Every UI surface is vision-verified** — render each surface to an image (via the
  project's render hook, e.g. `.ccf/hooks/render.sh` if present) and have a vision-capable agent
  read the PNG against the design system. Any un-rendered surface (especially ones only visible in
  their *rendered/embedded* state) = P1 coverage gap.
- **GAP-003 — Usability from the end-user POV** — for each primary user journey the project supports,
  a scripted-manual flow proves a real user can complete it unaided. Broken journey = P0.
- **GAP-004 — No metered/paid-API dependence in QA** — any QA path that hard-depends on a metered
  API (which can hit a $0 balance) is a gap; route vision/judgement through a non-metered agent
  (e.g. a subscription-tier agent) instead so QA can't silently SKIP when credits run out.

- **GAP-005 — Tools must pass an output-QUALITY check, not just no-500** — the smoke test (GAP-001)
  catches crashes, not *wrong/misleading* output. Each tool needs a minimal correctness assertion.
  *(Concrete class: a checker named for a broad capability was actually mechanical-only yet returned
  a "clean/pass" verdict on input it doesn't really validate — scope the name/response honestly or
  deepen the implementation. P3.)*

- **GAP-006 — No module-level name collisions in large source files; every deploy is gated** — in
  a large (especially auto-generated) source file, a duplicate module-level *helper function* or
  *constant* silently shadows earlier callers at runtime (last definition wins), so an earlier
  caller runs the wrong implementation. The risk compounds when CI/verification is degraded and
  unverified commits ship. *(How: a build-smoke gate runs at build/deploy time as a fail-closed
  check — AST scan for dup module-level helpers/constants (route handlers & request models excluded,
  they bind at decoration time), import-the-app, a route/symbol-count floor, and a core smoke. Wire
  it as an optional build-gate hook (`.ccf/hooks/build-gate.sh` if the project has one), with a
  regression test.)* **P0** (a shadowed helper can 500 an endpoint / corrupt a result in prod).

- **GAP-007 — Numeric/math endpoints must guard non-finite & overflow results** — endpoints that
  do arithmetic on user input can produce `inf`/`nan`/`complex`/`OverflowError`, which then either
  500 (Starlette `JSONResponse` uses `allow_nan=False`) or return a misleading result
  (`result:null`, a stringified complex) with `success:true`. The endpoint smoke (GAP-001) misses
  this because it only sends *benign* input. *(Found 2026-06-09 by adversarial probing, Rule 15:
  `/v1/math_eval` 500'd on `2**100000` (OverflowError) and returned complex on `(-1)**0.5`;
  `/v1/statistics` 500'd on a raw `NaN`/`Infinity` body. Fixed by rejecting non-finite input +
  validating outputs are finite real numbers; regressions in `tests/test_wave56.py`.) **How to
  check:** probe every math/number endpoint with `2**100000`, `(-1)**0.5`, `1e308*1e308`, and a
  raw `{"x":NaN}` body — assert no 500 and no misleading `result`. **Math family swept 2026-06-09**
  (iter 4): `compound_interest` (was 500), `lerp`/`loan_calculator`/`tip_calculator` (were
  `result:null`) all fixed via a shared `_finite_guard()` in `api/text_utils_ext.py`; div-by-zero
  cases (`bmi`/`normalize`/`percentage`) were already clean; DoS vectors are bounded (factorial
  n≤20, fibonacci n≤100, etc.). Regressions in `tests/test_gwax_gway.py::TestMathNonFiniteGuard`.
  *Leftover RESOLVED 2026-06-11 (security-qa lane):* `temperature_convert`/`unit_convert` did echo
  `{"success":true,"result":null}` on overflow/inf/nan (reproduced 6/6); fixed with the same
  `_finite_guard` on input + result; regression `tests/test_gap007_convert_finite_guard.py`.
  *Leftover RESOLVED 2026-06-12 (security-qa lane):* `/v1/normalize_number` computed
  (value-min)/(max-min) without the `_finite_guard` its twin `/v1/lerp` had, leaking
  `{"success":true,"normalized":null}` on overflow/nan (reproduced 3/3, re-attacked blocked,
  prod-verified on build 91665676); regression `tests/test_normalize_finite_guard.py`.

- **GAP-008 — Tests must be order-independent; flakiness hides real regressions** — a test that
  passes in isolation but fails in the full suite (or vice-versa) is a defect even when the
  product is fine: it makes the suite untrustworthy and masks true regressions behind noise. The
  usual causes are unreset module-level global state and **test-plugin / event-loop interactions**.
  *(Concrete class: a set of async unit tests passed alone but failed in the full run with a
  "cannot be called from a running event loop" error — a browser-test plugin that sorted earlier in
  the run left an event loop running on the main thread, so the async-test plugin couldn't start its
  own runner. Fixed by driving the coroutine through a private thread+loop helper instead of relying
  on the async-test plugin's loop.)* **How to check:** periodically run the full suite AND spot-check
  suspect files in isolation — any delta in pass/fail is a GAP-008. Diagnose with the real traceback,
  never guess; prefer driving async units through a synchronous test client or a dedicated
  thread+loop when the unit doesn't actually need the async-test plugin's loop, especially in a
  suite that also runs a browser-test plugin. P1 (flakiness is a top-priority fix).

- **GAP-009 — No duplicate (method, path) route registrations** — when two handlers are registered
  on the same HTTP method + path, the framework (FastAPI/Starlette) routes every request to the
  **first** one; the later registration is dead code that silently diverges from the live handler
  (different patterns, auth, response shape) while looking maintained. The build-smoke **collision
  gate deliberately skips route handlers** (they bind at decoration time, so module-level shadowing
  doesn't apply), leaving this path-level shadow ungated. *(Found 2026-06-09 (iter 14): 6 endpoints
  were each registered twice — extract_definitions, score_objectivity, features, compress,
  extract_emails (in-file) and extract_urls (cross-file: a `@router` route + an `@app` route on the
  same path). The OpenAPI duplicate-operation-id warning only caught 2 of the 6 — the ones whose
  function names also collided; the other 4 had distinct function names so nothing flagged them.)*
  **How to check:** enumerate the app's registered routes at runtime, count `(method, path)` pairs
  (ignore framework auto-added HEAD/OPTIONS), and fail on any count > 1 — wire this into the
  build-smoke gate. To resolve: confirm which copy is live via **runtime route order** (first wins) —
  not source order, since sub-routers are typically included before later top-level route decorators —
  then delete the dead copy and any request models/constants it alone used (Rule 10). P1.

- **GAP-020 — Copy↔code coherence: every user-facing text must match what the code actually does**
  (owner directive 2026-06-12) — QA must not only exercise functionality but verify that services,
  callouts, placeholders, hints, and guidance SEMANTICALLY match the wired behavior. Three sub-classes
  found in one sweep of a settings UI: (a) **wrong-key-type copy** — an API-key input (sent in OUR
  API's auth header) asked for a third party's key format; (b) **false privacy claim** — a "never sent
  to our servers" label on a value that actually rides every request header; (c) **unwired promise** —
  a "we'll use it as a proxy for X" claim while the field is never consumed by any proxy code.
  *(Encode each as a copy-coherence regression test.)*
  **How to check:** for every key/secret input, assert the placeholder/label names the key type the
  handler actually consumes; for every storage/privacy claim, trace where the value flows (local
  storage only vs request headers vs third-party) and assert the claim matches; for every "X will do Y"
  promise, grep for the code that does Y — no consumer means the copy lies. Sweep ALL surfaces
  whenever copy or wiring changes. P1 (incoherent guidance breaks trust and onboarding at the exact
  moment a user commits a credential).

- **GAP-021 — Vision QA must measure USER-VISIBLE DELTA and state-variant views, not just fresh-page
  token compliance** (owner verdict 2026-06-12: "90+ iterations and I don't see real progress" — the
  live header was pixel-identical across weeks of green ticks). Three blind spots: (a) renders are
  fresh-profile FIRST-VISIT only — localStorage states (returning user with saved provider, keyed
  user, error/offline states) are never rendered, so the silent-auto-route complaint was invisible;
  (b) no before/after diff per deploy — "nothing visibly changed" and "shipped a visible improvement"
  both read as green; (c) grades local renders, never the LIVE page the user actually sees.
  **How to check:** the render/vision step gains state-variant fixtures (seed client state per state)
  + a live-URL mode (`$CCF_LIVE_URL`); each deploy archives screenshots and diffs vs the previous
  deploy — a sustained no-visible-change streak across N deploys is a RED finding even with all gates
  green; track "visible delta per deploy" as a Step-6 KPI. P0 (the loop's entire output legibility to
  the owner depends on it).

- **GAP-022 — Interrogate existing DESIGN DECISIONS, not just defects** (owner directive 2026-06-12:
  "what about the positions of the windows when embedded in a host app? is there a better way to do
  it? those kinds of questions is what the gap skill should also keep in mind"). Gap detection must
  periodically re-question working-as-built UX choices: panel/popover position and anchoring within
  any host context, control placement vs the host's native conventions, modal vs inline, information
  density, what the user sees FIRST after an action. A choice that was fine at ship time can be wrong
  next to a host UI's evolution or simply beatable.
  **How to check:** each /gap run picks ≥1 standing design decision (rotate through the project's
  surfaces and flows), states the current choice, lists 2-3 alternatives with tradeoffs vs the
  relevant host/native patterns, and either re-affirms it or files a UX item with a concrete better
  proposal + mock/screenshot. Log the verdict so re-asks rotate rather than repeat. P1 (this is how
  the product gets BETTER, not just less broken).

- **GAP-023 — Persistent state must never silently reset: atomic writes + cross-process locks +
  rebuild-from-log** (found when a lifetime-usage counter reset across deploys). The class: a
  JSON/state file written with a non-atomic whole-file write by N workers → torn file → a silent
  `except → default` loader RESETS the record → the next save persists the reset. Compounds invisibly:
  every snapshot looks plausible while the true total is far higher (recurring silent resets). The
  module-level in-memory variant is the same bug for multi-worker dicts; this is the file-store
  variant. **How to check:** grep for non-atomic file writes (`write_text` / `open(.., 'w')`) on files
  read back by the app; each needs (a) tmp + atomic replace, (b) a lock (or single-writer) around the
  read-modify-write, (c) a loader that NEVER falls back to defaults while an append-only
  log/source-of-truth exists — rebuild instead, preserving first-seen/history; corrupt files renamed
  aside, not deleted. Regression pattern: a durability test (corrupt→rebuild, N-process hammer with
  exact totals). P1 wherever user-visible history/money/limits hang off the file.

**Add a new `GAP-NNN` here whenever a new class of gap is discovered.** New rules also flow back
into `/self-improve` Track-0 as standing checks.

- **GAP-010 — Raw-body handlers must validate field types (no 500 on hostile input)** — endpoints
  that read `body = await request.json()` directly (instead of a Pydantic model) get NO type
  validation, so a top-level non-dict body (a JSON list/scalar) or a wrong-type field value (dict/int
  where a str is expected) crashes `body.get(...)` / `.strip()` / `.encode()` with a 500. *(Found
  2026-06-09 (iter 17) by sweeping all 1821 POST /v1/* endpoints with hostile payloads: abbreviate,
  capitalize_words, compress, crypto/hmac, crypto/generate_key all 500'd. Fixed with `_json_str` /
  `_json_int` safe extractors; regression in `tests/test_raw_body_hardening.py`.)* **How to check:**
  POST a top-level list (`["x"]`) and wrong-type field values to every raw-body endpoint — assert 4xx,
  never 5xx. Prefer a Pydantic request model (free validation → 422) over raw `request.json()`; if raw
  is unavoidable, coerce every field through a type-guarded helper. P1 (a malformed request must never
  crash the worker).

- **GAP-011 — QA harnesses must distinguish "nothing ran" from "all green"** — a QA runner whose
  verdict is computed only from *findings* reports PASS when every probe crashed/SKIPped, because
  zero executed tests produce zero findings. The suite then looks green while verifying nothing.
  *(Concrete class: a browser/agent QA runner printed "PASS" while all of its target sites SKIPped
  because a required dependency was unavailable (e.g. a depleted metered API) — a GAP-004 dependence
  compounded by a false-green verdict. Fix: all-SKIP / zero-tests-run yields overall=SKIP and a
  non-zero exit code distinct from FAIL; add a regression that asserts this.)* **How to check:**
  for every QA/gate script, run it with its dependencies deliberately broken (no API key, no browser,
  no server) and assert the verdict is SKIP/FAIL — never PASS — and the exit code is non-zero. P1
  (a false-green QA gate masks every other regression behind it).

- **GAP-012 — Any user-supplied redirect/callback URL must be allowlist-validated before use** — a
  handler that 302s (or embeds) a caller-provided `redirect_uri`/`callback`/`next` URL without
  checking it against registered/known values is an open redirect; in OAuth it exfiltrates the live
  auth code (RFC 6749 §3.1.2.3 requires exact pre-registration matching, and §4.1.2.1 forbids
  redirecting to a bad URI — render the error in place instead). *(Found 2026-06-10 as SEC-MCP2:
  `/authorize` POST bounced the code to any URI; fixed 2026-06-11 with
  `api/oauth.py::is_redirect_uri_allowed` exact-match gate, prod re-attack blocked. Regression:
  `tests/test_oauth_redirect_uri_allowlist.py`.)* **How to check:** grep handlers for
  `RedirectResponse`/`Location` built from request params; probe each with an attacker-controlled
  absolute URL and assert 4xx-no-redirect, never a 30x to the hostile host. P0 when the redirect
  carries a credential/code/token. P1 otherwise.

- **GAP-013 — ASGI handlers that read the request body in the RESPONSE phase break behind
  BaseHTTPMiddleware on a real edge** — when a route hands the raw ASGI scope/receive to a sub-system
  that reads the body *after the endpoint returns* (e.g. an MCP/streaming session manager invoked from
  a returned Response's `__call__`), the request's receive channel is already torn down by the
  BaseHTTPMiddleware stack; on a production edge that delivers the body in a later ASGI frame than
  loopback, `await request.body()` raises `starlette.ClientDisconnect` and the request hangs → 504.
  It passes the in-process test client AND a local multi-worker run (body rides the first frame), so
  it is **invisible without prod runtime logs**. *(Concrete class: found after several local-only fix
  attempts; root-caused only by reading the deploy target's prod logs. Fix: read the body in the
  request phase (in the endpoint — the path normal endpoints use and that works on the edge), then
  replay it to the sub-system via a synthetic receive; add a regression.)* **How to check:** for any
  handler that passes raw ASGI receive to a streaming/session sub-system, assert the body is read in
  the endpoint before delegation; and **never declare a prod-only hang fixed from a local green — pull
  the deploy target's logs (`$CCF_DEPLOY_TARGET`) and re-fire the exact failing request against prod**
  (Rule 14). P0 (a hung handshake = the whole streaming integration dead).

- **GAP-014 — Every error response must use the {detail, code, request_id} contract — no raw
  framework internals** — FastAPI/Starlette ship default handlers that leak implementation detail to
  callers: `RequestValidationError` (422) emits a per-field array with `type/loc/msg/input/url/ctx`,
  and unhandled exceptions can surface tracebacks. A user-facing API must normalize ALL errors to the
  house shape (`{detail: <plain-English sentence>, code, request_id}`, CORS-safe) — leaking `input`
  (which echoes the caller's payload) is also a minor info-disclosure. *(Concrete class: a single
  endpoint returned the raw validation array while every other error used the house shape — fixed
  with a global validation-error handler; add a regression.)* **How to check:** POST a missing-field body and a
  wrong-top-level-type body to a representative model endpoint; assert the response has exactly
  `detail`(str)/`code`/`request_id` and contains none of `"loc"`/`"input"`/`"url"`/`"ctx"`. When
  stripping a field's location path, drop only the LEADING source marker (body/query/path/header/
  cookie) so a field literally named "query" survives. P2 (UX + minor info-leak).

- **GAP-015 — Malformed-unicode input (lone UTF-16 surrogate) must be rejected at the boundary, never
  500** — the JSON escape `"\ud800"` is **valid JSON but not UTF-8-encodable**. A hostile client sends
  it as a raw body and it crashes any endpoint that either (a) calls `str.encode("utf-8")` on the value
  (→ `UnicodeEncodeError`) or (b) echoes it into a response model (→ `PydanticSerializationError` at
  JSON-dump time, swallowed as a generic 500). The endpoint smoke (GAP-001) and raw-body sweep
  (GAP-010) both miss it because they send well-formed UTF-8. *(Found 2026-06-11 as SEC-SURROGATE: a
  lone surrogate 500'd **108** /v1 endpoints across both classes. Fixed with boundary exception
  handlers returning `422 {code:"invalid_encoding"}`, bytes never echoed (no leak); valid astral-plane
  emoji unaffected. Regression `tests/test_surrogate_hardening.py`.)* **How to check:** POST
  `--data-binary '{"text":"\ud800"}'` (NOT `json=` — that fails to encode client-side and hides the
  bug) to representative text endpoints; assert 422+`invalid_encoding`, never 500, and that the
  surrogate is not echoed in the body. Pair with a valid-emoji control that must still 200. P1 (a
  trivially-craftable input mass-500ing the API is a DoS + info-leak surface).

- **GAP-016 — A derived "result/savings" metric must stay HONEST on the EMPTY/DEGRADED input path
  for every processor, not just the ones already fixed** (applies to any project that reports a
  derived metric over user input) — a no-extraction / empty-result path that still reports a
  large/100% result is the most blatant dishonesty surface. *(Concrete class: an empty document
  extracted to a near-empty sentinel still reported `content_extracted:true` with a 100% reduction
  and a large savings number — the user gets nothing usable yet is told 100% was saved, while a
  real-content document reports honestly.)* **How to check:** feed each processor an empty / content-
  free input (image-only doc, scanned no-text PDF, etc.); assert that when the extracted body is
  empty/sentinel/below a small floor, the metric is zeroed (`content_extracted:false`, no fabricated
  savings) with an honest note ("No readable text found — this file may be image-only…"), never a
  100% claim. Fix in the shared no-extraction detector and extend it across ALL processors. P1.

- **GAP-017 — A backend metric fix isn't done until the USER-VISIBLE surface that displays it is
  verified, not just the API JSON** — when a client (a web page, an embedded UI, a dashboard)
  RE-COMPUTES a displayed metric from raw fields instead of trusting the server's authoritative
  value, a backend honesty fix can pass API-level verification while the UI still shows the old lie.
  *(Concrete class: after a backend fix zeroed a metric for an empty input, the UI badge STILL
  rendered the old 100% value because the client recomputed it locally from raw fields — caught only
  by a rendered-surface sweep, NOT by the API re-test. Fixed by making clients consume the server's
  authoritative field / honesty flag; add a frontend guard test.)* **How to check:** for any server
  metric a client also displays, grep the client for a local recompute of that metric from raw inputs
  — it must consume the server's authoritative field or gate on the server's honesty flag. Verify the
  rendered badge/number on the actual surface (browser QA / vision), never just the API response. P1
  (a fixed-on-the-server-but-lying-in-the-UI metric is indistinguishable from unfixed to the user).

- **GAP-018 — A query the product "defers to an LLM" must compute locally or DECLINE — never let a
  dumber fallback layer bare-answer it** (applies to any project with a tiered LLM-plus-fallback
  pipeline) — when a pipeline routes hard queries to an LLM but that LLM is unavailable (no credits,
  timeout, offline), the query falls through to cheaper layers (regex/pattern/ML) that answer with a
  DEGRADED result — e.g. dropping a filter or grouping and returning the global total — and dress it
  up as authoritative. A confidently-wrong answer is the most corrosive failure: unlike a visible
  error the user can't route around it. *(Concrete class: with the LLM layer dead in prod, "count
  rows where X=Y" returned the unfiltered total and "<measure> by <dim>" returned a single global
  sum. Fixed with data-aware resolvers that compute the filtered/grouped result against the real data
  BEFORE the bare-answerers run, and PASSTHROUGH/decline when unresolvable; add a reliability
  regression.)* **How to check:** with the LLM layer disabled (the prod reality when credits/keys are
  absent), probe every "answerable" path with a FILTERED/GROUPED variant and assert the result is
  correct OR an honest passthrough/decline — never a degraded number presented as the answer. Any
  layer that can bare-answer a query it failed to fully parse is the bug. P0 (silently wrong >
  visibly broken).

- **GAP-019 — Any list maintained by hand in multiple files must stay in parity, and every
  context-dependent feature must be capability-gated** — when the same set (hosts/targets, providers,
  feature entries) is duplicated across N source files, any drift leaves it half-wired: a miss in one
  file means the feature never activates in that context; a miss in another means it shows no active
  state/label. Same class for capability gating: an action rendered without checking the active
  context's capabilities offers features that only work in one context everywhere. *(Concrete class:
  a per-target list lived in three files and drifted when a target moved domains, silently no-op'ing;
  and context-only tools were offered in every context. Fixed with a parity regression test across
  all N files plus capability-gate tests.)* **How to check:** run the parity test; when ANY entry is
  added, it must appear in all the lists that maintain it + a capability entry, and periodically
  verify each target's CURRENT live address (targets move — check the live redirect, not the
  historical one). P0 when a headline target is unmatched; P1 for label/state-only drift.

- **GAP-023b — A natural-language intent classifier must route to the operation matching the NOUN,
  never default to a broad catch-all branch** (applies to any project with a NL query / intent
  router) — when a broad pattern (e.g. "how many …" → COUNT) is matched before noun-specific
  anchors, any phrasing the broad branch doesn't explicitly exclude is answered by the wrong
  operation. This is why per-noun guards must run *before* the broad branch. *(Concrete class:
  "how many columns are there?" returned a row count instead of a column count — fixed with a
  noun-specific guard ahead of the broad branch + a parametrized classify test.)* **How to check:**
  for every count-noun the product supports, assert the classifier returns the operation that
  ANSWERS that noun and that the answer LEADS with that quantity — not the catch-all default. P1
  (confidently-wrong answer). When a new intent is added, add its guard AND its parametrized classify
  test in the same commit.

- **GAP-024 — A compound NL query ("top N <entity> by <measure>") must return the correct
  per-entity aggregate, never an empty or value-count result** (applies to any NL query / intent
  router) — the top-N-by-measure path is brittle when two resolvers can both match the same phrasing.
  *(Concrete class: (a) "top N by <measure>" with no entity returned value-counts instead of a sorted
  result; (b) "top 2 <entity> by <measure>" returned zero groups because a grouping resolver matched
  first and INVERTED the "by" preposition — treating the measure as the dimension — so it grouped by
  the numeric measure and aggregated the string entity column. Fixed by deferring top-N/bottom-N
  phrasings to the resolver that assigns entity/measure correctly and applies the N limit; add
  guards.)* **How to check:** sweep `top N <entity> by <measure>` for every (entity, numeric-measure)
  pair on a known dataset and assert the answer names ≥1 group with a correct aggregate (not "0
  groups", not "rows total", not "by count"). P1 (confidently-empty/wrong answer to a common
  phrasing). LESSON: when two resolvers both match a phrasing, the one whose preposition semantics are
  INVERTED must defer — match order alone isn't enough.

- **GAP-025 — Every deterministic subpath of a multi-path feature must be covered by E2E/browser
  QA via generated fixtures with known answers, not just the primary flow** (applies to any system
  that routes inputs to multiple deterministic branches: query routers, file processors, classifiers,
  parsers) — when a feature has N deterministic subpaths, the E2E/browser QA suite typically drives
  only the primary/happy path. The other subpaths are guarded by unit tests + HTTP sweeps alone, so
  a regression in any non-primary branch ships green through the real UI while the user sees wrong
  output. *(Concrete class: a stat-resolver family covering difference/arithmetic/range/stddev query
  types had thorough unit tests and HTTP sweeps, but the browser QA drove only the primary
  row-count/top-scorer queries — a regression in any stat-resolver branch would ship green while the
  user saw wrong output from the live ask-box. Fixed by adding generated CSV fixtures with
  hand-computable expected answers for each resolver branch and wiring them into the browser QA
  suite; the suite now drives all subpaths and asserts exact answers per branch.)* **How to check:**
  enumerate the feature's deterministic subpaths (each resolver branch, processor type, or classifier
  class); for each, verify the E2E/browser suite contains ≥1 fixture that routes through that
  subpath and asserts the expected output. **Generated fixtures** (hand-computable inputs with known
  expected answers) are preferred over real-world files: they are deterministic, size-controlled, and
  their correct answer is provable without running the code. When adding a new subpath or resolver,
  add its generated fixture + E2E assertion in the same commit — a new branch with only a unit test
  is half-done (the wiring layer, config, and real UI invocation go untested). P1 (a non-primary
  subpath regression ships green while users see wrong output from a "passing" product).

- **GAP-025b — A browser QA fixture's (query, expected) pairs must also be exercised through a
  direct backend test, making the fixture a dual-layer (browser + backend) contract** — GAP-025
  closes the "unit-test-only is half-done" gap by requiring browser QA for every deterministic
  subpath. The companion gap runs in the opposite direction: when a browser fixture carries
  `(query, expected_substrings)` pairs, those pairs are exercised ONLY by the Playwright/browser
  run, which requires a live server and browser and never runs in the fast pytest gate. A
  regression in the backend that breaks a fixture answer sails through the pytest gate and
  surfaces only (if ever) during a scheduled or manual browser QA run. Fix: write a pytest test
  that drives the SAME fixture's `(query, expected)` pairs through the backend function or API
  endpoint directly (e.g., a test-client POST, or the processing function called without HTTP).
  The test must assert: (1) the query resolves to the expected mode (not passthrough/blank); and
  (2) all expected substrings appear in the answer. A companion guard prevents silent fixture
  coverage regression: assert that the fixture carries at least one query per major subpath/resolver
  by representative keyword — so a future fixture edit can't silently drop browser + backend coverage
  of a resolver while leaving both test files green. *(Concrete class: a code-Q&A feature with 25+
  resolver slices drove queries via Playwright against the live answer card but never through the
  backend processing function in a pytest gate — a resolver regression would ship green through CI
  and surface only in a manual browser run. Fixed by adding a pytest test that exercises the same
  fixture queries through the backend function directly; a resolver regression now fails CI within
  seconds, browser or not. The fixture also carries a coverage guard asserting representative queries
  for each major resolver remain present — preventing a future edit from silently dropping browser
  coverage of a resolver.)* **How to check:** for every browser QA fixture that carries `(query,
  expected)` or `(input, expected_output)` pairs, verify a pytest test drives those same pairs
  through the backend function/endpoint and asserts the expected output. When adding new fixture
  queries for a new subpath, add the backend contract assertion in the same commit — a new browser
  fixture entry with no backend pytest gate is half-done. Non-vacuous bar: disable the backend
  resolver and confirm the pytest test fails before any browser QA run. **P1** (browser-only
  fixture contracts let backend regressions ship green through every CI gate — they surface only
  during slow or manual browser runs, exactly when least expected).

- **GAP-026 — A file processor for a container format must handle EVERY content subtype, not just
  the primary type; the QA fixture set must synthetically generate files with each subtype** (applies
  to any project that parses container formats: office documents, PDFs, emails, archives, HTML with
  embedded content, etc.) — when a container format supports multiple content subtypes (e.g. DOCX has
  paragraphs, tables, embedded images; PPTX has slide text, tables, text boxes, notes; HTML has text
  nodes, tables, iframes; PDF has text layers, image layers, form fields), a processor that correctly
  handles the primary content type is often "container-blind" to the others. Two failure modes
  compound:

  (A) *Processor blindness*: the parser handles only the primary content type, silently skipping
  tables, text boxes, or other subtypes that are present in the file but produce no output.

  (B) *QA fixture blindness*: the autonomous QA harness uses real-world sample files which, by
  coincidence, contain only the primary content type — a regression in any secondary subtype's
  extraction runs green through the entire suite while users' files fail silently.

  *(Concrete class: a PPTX processor extracted slide titles but skipped shapes containing tables
  (shape.has_table) and crashed on any deck with a text box because the library raised ValueError for
  non-placeholder shapes in newer versions — most real decks contain text boxes. The QA fixture set
  used simple title-only slides. Both bugs were in production. Fixed by: (a) extending the processor
  to handle all subtypes; (b) adding a synthetic fixture generator that creates a file with ALL
  subtypes and wiring it into the QA harness so every autonomous run exercises the full surface.)*
  **How to check:** for each container format, enumerate its content subtypes from the library's
  object model; for each subtype, grep the processor for a handling branch (missing branch →
  container-blind); verify the QA fixture set has ≥1 synthetically generated file containing that
  subtype. **Synthetic generators** are preferred over real-world files: deterministic, size-
  controlled, provably correct, and they degrade gracefully (return `None`) when the library is
  absent so the harness skips rather than fails in minimal environments. When adding a new container
  format or extending an existing processor to a new subtype, the processor fix and its synthetic QA
  fixture must land in the same commit. Audit sibling processors for the same class of container
  blindness immediately after any fix — a bug in one family member usually exists in at least one
  sibling. P1 (secondary content types silently lost — tables, text boxes, or charts extract as empty
  while the test suite stays green).

- **GAP-027 — A processor that finds no extractable content must distinguish "genuinely empty" from
  "has content but currently unextractable", not return the same sentinel for both** (applies to any
  project with content processors: text extractors, document parsers, audio transcribers, image
  analyzers, etc.) — when a processor's output is empty, there are at least two distinct causes the
  user must differentiate: (1) *genuinely empty*: the file has no content of the expected type;
  (2) *has content but currently unextractable*: the file has content the processor cannot currently
  handle (e.g. a document whose content is entirely embedded scan images, an audio file with only
  background noise, a PDF with only scanned pages). Returning the same empty sentinel for both
  prevents users from taking the correct next action ("the file is empty — try a different file" vs.
  "OCR is required"), misleads downstream consumers that display derived metrics, and inflates false
  extraction-failure rates for format categories that simply need a different processing step.

  *(Concrete class: a .docx processor returned the same `"(Empty document)"` sentinel for both a
  genuinely empty file and a scan-only file that contained embedded images but no text runs — the
  user saw "NO TEXT EXTRACTED" and assumed the upload or the product was broken. Fixed by adding
  media-presence detection: check both inline_shapes and the package's image relationships (the
  relationship scan catches floating drawings that inline_shapes misses); when images are present but
  no text, return `{text: "contains only images or scanned content — no extractable text; OCR is
  required", image_only: True}` instead of the generic empty sentinel. The `image_only` flag enables
  UI and API consumers to show a specific next-step message rather than a generic failure banner.)*
  **How to check:** for each processor, identify the non-extractable content types it might encounter
  (embedded images, binary blobs, scanned media, password-protected layers); feed it a file with ONLY
  that non-extractable content type and no readable text; assert the response differs from a genuinely
  empty file in BOTH the human-readable explanation AND a structured metadata flag (`image_only`,
  `requires_ocr`, `binary_only`, etc.); verify the UI/API client consumes that flag to offer a
  specific next-step message. When a processor is extended to detect a new unextractable content
  type, add a test for the "has content but wrong type" case in the same commit. P1 (user cannot
  distinguish a broken upload from a file that needs a different processing step — both silent
  failures erode trust identically).

- **GAP-028 — Security controls must fail CLOSED, not OPEN: bare-except around security
  initialization is a silent guard removal** — when a security control (sandbox setup, access
  restriction, permission-check initialization, auth gate) is established in a `try` block and
  the corresponding `except` body is `pass` or a no-op, any failure of that setup at runtime
  silently removes the guard. The protected operation then runs UNSANDBOXED, WITHOUT the
  restriction applied, and produces no observable signal — no log, no metric, no error. A
  library upgrade, runtime flag removal, or environment regression that prevents security
  initialization from executing is therefore indistinguishable from a healthy run until the
  unguarded operation is exploited.
  *(Concrete class: a SQL executor sandboxed with `SET enable_external_access=false` had this
  call wrapped in `except: pass`. A comment claimed a downstream table-rewrite still applied,
  but that rewrite did NOT block `COPY ... TO` or `ATTACH` — so a runtime where the SET raised
  would execute untrusted SQL fully unsandboxed with no observable difference. Fixed: raise on
  setup failure, refuse to execute the protected operation. Regression: a test that monkeypatches
  the SET to raise and asserts no protected operation executes.)*
  **How to check:** grep every security-critical initialization block for `except: pass`,
  `except Exception: pass`, or any bare-except that does not re-raise or abort — each is a
  potential fails-open. Key contexts: SQL sandbox setup, filesystem permission restriction,
  auth dependency injection, capability-flag setting, network isolation (`enable_external`,
  `restrict_access`, `set_privilege`). The correct pattern: if the security gate can't be
  established, raise/return-error and refuse the operation. Wire the check into `/critical-checks`
  Block 4 (check 4f). **P0** (a fails-open security control silently removes the guard in prod
  with no observable signal — invisible until exploited).

- **GAP-029 — Features that support multiple input formats must have regression tests covering
  each format's distinct parser path, not just the primary development format** — when a feature
  (query engine, content analyzer, classifier, transformer) routes different input formats (CSV,
  JSON, XML, Parquet, database tables) through different parsers or loading paths, a regression
  test written against format A does NOT guard format B's code path. A bug in the JSON parser or
  JSON-specific schema normalization will pass all CSV regression tests, ship green, and surface
  only when a user uploads a JSON file. This is a common oversight when a feature is first
  developed and tested against the developer's primary format and later extended to others.
  *(Concrete class: a query-resolver family covering column-comparison and cross-group
  measure-difference operations had thorough unit tests and HTTP sweeps against CSV fixtures;
  JSON files used a different load/schema-unwrap path. Both operations computed the correct
  answer on JSON, but all regression guards were CSV-only — the JSON path was silently
  unguarded and a bug introduced in the JSON parser would have shipped green.)*
  **How to check:** for each feature that branches on input format (grep for `if format ==`,
  `isinstance(src, ...)`, or separate `load_csv`/`load_json`/`parse_xml` functions in the
  data-loading path), verify the regression suite has ≥1 test per supported format that
  exercises the same capability with a known expected answer. When adding support for a new
  format, add its regression fixture in the same commit. When adding a new resolver, operation,
  or classifier variant, ensure tests cover ALL currently supported formats, not just the one
  used during development. For container formats (DOCX, PPTX, PDF), the content-subtype rule
  in GAP-026 applies in addition to this format-path rule. **P1** (a regression in a
  non-primary format path ships green while users see wrong output on their files).

- **GAP-030 — File upload WRITE-path traversal is a distinct, often-untested security
  surface from the READ-path guard** — when a file-upload feature is hardened for path
  traversal, the READ path (resolving a stored file ID or server_path back to a filesystem
  location) is typically tested. The WRITE path — the upload FILENAME being used as or
  contributing to a filesystem path — is a SEPARATE, asymmetric surface that is frequently
  left unguarded and untested (CWE-22). A malicious filename like `../../etc/passwd.csv`,
  if the handler combines any part of the original name with a directory prefix, can write
  outside the intended uploads directory. The secure invariant: the filesystem storage key
  MUST be a server-assigned identifier (UUID4, content hash) and NEVER derived from or
  combined with user-supplied input; the only part of the original filename that may be
  consulted is the extension, and only against an allowlist.
  *(Concrete class: a file-upload endpoint correctly used a server UUID4 + allowlisted
  suffix and never exposed the raw filename to the filesystem — but only the READ-path
  traversal had a regression test. The asymmetric gap meant a future "use the original
  filename" refactor would silently reintroduce CWE-22 with no test to catch it. Fixed
  by adding a parametrized regression: upload files with traversal filenames
  (`../../etc/passwd.csv`, `....//evil.csv`, `/abs/x.csv`) and assert the stored path/id
  contains no `..`, `/`, or original traversal fragments — encoding the invariant that
  the raw filename never reaches the filesystem.)*
  **How to check:** (1) grep every upload handler for uses of the user-supplied filename
  after receipt — if it appears in `os.path.join`, `Path(...)`, `open()`, or any
  filesystem write, that is the vulnerable site; (2) confirm the storage key is
  server-assigned (UUID, hash), not derived from the original filename; (3) add a
  parametrized regression that probes traversal filenames and asserts the stored id/path
  contains no traversal fragments — a 200-with-UUID-id is correct, a 200-with-traversal-
  path is the bug. **The READ-path guard and WRITE-path guard are independent — guarding
  one does not imply the other is guarded.** P1 (a write outside the intended directory
  can overwrite config files, other users' data, or executable assets, and may be silent
  until exploited).

- **GAP-031 — A QA test that navigates + screenshots + bumps a counter without any
  structural assertion is a false PASS — `tests_run > 0` does not mean assertions
  ran** — GAP-011 catches the `tests_run == 0` failure mode (all tests skipped →
  verdict is PASS because zero findings). This rule covers the complementary case:
  `tests_run = N` with N > 0, but every "test" consisted of navigate → screenshot
  → queue a question for a vision AI that is dead. The counter is non-zero, yet
  nothing was deterministically verified. When the vision AI is unavailable, the
  run silently PASSES while the product may be visibly broken (e.g. a critical
  button is absent from the page).
  *(Concrete class: a browser UX audit navigated to N providers, took a screenshot
  per provider, incremented `tests_run`, and printed vision QUESTION prompts for a
  metered grader. The grader had no credits, so all N questions went unanswered —
  yet "ux: PASS (N tests)" was reported. A critical UI element was entirely absent
  from the page and went undetected. Fixed by adding a direct DOM query per critical
  element alongside the screenshot; the DOM check runs independently of vision
  grading and raises a finding when the element is absent or hidden.)*
  **How to check:** for every test that increments `tests_run` (or equivalent),
  verify it also contains at least one structural assertion (DOM element query,
  HTTP status check, non-None/non-empty response field, etc.) that can independently
  raise a finding without calling the AI/vision grader. If removing the AI/vision
  call would leave the test with no assertions at all, it is observation, not a test.
  Rule of thumb: a test that only takes a screenshot and asks a question is
  **documentation** — rename it as such and do not include it in the test count.
  Add a structural fallback for every critical capability the vision path is meant to
  verify: DOM presence, visibility, HTTP status, or a key response field — whichever
  is cheapest to check deterministically. P1 (false-green QA masks product regressions
  exactly when the AI-grading infrastructure is most likely to be dead).

- **GAP-032 — Adversarial input timeout in browser QA must distinguish "no-answer card"
  (graceful passthrough) from "UI hung" (actual failure)** — a hostile or unanswerable
  input (SQL injection probe, prompt-injection string, junk input) that the system
  correctly handles produces NO direct-answer UI element and may take several seconds
  to resolve server-side. A QA harness that waits for a result card and reports "crashed
  the UI (P2)" whenever the wait times out produces recurring false positives: a graceful
  no-result is misread as a crash. This is the inverse of the "couldn't verify → verified
  OK" anti-pattern: a non-crash timeout must not read as "crashed". *(Concrete class:
  an adversarial query correctly resolved to None → passthrough, producing no answer card;
  but the XSS probe alone took ~7.6s server-side, so the 15s wait fired as a false P2
  "crashed the UI". Fix: on timeout, call `input_el.is_editable(timeout=3s)` — still
  editable → graceful note, no finding; unresponsive → P2 "hung the UI".)*
  **How to check:** for every QA path that waits for a result element after a hostile
  input, verify the timeout branch probes the input element's responsiveness before
  raising a P2 finding — a page that is alive but simply produced no answer is not a
  crash. Other guards (content leak P1, blank-but-visible card P2, executed XSS P0)
  are distinct and must remain. P1 (false-positive P2s on correctly-handled hostile
  inputs mask real findings behind noise).

- **GAP-033 — Browser QA form submissions must use scroll + short explicit timeout +
  single retry, never the driver's default long timeout** — a browser QA harness that
  clicks a submit button with the driver's default timeout (e.g. Playwright's 30s)
  converts any transiently non-actionable button into a 30s hang followed by a false
  finding. Buttons become transiently non-actionable when the prior result panel or
  upload card is still settling in the DOM — a common condition when a QA harness
  submits back-to-back actions. The full 30s hang fires as a false P2 or P3 and rotates
  across different tests each run depending on which preceding action left the DOM in
  transition. *(Concrete class: bare `.click()` on a submit button with the 30s default
  produced a recurring flaky P2 on non-CSV fixtures because the prior upload/query
  result panel left the button briefly non-actionable. Fix: scroll into view → click
  with 8s timeout → wait 800ms for panel to settle → retry once with 8s timeout.
  A button still unresponsive after retry is caught by the outer handler as an honest
  finding. Coverage improved as a side-effect: previously timing-out tests now execute
  and assert correctly.)* **How to check:** grep every browser QA harness for `.click()`
  calls without an explicit short timeout — each is a potential 30s hang source. Apply
  the scroll + short-timeout + settle + retry-once pattern to every form-submit or
  action button. A button that is genuinely broken is still caught after the retry;
  a transiently non-actionable button no longer produces a false finding. P2 (false
  findings from transient DOM transitions inflate the noise floor and mask real P2s).

- **GAP-034 — Deploy config safety thresholds must have regression tests to prevent dangerous
  regressions** — a deploy config file that contains safety-critical settings (healthcheck
  window, startup grace period, retry count, timeout) can silently regress to a dangerous
  value when someone edits it for another reason or merges conflicting changes. A 30s
  healthcheck window (a common platform default) can take a healthy container fully DOWN
  during a slow cold-start deploy: the healthcheck fires before the process is ready, the
  healthcheck fails, the running container is killed, and the replacement is also killed
  before it starts — prod goes down even though the build was green. *(Concrete class:
  `healthcheckTimeout = 30` in a Railway config killed prod on a deploy whose build-smoke
  passed cleanly; the cold-start for a heavy ML dependency exceeded the window. Fix: widened
  to 300s. Regression test: parse the deploy config and assert `healthcheckTimeout >= 120` so
  the dangerous value can never silently return.)*
  A regression test that parses the deploy config and asserts minimum safe values costs one
  minute to write and permanently eliminates the class:
  ```python
  import tomllib  # or yaml / json — depends on your platform's config format
  def test_healthcheck_window_not_dangerously_short():
      with open("deploy.toml", "rb") as f:
          cfg = tomllib.load(f)
      timeout = cfg["deploy"]["healthcheckTimeout"]
      assert timeout >= 120, f"healthcheck window {timeout}s too short — slow cold starts will take prod DOWN"
  ```
  Apply to: Railway (`healthcheckTimeout`), Fly.io (`[checks] grace_period`), Render (timeout
  field), k8s liveness/readiness probes (`initialDelaySeconds`, `timeoutSeconds`), Docker
  `HEALTHCHECK` (`--timeout`, `--start-period`). **How to check:** does the project have a
  deploy config file with timeout/window/grace fields? Verify a regression test asserts each
  safety-critical field meets its minimum safe value. P0 (a dangerously short window can take
  a healthy service fully DOWN during a normal deploy — not a hypothetical risk).

- **GAP-035 — A feature-presence check where ALL probes return inconclusive must report
  DEGRADED, not PASS** — when a QA check verifies a feature across N environments,
  providers, or sites, and every probe returns INCONCLUSIVE (the probe mechanism itself
  could not run — JS injection disabled, vision/grader credentials dead, driver mode
  unsupported), the check has asserted NOTHING about feature presence or absence. Reporting
  PASS conflates "confirmed it works" with "couldn't even check." *(Concrete class: a
  native computer-use harness verifying a browser-extension button across 3 providers via
  JS injection. If JS-from-AppleEvents is disabled on all 3 and the vision grader is dead,
  every provider returns probe-inconclusive. The harness originally reported PASS — a real
  regression (button absent) would have been invisible indefinitely. Fix: track
  `_confirmed` and `_inconclusive` counters separately; gate: if `confirmed == 0 and
  inconclusive > 0`, raise a P1 "feature UNVERIFIED on all providers" and degrade the
  run.)*
  This is distinct from GAP-031 (tests ran but made no structural assertion — screenshot-only)
  and GAP-032 (timeout after adversarial input misread as a crash). GAP-035 is the "all
  probes executed but NONE could determine the answer" case — the probe mechanism itself
  is the environmental failure. **How to implement:** for any check that loops over N
  sites/providers, initialize `_confirmed = 0` and `_inconclusive = 0` before the loop;
  inside the loop, increment `_confirmed` on a positive assertion (feature detected) or
  `_inconclusive` on a probe-mechanism failure (environment limitation, not feature
  failure); after the loop: `if _confirmed == 0 and _inconclusive > 0: [add P1 finding +
  DEGRADED]`. A probe that definitively confirms absence (feature element not in DOM,
  endpoint 404) is a legitimate finding — not inconclusive. P1 (an all-inconclusive run
  masks real regressions behind a perpetually green verdict).

- **GAP-036 — The QA harness's own honesty gate (GAP-035) must be regression-tested**
  — implementing the `_confirmed`/`_inconclusive` counter pattern (GAP-035) is necessary
  but not sufficient: the honesty gate logic itself must be covered by a unit test that
  mocks all probe paths to return "inconclusive/error" and asserts the harness emits
  DEGRADED with an "UNVERIFIED" finding — not PASS. Without this guard-the-guard test, a
  refactor that removes or bypasses the honesty check will pass CI and silently re-introduce
  the false-PASS blind spot it was designed to prevent. *(Concrete class: a Computer Use QA
  harness that probes a UI element via DOM injection + vision grader fallback. When both
  paths are mocked to fail — JS injection disabled, grader unavailable — a test asserts the
  harness reports DEGRADED + P1 "UNVERIFIED". Without this test, a refactor that re-orders
  the probe logic quietly undoes the honesty gate and the harness goes back to reporting
  PASS with zero successful assertions.)*
  **How to implement:** write a test that (1) mocks/monkeypatches ALL probe mechanisms to
  return an inconclusive signal (e.g., empty string, error string, `None`), (2) calls the
  check function, (3) asserts `status == "DEGRADED"` and that at least one finding contains
  "UNVERIFIED" or equivalent. Run it in CI as part of the harness's own test suite — not
  just as an integration test. Keep the mock minimal: every probe call that could bypass the
  honesty gate must be intercepted. Sibling to GAP-031 (make assertions, not just
  screenshots) and GAP-035 (the gate this test guards). P1 (the honesty gate silently
  breaking masks regressions indefinitely behind a perpetual green verdict).

- **GAP-037 — A project with a server-side LLM API key that serves open or anonymous
  traffic must have a daily spend ceiling; the ceiling must be default-OFF and fail-open**
  (applies to any project where the server, not the user, pays for LLM calls — e.g. a free
  tier that routes to the server's API key) — when a burst of anonymous traffic, a loop
  misfire, or free-tier misuse occurs, there is no natural back-pressure on server-side LLM
  cost: each request drains the owner's API balance with no per-user quota, and credits can
  be exhausted in minutes at modest traffic. The risk is highest when the project also runs
  an autonomous improve/QA loop on the same key — a loop that misfires will spend as fast as
  it can iterate.

  The implementation pattern (three invariants):
  1. **Env-gated ceiling (default OFF):** read the ceiling from an env var (e.g.
     `PROJECT_MAX_DAILY_LLM_USD`). Unset or `<= 0` → the feature is OFF and every call is
     allowed. This makes the module safe to ship before the operator has chosen a value —
     adding it changes no behavior until the env var is set.
  2. **Check-before / record-after:** call `check_budget()` before making the LLM call. If
     over cap, refuse with a service-unavailable error and do NOT make the LLM call. After a
     successful LLM call, call `record_spend(cost_usd)` with the actual estimated cost. The
     call that crosses the cap completes; only the NEXT call is refused.
  3. **Fail-open:** if the budget infrastructure itself fails (file I/O error, lock timeout,
     corrupt state), the LLM call proceeds unmetered. Budget infra failure must never take
     down the feature — the ceiling is a safety constraint, not a primary control path.

  Implementation detail: the durable counter lives in a JSON file in the project's data
  directory (`{day, usd_spent}`), flock-guarded (`fcntl.LOCK_EX`) so it is exact across N
  workers and redeploys. Reset on UTC-day change: compare today's UTC date to the stored
  `day` field; if different, zero `usd_spent` before the check. Write atomically: write to a
  temp file in the same directory, then `os.replace()`.

  Companion practice: **admin-gated spend observability** — add a read-only `llm_budget`
  field to the admin health endpoint (admin-auth-gated so spend is never leaked publicly):
  `{spent_usd_today, cap_usd, remaining_usd, enforced, resets_utc}`. This gives the
  operator visibility to choose a sensible cap before enabling it.

  Also: **load test harnesses must be credit-safe by construction.** A load test that hits
  server-side LLM endpoints will (a) exhaust the daily ceiling mid-run, producing misleading
  "service refused" errors, and (b) produce non-deterministic results. Block all LLM/AI
  paths in the harness via an explicit `_FORBIDDEN` allowlist (a set of path substrings
  whose endpoints are skipped); any attempt to hit a forbidden path fails the harness
  immediately. Cover the credit-safety invariant with a unit test.

  **How to check:** (1) does the project use a server-side LLM API key for open traffic? if
  yes, grep for `check_budget` (or equivalent) before every LLM API call site — each path
  without a spend check is exposed; (2) verify the ceiling env var is unset by default in
  all deploy configs; (3) if a load-test harness exists, verify it blocks LLM endpoints with
  a forbidden-list and tests that invariant; (4) verify admin observability exposes the spend
  view behind admin auth. **P0** (server-key LLM spend with no ceiling can exhaust the
  owner's monthly API budget in minutes under modest anonymous traffic).

- **GAP-038 — Test gates must exclude by test path/marker, never by assertion message; must
  count ERROR lines** — a deploy gate using `grep '^FAILED' | grep -cv 'pattern'` filters by
  the TRUNCATED ASSERTION MESSAGE pytest appends to the summary line, not by the test's file
  path. This is wrong in both directions: (a) a real failure whose message contains the
  exclusion word scores 0 real failures (false green — the natural "fix" is renaming the
  assertion string to dodge the filter, masking the regression); (b) a browser-only test
  whose message doesn't contain the word survives exclusion even though its FILE should have
  excluded it. Additionally, `grep '^FAILED'` is ERROR-blind — a collection or teardown
  `ERROR` produces no FAILED line and scores 0. Fix: maintain an explicit allowlist of
  environment-only test FILE PATHS (or pytest markers); count by path/marker match, never by
  message content; count both `^FAILED` and `^ERROR` lines; fail closed when the counted
  lines are fewer than pytest's own trailing `N failed, M errors` summary — a truncated or
  `-rfonly` log that yields zero attributable lines must close the gate, not pass it.
  **How to check:** run the gate against a synthetic pytest log where a failure line contains
  the exclusion word in its message only — the gate must count it as a real failure. Add a
  unit test asserting the counter matches pytest's own trailing summary. **P1** (a
  message-text filter lets a real regression pass the gate when the assertion text coincides
  with the exclusion pattern — and the natural workaround makes the regression invisible).

- **GAP-040 — A test that exercises a destructive CLI path must sever the effecting layer;
  the test's safety must NOT depend on the guard under test** — when a test calls a CLI entry
  point that contains a destructive flag or subcommand (`--force`, `--yes`, `--apply`,
  `--prune`, `-D`, `rm -rf`, `worktree remove`, `branch -D`, `reset --hard`, `push --force`,
  `clean -f`), the test's safety traditionally relies on the production guard (an `if-condition`,
  a confirmation prompt, or an argparse rule) to intercept the flag before the filesystem or
  git call executes. That is insufficient: a regression in the guard under test — the very
  thing the test is meant to exercise — can silently turn the test run itself into a
  destructive act. The correct pattern: sever the effecting layer (monkeypatch/stub the `_git`,
  `subprocess.run`, or `os.system` call to raise or record) so the CLI path can be fully
  exercised without any mutation ever reaching the filesystem or remote.
  *(Concrete class: a test in `tests/test_worktree_hygiene.py` drove the real worktree-pruning
  CLI with `--yes` against the live repo. It was inert ONLY because one argparse line rejected
  the flag combination first; had that line regressed, the test run itself would have
  force-removed every worktree past the TTL. Docstring promised "NOTHING IN THIS FILE EXECUTES
  A REMOVAL." Fixed by monkeypatching `prune._git` to raise, so the CLI can be exercised
  without any git call ever reaching disk.)*
  **How to check:** (1) for every test whose string constants include a destructive flag in a
  `subprocess.run([...])`, `main([...])`, or `os.system("...")` call, confirm the effecting
  layer (`_git`, `subprocess.run`, `os.system`, or the underlying I/O function) is severed via
  monkeypatch/mock before the call — NOT by relying on the production guard itself; (2) verify
  the severing is tested: removing the mock must turn the test red (a companion test like
  `test_known_probe_still_severs_its_effecting_layer` asserts the guard would fire if the
  severing were removed); (3) optionally, add an AST meta-test that scans all test files for
  destructive string constants in callable arguments and flags each for human review — static
  detection cannot prove severing is in place, but it prevents silent copy-paste
  reintroductions of the pattern from reaching CI unreviewed. A ratcheted allowlist (frozen
  known-safe probes; any new one triggers review) prevents both false positives and gradual
  accumulation. **P1** (a test that becomes a real destructive operation when the guard under
  test regresses is a latent data-loss bug in the CI pipeline itself — triggered by the very
  act of testing the regression).

- **GAP-039 — A QA sweep that declares N surfaces must actually invoke ALL N; persisted
  artifacts are the audit trail** — when a browser/visual QA harness lists N surfaces
  (providers, sites, views) in its config but every invocation passes a fixed
  `--site primary` flag (or equivalent), the other N-1 surfaces are silently never swept.
  The harness may have "supported" all N for weeks; every persisted report covers only the
  default. From practice: 156 QA reports, all single-surface; the non-primary flagship
  surface went unverified for 7 weeks while all QA gates stayed green.
  **How to check:** count distinct `site`/`surface` values in the persisted `reports/qa/`
  artifacts; if any declared surface has zero reports in the last K runs, it has never been
  swept in the loop. Fix: (1) the default sweep must enumerate ALL surfaces explicitly — no
  `--site default` that silently omits others; (2) add a guard that reads the artifact
  directory and asserts each declared surface produced ≥1 report in the last K runs;
  (3) for surfaces requiring login, add a mock-DOM or route-interception smoke that exercises
  the detection/injection machinery at the real origin without credentials — a smoke that
  definitively passes or SKIPs is better than perpetual omission. **P1** (a surface that is
  never swept accumulates regressions invisibly while the QA gate stays green on the one
  surface it does check).

- **GAP-041 — CI/CD deploy pipelines must fire only on runtime-path changes; a missed runtime
  path (stale prod with green health) is worse than a wasted deploy** — when a project's deploy
  pipeline triggers on every push — including docs, tests, scripts, GOAL.md, memory files, and
  CI config — it accumulates unnecessary rebuild-and-swap cycles. On a heavy stack (ML
  dependencies, large Docker layers, platform cold-start), each wasted deploy costs minutes and
  counts against the CI budget; an autonomous improve loop that pushes test-only or doc fixes
  can exhaust the daily deploy budget before any real work ships.

  Fix: configure `watchPatterns` (Railway), `on.push.paths` (GitHub Actions), `paths`
  (Fly.io `[deploy]`), or the equivalent include list for your platform so a deploy fires ONLY
  when a runtime-affecting path changes. The include list must be **generous on purpose**: a
  missed runtime path means a real fix silently never ships while the health endpoint stays
  green on stale code — the "stale prod with green health" failure mode is far worse than a
  wasted deploy. Every directory the running server can import from must be listed, plus the
  build inputs (lock file, Dockerfile, platform config file).

  Explicitly OUT (do NOT trigger a deploy for changes to these only):
  `tests/`, `scripts/`, `docs/`, `reports/`, `*.md` (including GOAL.md), memory files,
  `.github/`, `.claude/`, CI-only configs unrelated to runtime behavior.

  Companion practice: **the loop must still deploy-verify every runtime push** by confirming the
  deployed build SHA matches the commit that changed a runtime path. A health-endpoint check
  alone proves nothing about freshness.

  ```toml
  # Example — Railway (railway.toml)
  # Generous by design: every runtime directory, plus build inputs.
  watchPatterns = [
    "api/**", "server/**", "core/**", "processors/**", "storage/**",
    "pyproject.toml", "requirements*.txt", "Dockerfile", "railway.toml",
  ]

  # Example — GitHub Actions (on.push.paths)
  # on:
  #   push:
  #     paths:
  #       - "src/**"
  #       - "app/**"
  #       - "package.json"
  #       - "Dockerfile"
  ```

  **How to check:** (1) does the deploy config scope triggers to runtime directories? if not,
  every doc/test push fires a wasted rebuild; (2) add a regression test that parses the deploy
  config and asserts each top-level runtime directory is listed and `tests/`/`docs/`/`scripts/`
  are absent from the include list; (3) after any watchPatterns change, verify that a runtime
  push still triggers a deploy (check the deploy log for a fresh build SHA) — silent no-deploy
  on a runtime push is the failure mode this rule guards against. **P2** (wasted deploy cycles;
  compounds to CI budget exhaustion when an autonomous improve loop pushes frequently;
  escalates to P1 if a watchPatterns misconfiguration causes a real runtime fix to silently
  never deploy while health stays green).

- **GAP-042 — An AI-backed Q&A fixture must carry "excluded entity values" when the query's
  subject type is constrained; a harness-side lint must detect answers whose leading label
  names a categorically wrong entity** (applies to any project with a natural-language query
  feature backed by an AI/ML layer — data Q&A, semantic search, summarization with attribution,
  intent routers) — the "confidently wrong entity type" failure class is distinct from the
  "wrong operation" class (GAP-023b/024): the AI executes the correct aggregation (SUM, top-N,
  max) but resolves the subject label from the wrong column type — e.g. a product name or a
  region code where a person's name is expected. This failure presents as a confident, complete,
  well-formatted answer, so positive expected-value assertions ("answer contains TopSalesperson")
  pass while the label is categorically wrong for the question asked ("Who has the highest
  revenue?" → "Gizmo — total revenue: …" where Gizmo is a product, not a person). Positive
  fixture assertions guard the operation; they do not guard the entity type.

  **Pattern — fixture-parametrized entity exclusion linting:** extend each fixture's query
  entry from a 2-tuple `(question, expected_values)` to an optional 3-tuple
  `(question, expected_values, excluded_subjects)`, where `excluded_subjects` is the
  caller-supplied set of values that the answer's leading subject must NOT match (e.g. for an
  employee dataset, supply the department names, product names, or region codes that should
  never appear as a person identifier). The harness:
  1. After extracting the answer, splits on the first label delimiter (`—`, `:`, `(`) to
     isolate the subject token.
  2. Checks the subject against each excluded value with word-boundary matching (so short
     tokens cannot false-fire as substrings of legitimate subjects).
  3. Reports P1 "confidently wrong entity type" if any excluded value matches.

  ```python
  # Fixture entry — 3rd element arms the entity-exclusion lint.
  # Without the 3rd element, existing 2-tuple fixtures are unchanged.
  QUERIES = [
      ("how many rows?", ["250"]),                       # 2-tuple: no entity check
      ("who has the highest score?", ["Alice"],          # 3-tuple: entity check armed
       ["Engineering", "Marketing", "Sales"]),           # these must never be the subject
  ]

  def _lint_entity_type(query, answer, excluded):
      """Return a P1 message if the answer's leading subject matches an excluded value."""
      import re
      if not excluded or not re.search(r"^\s*who\b|^\s*whom\b", query, re.I):
          return None  # lint only fires for person-selection phrasing; extend as needed
      subject = re.split(r"\s+[—:-]|\s+\(", (answer or "").strip(), maxsplit=1)[0].strip()
      for val in excluded:
          if val and re.search(rf"\b{re.escape(str(val).strip())}\b", subject, re.I):
              return f"answer named excluded entity '{val}' as subject — categorically wrong type"
      return None

  # In the harness loop:
  for entry in QUERIES:
      question, expected = entry[0], entry[1]
      excluded = entry[2] if len(entry) > 2 else None
      answer = query_feature(question)
      bad = _lint_entity_type(question, answer, excluded)
      if bad:
          findings.add("P1", "confidently-wrong entity type", bad)
  ```

  **Why this class is hard to catch otherwise:** the answer looks correct (right operation,
  right format), and any positive assertion about the expected person name passes unless that
  name also appears in the excluded set. The lint fires precisely when the AI confuses the
  entity column — a regression invisible to existing positive guards.

  **How to check:** for each fixture that tests a person-selection, actor-identification, or
  entity-attribution query, supply the known non-person column values from the test dataset as
  excluded subjects; run the harness against a known-broken version (one that resolves by the
  wrong column) and confirm the lint fires. The "non-vacuous" bar: remove the fix, run the
  harness, observe the P1 — only then is the lint trusted. When adding a new Q&A intent whose
  answer must name a specific entity type (person, company, country, product), add its excluded
  values to the fixture in the same commit. **P1** (a confident, correctly-formatted answer
  that names the wrong entity type is indistinguishable from a correct answer to the user —
  it erodes trust more than a visible error, which at least signals something went wrong).

- **GAP-043 — A UI element that displays data must derive from an actual data source; hardcoded
  values shown as real user data are fabricated display data** (applies to any project with stats
  panels, dashboards, status markers, or charts) — a UI element that renders specific values
  (percentages, counts, labels, status tags) without a live data source populating them is
  fabricated display data. It is a truthfulness violation distinct from an inaccurate metric —
  the data has *no source at all*. The most corrosive case: fabricated stats rendered beside
  honestly-hydrated values in the same panel, visually indistinguishable, so users see invented
  numbers alongside their real data. A secondary failure mode is the *test-enshrined lie*: a
  guard test asserting the fictional content exists (e.g., `assert "42%" in html`) passes as a
  correctness check and actively shields the fiction from detection. *(Concrete classes:
  pie-chart bars with hardcoded percentages beside a live-API stats card; a plan-status marker
  statically branded to a paid tier for all users including free ones — both with guard tests
  that asserted the fiction existed and treated it as correct.)* **How to check:** for every
  stats panel, dashboard tile, status badge, or chart: grep the template/component for
  hardcoded numeric literals or static class-name constants; trace each one to the code that
  populates it; confirm there is a real API call, DB query, or computed value on that path.
  For every status marker (plan badge, tier label, feature-gate indicator), confirm it is set
  dynamically from resolved runtime state, never from a static default. When a data source does
  not yet exist, the correct path is to hide the element with an honest "data unavailable" note —
  never render a fabricated value. Guard tests must assert the *absence* of the hardcoded
  literal, or assert the value derives from a real source; asserting the literal *exists*
  enshrines the fiction. **P1** (invented numbers shown as personal data erode trust more than
  empty state — a fabricated stat is a truthfulness bug, not cosmetic).

- **GAP-044 — Every error path in a user-facing UI surface (web page, chat thread, form,
  injected component) must map to first-party consumer copy — raw server implementation detail
  must never reach the user** (applies to any project with a web UI backed by a server) — a
  web surface whose error handler falls through to `String(err.detail)`, `err.message`, or any
  verbatim server response will expose implementation detail: internal tier names, package
  manager commands, environment variable names, raw exception messages, framework validation
  arrays. This is distinct from GAP-014 (API JSON contract shape): GAP-014 governs what the
  API returns to API callers; this gap governs what the *rendered UI* shows to end users.
  Typical failure pattern: the handler has one narrow case (e.g., a 401 branch) and falls
  through to `String(err.detail)` for every other status code. *(Concrete classes: a chat
  thread showing internal quota-tier jargon on a 429; a signup form leaking "pip install httpx"
  / "set ENV_VAR header" on server errors; a rate-limit message surfacing raw provider copy
  instead of the product's own upgrade path.)* **How to check:** for every HTTP status the
  backend can return (400, 401, 403, 422, 429, 5xx): trace the frontend error handler and
  confirm each code maps to a hardcoded first-party string, never a raw pass-through. Quick
  heuristic: search rendered error-state HTML for env-var patterns (`[A-Z_]{5,}_KEY`), package
  manager commands, and internal identifier strings — any hit is leaked implementation detail.
  Write a guard test that sends each error code through the render path and asserts none of the
  known raw-detail strings appear in the output. **P1** (leaked implementation detail confuses
  users, erodes trust, and is a minor info-disclosure surface).

- **GAP-045 — A product feature with a zero-friction "no user data" entry point (demo CTA,
  sample dataset, embedded fixture) is a DISTINCT call site from the main data-input path and
  must be independently verified in QA** (applies to any project with a try-with-sample /
  use-demo-data CTA alongside a main user-data upload or input flow) — when a feature can be
  reached via (a) the user supplies their own data/input, OR (b) a zero-friction demo path
  that synthesises or loads a built-in sample, these are two separate browser call sites.
  The main path being green does NOT verify the demo CTA: the demo handler, the sample data
  constant, and the pipeline entry point wired to the CTA are all untested. A regression
  breaking only the demo button will show a first-time visitor a dead CTA while every main-path
  test stays green — the worst possible failure mode for conversion. *(Concrete class: a "Try it
  with sample data" button that synthesises a File from an in-page constant and submits it
  through the same process→result pipeline as a real upload; QA drove only the file-input upload
  path; the CTA was silently broken across multiple deploys.)* This is the
  "one-call-site blind spot" applied to UI-level feature entry points: a QA dimension wired to
  one call site reads as coverage but is not. Sibling to GAP-025 (every deterministic code
  subpath needs E2E coverage) — this gap is specifically about UI entry points to the same
  pipeline, not internal code branches. **How to check:** for each feature with a demo/sample
  CTA, write a separate QA probe that clicks the CTA directly (not the main data-input control),
  waits for the result surface, and asserts the sample filename and core output invariants.
  Make the probe non-vacuous: remove the CTA handler, run the probe, observe the failure — only
  then is the test trusted. When adding any new "try with our data" or "see a demo" entry point,
  add the QA probe in the same commit. **P1** (a broken zero-friction path fails exactly the
  users most valuable to convert — first-time visitors with no data of their own).

- **GAP-046 — A feature that can be built both deterministically (zero-cost) and via an LLM call
  must ship the deterministic tier first; the LLM tier is a later, explicitly credit-gated slice**
  (applies to any project with a Q&A resolver, classifier, or pipeline step that could be driven by
  rules/AST/static analysis OR by an LLM) — when both approaches are possible, shipping only the
  LLM tier means the feature goes silently dark the moment API credits hit $0, and every regression
  test that calls the LLM is non-deterministic and potentially costly. Building the LLM tier first
  also leaves no fallback: users get a hard failure with nothing. The deterministic tier (regex, AST
  analysis, rules-based lookup, static analysis output) is always available, always costs nothing,
  and is regression-testable against known expected outputs. *(Concrete class: a code-Q&A resolver
  that can answer "where is symbol X defined" both by scanning a parsed AST outline — zero-LLM,
  grounded in real file content — and by prompting a language model. Shipping the LLM tier first
  meant the feature was dark in prod when credits were $0; the deterministic tier, shipped first
  as slice-1, gave users useful answers immediately and let every regression test run for free.)*

  **The two rules of deterministic-first slicing:**
  1. **Ship the deterministic tier first.** Build a resolver/handler that answers the question
     using local computation (AST parsing, regex, rules, pre-computed index, static analysis
     output). Prove it correct against hand-authored fixture answers. Merge and ship it as its
     own commit BEFORE touching the LLM path.
  2. **Gate the LLM tier on available credits.** The LLM slice ships behind an explicit runtime
     check (`if credits_available() / if api_key_set() / if budget_remaining() > threshold:`);
     when the gate is closed, the response MUST fall back to the deterministic answer or an honest
     "retrieval-only" degradation — never a silent error or a bare passthrough to a dumber layer.

  **Resolver grounding contract (complementary rule):** a deterministic resolver that specializes
  on a named entity (function name, column, key, symbol) must CONFIRM the entity is actually
  present in the source artifact before constructing its answer. If the entity is absent, the
  resolver must decline (return `None` / passthrough) so the next resolver in the pipeline can
  handle it — never fabricate an answer or return a "not found" for an entity that might simply
  be handled by a different resolver. A confident wrong answer ("I couldn't find symbol X" when
  X was never looked up correctly) is more damaging than a silent decline. Sibling to GAP-018
  (which covers the LLM-unavailability fallback case). **How to check:** for each feature with
  a deterministic + LLM path: (1) disable the LLM layer and assert the deterministic tier returns
  a correct, grounded answer; (2) assert the LLM tier is gated on an explicit budget/key check
  that fails closed (returns deterministic answer or honest decline, never a bare error); (3) for
  each named-entity resolver, supply a fixture where the entity is ABSENT and assert the resolver
  declines (returns None / passthrough) rather than answering falsely. **P1** (an LLM-only feature
  that goes dark silently is a reliability regression indistinguishable from a bug; a resolver
  that fabricates answers for absent entities erodes trust permanently).
- **GAP-047 — A skill (framework command) ships without a machine-readable contract:
  no frontmatter `description` and/or no per-skill eval.** A skill is the highest-privilege
  prompt-injection surface in the system AND an untested code path — prose that any refactor
  can silently gut with no gate to catch it. Adapted from github.com/NVIDIA/skills (declared
  per-skill metadata + a gate that enforces it — NOT the GPU/domain skills). Two sub-checks,
  both baseline-widened + shrink-only (see docs/SKILL_SCHEMA.md): (1) every `commands/*.md`
  declares a YAML `description`; (2) every skill has `commands/evals/<skill>.json` with >=1
  contract case asserting its essential invariant strings (`must_contain`/`must_not_contain`,
  STATIC text checks — never an LLM run, $0/offline). **How to check:** run
  `python3 scripts/skill_gate.py` (wired into `tests/run.sh`, the CI gate) — a skill not in the
  baseline that lacks a description or an eval FAILS; a present eval whose `must_contain` no
  longer matches the skill FAILS (rot-proof); a baselined skill that became compliant FAILS
  until removed from the baseline (forward-only ratchet). **How to fix:** add the missing
  frontmatter/eval and delete the skill's key from the relevant baseline. **P1** ("skill has no
  eval = gap" — an untested, unversioned instruction surface that steers autonomous agents is a
  standing reliability + supply-chain risk; close it one skill per tick until both baselines are
  empty).
- **GAP-048 — An intent resolver's trigger vocabulary is incomplete: synonymous or equivalent
  user phrasings silently route to passthrough instead of the matching resolver** (applies to
  any project with a natural-language router, intent classifier, keyword-triggered handler, or
  command dispatcher) — when implementing a resolver for a concept (e.g. "imports", "routes",
  "recursive functions"), the initial trigger vocabulary typically captures only the terms used
  during design-time testing. Users naturally express the same request with many synonymous words
  ("import" → "library" / "package" / "module" / "dependency"; "route" → "endpoint" / "path" /
  "handler" / "URL"; "recursive" → "calls itself" / "self-referential"). When a user's phrasing
  doesn't match any trigger, the resolver silently returns None / passthrough — the user gets "no
  answer available" for a question the system is supposed to handle, which is functionally the same
  as a wrong answer (and often harder to diagnose, because no error is raised). *(Concrete class: a
  code-Q&A resolver for "imports" only triggered on ["import", "dependency", "include"]; a user
  asking "what libraries/packages/modules does it use" fell through to passthrough — fixed by
  widening the trigger vocabulary to include all natural synonyms and adding a parametrized test
  that drives one phrasing from each synonym group through the resolver. Standing meta-lesson
  across 27+ code-Q&A resolver slices: "every resolver has a TWIN — enumerate and guard the full
  family.")*

  **The two rules of trigger-vocabulary completeness:**
  1. **Enumerate the full synonym family.** For each concept a resolver handles, identify every
     natural-language synonym group (nouns, verbs, adjectives) that describe that concept. The
     trigger vocabulary must include at least one representative term from each group. Brainstorm
     3–5 alternative phrasings a new user might type; if any cause passthrough, add them.
  2. **Guard the family with a parametrized test.** Add a test that drives one representative
     phrasing from EACH synonym group through the resolver and asserts it reaches the correct
     handler (not passthrough, not a catch-all). When a new synonym is added to the trigger list,
     add a corresponding test case in the same commit.

  **How to check:** for every intent resolver/classifier branch, list its current trigger keywords.
  Brainstorm synonymous phrasings for the concept. Drive each through the system and confirm it
  resolves correctly, not passthrough. A synonym that reaches passthrough is a P1 bug — it is
  silent failure for a supported question. Non-vacuous bar: disable the target resolver; all its
  parametrized family tests must fail (confirming they are actually testing the right handler, not
  a fallback). **P1** (a resolver that handles "import" but not "library" silently fails for a
  large share of real user queries; the user sees "no answer" with no indication the feature
  exists). Sourced from context-forge code-QA campaign meta-lesson across slices 10–27
  (2026-08-13 – 2026-08-16).

- **GAP-049 — Sibling superlative resolvers for the same entity type but different dimensions must
  each bind their trigger to a dimension-discriminating keyword; the superlative modifier alone is
  insufficient and causes one resolver to silently steal another's queries** (applies to any NL
  router / intent classifier with multiple "extreme value" handlers — most/fewest/longest/shortest/
  highest/lowest — over different dimensions of the same entity type, such as functions-by-call-
  frequency vs functions-by-line-count vs functions-by-parameter-count) — when a user asks "which
  function is called the most", the phrasing contains a superlative ("most"); if a sibling resolver
  for "longest function by lines" also fires on any query containing "most", it steals the query and
  returns a confidently-wrong answer. The superlative modifier alone does not discriminate between
  dimensions. *(Concrete class: a code-Q&A resolver for "most-called function" added alongside
  existing resolvers for "longest function" (line-span) and "most-parameter function" — without
  binding to a call-frequency verb, any "most X function" query would be grabbed by whichever
  resolver matched first. Fixed by requiring at least one word from a CALL/USE vocabulary set —
  `called|invoked|used|referenced` — before the resolver fires; without a dimension word the
  resolver returns None and defers. Sourced from context-forge code-QA slice-26,
  2026-08-16.)*

  **The two rules of sibling-superlative dimension fencing:**
  1. **Dimension-verb requirement.** Each superlative resolver must check for both the superlative
     modifier (most/fewest/longest/shortest) AND at least one dimension-discriminating keyword
     specific to its domain (e.g., "called/invoked/used/referenced" for call-frequency;
     "lines/length/span/body" for line-count; "parameters/arguments/args" for parameter-count).
     If the dimension keyword is absent, the resolver MUST return None/passthrough regardless of
     the superlative. Document the resolver's dimension vocabulary alongside its trigger regex.
  2. **No-steal guard.** When adding a new superlative-dimension resolver, cross-check the existing
     resolver set: for every sibling, construct a phrasing that uses the sibling's dimension
     keyword with the superlative modifier and assert the NEW resolver does NOT fire. Add this as
     a regression test in the same commit.

  **How to check:** enumerate all resolvers that handle a superlative modifier. For each ordered
  pair (R1, R2), take a phrasing that should route to R1 and assert R2 returns None when called
  first. A resolver that fires on a phrasing it should not own is a P1 "silent theft" bug — the
  user receives wrong data with no error raised. **P1** (wrong answer for a common phrasing;
  no exception raised; indistinguishable from correct behavior to the user).

- **GAP-050 — A resolver for a specific sub-action must dispatch before its parent-action
  resolver, explicitly exclude syntactic false positives that share the trigger token, and must
  not steal queries that belong to the parent** (applies to any NL intent router or pipeline
  where a specialised action is a proper subset of a more general one — e.g. "assignment" ⊂
  "reference/usage"; "creation" ⊂ "access"; "mutation" ⊂ "read"; "declaration" ⊂ "mention") —
  when a resolver handles a specific sub-action (e.g. writing a value to a variable) and a
  sibling resolver handles the broader parent action (e.g. any reference to a variable), the
  specific resolver will silently return wrong data if (a) it runs after the parent (the parent
  consumes the query first and returns all references, not just assignments), (b) it fires on
  syntactic occurrences of the trigger token that belong to different grammatical contexts (e.g.
  `=` appears in assignments `X = v`, comparisons `X == v`, keyword-args `f(X=v)`, and subscript
  targets `d[X] = v` — only the first is an assignment), or (c) it fires on the parent's
  phrasings ("where is X used" consumed by the assignment resolver). *(Concrete class: a
  code-Q&A resolver for "where is X assigned / how many times is X assigned" added alongside an
  existing "where is X used/referenced" resolver — without dispatch-before ordering the usages
  resolver answered "assignment" queries with every reference; without kwarg/subscript exclusion
  the assignment count was inflated; without a no-steal downward guard the resolver fired on
  "where is X used" and returned an empty result where references existed. Fixed by: dispatching
  the assignment resolver first in the chain; parsing `=` in context — tracking bracket depth to
  detect keyword-arg and subscript contexts, checking for comparison operators `==`, `<=`, `>=`,
  `!=` on either side; asserting the resolver returns None on representative parent-action
  phrasings. Sourced from context-forge code-QA slice-27,
  `feat(code-qa): slice-27 variable assignment sites (zero-LLM)`, 2026-08-16.)*

  **The three rules of sub-action resolver correctness:**
  1. **Dispatch-before-parent.** The specific sub-action resolver must appear earlier in the
     dispatch chain than the parent-action resolver. Document the ordering constraint in a
     comment at each resolver site. If the chain is reordered, both resolvers must still have
     test coverage so the breakage is immediately visible.
  2. **False-positive exclusion.** When a syntactic token also appears in unrelated grammatical
     contexts, the resolver must detect and reject those contexts before claiming a match. Use
     context-sensitive parsing (track bracket/paren depth, check adjacent characters, inspect the
     left-hand side of the token) rather than a plain token search. Enumerate the excluded
     contexts explicitly in comments and cover each with a test asserting the resolver returns
     None for that context.
  3. **No-steal downward guard.** Assert that the specific resolver returns None on representative
     phrasings that belong to the parent action. Commit this test in the same PR as the resolver.
     A resolver that fires on its parent's queries causes silent wrong answers: the parent is
     never reached and the query is answered with a narrower result than the user asked for.

  **How to check:** for each resolver, identify whether its action is a sub-type of another
  resolver's action. If yes: (a) confirm it appears before the parent in the dispatch chain;
  (b) enumerate every grammatical context where the trigger token appears outside the
  sub-action and add a test asserting each returns None; (c) take 3–5 phrasings natural for
  the parent action and assert the specific resolver returns None for all of them. A specific
  resolver that fires on a parent phrasing is a P1 "silent impersonation" bug — the user
  receives a subset result without any error or indication that a broader answer exists.
  **P1** (the user asking "where is X used" receives only assignment sites with no warning;
  the parent resolver is never reached; the bug is invisible unless the user cross-checks
  manually).
