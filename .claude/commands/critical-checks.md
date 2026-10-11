---
description: Universal pre-commit safety audit that scans the diff for secrets, broken syntax, and auth mistakes and blocks the commit on any [CRITICAL] finding.
---
# /critical-checks — Universal Pre-Commit Safety Audit

Runs layered critical checks across any codebase before committing, deploying, or merging.
Covers the failure patterns that have actually hurt this workspace.

Use after making any cross-cutting change (adding icons, replacing strings, touching YAML,
adding endpoints). Takes ~30 seconds. Blocks ship if any `[CRITICAL]` fires.

## Usage
```
/critical-checks            # run all checks in current directory
/critical-checks --fast     # JS + Python syntax only (< 5 seconds)
/critical-checks --security # secrets + auth + injection only
/critical-checks --frontend # JS/CSS/HTML checks only
/critical-checks --backend  # Python + YAML + API checks only
```

## Instructions for Claude

Run each block in order. Any `[CRITICAL]` tag → **stop and fix before proceeding**.
Any `[WARN]` → log and fix in the next iteration; never block ship for warnings alone.

---

### Block 1 — JavaScript Safety

```bash
echo "=== JS SAFETY ==="

# 1a. SVG/HTML in double-quoted JS strings → silent syntax error
# Root cause: emoji→SVG replacement breaks extension when viewBox="..." is
# inside a "..." string. Chrome swallows the error; feature silently disappears.
BAD=$(grep -rn '"<svg\|"<\/svg\|"<img\|"<div\|"<span' \
  extension/ src/ static/ *.js 2>/dev/null | grep -v "\.css\|\.md" | wc -l | tr -d ' ')
[ "$BAD" -eq 0 ] && echo "  [OK] No HTML/SVG in double-quoted strings" || \
  echo "  [CRITICAL] HTML/SVG inside double-quoted JS strings ($BAD lines) — use template literals or single quotes"
grep -rn '"<svg\|"<img\|"<div' extension/ src/ *.js 2>/dev/null | head -3

# 1b. textContent used to set HTML (strips tags silently — use innerHTML)
grep -rn '\.textContent\s*=\s*.*<[a-zA-Z]' \
  extension/ src/ *.js 2>/dev/null | grep -v "//\|test\|spec" | head -3 && \
  echo "  [WARN] textContent used for HTML — should be innerHTML"

# 1c. Missing await on async calls (returns Promise instead of resolved value)
grep -rn 'const .* = fetch(\|const .* = chrome\.\|const .* = browser\.' \
  extension/ src/ *.js 2>/dev/null | grep -v 'await\|\.then\|//\|test' | head -3 && \
  echo "  [WARN] fetch/async call without await or .then"

# 1d. querySelector without null check (crashes when element missing)
grep -rn '\.querySelector(.*)\.' extension/ src/ *.js 2>/dev/null | \
  grep -v "if \|&&\|??\|\|\|\|//\|test" | head -5 && \
  echo "  querySelector null-safety: spot-checked"

# 1e. console.log left in production code
COUNT=$(grep -rn 'console\.log(' extension/ src/ *.js 2>/dev/null | grep -v "//\|test\|spec\|debug" | wc -l | tr -d ' ')
[ "$COUNT" -eq 0 ] && echo "  [OK] No console.log in production JS" || \
  echo "  [WARN] $COUNT console.log statements in production code"
```

---

### Block 2 — Python Safety

```bash
echo "=== PYTHON SAFETY ==="

# 2a. Syntax check on all changed Python files
git diff --name-only HEAD 2>/dev/null | grep '\.py$' | while read F; do
  python3 -m py_compile "$F" 2>/dev/null && echo "  [OK] $F" || echo "  [CRITICAL] Syntax error: $F"
done
# Also always check the main server file if it exists
for F in api/server.py app.py main.py server.py; do
  [ -f "$F" ] && python3 -m py_compile "$F" 2>/dev/null && echo "  [OK] $F" || \
    echo "  [CRITICAL] Syntax error in $F"
done

# 2b. f-strings in YAML/SQL/HTML templates (braces get misinterpreted)
grep -rn 'f""".*{.*}.*"""' *.py api/ app/ 2>/dev/null | \
  grep -i "yaml\|sql\|html\|template\|query" | head -3 && \
  echo "  [WARN] f-string inside YAML/SQL/HTML template — use .format() or %"

# 2c. Mutable default arguments (classic Python gotcha)
grep -rn 'def .*=\[\]\|def .*={}\|def .*=()' *.py api/ app/ 2>/dev/null | \
  grep -v "//\|#\|test" | head -3 && \
  echo "  [WARN] Mutable default argument in function signature"

# 2d. Bare except (swallows all errors silently, hides bugs)
grep -rn '^\s*except:\s*$\|except Exception:\s*pass' *.py api/ app/ 2>/dev/null | \
  grep -v "test\|#" | head -5 && \
  echo "  bare-except check: done"

# 2e. print() in production server code (use logging)
grep -rn '^\s*print(' api/server.py app/server.py 2>/dev/null | \
  grep -v "#\|test" | head -3 && echo "  print() check: done"
```

