---
description: Continuous product-QA orchestrator — drives real end-user flows (upload, query, click) to find bugs and UX regressions even when no code changed.
---
# /qa — Product QA Orchestrator

Runs a continuous product testing pipeline. Works even with no code changes — finds bugs
by USING the product like a real user. Spawns parallel sub-agents, collects findings,
synthesizes a prioritized report.

## Usage
```
/qa                    # full run (all sub-agents)
/qa --api-only         # API + tests only
/qa --web-only         # web UI only
/qa --smoke            # quick smoke test (< 2 min)
/qa --skip-browser     # skip Playwright tests
```

## Setup (first run)
```bash
source .ccf/project.env 2>/dev/null || true
echo "QA target: ${CCF_LIVE_URL:-local} category: ${CCF_PROJECT_CATEGORY:-app}"
```

## Configuration gating

QA adapts to `.ccf/project.env` — a missing prerequisite means the sub-agent reports
**SKIP, never FAIL**:

- **No `CCF_LIVE_URL`** → sub-agents A (API), B (Web UI), and D (Browser) run against a
  local server only if one is detectably running; otherwise SKIP.
- **No `CCF_TEST_CMD`** → sub-agent C becomes a **static-review agent**: run
  `/critical-checks` on recently changed files and review the diff against GOAL.md
  acceptance criteria.
- **`CCF_PROJECT_CATEGORY=idea`** → content/doc QA mode: skip A/B/D entirely; sub-agents
  instead check document structure (GOAL.md parses, referenced files exist), internal
  link validity, terminology/voice consistency across documents, and whether each
  GOAL.md item marked done is actually reflected in the docs.

## Severity
- **P0 — Broken**: blocks real users. Triggers /self-improve + GOAL.md Blockers.
- **P1 — Degraded**: wrong results, slow, misleading. Triggers /self-improve.
- **P2 — Friction**: confusing, ugly, missing feedback. Logged, reviewed next cycle.
- **P3 — Improvement**: ideas. Logged only.

---

## Orchestrator Instructions

### Step 1 — Pre-flight

```bash
source .ccf/project.env 2>/dev/null || true
BASE="${CCF_LIVE_URL:-http://localhost:8000}"
curl -sf "$BASE${CCF_HEALTH_PATH:-/health}" --max-time 8 \
  -w "\nHTTP:%{http_code}" 2>/dev/null | tail -2 || echo "CRITICAL: unreachable"
```

If non-200: report P0 immediately, still run local tests.

### Step 2 — Spawn sub-agents in parallel

Spawn all agents simultaneously.

**Sub-agent A — Core API flows:**
- Health endpoint returns expected structure
- Primary CRUD flows work end-to-end
- Auth rejection on invalid credentials (401, not 500)
- Error responses are clean (no raw tracebacks)
- Response headers include request ID

**Sub-agent B — Web UI (if applicable):**
- Landing page / home loads (200, not blank)
- Primary user flow works (signup/onboarding/main feature)
- Mobile viewport (375px) — nothing overflows or breaks
- Error states show friendly messages, not stack traces
- Forms: validation works, submit gives feedback
- **Theme toggle state machine completeness**: if the page ships a dark/light mode
  toggle, assert BOTH state icons (e.g. moon AND sun) are present in the served HTML.
  The swap is JS-driven, so a missing dark-mode icon silently renders an empty toggle
  after one click yet passes every at-rest check. Only a test asserting both icon paths
  are present catches this failure mode at the static-render layer.
- **Channel-appropriate error messaging**: error messages must propose remedies
  available in the current channel. A web UI error must not tell the user to "use the
  CLI" or "use the API" — those channels are unavailable to someone uploading a file in
  a browser tab. Check the top 3 user-visible error states and verify each remedy is
  actionable from the channel that surfaces it.

**Sub-agent C — Run test suite (or static review when no tests configured):**
```bash
source .ccf/project.env 2>/dev/null || true
if [ -n "${CCF_TEST_CMD:-}" ]; then
  eval "$CCF_TEST_CMD" 2>&1 | tail -20
else
  echo "no test command — switching to static review mode"
fi
```
When `CCF_TEST_CMD` is empty, this agent instead runs `/critical-checks` on files
changed in the last 5 commits and reviews them against GOAL.md acceptance criteria.

**Sub-agent D — Browser (if CCF_LIVE_URL set):**

If the project ships an optional browser-QA hook, run it; otherwise SKIP.
```bash
source .ccf/project.env 2>/dev/null || true
if [ -n "${CCF_LIVE_URL:-}" ] && [ -x ".ccf/hooks/qa-browser.sh" ]; then
  CCF_LIVE_URL="$CCF_LIVE_URL" .ccf/hooks/qa-browser.sh 2>&1 | tail -20
else
  echo "browser QA: no .ccf/hooks/qa-browser.sh — SKIP"
fi
```
The hook is project-supplied (e.g. a headless browser script that loads
`$CCF_LIVE_URL` and checks the primary UI). With no hook and no live URL, this
sub-agent reports SKIP.

**Auditable browser QA artifacts — what a well-behaved hook MUST produce:**

