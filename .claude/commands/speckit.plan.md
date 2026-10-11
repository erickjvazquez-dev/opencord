---
description: Turns a spec into an ordered, step-by-step implementation plan.
---
# /speckit.plan — Turn a Spec Into an Implementation Plan

Part of the Spec-Driven Development (SDD) workflow. Run after `/speckit.specify`.

## Usage
```
/speckit.plan specs/expose-smart-query.md
/speckit.plan              # uses the most recently modified spec
```

## What it produces

Updates the spec file with an **Implementation Plan** section:
- **Files to modify**: exact paths, what changes
- **Files to create**: exact paths, what goes in them
- **Execution order**: numbered steps, each one independently committable
- **Reuse opportunities**: existing functions/utilities to call instead of reimplementing
- **Test plan**: what to run/observe to verify each step

## Instructions for Claude

1. Read the spec file (or find the latest one in `.claude/specs/`)
2. Read ALL files listed in "Files to modify" to understand current state
3. Search for existing utilities that can be reused (grep before you write)
4. Write the plan as a new section appended to the spec file
5. Keep steps small — each step should be a single commit
6. Identify the riskiest step and put it FIRST (fail fast)
7. Present the plan and ask: "Ready to implement? Or should we adjust the plan?"

## Do NOT
- Start writing code
- Skip reading the existing files
- Plan more than one phase at a time
