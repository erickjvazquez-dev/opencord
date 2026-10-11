---
description: Generates a weekly progress report from git history and loop metrics.
---
# /progress — Weekly Progress Report

Generates a human-readable summary of what was built, fixed, and improved.
Reads commit history and GOAL.md to produce a plain-English report anyone can understand.

## Usage
```
/progress              # last 7 days
/progress --last 30d   # last 30 days
/progress --since 2026-06-01
```

## Instructions for Claude

First load the project config so all steps target the right repo:
```bash
source .ccf/project.env 2>/dev/null || true
REPO_DIR="${CCF_PROJECT_DIR:-.}"
```

### Step 1 — Gather commit history
```bash
git -C "$REPO_DIR" log --oneline --since="7 days ago" --format="%h %s"
```
(Adjust `--since` based on flags)

### Step 2 — Read the plain-English commit log
Read `~/.claude/projects/$(pwd | sed 's|[^a-zA-Z0-9]|-|g')/memory/commit_log.md`
Filter entries from the relevant time window.

### Step 3 — Check GOAL.md for completed items
```bash
grep "^\- \[x\]" "$REPO_DIR/GOAL.md"
```

### Step 4 — Check the live version (skip if no live URL configured)
```bash
if [ -n "${CCF_LIVE_URL:-}" ]; then
  curl -sf "${CCF_LIVE_URL}${CCF_HEALTH_PATH:-/health}" \
    | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('version','?'))" 2>/dev/null \
    || echo "unreachable"
else
  echo "no live URL configured — skipping"
fi
```

### Step 5 — Generate the report

Output in this format (plain English, no jargon):

```
## ${CCF_PROJECT_NAME:-Project} Progress — [date range]

### What was shipped
[Bullet list of major features/fixes in plain English, sourced from commit_log.md summaries]

### What's live
Version: X.Y.Z | Status: ready
[1-2 sentences about what users can now do that they couldn't before]

### Goals completed
[List from GOAL.md [x] items]

### What's next
[First 3 unchecked items from GOAL.md backlog]

### Numbers
- Commits: N
- Tests passing: N (run the project's test command in collect-only mode to count)
- Deploy status: [ready/unreachable/n-a]
```

### Step 6 — Save to memory
Append the report to `~/.claude/projects/$(pwd | sed 's|[^a-zA-Z0-9]|-|g')/memory/progress_log.md`
(Create the file with a header if it doesn't exist)

## Does NOT
- Make any code changes
- Commit anything
- Run tests (just reports their last known count)
- Require a live URL or deploy target — if none is configured, those sections are marked n/a
