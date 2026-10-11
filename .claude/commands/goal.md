---
description: Autonomous self-improvement agent that executes the next unchecked item from the GOAL.md backlog.
---
# /goal — Autonomous Self-Improvement Agent

Reads GOAL.md, picks the next unchecked improvement, executes it fully, commits, marks done, and loops.
Runs without human intervention. Makes all decisions autonomously.

Config is read from `.ccf/project.env` (e.g. `$CCF_PROJECT_DIR`, `$CCF_GITHUB_REPO`,
`$CCF_TEST_CMD`, `$CCF_LIVE_URL`, `$CCF_DEPLOY_CMD`, `$CCF_DEPLOY_TARGET`). Source it
at the top of any bash block: `source .ccf/project.env 2>/dev/null || true`.

## Usage
```
/goal                    # run one improvement iteration
/goal --status           # show current backlog status without executing
```

## Instructions for Claude (FULLY AUTONOMOUS — DO NOT ASK USER)

### Step 0 — Read context
1. Read `${CCF_PROJECT_DIR}/GOAL.md` — find the first unchecked `[ ]` item
2. Read the project commit log / memory (e.g. `~/.claude/projects/<workspace>/memory/commit_log.md` if present) — know what was done
3. If `--status` flag: just print the backlog and stop

**CI consecutive-failure hard stop (LI3):** Before picking any item, check:
```bash
RECENT=$(gh run list --repo ${CCF_GITHUB_REPO} \
  --limit 3 --workflow ci.yml --json conclusion --jq '[.[].conclusion]' 2>/dev/null)
```
- If result is `["failure","failure","failure"]` AND the next backlog item is NOT an LI-type or CI-fix item → **pick the highest-priority LI item instead** and log reason in GOAL.md Blockers.
- If the next item IS already LI/CI-fix → proceed normally (don't recurse).
- This rule prevents shipping product features on a broken CI.

### Step 1 — Pick the item
- Take the **first unchecked item** in the backlog, top to bottom. The
  **"TOP PRIORITY — UX / Usability / Performance"** section sits at the top of
  GOAL.md by design (2026-06-10 rebalance): its `UX-*` items are picked BEFORE any
  GWC* / "detect X language" backend detector wave. Those detector waves are filler
  and only resume once the TOP PRIORITY list is empty.
- If the item starts with `UX-D`/`UX-U` (design/usability) or `C` (design): invoke the `frontend-design` skill for UI generation
- If technically blocked (missing library, broken dep): log to GOAL.md `## Blockers` section, move to next item
- Never ask the user which item to pick

### Step 2 — Spec it (internal, no file needed for small items)
For items A1-A5 and D1-D3: go directly to implementation (they're already well-defined)
For items B* and C*: spend 2 minutes generating a concise internal spec before coding

### Step 3 — Implement
Execute the improvement fully:
- Read every file that will change before touching it
- Make the changes
- Run tests after changes: `cd "${CCF_PROJECT_DIR:-$(pwd)}" && ${CCF_TEST_CMD} 2>&1 | tail -20`
- If tests fail: fix before committing (max 2 fix attempts, then log as blocker)
- For design items (C*): generate the full HTML/CSS using the `frontend-design` skill first

### Step 4 — Commit and push
```bash
cd "${CCF_PROJECT_DIR:-$(pwd)}"
source .ccf/project.env 2>/dev/null || true
git add -A
git commit -m "feat(goal): <item-id> — <plain-English description>"
git push  # current branch → its upstream; triggers CI/CD (if a remote/CI is configured)
# Deploy, if a deploy target is configured (blank = skip — e.g. local-only projects)
[ -n "${CCF_DEPLOY_CMD}" ] && eval "${CCF_DEPLOY_CMD}"
```
A post-commit hook (if installed) will auto-generate a plain-English summary.

### Step 4.5 — QA gate (runs after every commit)
After pushing, run the project's QA pipeline to verify the deploy and catch regressions.
If the project ships a QA hook, run it; otherwise re-run the test command:
```bash
cd "${CCF_PROJECT_DIR:-$(pwd)}"
source .ccf/project.env 2>/dev/null || true
if [ -x .ccf/hooks/qa.sh ]; then
  CCF_LIVE_URL="${CCF_LIVE_URL}" CCF_GITHUB_REPO="${CCF_GITHUB_REPO}" .ccf/hooks/qa.sh
else
  ${CCF_TEST_CMD}
fi
```

**CI green gate (I2):** Before marking an item done, check CI status (skip if no `$CCF_GITHUB_REPO` / no CI configured):
```bash
[ -n "${CCF_GITHUB_REPO}" ] && CI_RESULT=$(gh run list --repo ${CCF_GITHUB_REPO} \
  --limit 1 --workflow ci.yml --json conclusion --jq '.[0].conclusion' 2>/dev/null)
echo "CI: ${CI_RESULT:-n/a}"
```
- `CI_RESULT == "success"` → proceed to Step 5
- `CI_RESULT == "failure"` → get the failing log (`gh run view --log-failed`), fix the issue, push, wait for next CI run
- `CI_RESULT == "in_progress"` → wait up to 5 minutes, check again, then proceed if still running (don't block indefinitely)
- CI not found / not configured → proceed (first push, or a local-only project)

**Other QA gates:**
- **Live tests fail**: investigate the failure, fix in a follow-up commit, re-run QA
- **Error tracker shows new errors** (if one is configured): log to GOAL.md `## Blockers`, treat as a regression to fix before continuing
- **All pass**: proceed to Step 5 (mark done)

### Step 5 — Mark done
Edit `${CCF_PROJECT_DIR}/GOAL.md`:
- Change `- [ ] A1:` to `- [x] A1:`
- Move the completed item line to `## Completed` section with today's date

### Step 6 — Report and continue
Print a one-line status: `✓ A1 done — QA passed (deploy live, error tracker clean). Next: A2`
The loop will call /goal again automatically.

## Bypass rules (no human needed)
- If a file is missing: create it with sensible defaults
- If a test is flaky: run 3 times, if 2/3 pass, proceed
- If a dependency is missing: `pip install <dep>` and continue
- If git push fails: retry once with `git pull --rebase` then push
- If the deploy (`$CCF_DEPLOY_CMD`) fails: log to GOAL.md Blockers, continue to next item
- If context is running low (>80% used): commit what's done, push, then stop cleanly

## Quality bar (enforce autonomously)
- Every new function/public API must be documented (docstring or equivalent)
- Every new tool/handler must declare its input contract (schema + descriptions)
- Every new endpoint/route must enforce the project's auth/authorization convention where applicable
- Tests must pass before any commit
- No stray debug output in production code — use the project's logging convention, not `print()`

## Design items (C*) — special handling
When item starts with C:
1. Read the existing HTML/CSS (or UI) files first
2. Use the `frontend-design` skill, grounded in the project's design system
   (`DESIGN_TOKENS.md` if present) and `$CCF_NORTH_STAR`. Keep it consistent with
   the rest of the product's surfaces, accessible, and mobile-first.
3. Write the generated design to the appropriate file
4. Do NOT use placeholder content — generate real, production-quality UI