---

### Block 3 — YAML/CI Safety

```bash
echo "=== YAML SAFETY ==="

# 3a. Workflow YAML validity
# Root cause: multi-line Python inside YAML run: block caused 'workflow file issue' in CI
python3 -c "
import yaml, glob, sys
errs = []
for f in glob.glob('.github/workflows/*.yml') + glob.glob('.github/workflows/*.yaml'):
    try:
        yaml.safe_load(open(f).read())
    except yaml.YAMLError as e:
        errs.append(f'{f}: {str(e)[:120]}')
if errs:
    for e in errs: print(f'  [CRITICAL] YAML broken: {e}')
    sys.exit(1)
else:
    n = len(glob.glob('.github/workflows/*.yml') + glob.glob('.github/workflows/*.yaml'))
    print(f'  [OK] All {n} workflow YAMLs valid')
" 2>/dev/null || echo "  [OK] No workflows found"

# 3b. Multi-line shell code inside YAML that might confuse the parser
grep -rn 'python3\s*-c\s*"' .github/workflows/ 2>/dev/null | head -3 && \
  echo "  [WARN] Inline Python (-c \"...\") in YAML — prefer heredoc or script file"

# 3c. Required secrets referenced but potentially missing
grep -rn '\${{ secrets\.' .github/workflows/ 2>/dev/null | \
  grep -oP 'secrets\.\K[A-Z_]+' | sort -u | while read S; do
    echo "  Secret used: $S (verify it's set in GitHub → Settings → Secrets)"
  done

# 3d. workflow_dispatch silent-skip trap
# If a workflow declares `workflow_dispatch` but a job's `if:` checks only a specific
# event object (e.g. deployment_status.state == 'success'), manual dispatch silently
# skips the job — a hard-to-spot gap where "manual runs still work" is false.
python3 -c "
import glob, re
warn = []
for f in glob.glob('.github/workflows/*.yml') + glob.glob('.github/workflows/*.yaml'):
    txt = open(f).read()
    if 'workflow_dispatch' not in txt:
        continue
    for m in re.finditer(r'^\s+if:\s+(.+)$', txt, re.MULTILINE):
        cond = m.group(1)
        if re.search(r'github\.event\.(deployment|release|pull_request|push)', cond) and \
           'workflow_dispatch' not in cond and 'event_name' not in cond:
            warn.append(f'{f}: job if-condition blocks dispatch — add: || github.event_name == \"workflow_dispatch\"')
if warn:
    for w in warn: print(f'  [WARN] {w}')
else:
    print('  [OK] No workflow_dispatch silent-skip traps found')
" 2>/dev/null || echo "  [OK] No workflows to check"
```

---

### Block 4 — Security Checks

> **Item source:** these pre-commit spot-checks are the **static P0 subset** of the
> framework's 20-item pre-launch bar — `/security-checklist` (SSOT in
> `commands/security-checklist.md`, run the fast subset with `/security-checklist --fast`).
> The full 20-item bar (incl. live headers/HTTPS/cookies/RLS/IDOR/PoW-enforced-in-prod)
> runs in `/self-improve`'s Security step and the daily `<project>-security-audit` routine.
> Keep the two in sync — when an item is added here, add it there (do not fork the list).

