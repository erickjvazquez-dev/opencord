---
description: Tiered contained sandbox for running security probes and experiments and capturing lab results safely.
---
# /lab — Tiered Contained Sandbox (security probes + experiments + lab results)

A project-agnostic isolated sandbox for running security probes, adversarial tests,
and risky experiments WITHOUT touching production. Picks the **lowest tier that safely
contains the blast radius** and writes a `lab-results.md` report.

Integrates with `.ccf/project.env` and optional `.ccf/hooks/lab-probes.sh`.

## Usage
```
/lab                        # tier-0 functional smoke (worktree + throwaway process)
/lab --security             # tier-1 adversarial battery (Docker, locked bridge)
/lab --hijack               # tier-2 exfil/escape probes (Docker, --network none)
/lab --change <files>       # auto-pick tier by change surface, then run
/lab --full                 # all tiers in order, emit lab-results.md
```

## Tier model — lowest tier that safely contains the experiment

### Tier 0 — Worktree + throwaway local process (functional / regression)
- **When:** new features, fixes, regressions, gate coherence checks. The 95% case.
- **Isolation:** `git worktree add` + local server/CLI on a throwaway port, with env
  scrubbed (no prod API keys) and data dir pointed at `$(mktemp -d)`.
- **Verdict source:** `CCF_TEST_CMD` + `CCF_SMOKE_CMD`, both green → WORKS ✓.

### Tier 1 — Docker, locked bridge (adversarial input battery)
- **When:** change touches an attack surface (file input, SQL, auth, network, CLI args).
- **Isolation:** existing `Dockerfile` built from source, run with:
  `--env-file <synthetic-creds> --read-only --tmpfs /data --cap-drop ALL
   --security-opt no-new-privileges --memory 512m --pids-limit 256
   -p 127.0.0.1:<port>:<port>` — no route to prod hosts or the operator's LAN.
- **Typical probes:** SQL/query injection, path traversal, auth bypass,
  oversized/unicode/malformed payloads, prompt-injection with LLM paths stubbed.

### Tier 2 — Docker, `--network none` (exfil / hijack / escape)
- **When:** exfiltration, SSRF, credential-theft, container-escape, supply-chain probes.
- **Isolation:** Tier-1 + `--network none` — no network interface at all. An exfil
  "success" can only write to the throwaway tmpfs, which is destroyed on teardown.
- **Typical probes:** SSRF (network none proves it fails at kernel level), credential
  theft (synthetic canaries only — asserted at start), RCE-via-processor, dep beaconing.

---

## Pre-flight — fail CLOSED if ANY check fails

Run this block before ANY Tier-1 / Tier-2 probe. If a check fails, write
`UNSAFE ⚠ — containment not established` to lab-results and **refuse to run the probe**.

```bash
source .ccf/project.env 2>/dev/null || true

# 1. No prod credentials in the container/process env
#    List the key shapes this project uses (from .ccf/project.env or CLAUDE.md).
#    Any real key present → abort.
env | grep -iE "(API_KEY|SECRET|TOKEN|PASSWORD)" | grep -v "FAKE_\|TEST_\|LAB_\|SYNTHETIC" && \
  { echo "LAB PRE-FLIGHT FAIL: prod credential detected — abort"; exit 1; } || true

# 2. Data dir is throwaway, not the real one
DATA_DIR="${CCF_LAB_DATA_DIR:-}"
[ -z "$DATA_DIR" ] && DATA_DIR=$(mktemp -d)
echo "$DATA_DIR" | grep -qE "^/tmp|^/var/folders|mktemp" || \
  { echo "LAB PRE-FLIGHT FAIL: data dir is not throwaway — abort"; exit 1; }

# 3. Checkout is NOT the main branch / production ref
BRANCH=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "")
[ "$BRANCH" = "main" ] || [ "$BRANCH" = "master" ] && \
  { echo "LAB PRE-FLIGHT FAIL: running on $BRANCH — use a worktree"; exit 1; } || true

# 4. For Tier-2: verify --network none took effect (canary host must be unreachable)
if [ "${LAB_TIER:-0}" = "2" ]; then
  curl -sf --max-time 2 https://api.ipify.org >/dev/null 2>&1 && \
    { echo "LAB PRE-FLIGHT FAIL: network is reachable on Tier-2 — abort"; exit 1; } || \
    echo "Tier-2 network isolation: confirmed"
fi

echo "Pre-flight passed"
```

