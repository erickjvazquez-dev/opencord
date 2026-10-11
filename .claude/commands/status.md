---
description: Live environment inspector that reports the project's config, health, and loop state at a glance.
---
# /status — Live Environment Inspector

Shows exactly what is running right now vs what is installed — the interactive
drill-down companion to the statusline strip. Think of it as `ps` + `ls` for
your Claude Code environment.

## Usage
```
/status              # full dashboard — all sections
/status agents       # only running agents + background tasks
/status loops        # only active loops + scheduled wakeups
/status skills       # list all installed skills with descriptions
/status plugins      # list installed plugins with versions
/status rules        # list all core rules from CLAUDE.md
/status ci           # CI status for all repos
/status cloud        # cloud routine status (last fired, next fire)
```

## Instructions for Claude

Run the requested section(s). For `/status` (no args), show all sections in order.

---

### Section: AGENTS (what's running right now)

```bash
echo "═══ RUNNING AGENTS ══════════════════════════════════"

# Background sub-agent output files (most recent first, any session)
echo "Background task files:"
ls -t /tmp/claude-*/*/*/tasks/*.output /private/tmp/claude-*/*/*/tasks/*.output 2>/dev/null | head -10 | sed 's/^/  /' || echo "  none"

# claude processes
echo ""
echo "Claude processes (pgrep):"
pgrep -a "claude" 2>/dev/null | head -10 | while read PID CMD; do
  echo "  PID $PID: ${CMD:0:80}"
done || echo "  none found"

# ScheduleWakeup / loop processes
echo ""
echo "Scheduled wakeups / loops:"
pgrep -a "ScheduleWakeup" 2>/dev/null | head -5 | while read PID CMD; do
  echo "  PID $PID: ${CMD:0:80}"
done || echo "  none active"
```

---

### Section: LOOPS (active self-improvement / scheduled)

```bash
echo "═══ ACTIVE LOOPS ════════════════════════════════════"

# ScheduleWakeup state
LOOPS=$(pgrep -f "ScheduleWakeup" 2>/dev/null | wc -l | tr -d ' ')
echo "Active ScheduleWakeup processes: $LOOPS"

# CronCreate jobs (if any)
# These are managed by the Claude Code harness - report what we know
echo ""
echo "Cloud routines (auto-running, if any are registered for this project):"
echo "  Run: RemoteTrigger list   for the live list + last_fired_at timestamps"

# Last self-improve iteration
echo ""
echo "Last self-improve activity:"
source .ccf/project.env 2>/dev/null || true
git -C "${CCF_PROJECT_DIR:-$(pwd)}" log --oneline -5 --format="%ar | %s" 2>/dev/null | head -5
```

---

### Section: SKILLS (all installed slash commands)

```bash
echo "═══ INSTALLED SKILLS ════════════════════════════════"
echo ""
for F in ~/.claude/commands/*.md; do
  NAME=$(basename "$F" .md)
  # Extract first non-empty, non-# line as description
  DESC=$(grep -m1 "^[^#]" "$F" 2>/dev/null | head -c 70)
  printf "  \033[36m/%-22s\033[0m %s\n" "$NAME" "$DESC"
done
echo ""
echo "Total: $(ls ~/.claude/commands/*.md 2>/dev/null | wc -l | tr -d ' ') skills"
```

---

### Section: PLUGINS (installed plugins)

```bash
echo "═══ INSTALLED PLUGINS ═══════════════════════════════"
echo ""
python3 << 'PYEOF'
import json, os
data = json.load(open(os.path.expanduser("~/.claude/plugins/installed_plugins.json")))
for plugin_key, installs in data.get("plugins", {}).items():
    info = installs[0] if installs else {}
    sha = info.get("gitCommitSha", "?")[:8]
    installed = info.get("installedAt", "?")[:10]
    path = info.get("installPath", "")
    # Try to get description from SKILL.md
    desc = ""
    skill_dir = os.path.join(path, "skills")
    if os.path.exists(skill_dir):
        for d in os.listdir(skill_dir):
            skill_md = os.path.join(skill_dir, d, "SKILL.md")
            if os.path.exists(skill_md):
                for line in open(skill_md):
                    if line.startswith("description:"):
                        desc = line.replace("description:", "").strip().strip('"')[:60]
                        break
    print(f"  ⬡ {plugin_key:<40} sha:{sha}  installed:{installed}")
    if desc:
        print(f"    └─ {desc}")
PYEOF
echo ""
echo "Total: $(python3 -c "import json,os; d=json.load(open(os.path.expanduser('~/.claude/plugins/installed_plugins.json'))); print(len(d.get('plugins', {})))" 2>/dev/null) plugins"
```