```bash
echo "=== SECURITY ==="

# 4a. Secrets accidentally committed
# Patterns that indicate real keys (not placeholders)
git log --oneline -5 --patch 2>/dev/null | \
  grep -iE '(api_key|token|secret|password|private_key)\s*=\s*["\x27][a-zA-Z0-9_\-]{16,}' | \
  grep -iv "environ|getenv|test|mock|example|your.key|placeholder|cf-demo|dummy|fake" | \
  head -3 && echo "  secrets check done"

# 4b. Mutation endpoints without auth (POST/PUT/PATCH/DELETE with no auth dependency)
# Only checks FastAPI-style — adapt for other frameworks
if [ -f "api/server.py" ] || [ -f "app.py" ]; then
  grep -n "^@app\.\(post\|put\|patch\|delete\)" api/server.py app.py 2>/dev/null | \
    grep -v "require_auth\|require_admin\|admin_only\|auth_dep\|signup\|webhook\|demo\|public" | \
    head -5 && echo "  mutation auth: spot-checked"
fi

# 4c. SQL injection risk (string interpolation into SQL)
grep -rn "execute(.*%\|execute(.*format(\|execute(.*f\"" \
  *.py api/ app/ 2>/dev/null | grep -v "#\|test" | head -3 && \
  echo "  SQL injection: spot-checked"

# 4d. User input directly in shell command (command injection)
grep -rn "os\.system(\|subprocess.*shell=True" \
  *.py api/ app/ 2>/dev/null | grep -v "#\|test" | head -3 && \
  echo "  shell injection: spot-checked"

# 4e. Hardcoded credentials (localhost is OK, real keys are not)
grep -rn "password\s*=\s*[\"'][^\"']\{6,\}[\"']\|api_key\s*=\s*[\"'][^\"']\{10,\}[\"']" \
  *.py api/ app/ 2>/dev/null | \
  grep -iv "environ|getenv|test|placeholder|example|your|cf-demo|dummy" | head -3 && \
  echo "  hardcoded creds: spot-checked"

# 4f. Security controls that fail OPEN (bare-except silences a security gate — GAP-028)
# A security initialization (sandbox, access restriction, auth gate) wrapped in except: pass
# is a fails-open vulnerability: if the guard throws at runtime the protected operation runs
# unguarded with no log or observable signal.
python3 -c "
import re, glob
SECURITY_WORDS = ['sandbox', 'external_access', 'restrict', 'permission', 'privilege', 'security', 'auth']
issues = []
for f in glob.glob('**/*.py', recursive=True):
    if any(x in f for x in ['test', '.git', 'venv', 'node_modules']): continue
    try: lines = open(f).readlines()
    except: continue
    for i, line in enumerate(lines):
        s = line.strip()
        if re.match(r'except(\s+\w[\w.]*(\s+as\s+\w+)?)?\s*:', s):
            if i + 1 < len(lines) and lines[i+1].strip() == 'pass':
                ctx = ''.join(lines[max(0,i-10):i]).lower()
                if any(w in ctx for w in SECURITY_WORDS):
                    issues.append(f'{f}:{i+1}')
if issues:
    print('  [WARN] bare-except near security setup — may fail OPEN (see GAP-028):')
    for iss in issues[:3]: print(f'    {iss}')
else:
    print('  [OK] No bare-except found near security initialization')
" 2>/dev/null || echo "  fail-closed check: skipped (no Python)"

# 4g. Agent/editor hook injection (supply-chain compromise indicator)
# A supply-chain worm can inject pipe-to-shell commands into .claude/settings.json,
# .claude/settings.local.json, or .vscode/tasks.json to exfiltrate credentials on
# agent startup or editor task run. These files must never contain curl|sh, wget|bash,
# base64-d|sh, or eval-fetch patterns. (Ref: npm supply-chain worm class, 2026-08-04.)
python3 -c "
import re
from pathlib import Path

HOOK_DANGER = re.compile(
    r'(curl|wget)\b[^\n|]*\|\s*(sh|bash|node|bun)'
    r'|node\s+setup\.mjs|bun\s+\w*\s*setup'
    r'|base64\s+-d[^\n|]*\|\s*(sh|bash|node)'
    r'|eval\s*\(.*(fetch|require\(.https?)',
    re.I,
)
AGENT_HOOKS = [
    '.claude/settings.json', '.claude/settings.local.json',
    '.vscode/tasks.json', '.vscode/settings.json',
]
issues = []
for path in AGENT_HOOKS:
    p = Path(path)
    if not p.exists(): continue
    try:
        text = p.read_text(encoding='utf-8', errors='ignore')
        for m in HOOK_DANGER.finditer(text):
            issues.append(f'{path}: suspicious command — {m.group(0)[:80]}')
    except OSError:
        pass
if issues:
    print('  [CRITICAL] Agent/editor hook injection detected:')
    for iss in issues: print(f'    {iss}')
    print('  If unexpected: do NOT run; rotate all tokens/credentials; review git log --all')
else:
    print('  [OK] No pipe-to-shell commands found in agent/editor hook configs')
" 2>/dev/null || echo "  hook injection check: skipped (no Python)"
```