Every browser/visual QA run must generate persisted artifacts so "PASS" is
distinguishable from "nothing ran" (GAP-011 principle):

1. **Timestamped markdown report** — write `reports/qa/browser-<UTC-ts>.md` with
   the run verdict (PASS / FAIL / DEGRADED), per-site or per-surface notes, and
   paths to any screenshots taken. A missing report after the hook exits = SKIP, not PASS.
2. **Video recording (Playwright)** — if the hook uses Playwright, enable
   `record_video_dir` (e.g. `reports/qa/videos/<ts>/`). Videos are gitignored
   (large binaries); the markdown report is committed (lightweight evidence).
3. **Vision delta (if applicable)** — for UI projects with `CCF_LIVE_URL`,
   archive per-surface screenshots under `reports/qa/deploy-shots/<build-sha>/`
   and pixel-diff vs the previous build. A sustained NO-VISIBLE-CHANGE streak
   across N deploys is a RED finding even when all other gates are green (GAP-021).

Recommended `.gitignore` additions for any project using browser QA:
```
reports/qa/videos/
reports/qa/deploy-shots/
```
These dirs are gitignored (heavy binaries); the tracked `reports/qa/*.md` reports
are the durable, auditable record of every QA run.

**Concurrent QA run isolation — per-process staging directories:**

When QA harnesses stage fixture files to a shared temp directory AND call cleanup
(`shutil.rmtree`), concurrent runs race: one process's cleanup removes another's
files mid-upload, causing `[Errno 2] No such file or directory` crashes.

Fix: scope the staging path to the process ID so each process owns and cleans up
only its own staging area:
```python
import os, tempfile, pathlib
STAGING = pathlib.Path(tempfile.gettempdir()) / f"qa_fixtures_{os.getpid()}"
```
`cleanup()` calls `shutil.rmtree(STAGING, ignore_errors=True)` — this is now
safe under concurrent runs because each process's dir is unique. Add a regression
that runs two fixture-staging instances concurrently and verifies neither crashes.

**Browser QA: startup sweep for crash-orphaned temp entries:**

QA harnesses that create temp dirs per-run (browser profiles, fixture-staging dirs, upload
temps) accumulate orphaned entries when a run is killed or crashes before its `finally`
cleanup executes — silently growing without bound. Fix: at harness startup, sweep the temp
dir for the harness's own filename namespace (prefixes the harness owns) and remove entries
older than `max_age_seconds` (default 1 hour). The age gate ensures a concurrent run's
recently created dirs are never touched.

```python
import shutil, tempfile, time
from pathlib import Path

def _sweep_stale_temp(prefixes, max_age_seconds=3600):
    """Remove crash-orphaned temp entries matching prefixes, older than max_age. Returns count."""
    tmpdir = Path(tempfile.gettempdir())
    now = time.time()
    removed = 0
    for prefix in prefixes:
        for p in tmpdir.glob(f"{prefix}*"):
            try:
                if now - p.stat().st_mtime < max_age_seconds:
                    continue  # recent — could be a concurrent run, don't touch
                shutil.rmtree(p, ignore_errors=True) if p.is_dir() else p.unlink(missing_ok=True)
                removed += 1
            except Exception:
                pass  # best-effort; never fail the QA run on cleanup
    return removed

# Call at harness startup, before any test runs:
swept = _sweep_stale_temp(["qa_profile_", "qa_staging_", "qa_adv_"])
if swept:
    print(f"  [qa] swept {swept} stale temp entries")
```

Additionally, track this run's temp dirs in a module-level list and clean them in
`run()`'s `finally` block — this ensures cleanup on both clean exit and
signal-caught termination (not just normal return).

**AI-graded QA robustness — when a vision or assertion API is metered:**

When Sub-agent D uses an AI/vision API to grade screenshots or validate responses:

- **Latch on first credit/auth failure** — after the first `credit balance too low`,
  billing error, or authentication failure, set a run-level "unavailable" flag and
  short-circuit all subsequent API calls. Do NOT call the dead endpoint per
  screenshot/assertion — it produces N identical errors and floods the report with
  what looks like product failures. Emit one honest `[DEGRADED] vision unavailable —
  grade via agent vision` line in the report.
- **Transient vs. fatal** — network timeouts and 5xx server errors are transient
  (skip and continue); credit exhaustion and auth failure are fatal for the run (latch).
- **Fall back to non-AI path** — if the first AI call fails fatally, pivot to the
  project's non-AI test path (direct HTTP assertions, structural checks) so the run
  produces real coverage. Report honestly: `[DEGRADED] vision unavailable, N structural
  tests ran` rather than treating the whole run as SKIP.
- **Never mark the run FAIL** solely because vision was unavailable — it's DEGRADED
  if structural assertions pass.
