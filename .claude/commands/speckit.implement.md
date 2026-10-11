---
description: Executes an implementation plan step by step with one commit per step.
---
# /speckit.implement — Execute the Plan Step by Step

Part of the Spec-Driven Development (SDD) workflow. Run after `/speckit.plan`.

## Usage
```
/speckit.implement specs/expose-smart-query.md
/speckit.implement              # uses the most recently modified spec
/speckit.implement --step 3     # resume from step 3
```

## What it does

Executes each implementation step from the plan:
1. Announces the step being executed
2. Makes the code changes
3. Verifies the step works (runs tests, checks output)
4. Creates a conventional commit: `feat: <step description>`
5. Moves to the next step

## Instructions for Claude

1. Read the spec file to find the Implementation Plan section
2. Check which steps are already done (look at recent git log)
3. Execute steps in order — one step = one commit
4. After each step: run the test command from the plan, confirm pass before proceeding
5. If a step fails: stop, report the error, ask the user how to proceed (do NOT skip)
6. After all steps: announce "Implementation complete — run `/speckit.checklist` to verify"

## Commit format
Each commit message: `feat(<feature-slug>): <step description>`

## On failure
Never skip a failing step silently. Always surface the error and wait for user input.
