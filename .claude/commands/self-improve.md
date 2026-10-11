---
description: Autonomous, config-driven self-improvement loop — health, QA, improve, and ship for any framework project, paced by an adaptive idle-backoff cadence.
---
# /self-improve — Autonomous Self-Improvement Loop (generic, config-driven)

A **project-agnostic** autonomous loop: health → QA → improve → ship, paced by an
adaptive idle-backoff cadence. It references **no specific project** — every
project-specific value is read from the project's `.ccf/project.env`, and any
bespoke behavior is supplied by optional `.ccf/hooks/*` scripts. Drop the
framework into any repo, fill in `.ccf/project.env`, and `/self-improve` adapts.

North Star: read it from the project (`CCF_NORTH_STAR` in `.ccf/project.env`, or
the top of the project's `GOAL.md`). Re-anchor on it every tick; it is never
"met" (Sisyphus) — keep iterating toward greatness.

## Step −1 — Project routing gate (RUN FIRST — self-route by folder)

`/self-improve` self-routes to the project the **current working directory** belongs
to, so a fresh `/clear` → `/self-improve` in any framework repo runs *that* repo's
loop — never another project's. Two extension patterns are supported; this gate picks
the right one before any step below runs:

```bash
ROOT=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
set -a; [ -f "$ROOT/.ccf/project.env" ] && . "$ROOT/.ccf/project.env"; set +a
NAME=$(printf '%s' "${CCF_PROJECT_NAME:-$(basename "$ROOT")}" | tr 'A-Z' 'a-z')
HIT=""
for d in "$ROOT/.claude/commands" "$HOME/.claude/commands"; do
  [ -f "$d/self-improve-$NAME.md" ] && { HIT="self-improve-$NAME"; break; }
done
echo "${HIT:+ROUTE -> /$HIT}${HIT:-ROUTE -> generic engine (this file)}   (project=$NAME root=$ROOT)"
```

- **A bespoke `self-improve-<name>.md` exists** (the fork pattern, e.g. a project with
  rich custom QA it chose to keep as its own skill) → invoke `/self-improve-<name>` via
  the **Skill** tool and **STOP — do not run any step below**; that project's loop owns
  the tick.
- **You are already running as `/self-improve-<name>`** (this engine was copied into a
  bespoke loop) → skip this gate and continue below; routing already happened.
- **No bespoke loop** (the preferred pattern) → continue with this generic engine,
  which adapts via `.ccf/project.env` + optional `.ccf/hooks/*`. This is the path most
  projects should take — bespoke steps belong in `.ccf/hooks/*`, not a forked skill.

## How it adapts — load config FIRST, every tick

```bash
cd "${CCF_PROJECT_DIR:-$(pwd)}" 2>/dev/null || cd "$(pwd)"
set -a; [ -f .ccf/project.env ] && . .ccf/project.env; set +a
PROJ="${CCF_PROJECT_NAME:-$(basename "$(pwd)")}"
```

Every step below is **gated on config** — an unset/blank variable disables its
step. There is **no hardcoded fallback to any project, URL, repo, or command.**

| Variable | Drives | Blank ⇒ |
|---|---|---|
| `CCF_PROJECT_NAME` | loop label + per-project heartbeat file | dir basename |
| `CCF_PROJECT_CATEGORY` | which steps run (`app`/`library`/`cli`/`idea`) | `app` |
| `CCF_PROJECT_DIR` | where ALL work happens | cwd |
| `CCF_TEST_CMD` | the test gate | review gate instead |
| `CCF_GITHUB_REPO` | CI checks via `gh` | skip CI |
| `CCF_LIVE_URL` + `CCF_HEALTH_PATH` | live health + E2E | skip health/live checks |
| `CCF_DEPLOY_CMD` | deploy after ship | commit only, never deploy |

**Category gates** (`CCF_PROJECT_CATEGORY`): `app` → health + tests + deploy ·
`library`/`cli` → tests only (no live URL / deploy) · `idea` → no code gates, doc
checks + review gate only.

## Optional per-project extensions — keep the engine generic

A project adds bespoke steps WITHOUT forking this file by dropping executable
hooks in `.ccf/hooks/`. Each runs if present (and executable), else is skipped:

| Hook | When | Example use |
|---|---|---|
| `.ccf/hooks/health.sh` | Step 1 | extra liveness probes |
| `.ccf/hooks/qa.sh` | Step 2 (Track 0) | browser/visual/E2E, vision grading |
| `.ccf/hooks/security.sh` | Step 2/4 | project-specific adversarial probes |
| `.ccf/hooks/predeploy.sh` | before Step 6 ship | invariant gates, smoke builds |

This is how a project with rich custom QA stays on the generic engine instead of
maintaining its own copy of this skill.

## Cadence — adaptive idle-backoff (self-paced via ScheduleWakeup)

The local `/loop /self-improve` does NOT self-pace a flat interval — it widens as
the product goes green so a healthy project isn't hammered:

| Tick outcome (`idle_streak`) | next ScheduleWakeup |
|---|---|
| **Active** — shipped a fix, OR an unchecked P0/P1 in GOAL.md "## Now"/"## TOP PRIORITY" (→ reset 0) | **1800s** (~30m) |
| 1 consecutive green tick | **2700s** (~45m) |
| 2+ consecutive green ticks | **3600s** (~60m — ScheduleWakeup ceiling) |

- Track `idle_streak`, `count`, and `overloaded_streak` in the **per-project**
  heartbeat `~/.claude/loops/$PROJ.json` — never the global `loop-iterations.json`
  (that would collide with other projects' loops).
- **Precedence:** a transient-AI-error **529 backoff (Step 0.5) OVERRIDES** the
  idle ladder. Idle-backoff is only for a healthy, green, nothing-to-do loop.

## Instructions for Claude

> **GAP-FIRST:** every iteration, after the Step 0 counter bump, the first
> substantive action is to run `/gap` to detect what's missing — then Health →
> Security → E2E → Improve. Feed `/gap`'s findings into Track 0 and the Step 6
> report. Per Rule 17, fan independent gap-fixes out to parallel sub-agents.
> Cadence is adaptive idle-backoff with **no overlap**: schedule the next
> iteration only AFTER the current one finishes.

### Step 0 — Context gate

**Stop switch.** If `~/.claude/STOP_LOOPS` exists (set by `/stop-loops`): on a
**scheduled-wakeup re-entry**, `rm ~/.claude/STOP_LOOPS`, report "loop stopped",
and **end the turn WITHOUT calling ScheduleWakeup**. If the user **manually
(re)started** the loop this turn, `rm` it and continue.

```bash
[ -f "$HOME/.claude/STOP_LOOPS" ] && echo "STOP_LOOPS present — see stop-switch rule"
python3 - "$PROJ" <<'PY' 2>/dev/null || echo "iter=1 idle_streak=0"
import json, os, sys, time
proj = sys.argv[1]
os.makedirs(os.path.expanduser("~/.claude/loops"), exist_ok=True)
p = os.path.expanduser(f"~/.claude/loops/{proj}.json")
try: d = json.load(open(p))
except Exception: d = {}
d["count"] = d.get("count", 0) + 1
d.setdefault("started_at", time.time()); d["last_at"] = time.time()
d["label"] = f"/self-improve {proj}"
json.dump(d, open(p, "w"))
print(f'iter={d["count"]} idle_streak={d.get("idle_streak",0)} overloaded_streak={d.get("overloaded_streak",0)}')
PY
```

**Loop diagnostics (report-only — inform the tick, never gate it).** Surface open
blockers and installed-skill drift so the tick starts with an accurate picture.
Both are report-only (never fail, never auto-clear); SKIP silently if the framework
scripts are not present.

```bash
FW="${CCF_FRAMEWORK_DIR:-$HOME/Claude Code Framework}"
# F15 — open blockers, EXPECTED (accepted constraints) vs to-review; never auto-cleared.
[ -f "$FW/scripts/check_blockers.py" ] && \
  python3 "$FW/scripts/check_blockers.py" --goal "${CCF_PROJECT_DIR:-.}/GOAL.md" || true
# F16 — generalized skills shadowed by a project-hardcoded ~/.claude/commands copy.
[ -f "$FW/scripts/check_skill_drift.py" ] && \
  python3 "$FW/scripts/check_skill_drift.py" --commands-dir "$FW/commands" || true
```

A `[review]` blocker (not marked `EXPECTED`) is a prompt to confirm it still blocks
or resolve it — it is **evidence for the tick, not an auto-action**. Skill-drift
NOTICEs are informational (project instances legitimately differ); act only if a
skill you rely on being generic has been re-hardcoded.

**Context management — clear when ctx < 20% OR after a self-inflicted regression.**
If within this session you introduced a regression you then had to fix (shipped an
edit that broke something working, re-broke a fixed thing, left a syntax error,
clobbered config, or contradicted a prior decision), treat it as a context-loss
signal and clear the same as the <20% path. Durable state (GOAL.md, memory, the
per-project heartbeat, the pending ScheduleWakeup) survives the clear.

- **Unattended (cron/scheduled tick):** auto-clear — reset counters,
  `touch ~/.claude/needs_clear` (the Stop hook fires `/clear`), say "auto-clearing;
  loop resumes next tick." Do NOT ScheduleWakeup if a standing cron carries it.
- **Interactive (user prompting):** do NOT clear out from under them — offer via
  AskUserQuestion: (A) trigger `/loop /self-improve` now · (B) `/clear` · (C)
  keep going. On (B) → `touch ~/.claude/needs_clear`.

### Step 0.5 — Transient AI-service resilience (529 / Overloaded / rate-limit)

A transient Anthropic error (`529 Overloaded`, `overloaded_error`, `rate_limit`,
`500/502/503/504`, connection reset/timeout) is **upstream capacity, NOT our bug
and NOT a stop condition.** Never let the loop die on it.

1. **Identify** the error string → it is transient. Do NOT commit a "fix", mark the
   tick red, or hammer immediately.
2. **Exponential backoff, resume the SAME work.** Re-schedule with a growing delay
   and retry the interrupted step: attempt 1 → ~120s · 2 → ~300s · 3 → ~600s · 4+
   → ~1200–1800s (cap). Pass the same `/loop /self-improve` prompt. Track
   `overloaded_streak` in the per-project heartbeat (bump on hit, reset to 0 on the
   first successful model call).
3. **Monitor (optional, when it persists ≥3 attempts):** arm a persistent `Monitor`
   on a cheap liveness probe (a tiny model ping, or `WebFetch https://status.claude.com`)
   so the loop wakes the instant capacity returns; tear it down once a call succeeds.
4. **A workflow/sub-agent that died on 529 is RE-RUNNABLE** — `agent()` returns
   `null` after retries; `.filter(Boolean)` and re-launch the dropped lane next tick
   via `resumeFromRunId`, don't ship a partial result. **How to capture the runId for
   next-tick resume:** when launching a multi-lane workflow, write its `runId` (plus a
   SHA-256 hash of the workflow script and the current `git rev-parse HEAD` as
   `workflow_base_head`) into the per-project heartbeat JSON under
   `last_workflow_run_id`, `workflow_script_hash`, and `workflow_base_head`. On the
   next tick's Step 0.5, if a saved `last_workflow_run_id` is present, attempt
   `Workflow({scriptPath, resumeFromRunId})` — but first verify two safety interlocks:
   (a) the persisted script hash must equal the hash of the current script file (the
   native "longest unchanged prefix" cache is only valid for an identical script), and
   (b) the saved `workflow_base_head` must equal the current HEAD (saved lane patches
   won't apply against a moved base). If either interlock fails, restart clean and
   clear the saved runId. Also persist each lane's `git diff BASE HEAD` patch to
   `.claude/pending/lane_<key>.patch` before returning from the workflow — these
   survive a `/clear` and let the next tick re-apply completed-lane work without
   re-running the lane agent.

   **Patch resume procedure (saved-patch path — for cross-session and post-`/clear` resumes
   where the native journal is unreachable):** if `.claude/pending/lane_<key>.patch` files
   exist and the saved `workflow_base_head` equals the current HEAD: (1) for each done lane,
   `git apply .claude/pending/lane_<key>.patch && git commit -m "chore: re-apply done lane
   <key>"` to bring main current; (2) re-anchor `baseHead` to the new HEAD; (3) re-invoke
   the workflow with `args: { baseHead: newHEAD, skipLanes: done_lane_keys }` so only the
   still-pending lanes re-run. **Workflow script pattern:** every multi-lane workflow should
   derive its active set from `args.skipLanes`:
   ```js
   const SKIP_LANES = new Set(
     ((args && (args.skipLanes || args.skip)) || [])
       .map((k) => String(k).trim()).filter(Boolean))
   const ACTIVE_LANES = LANES.filter((L) => !SKIP_LANES.has(L.key))
   // Fan out ACTIVE_LANES (not LANES) so a resume re-runs only pending lanes.
   ```
   Absent/empty `skipLanes` → a fresh run over every lane (zero behavior change to the
   normal path). Within a continuing session, `resumeFromRunId` is the faster path (no
   patch re-apply needed) — use it opportunistically, but the saved-patch path is the
   durable baseline that works across sessions and after `/clear`.
5. **Report honestly:** `AI-SERVICE: overloaded (529) — backed off Ns, retrying`,
   never a green tick or a red deploy gate. Product health and a 529 are orthogonal.

### Step 1 — Health / build gate (fix-first)

In `CCF_PROJECT_DIR`, gated on config:
```bash
# Test gate (the floor). Blank CCF_TEST_CMD → use a review gate instead.
[ -n "$CCF_TEST_CMD" ] && eval "$CCF_TEST_CMD" || echo "no CCF_TEST_CMD — review gate"
# CI — only if a repo is configured.
[ -n "$CCF_GITHUB_REPO" ] && gh run list --repo "$CCF_GITHUB_REPO" --limit 3 \
  --json conclusion,status --jq '[.[]|.conclusion // .status]' 2>/dev/null
# Live health — only if a live URL is configured.
[ -n "$CCF_LIVE_URL" ] && curl -sf "${CCF_LIVE_URL}${CCF_HEALTH_PATH:-/health}" --max-time 8 \
  2>/dev/null && echo " (health OK)" || { [ -n "$CCF_LIVE_URL" ] && echo "CRITICAL: unreachable"; }
# Project-specific extra health probes, if the project supplies them.
[ -x .ccf/hooks/health.sh ] && ./.ccf/hooks/health.sh
# Unpushed commits block CI from seeing new code.
git log --oneline @{upstream}..HEAD 2>/dev/null | wc -l | tr -d ' ' | sed 's/^/unpushed: /'
```
**If RED: fix immediately, skip remaining steps.** If CI is red, get the failure
log (`gh run view --log-failed`), root-cause it, fix and verify **locally** before
re-dispatching ONE run — never blind-retry (Actions minutes cost money).

**Deploy-staleness false-positive:** when checking whether the deployed build is
current by comparing `live_sha` to `HEAD`, a naive `live_sha != HEAD` test produces
false positives when the deploy system has runtime-path filters (`watchPatterns`,
`on.push.paths`, `paths:` in CI). Docs/tests/scripts-only commits legitimately leave
`live_sha` behind `HEAD` — the deploy skip was intentional and the latest runtime code
is already live. The correct check: count commits in `live_sha..HEAD` that touch a
runtime-eligible path (`git diff --name-only live_sha HEAD` filtered by the project's
watch-path list); if 0, log `DEPLOY CURRENT` and do NOT redeploy. Forcing a
byte-identical redeploy wastes compute, incurs a cold-start restart, and adds
unnecessary deploy risk. This is the monitoring-side complement to GAP-041 (which
covers the pipeline-config side).

### Step 2 — Track 0: QA & gaps (do this MOST)

Grow the project's **fully-autonomous** test suite toward the north star:
end-to-end for every tool/surface, automated and scripted-"manual". Each tick:
find ONE real coverage/quality gap and **close it** — an untested endpoint/flow, a
protocol edge case (reconnect, oversized/garbage input), or a **Rule 15**
adversarial probe (auth bypass, injection, traversal, oversized/malformed payload).
Run `.ccf/hooks/qa.sh` if present (project-specific browser/visual/E2E). Log every
gap as a rule in `/gap` (gap → `/gap` → `/self-improve` is the orchestra).

### Step 3 — Track 1: continuous fixes (auto-ship)

Health / security / E2E / quality checks; **fix what already exists** — bugs,
regressions, drift. A real fix → commit + push. Nothing wrong → **log green, no
commit** (anti-churn — never manufacture work to look busy).

**Security enforcement (backs the `SECURITY:` report field — never assert it unbacked).**
Run **`/security-checklist`** (the framework's 20-item pre-launch bar, SSOT in
`commands/security-checklist.md`) plus `.ccf/hooks/security.sh` if present:
- **Any P0 FAIL is a hard ship gate** — do NOT push this tick until it is fixed, exactly
  like a red test (a P0 finding IS the tick's fix). Compute `SECURITY:` from the result:
  `clean` (P0 CLEAN, 0 P1 open), `N P1 open`, or `BLOCK (P0)`.
- **P1 findings** → file into `GOAL.md` "## TOP PRIORITY" as `SEC-*` items (don't fix inline
  unless trivial); the loop closes them on later ticks.
- **Owner-only items** (an env var to set on the host, a key to rotate, a repo to create) →
  append to `~/.claude/owner-todos.md` (Rule 19) — they can't be fixed by the loop.
The static P0 subset also runs pre-commit via `/critical-checks` Block 4; this step adds the
live-surface probes (headers, HTTPS, cookies, PoW-enforced-in-prod) the pre-commit pass can't see.

### Step 4 — Track 2: features (spec → build → prove → ship)

Advance the highest unchecked item in `GOAL.md` ("## TOP PRIORITY" / "## Now"
first, then "## Next"). **Spec-first (Rule 6)** for any >3-file or new-user-facing
flow (append to the project's `SPEC.md`). Build the minimal thing; do an
adversarial pass (Rule 15) on any new input surface. **The gate is the quality bar
(clean / verified / secure), NOT human approval** — ship autonomously when clean.

**Blast-radius guard:** alongside each code edit, spawn a parallel `Agent` whose
sole job is to find everything downstream of the changed symbols/files and prove
none of it breaks; fix what it flags before the ship gate.

**Parallel domain lanes with worktree isolation:** when fanning out N improvement
agents in parallel (e.g. one per domain: UI, backend, performance, security), run
each with `isolation: 'worktree'` so concurrent edits never collide on shared files.
After all lanes finish, integrate green patches to main sequentially: for each patch,
`git apply --check` first (skip on conflict), then `git apply`, re-run the gate;
commit if the gate passes, hard-discard (`git checkout -- . && git clean -fd`) if it
fails. Main must never hold an ungated change. A lane that finds nothing actionable
returns noop — never invent work to fill the slot. `.filter(Boolean)` after any
`parallel()` call to drop lanes whose agents errored rather than letting a null crash
the integration loop.

**Disjoint file partition:** when defining parallel lanes, explicitly assign every
file to exactly one lane and enforce it in the lane's prompt ("edit ONLY these files:
X, Y, Z"). Overlapping file ownership is the root cause of merge conflicts across
worktrees — make ownership explicit in the lane definition, not assumed at runtime.

**Explicit staging — never `git add -A` in a parallel lane:** each lane agent MUST
stage files by explicit path (`git add <exact/file/path>`) and NEVER with `git add -A`
or `git add .`. Blanket staging in a worktree sweeps every untracked file in the repo
(QA reports, build artifacts, test fixtures) into the commit alongside the real change,
burying the diff in noise and corrupting blame. From practice: a 12-line CSS fix was
swept up with 151 untracked `reports/qa/*.md` files (26,400 lines). After each lane
commits, run `git status --porcelain` to verify no undeclared paths were staged. The
integrator should cross-check the lane's declared file list against the actual diff
(`git diff <base> HEAD --name-only`) and skip any lane whose patch touches paths the
lane did not declare.

**Stale worktree pruning:** after all patches are applied and the integration run
completes, prune abandoned worktrees under `.claude/worktrees/`. Finished runs leave
checked-out dirs behind; accumulated stale worktrees can be pinned at a pre-fix
commit, so a lane handed one silently rolls back shipped work. Safe pruning criteria:
remove only entries strictly under `.claude/worktrees/`, exclude the current run's
worktrees (matched by the run ID prefix), skip any `git worktree lock`-marked entry,
and skip entries newer than the run's start time (a concurrent lane's active worktree).
Run the prune from the main checkout only — never from inside the worktrees directory.

**Post-implementation gap-check:** after all lanes produce their diffs, run a
*collective* reviewer agent that sees ALL diffs simultaneously before any integration
happens. Have it check for: cross-lane breakage (lane A removes a symbol or CSS
variable that lane B reads), rule violations that only become visible in aggregate,
and correctness issues a per-lane self-gate misses because each lane only sees its
own diff. An empty findings list is a valid, good outcome — never manufacture issues.
Route P0/P1 findings to targeted per-lane fixer agents; clean (P2-only or zero)
lanes integrate without a re-run. This phase is distinct from the blast-radius guard
— it checks cross-lane consistency, not downstream callers.

**Multi-iteration focus sprint — baseline first:** when starting a sprint dedicated
to improving an algorithm or metric across N categories/types (e.g. "improve
compression for all file types", "improve classifier accuracy across all labels",
"harden all input paths"), do NOT start iterating immediately. First: (1) build a
per-category baseline benchmark that measures current performance per category,
sorted by opportunity (lowest current performance = highest gain potential first);
(2) gate the baseline with regression tests that encode each category's current
value so improving category X can't silently degrade category Y. Each subsequent
tick records the delta vs. baseline ("X improved from Y% to Z%; A/B/C unchanged").
Without the baseline: (a) already-optimal categories get re-visited, (b) categories
that already work regress silently, (c) N ticks pass with no legible progress.

**Named improvement campaign (scoped gap audits):** for a large, multi-tick focus
effort on a specific feature area (e.g. "improve code Q&A quality over 300 ticks"),
define it as a named campaign with explicit state so progress is legible and the loop
stays scoped:

```python
# In ~/.claude/loops/$PROJ.json — campaign state stored alongside idle_streak
{
  "my_campaign": {
    "target": 300,
    "done": 0,
    "started_iter": 47,
    "active": true
  }
}
```

Loop behaviour while a campaign is active:
1. **Scope `/gap` to the campaign area** — run gap detection targeted at the
   campaign's feature domain (e.g. "gap in the <feature> answering path"), not the
   full product, so each tick finds and closes ONE real gap in that domain.
2. **Build → prove → ship → bump `done`** — increment `campaign.done` ONLY after a
   verified gap is closed with a regression test. A refactor with no user-visible
   behaviour change does NOT count.
3. **Sequence deterministic slices before LLM slices** — see GAP-046: within the
   campaign, always ship the zero-cost, regression-testable tier of each feature
   before the LLM-gated tier.
4. **End the campaign when `done >= target`** — set `active: false`, note it in
   GOAL.md, resume broad gap detection on the next tick. The campaign's done counter
   and started_iter stay in the heartbeat for auditing.

A campaign that lets `/gap` roam the full product during each tick will close easy
unrelated gaps while the focal feature stagnates. Scope is the discipline.

### Step 5 — Verify before "done" (Rule 14)

The configured gate (`CCF_TEST_CMD` + `go vet`/lint/build as applicable) green is
the **floor**. For user-facing changes, verify in the **running app** (boot the
stack via the project's documented run path and exercise the real flow), not just
green tests. Run `.ccf/hooks/predeploy.sh` if present. State the verification
method explicitly; if you can't verify in-context, say so — don't claim success.

### Step 5b — Regression capture (after every fix)

No fix is done until the bug can never silently return. The fix commit MUST
add/extend a test that **fails without the fix and passes with it** (Rule 15).
Before adding, grep the suite to EXTEND an existing guard rather than duplicate it.
If the change deployed (`CCF_DEPLOY_CMD` ran), re-verify the regression's core
assertion against the live URL. Log the bug + guard as a `/gap` rule + a one-line
memory so the *class* of bug is watched. This closes the loop: find → fix → ship →
**guard + verify → remember**.

**Sibling audit** — after fixing a bug in one member of a family (processors for different
formats, resolvers for different query types, handlers for different content types), immediately
scan sibling members for the same class of bug before moving on. A bug that exists in one family
member usually exists in at least one sibling: same structural pattern, same library version,
same missing branch. The audit is fast (grep + skim the sibling's handling code) vs. the cost
of re-discovering the same bug from a user report. Scope: same inheritance chain, same duck-typed
interface, same structural pattern. Report "no sibling exposure — confirmed clean" when none
found, or queue "sibling `<X>` has same class" as a GOAL.md item when found.

### Step 6 — Ship, report, and set the adaptive next-wakeup

**Ship:** if something real changed, ONE surgical Conventional-Commits commit →
`git push` to the project remote. If `CCF_DEPLOY_CMD` is set, run it; else commit
only. Update the live breadcrumb `~/.claude/loop_current_task` at each phase
(rendered as the statusline `▸ NOW:` row; on idle write `idle — next tick <time>`).

**Report (≤10 lines):**
```
SELF-IMPROVE [$PROJ] ITER [N] — [time]
HEALTH: [green/red] | SECURITY: [clean/issues] | GAPS: [n closed]
FIXED / SHIPPED: [what]
NEXT: [delay (s) + idle_streak + reason]
```

**Update the per-project heartbeat and pick the next delay from the ladder:**
```bash
# SHIPPED=1 if this tick committed a real fix OR an unchecked P0/P1 is open; else 0.
python3 - "$PROJ" "${SHIPPED:-0}" <<'PY'
import json, os, sys, time
proj, shipped = sys.argv[1], sys.argv[2] == "1"
p = os.path.expanduser(f"~/.claude/loops/{proj}.json")
d = json.load(open(p)) if os.path.exists(p) else {}
d["last_at"] = time.time(); d["label"] = f"/self-improve {proj}"
if d.get("overloaded_streak", 0) > 0:
    print(f"delay=BACKOFF (overloaded_streak={d['overloaded_streak']}) — use Step 0.5 ladder")
else:
    d["idle_streak"] = 0 if shipped else d.get("idle_streak", 0) + 1
    json.dump(d, open(p, "w"))
    delay = {0: 1800, 1: 2700}.get(d["idle_streak"], 3600)
    print(f"delay={delay} idle_streak={d['idle_streak']} "
          f"({'active — 30-min pace' if shipped else 'green — widening'})")
PY
```
Then call **ScheduleWakeup** with that `delaySeconds`, re-firing `/loop /self-improve`.

## Loop rules (self-improve specific — the project's CLAUDE.md covers the rest)

0. **Full autonomy** — act and report; never pause to ask on routine maintenance
   (push, CI, QA). Human intervention only on deviation (shipping broken/insecure
   code, or drifting off the project's north star).
1. **Never stop** — adaptive idle-backoff (table above): ~30m when fixing, widening
   to 3600s as the product goes green. A green tick with nothing to fix is valid AND
   is the signal to widen — log it, don't invent work.
2. **Everything auto-ships once verified** — the gate is quality (tests + Rule 15 +
   Rule 14 verification), not approval. Batch related changes into one push.
3. **find-skill first** — before any non-trivial implementation, invoke `find-skill`.
4. **Real testing** — not just HTTP 200s / green unit tests; exercise the actual
   user-visible behavior (Rule 14).
5. **Diagnose CI failures, don't blind-retry** — `gh run view --log-failed`,
   reproduce/fix locally, then ONE re-dispatch. Two consecutive post-fix CI failures
   → stop dispatching until a faithful local repro passes (Actions minutes cost money).
5a. **CI budget discipline** — if a project's per-push CI triggers exhaust the monthly
   budget, split into two tiers instead of disabling CI: (a) **fast gate** — invariants
   only (build smoke + critical guards, ~5–10 min/run), run nightly via `schedule:`
   cron; (b) **full suite** — complete tests + security, run weekly or on `workflow_dispatch`.
   The local `hooks/pre-push-gate.sh` is the per-commit safety net that substitutes for
   per-push CI. Never re-enable per-push triggers without computing monthly budget math
   (`runs/day × min/run × 30 ≤ cap`).
6. **Survive AI-service blips** — Step 0.5: back off and retry, never die or report red.
7. **Docs-sync** — a change that makes README/docs/GOAL stale lands with its doc
   delta in the same push.
8. **Per-project isolation** — never touch, reference, or push to any repo other than
   `CCF_PROJECT_DIR`. All loop state lives in `~/.claude/loops/$PROJ.json`, never the
   global loop files. No hardcoded project name, URL, repo, or command anywhere — read
   it from `.ccf/project.env`.
9. **Treat the loop's own instrumentation as product** — never ship unverified harness
   edits: `bash -n` + run-and-assert any script change; never auto-mutate user config;
   use local-day/ASCII-safe rollovers. The tooling that reports on the loop meets the
   same bar as the loop's output.