---

### Block 5 — Frontend / Extension Safety

```bash
echo "=== FRONTEND ==="

# 5a. Extension manifest.json validity
[ -f "extension/manifest.json" ] && python3 -c "
import json
try:
    m = json.load(open('extension/manifest.json'))
    cs = len(m.get('content_scripts', []))
    perms = m.get('permissions', [])
    print(f'  [OK] manifest.json valid — content_scripts={cs}')
except Exception as e:
    print(f'  [CRITICAL] manifest.json broken: {e}')
" 2>/dev/null

# 5b. CSS color-scheme not set → blank popup in dark mode (real past incident)
if [ -f "extension/popup.html" ]; then
  grep -q "color-scheme.*light" extension/popup.html && echo "  [OK] popup color-scheme: light" || \
    echo "  [WARN] popup.html missing color-scheme:light — may be blank on macOS dark mode"
fi

# 5c. Missing aria-label on icon-only buttons (across HTML/JS files that render buttons)
grep -rn '<button[^>]*>' extension/ src/ static/ templates/ *.html 2>/dev/null | \
  grep -v "aria-label\|aria-hidden\|title\|>.\{2,\}<" | \
  grep -v "//\|class=\"tab\"\|class=\"btn" | head -5 && \
  echo "  aria-label check: done"

# 5d. Viewport meta tag on every HTML page
# Guard on a non-empty file list — without it, an empty $(...) makes grep -rL
# default to scanning the whole cwd and flag every non-HTML file (false WARN flood
# on backend/library/CLI projects with no HTML).
_HEADFILES=$(grep -rl "<head" extension/ src/ static/ templates/ *.html 2>/dev/null)
[ -n "$_HEADFILES" ] && grep -rLn "viewport" $_HEADFILES 2>/dev/null | \
  while read F; do echo "  [WARN] Missing viewport meta in $F"; done
echo "  viewport check: done"

# 5e. Content Security Policy — does it allow unsafe-inline for scripts?
grep -rn "script-src.*unsafe-inline\|script-src.*'none'" \
  *.py api/ app/ src/ static/ templates/ 2>/dev/null | head -3 && echo "  CSP check: done"
```

---

### Block 6 — Test Quality Gates