---

## Verdict classification

| Verdict | Meaning | Lab-results action |
|---|---|---|
| `WORKS ✓` | All assertions green | Feature can proceed to ship gate |
| `FAILS ✗` | Functional failure or broken assertion | Block ship; list failing node |
| `UNSAFE ⚠` | Exploit reproduced inside containment | Graduate to GOAL.md Blockers P0 |
| `INCONCLUSIVE` | Probe matched 0 targets or auth error mid-run | Fix the probe; NOT a pass |

**INCONCLUSIVE is never success.** A security probe that found 0 routes or 0 matching
lines is broken, not green — same discipline as `selector-matched-nothing-reads-as-pass`
and `grep-absence-needs-live-artifact-check` in the QA harness.

---

## `lab-results.md` — the report

Write to `lab-results.md` at the project root (or `reports/lab-results.md` if a
`reports/` directory exists). Append a row per experiment; never truncate.

```markdown
# Lab Results

| id | kind | tier | verdict | finding | evidence | next action |
|---|---|---|---|---|---|---|
| LAB-0003 | security | T1 | UNSAFE ⚠ | path traversal reaches parent dir | lab/repro/LAB-0003.sh | graduate to GOAL Blockers |
| LAB-0002 | feature | T0 | WORKS ✓ | new auth flow passes gate | lab/logs/LAB-0002/ | open PR |
| LAB-0001 | hijack | T2 | WORKS ✓ | SSRF blocked: --network none | lab/logs/LAB-0001/ | add regression test |
```

- **kind:** `feature` | `fix` | `security` | `hijack` | `load`
- **evidence:** a rerunnable script or log directory — not a prose summary
- **Graduation:** an `UNSAFE ⚠` row becomes a GOAL.md Blocker P0; a compelling
  `WORKS ✓` becomes a PR. Once graduated, remove the row from lab-results (it drains).

---

## Credit-safe LLM stubs

Before any probe that touches an LLM-backed path: assert the path is stubbed with a
deterministic fake (e.g. a mock server returning canned JSON). A probe that actually
calls a metered LLM is a credit leak. Assert at lab startup:

```bash
# Confirm LLM calls are stubbed or the env key is a synthetic fake
echo "${ANTHROPIC_API_KEY:-}" | grep -qE "^sk-ant-" && \
  { echo "LAB: real Anthropic key in env — stub the LLM path first"; exit 1; } || \
  echo "LLM stub check: OK"
```

---

## Loop integration + cadence

- **On risky change** — a diff touching an attack surface (file input, SQL, auth, file
  paths, external calls) auto-escalates through Tier-1 before ship.
- **Every-Nth tick** — a full Rule-15 battery + Tier-2 sweep on a cadence (e.g. every
  10 ticks or weekly), not per tick. Charge guard: `/lab --full` fires only when
  `$CCF_LAB_SWEEP_ENABLED` is set in `.ccf/project.env`.
- **On demand** — invoke `/lab` with a specific tier + probe script at any time.

Configure in `.ccf/project.env`:
```bash
CCF_LAB_SWEEP_ENABLED=1           # allow full Tier-2 sweeps on cadence
CCF_LAB_DATA_DIR=/tmp/lab-data    # override throwaway data dir
CCF_LAB_IMAGE="${CCF_PROJECT_NAME:-app}:lab"  # Docker image tag for T1/T2
```

Provide project-specific probes in `.ccf/hooks/lab-probes.sh` — called after
pre-flight passes, before writing lab-results. The hook receives `LAB_TIER` (0/1/2)
and `LAB_DATA_DIR` as env vars; it should exit non-zero to record `FAILS ✗` / `UNSAFE ⚠`.

---

## Integration with existing commands

- `/critical-checks --security` runs a static security audit (no isolation required).
  Run it first; use `/lab` only for the probes that need a live target.
- `.ccf/hooks/security.sh` in `/self-improve` runs on each tick; for destructive probes
  that hook should invoke `/lab --security` rather than hitting the real instance.
- A `FAILS ✗` or `UNSAFE ⚠` result in lab-results **blocks the ship gate** in
  `/self-improve` for that change. A `WORKS ✓` is a green light only for the
  worktree it was run in — the main ship gate (tests + pre-push hook) still runs.