---

### Section: RULES (core rules from CLAUDE.md)

```bash
echo "═══ CORE RULES ══════════════════════════════════════"
echo ""
grep -E "^### Rule [0-9]+" ~/CLAUDE.md | while read LINE; do
  NUM=$(echo "$LINE" | grep -oP "Rule \K\d+")
  TITLE=$(echo "$LINE" | sed 's/### Rule [0-9]* — //')
  printf "  \033[33m[%2s]\033[0m %s\n" "$NUM" "$TITLE"
done
echo ""
TOTAL=$(grep -c "^### Rule " ~/CLAUDE.md 2>/dev/null || echo 0)
echo "Total: $TOTAL core rules (see ~/CLAUDE.md for full text)"
```

---

### Section: CI (CI status for all repos)

```bash
echo "═══ CI STATUS ═══════════════════════════════════════"
echo ""
source .ccf/project.env 2>/dev/null || true

if [ -z "${CCF_GITHUB_REPO}" ]; then
  echo "  No \$CCF_GITHUB_REPO configured — no remote CI to inspect."
else
  # Main CI
  echo "${CCF_PROJECT_NAME:-project} (${CCF_GITHUB_REPO}):"
  gh run list --repo ${CCF_GITHUB_REPO} --limit 3 --workflow ci.yml \
    --json conclusion,status,headSha,createdAt \
    --jq '.[] | "  \(.conclusion // .status) | \(.headSha[:7]) | \(.createdAt[:16])"' 2>/dev/null

  echo ""
  echo "Security audit:"
  gh run list --repo ${CCF_GITHUB_REPO} --workflow security-audit.yml \
    --limit 2 --json conclusion,createdAt \
    --jq '.[] | "  \(.conclusion) | \(.createdAt[:16])"' 2>/dev/null

  echo ""
  echo "E2E / latest run:"
  gh run list --repo ${CCF_GITHUB_REPO} --limit 1 \
    --json conclusion,workflowName,createdAt \
    --jq '.[] | "  \(.workflowName): \(.conclusion) | \(.createdAt[:16])"' 2>/dev/null
fi
```

---

### Section: CLOUD (cloud routine status)

```bash
echo "═══ CLOUD ROUTINES ══════════════════════════════════"
echo ""
source .ccf/project.env 2>/dev/null || true
# Use RemoteTrigger if available, otherwise show last-known state from git
echo "Checking cloud routine last-fired times..."
# Git-based proxy: cloud routines push commits, so last commit age shows activity
PROJ_DIR="${CCF_PROJECT_DIR:-$(pwd)}"
LAST=$(git -C "$PROJ_DIR" log --oneline -1 --format="%ar — %s" 2>/dev/null || echo "unknown")
LAST_2H=$(git -C "$PROJ_DIR" log --oneline --since='2 hours ago' 2>/dev/null | wc -l | tr -d ' ')

echo "  Status: $([ "${LAST_2H:-0}" -gt 0 ] && echo '● ACTIVE' || echo '○ DORMANT') (${LAST_2H:-0} commits in last 2h)"
echo "  Last activity: $LAST"
echo ""
echo "  → Run: RemoteTrigger list   for the registered routines + live last_fired_at timestamps"
```

---

### Output format

For full `/status` run all sections with headers. Keep each section under 20 lines.
Color coding:
- **Green** = running / active / passing
- **Yellow** = warning / unknown / dormant
- **Red** = failed / broken
- **Cyan** = skills / commands
- **Magenta** = plugins
- **Grey** = inactive / installed-but-not-running

At the end of every full `/status` run, print:
```
─────────────────────────────────────────────
TIP: Statusline shows live counts. Use /status <section> to drill in.
     /status agents | loops | skills | plugins | rules | ci | cloud
─────────────────────────────────────────────
```