```bash
echo "=== TEST QUALITY ==="
source .ccf/project.env 2>/dev/null || true

# 6a. Run the test suite via the project's configured test command (skip if none).
#     CCF_TEST_CMD is set per-project in .ccf/project.env (e.g. a fast test invocation).
if [ -n "${CCF_TEST_CMD:-}" ]; then
  eval "$CCF_TEST_CMD" 2>&1 | tail -3
else
  echo "  [OK] No test command configured (CCF_TEST_CMD empty) — skipping test run"
fi

# 6b. No growing list of skipped/xfailed tests (they rot).
#     Generic capability check; self-disables unless a Python test runner is present.
PYTEST_PROJECT=0
[ -f "pyproject.toml" -o -f "pytest.ini" -o -f "setup.cfg" ] && PYTEST_PROJECT=1
if [ -d tests ] && command -v python3 >/dev/null 2>&1 && [ "$PYTEST_PROJECT" -eq 1 ]; then
  SKIPS=$(python3 -m pytest tests/ --collect-only -q 2>/dev/null | grep -c "SKIP\|xfail" || echo 0)
  [ "$SKIPS" -lt 100 ] && echo "  [OK] $SKIPS skipped/xfail tests (within limit)" || \
    echo "  [WARN] $SKIPS skipped tests — review if intentional"
fi

# 6c. No tests with hardcoded absolute paths (breaks CI on other machines)
grep -rnE '"/(Users|home)/[a-zA-Z0-9_]+/' tests/ 2>/dev/null | grep -v "#" | head -5 && \
  echo "  absolute paths in tests: check done"

# 6d. Test files have at least one assertion (tests that always pass hide bugs)
for F in tests/test_*.py; do
  [ -f "$F" ] || continue
  COUNT=$(grep -c "assert\s\|assertEqual\|assertTrue\|assertRaises" "$F" 2>/dev/null || echo 0)
  [ "$COUNT" -eq 0 ] && echo "  [WARN] $F has no assertions — may always pass"
done
echo "  assertion presence: checked"

# 6e. Destructive CLI probe without effecting-layer severing (GAP-040)
# Flags test files whose string literals in callable arguments contain destructive
# flags/subcommands. AST-based to avoid false positives from prose/docstrings/f-strings.
python3 - <<'PYEOF' 2>/dev/null || echo "  destructive-probe check: skipped (no Python)"
import ast, re, sys
from pathlib import Path

DESTRUCTIVE = re.compile(
    r"(--force\b|--yes\b|--apply\b|--prune\b|(?<!\w)-D\b|"
    r"\brm\s+-rf?\b|worktree\s+remove\b|branch\s+-D\b|"
    r"reset\s+--hard\b|push\s+--force\b|clean\s+-[a-z]*f)")

def string_args_in_calls(tree):
    """Yield string literals that appear as direct or list/tuple/set elements in a Call."""
    for node in ast.walk(tree):
        if not isinstance(node, ast.Call):
            continue
        for arg in node.args:
            if isinstance(arg, ast.Constant) and isinstance(arg.value, str):
                yield arg.value
            elif isinstance(arg, (ast.List, ast.Tuple, ast.Set)):
                for elt in arg.elts:
                    if isinstance(elt, ast.Constant) and isinstance(elt.value, str):
                        yield elt.value

issues = []
for p in Path("tests").glob("test_*.py") if Path("tests").exists() else []:
    try:
        tree = ast.parse(p.read_text())
    except SyntaxError:
        continue
    joined = " ".join(string_args_in_calls(tree))
    if DESTRUCTIVE.search(joined):
        issues.append(str(p))

if issues:
    print("  [WARN] Test files with destructive CLI args in callable arguments (GAP-040):")
    print("         Confirm each has its effecting layer monkeypatched/severed:")
    for f in issues[:5]:
        print(f"    {f}")
else:
    print("  [OK] No unsevered destructive CLI probes detected in tests/")
PYEOF
```

---

### Block 7 — Spec-Kit Compliance (for new features)

Before implementing any M/L/XL item from the backlog:
```bash
# Check if this change warrants a spec first
# Rule: any change touching >3 files OR adding a new user-facing flow needs a spec

CHANGED=$(git diff --name-only HEAD 2>/dev/null | wc -l | tr -d ' ')
if [ "$CHANGED" -gt 5 ]; then
  echo "  [WARN] $CHANGED files changed — consider /speckit.specify before proceeding"
  echo "         Large changes without a spec often cause scope creep and regressions"
else
  echo "  [OK] Change scope is focused ($CHANGED files)"
fi
```

---

### Severity Tiers

| Tag | Meaning | Action |
|---|---|---|
| `[CRITICAL]` | Will break production or security | Fix NOW, block all work |
| `[WARN]` | Will cause bugs or confusion | Fix in next iteration |
| `[OK]` | Check passed | Continue |

### Historical incidents these checks prevent

| Check | Incident prevented |
|---|---|
| SVG in double-quoted JS strings | An injected toolbar button silently vanished from the page |
| YAML syntax (inline Python) | A CI workflow failed every run with "workflow file issue" |
| `color-scheme: light` in popup | An extension popup was completely blank on macOS dark mode |
| YAML in YAML run blocks | Embedded Python with f-strings broke CI workflow parsing |
| Hardcoded paths in tests | 100% CI failure rate until paths were made relative |
| Missing auth on endpoints | Unauthenticated users reaching gated features |
| `textContent` for HTML | Icon SVGs rendered as literal text in UI |
| `workflow_dispatch` silent-skip | Security-audit job never ran on manual dispatch — job `if:` only allowed the deployment event |

### Integration with spec-kit and skills

- Run `/critical-checks` at the **start** of any `/speckit.implement` step
- If `[CRITICAL]` fires, the spec step is paused — fix it first
- After fixing, re-run `/critical-checks --fast` to verify before continuing
- `/security-review` (when available) runs a deeper OWASP pass — use before deploys
- `/qa` runs end-to-end after deploy — use after `/critical-checks` passes locally

## Docs-sync check

Before any commit: if the diff touches user-facing flows (API routes, extension/, web UI,
CI/dev workflow) and no .md changed, flag `[WARN] docs-sync` — either update
README.md / docs/ in the same push or state why no doc is affected.
A doc that contradicts shipped behavior is a coherence bug.
