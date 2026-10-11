---
description: Routes a request to the right framework skill when it is unclear which command to run.
---
# /find-skill — Auto-Route to the Right Skill

Reads the skills registry and matches the user's intent to the best skill.

## Usage
```
/find-skill "I want to make sure nothing broke before I push"
/find-skill "before I start this big refactor"
/find-skill                    # matches last user message automatically
```

## Instructions for Claude

1. **Read** `~/.claude/SKILLS_REGISTRY.md`
2. **Extract intent** from the user's message (or last message if no args)
3. **Match** against trigger keywords — prefer the most specific match
4. **If one clear match**: propose it with a one-line justification, then ask "Run `/skill-name` now?"
5. **If multiple matches**: present top 2-3 options and ask which fits best
6. **If no match**: say "No skill matches this — here's what's available:" and list the registry

## Output format
```
Suggested: /snapshot
Reason: "before I start this big refactor" matches the checkpoint/save-state pattern.

Run /snapshot now? (You'll be asked for a label)
```

## Do NOT
- Run the matched skill without asking first
- Invent skills not in the registry
- Skip reading SKILLS_REGISTRY.md — always read it fresh (it may have been updated)