- **Gate live-AI-graded tests behind an explicit opt-in env var** — any test that sends
  live traffic to an AI/LLM API to grade results (vision grading, NL response correctness
  checks, ask-box QA) is inherently non-deterministic: the model can return a
  different-but-valid answer on each run, producing recurring false-positive P1s in CI.
  Gate the entire AI-graded test tier behind an explicit env var (e.g.
  `CCF_RUN_LIVE_AI_E2E=1`) and exclude it from the default suite and CI. Only
  deterministic structural checks (response shape, non-empty, status code) run by default.
  Running the AI-graded tier is a deliberate manual action, not a CI default. *(Practical:
  `pytest -m 'not live_ai'` as the CI gate; `CCF_RUN_LIVE_AI_E2E=1 pytest -m live_ai` as
  the opt-in tier.)* This is distinct from the "latch on credit failure" rule above — that
  handles unexpected API outages; this handles the inherent non-determinism of correct AI
  responses making hard assertions flaky.
- **Shared-credential exhaustion — classify as environmental, not P1, and mint an
  ephemeral credential per run:** when QA uses a shared demo/test credential (API key,
  auth token) that other runs or users also consume, a heavy session can exhaust its
  quota — the product correctly returns a rate-limit/quota response that the test
  harness then misclassifies as a wrong-answer P1 (false positive). Two complementary
  fixes: (1) *classify*: centralize rate-limit/quota string detection in a single
  helper (e.g. `_is_rate_limit(text)`) and apply it at EVERY check site — both the
  answer panel and the upload/error panel — so a quota response is logged as an
  environmental cap and skipped, never raised as a P1; (2) *root-cause fix*: at QA
  run startup, mint a fresh ephemeral credential via the product's own signup/
  registration endpoint; inject it into the test session (e.g. localStorage seed,
  session header, or env override); each run draws from its own fresh quota instead
  of the shared one. Fall back gracefully to the shared credential if minting fails,
  so QA always runs. Without (1), quota exhaustion pollutes the signal and can block
  the improve loop; without (2), (1) is a symptom-mask that accumulates DEGRADED runs.
- **All probes inconclusive → DEGRADED, never PASS** — when a feature-presence check
  loops over N sites, providers, or environments, track two counters separately:
  `_confirmed` (probe returned a positive assertion — feature detected) and
  `_inconclusive` (probe mechanism itself couldn't run — JS disabled, grader dead,
  driver mode unsupported). After the loop: if `_confirmed == 0 and _inconclusive > 0`,
  the check verified NOTHING — emit a P1 finding ("feature UNVERIFIED on all providers")
  and degrade the run. Do NOT conflate "couldn't run the probe" with "probe ran and
  passed." A probe that definitively confirms absence (element not in DOM, endpoint 404)
  is a legitimate finding, not inconclusive. (GAP-035) **Guard the gate (GAP-036):**
  once you implement this counter logic, add a unit test that mocks ALL probe paths to
  return inconclusive and asserts `status == "DEGRADED"` with an "UNVERIFIED" finding.
  Without this test, a refactor that silently removes the honesty gate passes CI and the
  false-PASS blind spot returns invisibly.

**QA assertion discipline — assert deterministic capabilities only:**

Systems that route some inputs to a local deterministic resolver and others to an
AI/LLM backend produce two classes of output:
- **Deterministic** — local rule-based resolver; same answer every run.
- **AI-deferred** — sent to an LLM; answer varies by model/context/temperature.

Write hard pass/fail assertions only for the **deterministic** class. Asserting the
exact content of an AI-deferred response produces recurring false-positive P1s whenever
the model gives a different (equally valid) answer. To identify the boundary: if
removing the AI/LLM would make the answer undefined → the assertion must be soft
(check non-empty or structural shape, not exact content).

Red flag: a P1 that fires identically every QA run against an AI-backed feature → the
assertion is treating an AI-deferred response as if it were deterministic output.

**Browser QA: vision-found bug → companion text lint to lock in the fix permanently:**

When a browser QA pass uses a vision grader to assess interaction results, also extract
the raw output text and run a targeted text lint for any artifact class that the vision
grader previously found by eye. The lint is a *companion* to vision, not a fallback:
both run on each pass.

Why two guards? Vision catches novel quality issues; the text lint keeps already-found
artifact classes fixed permanently — independent of vision availability, cost, or API
quota. Removing either makes the other weaker, not equivalent.

Adoption pattern — each time vision finds a class of output corruption:
1. Characterize the artifact as a string-detectable pattern (a literal token, a regex,
   a forbidden prefix/suffix in the extracted text).
2. After fixing the root cause, add a deterministic text lint alongside the vision call:
   ```python
   # Extract the result text from the page before grading
   out_txt = page.locator("#result-output").inner_text() \
       if page.locator("#result-output").count() else ""
   # Companion lint — runs regardless of vision grader availability or cost
   if "<artifact-pattern>" in out_txt:
       result.add("P1", "output contains <artifact class> corruption",
                  "vision-found regression: <artifact> leaked into output")
   else:
       result.notes.append("no <artifact class> leak ✓")
   # Vision grade is still run separately — both checks happen every pass
   if use_vision:
       verdict = grade_screenshot(screenshot, ...)
   ```
3. The lint must be **non-vacuous**: confirm the output text was actually extracted
   (non-empty or from a known-populated surface) before calling the check clean.
   A lint that reads empty text and reports "no artifact" has passed vacuously.
4. Do not remove lint entries once added — each entry represents a known regression
   surface. Accumulate them as a permanent artifact-class registry.

