---
description: Stops all self-paced loops and monitors in the active session — the kill switch for /loop /self-improve.
---
# /stop-loops — Stop All Loops in the Active Session

Halts every self-paced loop and monitor running in the current session: the
`/loop /self-improve` wakeup chain, any other `/loop` wakeups, and armed Monitors.

Durable **cloud routines** created via `/schedule` are *not* killed by default — they
persist across sessions. This command lists them so you can decide; manage them with
`/schedule` or `CronDelete`.

## How the stop works

A self-paced loop continues only because each tick schedules the next one
(`ScheduleWakeup`, which appears as a one-shot `[session-only]` cron). Cancelling that
pending wakeup stops the chain immediately. The sentinel below is a belt-and-suspenders
for a tick that is already mid-flight.

## Instructions for Claude

When invoked:

1. **Set the stop sentinel** (an in-flight tick checks this before rescheduling):
   ```bash
   date -u +"%Y-%m-%dT%H:%M:%SZ" > "$HOME/.claude/STOP_LOOPS" && echo "sentinel set"
   ```
2. **Cancel pending session wakeups.** Call `CronList`. For every `[session-only]`
   one-shot job — especially any whose prompt contains `/loop` or `/self-improve` —
   call `CronDelete` with its ID. Leave recurring **cloud routines** alone unless the
   user asked to remove them; collect their IDs for the report.
3. **Stop monitors / background tasks.** Call `TaskList`; for each running task that is
   a loop Monitor, call `TaskStop`. Leave unrelated user tasks running.
4. **Do NOT call `ScheduleWakeup` this turn** — that is what ends the dynamic loop.
5. **Report**:
   ```
   STOP-LOOPS — cancelled N wakeup(s), stopped M monitor(s)
   Cancelled wakeups: <ids or none>
   Stopped monitors:  <ids or none>
   Cloud routines still running: <ids or none>  (use /schedule to manage)
   Resume anytime: /loop /self-improve
   ```

To **resume**, just run `/loop /self-improve` again — a manual start clears the
sentinel and proceeds (see the self-improve Step 0 stop-switch).
