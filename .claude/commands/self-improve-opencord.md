# /self-improve-opencord — Autonomous Self-Improvement Loop (Opencord only)

Local, self-paced self-improvement loop scoped to **Opencord and nothing else**.
Config-driven via `.ccf/project.env`. Mirrors the Claude Code Framework
self-improve structure (health → QA → improve → ship), but is fully isolated:
it NEVER touches ContextForge (its repo, its Railway project, its CI) or any other
repo. Opencord has its OWN Railway deploy (project `talented-curiosity`, service
`opencord`, `CCF_LIVE_URL`) that this loop ships to via `CCF_DEPLOY_CMD`
(`railway up` — a `git push` alone does NOT deploy) and then verifies — distinct
from ContextForge's, which stays off-limits.

## Scope guard — read FIRST, every tick (hard stop if violated)

- Work ONLY inside `CCF_PROJECT_DIR` (`~/Opencord`). Load config:
  `cd ~/Opencord && set -a && . .ccf/project.env && set +a`.
- If `CCF_PROJECT_NAME` != `opencord`, **STOP immediately** — wrong project.
- Do NOT `cd ~/contextforge`, read its deploy/CI state, run `pytest`, hit **its**
  Railway URL, or push to any repo other than `origin` of this one. Opencord's own
  `CCF_LIVE_URL` (the `opencord-production-*.up.railway.app` deploy) IS in scope —
  curl it to verify the deploy; just never touch ContextForge's. Use the Homebrew
  toolchain: `export PATH="/opt/homebrew/bin:$PATH"`.

## Each tick

0. **Backoff guard** — if the previous tick hit a transient AI/API error, widen
   the next sleep instead of hammering.

1. **Build / health gate (fix-first).**
   - `export PATH="/opt/homebrew/bin:$PATH"; cd "$CCF_PROJECT_DIR"`
   - `go build ./...` · `go vet ./...` · `eval "$CCF_TEST_CMD"` (go test).
   - If `CCF_LIVE_URL` is non-empty, curl `"${CCF_LIVE_URL}${CCF_HEALTH_PATH}"`
     (blank here → skip live health).
   - Any red = a P0. Fix it THIS tick before doing anything else.
   - **Self-heal docker before declaring it "down" (iter 271).** This machine has
     NO Docker Desktop — the daemon is provided by **colima**. If `docker info`
     fails, run **`colima start`** ONCE (the runtime is installed; it takes ~30–90s)
     and re-check, BEFORE concluding browser QA / DB integration is blocked. Leave
     colima running for subsequent ticks. Only after a failed `colima start` is
     docker genuinely "down" for the tick — then fall back to zero-stack Track-0
     (the pure-module coverage veins). Don't let "docker=DOWN" stand unchallenged.