Common text-lint targets by domain:
- **Text extraction** — encoding artifacts (e.g. unmapped glyph tokens), truncation
  markers left in output, blank runs that claim a large size reduction
- **Template engines** — unfilled `{{placeholder}}` tokens, raw `undefined` strings
- **AI output** — debug `[INST]`/`<|system|>` leakthrough, JSON bleed-through in prose
- **UI rendering** — internal ID tokens that should have been resolved to display names,
  literal `null` or `None` in rendered copy

(Sourced from context-forge fix(pdf)/iter456, 2026-08-12.)

**Browser QA: adversarial surface parity registry — every input surface must be covered or deferred with a reason:**

When a QA dimension exercises hostile inputs on some surfaces but not others, new credential-
or input-bearing surfaces that are added to the product quietly fall outside the adversarial
dimension — no test fails, no gap is visible, the surface simply accumulates unchecked. The
fix: maintain an explicit registry as the **single owner** of "which surfaces the adversarial
dimension must cover." A parity guard test enforces both directions so the registry stays live.

```python
# Every surface that accepts user- or credential-supplied input and must survive a hostile
# payload (XSS / SQL injection / oversize / lone surrogate): a graceful invalid-input notice,
# no executed script, no backend leak.
# value = (path, probe-function-name). The named function must exist in this module so
# "covered" means "actually driven", not just "listed".
_ADVERSARIAL_SURFACES = [
    ("/app",        "_app_adversarial_query"),    # hostile smart_query / file upload
    ("/login",      "_login_hostile_credential"), # hostile login credential
    ("/admin",      "_admin_hostile_credential"), # hostile admin key
    ("/settings",   "_settings_adversarial"),    # hostile config/preference values
    ("/api/upload", "_upload_hostile_filename"), # traversal filename + binary body
]

# Surfaces whose probe is not yet implemented, each with a MANDATORY reason.
# This keeps the gap VISIBLE (the parity guard lists it every run) instead of letting it
# vanish from a dimension nobody is watching. Remove the entry the moment the probe lands.
_ADVERSARIAL_PENDING = {
    "/authorize": "needs a registered OAuth client + valid PKCE flow to reach the "
                  "key-validation branch — raw GET/POST fails prerequisites first; "
                  "probe deferred until client-registration setup lands.",
}
```

