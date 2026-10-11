---
description: Creates a named git recovery point you can branch off or restore later.
---
# /snapshot — Create a Named Recovery Snapshot

Create a git commit + tag so you can restore to this exact state later.

## Usage
```
/snapshot "brief label"
/snapshot "before refactoring token budget"
```

## What it does

1. Stages all current changes (`git add -A`)
2. Creates a commit: `snapshot: <label>`
3. Tags it: `snapshot/<slug>-<timestamp>` (e.g. `snapshot/before-refactor-20260603`)
4. Prints the tag name so you can reference it in `/restore`

## Instructions for Claude

When the user runs /snapshot with a label:

First load the project config so all steps target the right repo:
```bash
source .ccf/project.env 2>/dev/null || true
REPO_DIR="${CCF_PROJECT_DIR:-.}"
```

1. Run: `git -C "$REPO_DIR" add -A`
2. Run: `git -C "$REPO_DIR" commit -m "snapshot: $LABEL"`
   - If nothing to commit, inform the user and still create the tag at HEAD
3. Generate slug: lowercase the label, replace spaces/special chars with `-`
4. Generate timestamp: `$(date +%Y%m%d-%H%M)`
5. Run: `git -C "$REPO_DIR" tag "snapshot/$SLUG-$TIMESTAMP"`
6. Report: "Snapshot created: `snapshot/$SLUG-$TIMESTAMP` at commit `$(git -C "$REPO_DIR" rev-parse --short HEAD)`"

The post-commit hook will automatically generate a plain-English summary in the background.

## Safety
- Never destructive — only adds commits and tags
- Works even if working tree is clean (tags HEAD)
