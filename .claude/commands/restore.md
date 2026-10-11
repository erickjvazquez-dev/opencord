---
description: Restores the working tree to a named snapshot or any commit, non-destructively by branching off it.
---
# /restore — Restore to a Named Snapshot or Any Commit

Branch off any previous snapshot or commit. Non-destructive — always creates a new branch.

## Usage
```
/restore                          # lists all snapshots
/restore snapshot/before-refactor-20260603
/restore abc1234                  # restore to any commit hash
```

## What it does

1. **No args**: Lists all snapshot tags and recent commits with their plain-English summaries from `commit_log.md`
2. **With tag or hash**: Creates a new branch `restore/<target>` from that point and checks it out

## Instructions for Claude

First load the project config so all steps target the right repo:
```bash
source .ccf/project.env 2>/dev/null || true
REPO_DIR="${CCF_PROJECT_DIR:-.}"
```

**Case 1 — no argument (list mode):**
1. Run: `git -C "$REPO_DIR" tag -l 'snapshot/*' --sort=-creatordate | head -20`
2. Run: `git -C "$REPO_DIR" log --oneline -10`
3. If `~/.claude/projects/$(cd "$REPO_DIR" && pwd | sed 's|[^a-zA-Z0-9]|-|g')/memory/commit_log.md` exists, show the last 10 entries from it
4. Present a combined list and ask the user which to restore

**Case 2 — target provided:**
1. Validate the ref exists: `git -C "$REPO_DIR" rev-parse --verify "$TARGET"`
2. Generate branch name: `restore/$(echo $TARGET | sed 's/[^a-zA-Z0-9]/-/g')-$(date +%Y%m%d)`
3. Run: `git -C "$REPO_DIR" checkout -b "$BRANCH_NAME" "$TARGET"`
4. Report: "Restored to `$BRANCH_NAME` from `$TARGET`. Your previous work is safe on the previous branch."
5. Offer to show a diff: `git diff $TARGET HEAD~1 --stat`

## Safety
- NEVER runs `git checkout --` or `git reset --hard`
- NEVER modifies existing branches — always creates a new branch
- If restore fails, report the error and suggest: `git stash` first, then retry