Parity guard test (run in CI as part of the harness's own test suite):
```python
def test_adversarial_surface_parity():
    import <your_qa_module> as qa
    defined = {name for name in dir(qa) if callable(getattr(qa, name))}
    for path, probe_fn in qa._ADVERSARIAL_SURFACES:
        if path in qa._ADVERSARIAL_PENDING:
            continue   # deferred with a reason — allowed
        assert probe_fn in defined, (
            f"Surface {path!r} lists probe {probe_fn!r} but that function "
            f"is not defined in the QA module — add the probe or move to _ADVERSARIAL_PENDING"
        )
    # Reverse: every pending entry must still be in _ADVERSARIAL_SURFACES
    surface_paths = {p for p, _ in qa._ADVERSARIAL_SURFACES}
    for path in qa._ADVERSARIAL_PENDING:
        assert path in surface_paths, (
            f"_ADVERSARIAL_PENDING entry {path!r} has no matching entry in "
            f"_ADVERSARIAL_SURFACES — add the surface or remove the pending entry"
        )
```

When adding a new form-bearing surface to the product: add it to `_ADVERSARIAL_SURFACES` in
the same commit as the feature; if the probe isn't ready yet, add it to `_ADVERSARIAL_PENDING`
with a reason. The parity test catches a new entry with neither — a new surface cannot ship
outside the dimension unnoticed. (Sourced from context-forge adversarial QA pattern, 2026-07-31.)

**Scrutinize deferral reasons before accepting them.** A reason like "needs a registered
OAuth client to reach the POST key-validation branch" can be technically correct while
missing a distinct attack surface that is reachable without those prerequisites — e.g.
values reflected into page attributes at GET-render time can be tested with a bare
parameterized GET, regardless of whether the POST flow requires client registration.
When implementing a probe for a neighboring surface, re-read every `_ADVERSARIAL_PENDING`
entry and ask: "is there a simpler request path that exposes the same risk?" An overly
conservative deferral reason can defer the wrong thing.

(Sourced from context-forge test(qa) iter411, 2026-08-08.)

**Browser QA: adversarial input timeout — distinguish "no-answer" (graceful) from "UI hung" (real failure):**

A hostile or unanswerable input (SQL injection probe, prompt-injection string, junk)
that a system correctly handles produces NO direct-answer UI element — and may take
several seconds to resolve server-side. A harness that waits for the answer card and
flags the timeout as P2 "crashed the UI" produces recurring false positives: a
graceful no-result is being misread as a crash. This is the inverse of the false-PASS
anti-pattern: a "not visible within timeout" must not be read as "crashed", just as
a "couldn't verify" must not be read as "verified OK" (GAP-032).

Fix: on timeout waiting for the result element, probe the input element's
responsiveness:
- **Still editable** → the system is alive and gracefully produced no answer. Log a
  note (e.g. `"adversarial '{q}' → no direct-answer (graceful, page responsive)"`);
  do not raise a finding.
- **Not editable / unresponsive** → the UI is genuinely hung. Raise P2 "hung the UI"
  with the query as evidence.

Other guards remain unchanged — content leak (P1), blank card visible but empty (P2),
executed XSS (P0) — only the timeout-no-card case changes.

**Browser QA: flaky interactive element — scroll + explicit short timeout + single retry:**

A browser QA harness that clicks a submit button with the driver's 30s default timeout
converts transient non-actionable elements (prior result panel still settling, DOM
transition in progress) into 30s hangs followed by false failure findings. This
flakiness rotates across different tests as different preceding actions leave different
elements settling (GAP-033).

Fix pattern for any form-submit or action button:
```python
btn = page.locator("#submit-btn")  # or equivalent
try:
    btn.scroll_into_view_if_needed(timeout=3_000)
except Exception:
    pass  # scroll failure is non-fatal; proceed to click
try:
    btn.click(timeout=8_000)
except Exception:
    page.wait_for_timeout(800)  # let the preceding result panel settle
    btn.click(timeout=8_000)    # retry once; still-stuck is now an honest finding
```

A button still unresponsive after the retry is caught by the outer handler as a
genuine finding. A transient non-actionable button no longer hangs 30s or fires a
false-positive P2.

**Browser QA: async-loaded components — poll until settled before capturing screenshot:**

When a page section contains components loaded asynchronously after the initial render
(data cards, insight panels, feed items, widgets populated by secondary API calls),
capturing a screenshot before they settle produces a **false-PASS on vision grading**:
skeleton/loading-bar UI is visually indistinguishable from real content in the captured
image, so a genuinely hung or empty component passes every vision check silently. This
is the same false-PASS class as GAP-044, applied to screenshot timing rather than probe
aggregation.

Fix: for any section that may contain async sub-components, check for visible loading
indicators before capturing the screenshot, poll until none remain, then encode a
FAILABLE programmatic assertion — vision grading alone cannot catch the hung case:

```python
# Before capturing a screenshot of a section with async sub-components:
settled = False
for _ in range(30):  # ~9 s cap (30 × 300 ms)
    if page.locator(".skeleton.visible, .loading-indicator.visible").count() == 0:
        settled = True
        break
    page.wait_for_timeout(300)
stuck = page.locator(".skeleton.visible, .loading-indicator.visible").count()
# A component visible but rendering no text after settle is a broken load.
empty = page.evaluate(
    "() => Array.from(document.querySelectorAll('.async-answer.visible'))"
    ".filter(el => !el.textContent.trim()).length"
)
if not settled or stuck:
    result.add("P1", "async components stuck loading",
               f"{stuck} loading indicator(s) never resolved after ~9s — hung backend call")
elif empty:
    result.add("P1", "async component empty after load",
               f"{empty} component(s) settled but rendered no content")
# Only capture the screenshot after settling — a skeleton in the captured image
# looks like a placeholder and passes vision checks even when the backend call hung.
```

Adjust the CSS selectors and poll cap to match your project's loading-indicator class
names. The programmatic assertion (not the vision check) catches the stuck case.

**QA harness: three-tier outcome classification for features backed by external services:**

When a feature proxies to an external service (an AI/LLM API, a metered backend, or a
third-party quota system), the QA harness must distinguish at least four outcome classes —
only the last is a real product finding:

1. **VERIFIED** — the feature answered correctly and the answer is visible.
2. **Rate-limited** — the test credential hit its per-run quota (detect with `_is_rate_limit(text)`);
   the product is working correctly. Log as UNVERIFIED (environmental cap), never P1.
3. **Transient network error** — the app rendered its OWN network-unreachable copy
   (e.g. "Could not reach the server — try again", "Offline", "request timed out").
   This is an infra blip, not a product bug. Create a `_is_transient_neterror(text)` helper
   to detect this class and skip the assertion, logging it as an environmental note.
4. **Upstream service error** — the backing external service (LLM API, provider, third-party)
   is unavailable or has exhausted its own capacity (e.g. "depleted API credits", "provider
   quota exceeded", "model overloaded", "upstream 402"). The product surfaced the error
   correctly — it is not a wrong-answer regression. Create a `_is_upstream_llm_error(text)`
   helper and log as UNVERIFIED, not P1.
5. **P2 — no answer and none of the above** — the product produced no output AND it's not
   a rate-limit, infra blip, or upstream error. This is the only real regression signal.

Group these as sibling boolean helpers (`_is_rate_limit`, `_is_transient_neterror`,
`_is_upstream_llm_error`), applied at EVERY answer-grading site. A single if/elif chain at
each check site; the "none of these" branch is where `result.add("P2", ...)` fires.

Without this classification, a network hiccup or a depleted upstream API key flips the
entire QA run to DEGRADED on a false positive — blocking the improve loop on a condition
the product handled correctly. (Sourced from context-forge GAP-061 + meta-QA dim-1, 2026-07-28.)

**Browser QA: async settle — grade by progress, not raw timeout:**

A fixed settle timeout (e.g., "30 × 300 ms ≈ 9 s") causes false P1s when QA runs on large
data: components that each re-upload the full fixture file are network-bound and can exceed
a flat cap while making genuine progress. Grading "still loading at timeout → P1"
misrepresents slow-but-working as broken.

Fix: grade the settle check by PROGRESS observed during the budget period:
- **Zero components resolved after the budget** → P1 "genuinely hung" (no backend response at all)
- **Some-but-not-all resolved** → P2 "slow to settle" (network-bound, perf issue, not broken)
- **All resolved** → PASS

Also scale the settle budget to expected work size when components re-upload data:

```python
# ~9s base + 3s per 25KB of upload, capped at 30s.
cap_iters = min(100, 30 + (file_size_bytes // (25 * 1024)) * 10)
start_count = page.locator(".loading-indicator.visible").count()
for _ in range(cap_iters):
    if page.locator(".loading-indicator.visible").count() == 0:
        break
    page.wait_for_timeout(300)
stuck = page.locator(".loading-indicator.visible").count()
if start_count and stuck < start_count:
    result.add("P2", "components slow to settle",
               f"{stuck}/{start_count} still loading after budget — network-bound on large data")
elif stuck:
    result.add("P1", "components stuck loading",
               f"{stuck} loading indicator(s) never resolved (zero progress — genuinely hung)")
```

The principle: separate "slow" from "broken". A check that re-uploads data N× must budget
for N× the network time. "Timed out" ≠ "hung"; the progress counter is the honest arbiter.
(Sourced from context-forge GAP-059, 2026-07-28.)

**Test coverage: cross-branch invariant tests for shared output classes:**

When a defect is a property of a class of output (e.g., "label format must be consistent",
"no adjacent duplicate tokens") rather than a property of a single code path, fixing it only
in the branch where it was first found leaves every other branch vulnerable to the same bug.
Each regression then gets fixed in isolation as new branches are added — the signal is two
independent fixes of the same defect in different branches.

Pattern: identify the invariant property, then write a single test that exercises ALL branches
that produce that output class and asserts the invariant:
1. Enumerate every code path that can produce the output class (e.g., every executor branch
   that formats a "N items where <label>" result string).
2. Write one parameterized test that runs each path and asserts the invariant property
   (e.g., no adjacent duplicate word, no doubled column name in the label).
3. Assert the structural invariant, not the exact output — exact-output assertions break
   on any valid rephrasing; invariant assertions survive refactors.

Red flag: the same class of defect appears in a second independent code branch a few
iterations after being "fixed" in the first. This signals a missing cross-branch invariant
test. A future branch that violates the invariant should fail CI immediately instead of
shipping to users and requiring another one-off fix.
(Sourced from context-forge meta-QA dim-4, 2026-07-28.)

**Browser QA: expand collapsed panels before interacting with their contents:**

`is_visible()` returns `True` for elements that are CSS-transformed off-canvas (e.g., a
drawer that slides in from the side, a panel hidden with `transform: translateX(-100%)` or
`display: none` after the initial render). The element has a non-zero bounding box, so the
visibility guard passes — but `.click()` then waits on actionability and hits the full 30s
default timeout every run. This produces a recurring false-negative finding ("button not
reachable") that trains reviewers to ignore the report — while the real UX (a user tapping
the toggle to reveal the panel) is perfectly functional.

Fix: before interacting with an element inside a collapsible/off-canvas panel, check for
and click the panel's toggle button first. Use a bounded timeout on the inner element click
to surface a genuinely unreachable element as a deterministic finding rather than a stall:

```python
# Open the collapsible panel first (sidebar, drawer, accordion, etc.)
panel_toggle = page.locator("#panel-toggle-btn")  # adapt selector to your project
if panel_toggle.count() and panel_toggle.is_visible():
    panel_toggle.click()
    page.wait_for_timeout(450)  # wait for open transition

# Now click the inner element with a bounded timeout
inner_btn = page.locator("#inner-element")
if inner_btn.count():
    try:
        inner_btn.click(timeout=6_000)  # bounded: never stall the whole run 30s
    except Exception:
        # Still unreachable AFTER opening the panel = a real product bug, not a harness gap.
        result.add("P2", "<feature> panel element unreachable",
                   "#inner-element not clickable after opening the panel — real UX regression")
```

**Always open the panel/drawer; never rely on `is_visible()` alone as an actionability
guarantee for elements inside animated or CSS-transformed containers.** The bounded timeout
converts a 30s stall into a 6s deterministic finding.

(Sourced from context-forge fix(qa) mobile-drawer pattern, 2026-08-05.)

**QA harness: derive expected values from the same SSOT as production code:**

A QA harness that hardcodes its own list of expected values (tier names, route paths, field
labels, configuration keys, feature flags) will inevitably drift from the production SSOT —
especially after product decisions quietly remove or rename items. The harness then flags
every run with a false positive ("missing tier X", "expected label Y not found") that turns
out to describe a correct product state. After two or three of these cycles, reviewers learn
to ignore the QA report — which is the failure mode the QA system exists to prevent.

Fix: wherever the production code has a single source of truth (an SSOT registry, a
constants module, a config file, a database table), the QA harness must import and use
that same source:

```python
# WRONG — hardcoded list that silently diverges from the production SSOT:
EXPECTED_TIERS = ["free", "pro", "enterprise", "legacy"]  # "legacy" was removed last month

# RIGHT — import the same SSOT the production code uses:
from your_app.config import TIERS_REGISTRY  # or the project's equivalent SSOT
EXPECTED_TIERS = list(TIERS_REGISTRY.keys())

# Now run your assertions against EXPECTED_TIERS.
# If the SSOT changes, both production and the harness change together — no drift.
```

**The guard test:** add a test that asserts "the harness list equals the SSOT list" and
make it non-vacuous by confirming the test would fail if you added a spurious entry to
either side. A guard that only reads one list and never cross-checks it can vacuously pass.

**Scope this rule to structural/enumerable SSOTs** (tiers, route paths, supported formats,
field names). Do not apply it to content values (button copy, success messages) where the
harness should be an independent check, not a mirror of the implementation.

(Sourced from context-forge fix(qa) SSOT-funnel-owner pattern, 2026-08-05.)

**Non-vacuous guard test bar — verify every new guard test fails against unfixed code:**

A test added as a regression guard is only trustworthy if it demonstrably catches the bug
it guards against. A test that passes both before and after the fix is vacuous — it guards
nothing and can survive silently while the bug reappears.

**The discipline:** before merging any new guard or regression test, remove the fix (or
revert to the un-fixed state) and confirm the test fails. Re-apply the fix; confirm the
test passes. Only then is the test accepted. Commit notation: *"fails vs HEAD — non-vacuous"*
records that this was done.

Common failure modes that produce vacuous tests:
- Test asserts `"feature" in html` — passes whether the value is hardcoded fiction or live data.
- Test checks structure (element exists) without asserting the value derives from a real source.
- Test was written against the already-fixed code and never ran against the broken state.

Tests that assert the *absence* of an incorrect value (`assert "raw_error_string" not in
html`) must also be checked against unfixed code — confirm the bad string IS present before
the fix, absent after.

**How to apply:** after writing the test, run it against the un-fixed state (revert to
parent commit temporarily if needed). Confirm failure. Apply the fix; confirm the test
passes. This check takes under a minute and prevents the worst class of false-green tests.

(Sourced from context-forge UX-campaign practice, 2026-08-09.)

**Browser QA: use `text_content()` to verify DOM presence before asserting visibility:**

Playwright's `inner_text()` returns an empty string for nodes that are CSS-hidden (inside a
collapsed `<details>`, a `display:none` element, or any container that hides its descendants).
If a product redesign deliberately hides a value at rest (e.g., demoting a sensitive result
into an "Advanced" disclosure), a check using `inner_text()` always reads empty and raises a
false P1 "value not rendered" even though the value is correctly in the DOM.

Fix — **two-step verification pattern:**
1. Read `text_content()` (visibility-insensitive, reads the raw DOM regardless of CSS state)
   to confirm the value is present in the DOM at all.
2. Open the parent disclosure/panel, then assert `is_visible()` to prove the value is
   reachable by the user.

This catches "absent from DOM" (step 1 fails) and "in DOM but unreachable" (step 2 fails)
as distinct, separately diagnosed findings.

```python
# Step 1: confirm the value exists in the DOM (visibility-insensitive)
el = page.locator("#value-display")
displayed_text = ""
if el.count():
    displayed_text = (el.first.text_content() or "").strip()  # NOT inner_text()

if displayed_text:
    # Step 2: open the parent disclosure and assert the element becomes visible
    reachable = False
    try:
        # If nested inside a <details>, open it first
        details = el.locator("xpath=ancestor::details[1]")
        if details.count() and details.first.get_attribute("open") is None:
            summary = details.first.locator("summary")
            if summary.count():
                summary.first.click()
                page.wait_for_timeout(300)
        reachable = bool(el.first.is_visible())
    except Exception:
        pass
    if not reachable:
        result.add("P1", "value rendered but not reachable",
                   "#value-display holds a value but stays hidden after opening its container")
else:
    result.add("P1", "value absent from DOM",
               "#value-display text_content empty — value was never rendered")
```

**The invariant:** `text_content()` answers "is it there?"; `is_visible()` after opening
the container answers "can the user reach it?". Never use `inner_text()` as a DOM-presence
check — it silently conflates "absent from DOM" with "hidden by CSS".

(Sourced from context-forge test(qa) iter408, 2026-08-08.)

**QA classifier copy-drift tripwire — tune error-text matchers to actual production copy:**

A QA harness that classifies rate-limit or error responses via text matching (e.g.
`"rate limit" in text`, `"429" in text`) silently goes blind when the production error
copy uses different phrasing than the matcher expects. The env-skip added to prevent false
positives becomes dead code, and the false positive returns on every heavy run.

Two complementary fixes:

1. **Tune to actual production copy.** When adding a new error source (a new rate-limit
   path, a new error endpoint), read its actual message from the production source file and
   add the specific stem that appears in it. Generic guesses ("rate limit", "too many") may
   not match any real copy the product ships.

2. **Copy-drift tripwire test.** A guard test reads the live production source and asserts
   the matched stem still appears in it. If production copy is ever reworded, the guard reds
   and the classifier is re-pointed — rather than silently going blind while the env-skip
   becomes dead code again.

```python
# Read the live production source to build the tripwire.
# Path must point to the file that owns the error copy being classified.
_RATE_LIMITER_SRC = (
    Path(__file__).parent.parent / "your_app" / "rate_limiter.py"
).read_text()

def _is_rate_limit(text: str) -> bool:
    """True when text matches a known rate-limit or quota-exhausted copy."""
    t = (text or "").lower()
    return (
        "rate limit" in t
        or "too many requests" in t
        or "your network is sending" in t  # tune to your app's actual copy
    )

class TestRateLimitClassifier:
    @pytest.mark.parametrize("copy", [
        # List the actual production error strings; update when the copy changes.
        "Your network is sending requests too fast — try again in a moment.",
        "Daily request cap reached (500/day). Upgrade for more.",
    ])
    def test_known_rate_limit_copies_are_classified(self, copy):
        assert _is_rate_limit(copy), f"{copy[:40]!r} must classify as a rate-limit env cap"

    def test_live_source_still_carries_matched_stem(self):
        """Copy-drift tripwire: if production rewrites the error message, this test reds
        and the classifier is re-pointed rather than silently going blind."""
        assert "your network is sending" in _RATE_LIMITER_SRC.lower(), (
            "rate_limiter.py copy drifted — update _is_rate_limit to the new stem"
        )
```

**Why this recurs:** env-skip logic is only exercised during heavy runs that actually trigger
the rate limit. In lighter runs it's dead code with no CI signal. The copy-drift tripwire
runs on every CI invocation and catches divergence before a heavy run re-surfaces the false
positive. Generic matchers that "should" cover the copy are not a substitute — read the
actual production copy and derive the stem from it.

(Sourced from context-forge test(qa) iter409, 2026-08-08.)

### Step 3 — Synthesize

1. Deduplicate findings across agents.
2. Sort: P0 → P1 → P2 → P3.
3. Note which agent caught each finding.

### Step 4 — Log + escalate

Append to session log:
```
**QA [YYYY-MM-DD HH:MM]** — [PASS|FAIL|DEGRADED] | [X passed, Y failed] | P0:[n] P1:[n] P2:[n] P3:[n]
```

- Any **P0**: add to `## Blockers` in `GOAL.md`, invoke `/self-improve`.
- Any **P1** (no P0): invoke `/self-improve` with P1 context.
- Only P2/P3: log only.

### Step 5 — Output

```
QA COMPLETE — [PASS | FAIL | DEGRADED]
─────────────────────────────────────────
Sub-agent A (API):          PASS | FAIL | DEGRADED
Sub-agent B (Web UI):       PASS | FAIL | DEGRADED | SKIP
Sub-agent C (Tests):        PASS | FAIL | DEGRADED
Sub-agent D (Browser):      PASS | FAIL | DEGRADED | SKIP

FINDINGS:
[P0] ...
[P1] ...
```

---

**Data-gated surface seeding — register seeded-state renders so conditionally-hidden UI is exercised in QA:**

A UI component that only renders when certain data exists (gated by storage, an API response
field, an auth flag, or a runtime toggle) is invisible to QA when the render pipeline produces
only the plain/default render. In the default/empty state the component is absent; source-grep
guards pass vacuously; visual regressions go undetected.

**Fix:** for each data-gated surface, add a seeded render alongside the plain render:
1. Identify the runtime signal that gates the surface (a storage key, a session variable,
   an API field, a feature flag).
2. Mock or inject that signal into the render call (seed the storage, provide a mock API
   response, set the flag).
3. Confirm the surface appears in the seeded output and add source/vision assertions
   against the seeded artifact (not the plain one).
4. Register the seeded render in the surface list so it runs on every QA cycle.

The plain render validates the "empty/default" state. The seeded render validates the
"populated/active" state. Both states must be covered.

**When to apply:** any time a feature is hidden behind a data availability check
(`if data: render`, `storage.get() ? show : hide`, `v-if="hasData"`). If a new feature's
guard tests only assert against the plain/empty render, flag it as a data-gated blind spot.

**How to check:** for each registered render surface, confirm that every conditional UI
block has either (a) a seeded render that exercises it, or (b) an explicit note explaining
why the gated path is covered another way (e.g., a live-URL browser test always has data).

(Sourced from context-forge feat(qa) meta-QA/iter438, 2026-08-09.)

---

## Sub-agent Result Format

```
SUB-AGENT [letter] ([name]) — [PASS | FAIL | DEGRADED | SKIP]
Tests run: N
Findings:
  [P0] <title>: <evidence>
  [P1] <title>: <evidence>
Notes: <anything else>
```

## Customizing for your project

Add project-specific sub-agents by appending to this file in your project's
`.claude/commands/qa.md`. The orchestrator will pick up any additional agents
defined there.

Example additions:
- **Sub-agent E (Database)**: verify migrations ran, check row counts on key tables
- **Sub-agent F (External APIs)**: ping third-party integrations you depend on
- **Sub-agent G (Performance)**: check P95 response time on key endpoints
