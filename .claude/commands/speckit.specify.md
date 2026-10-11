---
description: Writes a detailed spec before any building begins.
---
# /speckit.specify — Write a Detailed Spec Before Building

Part of the Spec-Driven Development (SDD) workflow. Run this FIRST before any non-trivial feature.

## Usage
```
/speckit.specify "add subscription billing to the app"
/speckit.specify "expose the search endpoint as an MCP tool"
```

## What it produces

A structured specification saved to `.claude/specs/<feature-slug>.md` containing:
- **Goal**: one sentence, what this achieves for the user
- **Context**: why now, what prompted this
- **Acceptance criteria**: numbered, testable, written as "Given/When/Then" or plain assertions
- **Out of scope**: explicit list of what this does NOT do
- **Open questions**: anything that must be decided before implementation
- **Risk flags**: security, performance, backwards-compat concerns

## Instructions for Claude

1. Ask the user 2-3 clarifying questions if the feature is underspecified (ambiguous scope, unknown constraints)
2. Research the relevant code — read the files that will be affected
3. Write the spec to `.claude/specs/<slug>.md` (create dir if needed)
4. Present the spec to the user and ask: "Does this match your intent? Any changes before we plan?"
5. Do NOT start implementation — run `/speckit.plan` next

## Quality bar
- Every acceptance criterion must be independently verifiable
- "Out of scope" should be at least 2 items (forces you to think about what you're NOT building)
- If any open question could block implementation, flag it as BLOCKING
