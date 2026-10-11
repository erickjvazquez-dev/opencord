---
description: Safely reverts the last commit when it broke something, preserving history.
---
# /rollback — Safely Revert the Last Commit If It Broke Something

When a commit breaks CI, the deploy, or tests, rollback reverts it cleanly and logs why.
Non-destructive: always creates a new revert commit, never force-pushes.

## Usage
```
/rollback                    # revert last commit (asks for confirmation)
/rollback --auto             # revert without asking (used by /goal on QA failure)
/rollback --commit abc1234   # revert a specific commit
```

## Instructions for Claude

First load the project config so all steps target the right repo:
```bash
source .ccf/project.env 2>/dev/null || true
REPO_DIR="${CCF_PROJECT_DIR:-.}"
```

### Step 0 — Show what will be reverted
1. Run: `git -C "$REPO_DIR" log -1 --oneline`
2. Show the user the commit and ask "Revert this commit?" — UNLESS `--auto` flag is set

### Step 1 — Check if safe to revert
Run: `git -C "$REPO_DIR" status --short`
- If working tree has uncommitted changes: `git stash` first, then proceed
- If clean: proceed directly

### Step 2 — Create the revert commit
```bash
git -C "$REPO_DIR" revert HEAD --no-edit
# This creates a new commit: "Revert: <original message>"
```
- NEVER use `git reset --hard` — that destroys history
- NEVER force-push

### Step 3 — Push the revert
```bash
git -C "$REPO_DIR" push   # pushes the current branch to its upstream (branch/remote-agnostic)
# If a deploy command is configured, run it to ship the revert (skip if blank):
[ -n "${CCF_DEPLOY_CMD:-}" ] && (cd "$REPO_DIR" && eval "$CCF_DEPLOY_CMD")
```

### Step 4 — Log the rollback
Append to `~/.claude/projects/$(cd "$REPO_DIR" && pwd | sed 's|[^a-zA-Z0-9]|-|g')/memory/commit_log.md`:
```
**[date]** `REVERT` — Rolled back: [original commit message]. Reason: [QA failure / CI failure / user request]
```

### Step 5 — Report
Print: "↩ Reverted — [original commit]. The repo is back to the previous state. Run /qa to verify."

## Used by /goal
When the `/goal` QA gate (Step 4.5) fails after a commit and it's a deploy or test regression
(not a pre-existing issue), `/goal` should call `/rollback --auto` before logging the blocker.

## Does NOT
- Revert more than one commit at a time (call multiple times if needed)
- Modify any branch other than the current one
- Reset working tree changes (uses `git revert`, not `git reset`)
