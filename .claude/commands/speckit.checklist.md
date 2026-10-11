---
description: Post-implementation verification that every acceptance criterion in the spec passes.
---
# /speckit.checklist — Post-Implementation Verification

Part of the Spec-Driven Development (SDD) workflow. Run after `/speckit.implement`.

## Usage
```
/speckit.checklist specs/expose-smart-query.md
/speckit.checklist              # uses most recently modified spec
```

## What it verifies

Goes through every acceptance criterion in the spec and checks each one:
- Runs the test plan steps
- Checks that each "Given/When/Then" criterion is satisfied
- Runs `/security-review` if the spec has risk flags
- Runs `/code-review` on the diff since the spec was created

## Instructions for Claude

1. Read the spec file — extract all acceptance criteria
2. For each criterion: state "Checking: [criterion]", run the relevant test/command, report PASS or FAIL
3. If any criterion FAILs: stop the checklist, report what failed, ask user how to fix before continuing
4. After all criteria pass:
   - Run: `git log --oneline $(git merge-base HEAD main)..HEAD` to show commits in this feature
   - Summarize the plain-English changes from `commit_log.md` for this feature
   - Ask: "All checks pass. Mark this spec as DONE and merge to main?"
5. If user confirms: update spec file with `**Status: DONE**` at the top

## Do NOT
- Mark anything PASS without actually verifying it
- Skip the security review if risk flags are present