2. **Track 0 — QA (do this MOST): coverage + real-UI QA + AI manual test.** The
   loop must exercise the product the way a *user* does, not just `go test`.
   - **Autonomous test coverage:** pick ONE real gap and close it — an untested
     endpoint, a WS edge case (reconnect, oversized/garbage frame), a Rule-15
     adversarial probe (auth bypass, JWT tampering, injection, oversized body), or
     extend the DB integration suite. Two-client WS flows count.
   - **Browser QA (run when any UI surface changed this tick, else every 3rd
     tick):** run `bash qa/run.sh` (= `make qa-browser`). It boots a dev stack and
     drives the REAL rendered UI with Playwright — register → send → avatar → edit
     → create/switch channel → delete — **logging every click** and screenshotting
     each step into `qa/qa-screenshots/`. A non-zero exit is a **P0**: a feature is
     broken in the actual UI (which the curl/WS E2E can't catch). Fix it this tick.
   - **AI manual test (vision):** after browser QA, **`Read` each screenshot in
     `qa/qa-screenshots/`** and judge it like a human tester — layout right? text
     readable? avatars / reaction chips / typing line visible and not
     overlapping/clipped? does it look like a polished chat app? Log every visual
     issue as a GOAL.md **P0** (broken) / **P1** (polish). Use your own
     Max-subscription vision — never a metered API.
   - **Grow the QA itself (Step 5b for the UI):** each tick, add at least one new
     `qa/browser.mjs` assertion/flow covering whatever you just shipped, so the
     browser QA always exercises the newest feature.

3. **Track 1/2 — improve.** Advance the highest unchecked item in `GOAL.md`
   "## Now", then "## Next". Spec-first (append to `SPEC.md`) for any >3-file or
   new-user-facing-flow change (Rule 6). Build the minimal thing; for new input
   surfaces do an adversarial pass (Rule 15).

   **Per-component excellence (owner-set 2026-06-14).** Don't stop at "the feature
   exists" — drive **each component toward its own north star** (see GOAL.md
   "Non-negotiables" + each component's NORTH STAR note), continuously and in
   rotation so no component stagnates: **audio** → thousands/HD/no-drops/free (the
   path is mesh → OSS SFU like LiveKit with active-speaker selection → cascaded SFUs;
   adopting an SFU runs `stack-guardian` first, Rule 16, and must stay free to
   self-host); **chat** → instant + lossless realtime; **infra** → one-command +
   scales; **UI** → polished/fast/accessible; **security** → hostile-input-proof.
   Each tick, prefer the component furthest from its north star (or the one the owner
   just flagged). Use the best open-source tech; never adopt anything that isn't free
   to self-host. Record which component you advanced (and toward what bar) in Step 6.5.

4. **Verify before "done" (Rule 14).** `go build`/`vet`/`test` green is the floor.
   For user-facing changes, boot the stack (`docker compose up -d db` + run the
   server) and exercise it for real (e.g. the two-client WebSocket check) — never
   claim done on green tests alone. Tear the stack back down.

5. **Ship + confirm the deploy.** If something real changed: ONE surgical
   Conventional-Commits commit, then `git push origin main` (source control), then
   **deploy to Railway by running `CCF_DEPLOY_CMD`** (`railway up --service
   opencord --ci`). **A `git push` alone does NOT deploy** — Railway is NOT wired
   to GitHub here; `railway up` uploads the dir + builds the Dockerfile + rolls out
   (owner directive 2026-06-14: "always push it to railway"). Then **verify the
   rollout**: after the deploy finishes, fetch the live SPA bundle and confirm the
   new code is actually serving (e.g. `curl -s "$CCF_LIVE_URL/" | grep -o
   'assets/index-[^"]*\.js'` → fetch it → grep for a string from what you just
   shipped), not just that `/healthz` is 200 (the OLD build also returns 200). A
   deploy that builds but doesn't serve the new code, or doesn't come up healthy,
   is a **P0** for next tick. If nothing changed, log green, make **no commit**,
   and do **not** deploy (anti-churn, Rule 10).

6. **Heartbeat (always, even on a green no-op tick)** — so the statusline shows
   this loop as `/self-improve opencord`, distinct from other projects' loops:
   ```bash
   mkdir -p ~/.claude/loops
   python3 - "$SHIPPED" "$OPEN_P0" <<'PY'
   import json, os, sys, time
   shipped = (sys.argv[1] if len(sys.argv) > 1 else "0") == "1"
   open_p0 = (sys.argv[2] if len(sys.argv) > 2 else "0") == "1"
   p = os.path.expanduser("~/.claude/loops/opencord.json")
   try: d = json.load(open(p))
   except Exception: d = {"count": 0, "idle_streak": 0}
   d["label"] = "/self-improve opencord"
   d["last_at"] = time.time()
   d["count"] = d.get("count", 0) + 1
   d["idle_streak"] = 0 if (shipped or open_p0) else d.get("idle_streak", 0) + 1
   json.dump(d, open(p, "w"))
   print("idle_streak", d["idle_streak"])
   PY
   ```
   Write ONLY this file — never the global `~/.claude/loop-iterations.json` /
   `loop-state.json` (those belong to other projects' loops).

6.5. **Reflect — what can this loop improve? (every tick).** Look at THIS tick's
   output, the QA results, and the loop's own rules, then log the ONE highest-value
   improvement to `qa/IMPROVEMENTS.md` (create if missing; date each entry) — and
   open a GOAL.md item when it's actionable:
   - **QA gaps:** a flow the browser QA doesn't click yet · a shipped feature with
     no regression test · an interaction that needs AI-vision review.
   - **Loop-process gaps:** a rule that misfired this tick · a verification you
     skipped · a step that should be automated. *Improve the rule, don't just note
     it.*
   - **Coverage:** which part of the product is least tested? Target it next tick.
   One item, highest value — this is the loop improving itself, per the owner ask.

7. **Self-pace (the loop).** This runs under `/loop` dynamic mode: set the next
   `ScheduleWakeup` from the idle-backoff ladder, re-firing prompt
   `/self-improve-opencord`:
   | tick outcome | next sleep |
   |---|---|
   | shipped a fix, OR an unchecked P0/P1 remains in GOAL.md | 1800s (~30m) |
   | 1 consecutive green tick | 2700s (~45m) |
   | 2+ consecutive green ticks | 3600s (~60m, ceiling) |

## Never

- Touch ContextForge or any repo other than `~/Opencord`.
- Manufacture churn (commit on a no-change tick).
- Claim a fix/feature done without build+vet+test green and, for user-facing
  changes, a real end-to-end check in the running app (Rule 14).
