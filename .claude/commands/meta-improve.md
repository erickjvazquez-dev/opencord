---
description: Analyzes the self-improvement loop's own performance and adds concrete loop-improvement items back to the backlog.
---
# /meta-improve — Improve the Self-Improvement Loop Itself

Every 5th goal iteration (or on demand), analyze the loop's own performance and add
improvements back to the GOAL.md backlog. The loop that improves the loop.

## Usage
```
/meta-improve                  # analyze and add 1-3 improvements to GOAL.md
/meta-improve --force          # run even if fewer than 5 iterations since last run
```

## When to auto-run
The /goal skill should call this automatically after every 5th completed item.
Track count in GOAL.md under `## Meta-Improve Counter`.

## Instructions for Claude

### Step 1 — Gather loop performance signals

First load the project config:
```bash
source .ccf/project.env 2>/dev/null || true
REPO_DIR="${CCF_PROJECT_DIR:-.}"
```

Read ALL of these:
- `~/.claude/projects/$(pwd | sed 's|[^a-zA-Z0-9]|-|g')/memory/commit_log.md` — what was shipped
- `~/.claude/projects/$(pwd | sed 's|[^a-zA-Z0-9]|-|g')/memory/session_log.md` — session patterns
- `$REPO_DIR/GOAL.md` — what's done vs pending
- Recent CI failures (skip if no repo configured):
  `[ -n "${CCF_GITHUB_REPO:-}" ] && gh run list --repo "${CCF_GITHUB_REPO}" --limit 10 --json conclusion`

### Step 2 — Diagnose friction points

Look for:
- **CI failures** — which jobs fail most? Are they the same root cause?
- **Blockers in GOAL.md** — items stuck in `## Blockers` reveal missing capabilities
- **Repeated fix commits** — same component fixed 2+ times → systematic issue
- **Long gaps in commit_log** — items that took many iterations → complexity underestimated
- **Missing skills** — did the loop have to do something manually it could have automated?
- **QA false negatives** — did something ship that broke in production?

### Step 3 — Generate improvements

For each friction point, create one concrete improvement. Format:
- For a new GOAL.md item: `- [ ] F1: <description>`
- For a new skill: write the skill file directly to the framework's commands dir
  (`${CCF_FRAMEWORK_DIR:-$HOME/Claude Code Framework}/commands/<name>.md`)
- For a new rule in /goal: edit the relevant section of the `/goal` skill file

**Do not add more than 3 improvements per run** — quality over quantity.

### Step 4 — Add to GOAL.md

Add the new items to the appropriate section in `$REPO_DIR/GOAL.md`.
If a new `## Loop Improvements` section doesn't exist, create it.

### Step 5 — Update counter

In GOAL.md, update `## Meta-Improve Counter`:
```
Last run: [date] | Items added: N | Next run: after 5 more goal completions
```

### Step 6 — Report

Print: "🔁 Meta-improve complete — added N improvement(s): [brief list]"

## Example improvements this might generate

- "The loop always manually pushes to two remotes — add a `push-both` alias"
- "Error monitoring isn't wired into local tests — add a mock in the test fixtures"
- "Benchmark gate skips when the deploy is down — should cache last known score"
- "No way to pause the loop gracefully — add a /pause-loop skill"
