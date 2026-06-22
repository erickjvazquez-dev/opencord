#!/usr/bin/env bash
# Regression guard for the OFF-BY-DEFAULT optional coturn TURN relay (iter 218).
#
# The bundled coturn must stay gated behind the `turn` Compose profile so a plain
# `docker compose up` never pulls in a TURN relay: Rule A (the one-command stack stays
# TURN-free / no extra service) and Rule 16 (never silently run a relay). This encodes
# the iter-218 manual verification so a future docker-compose edit can't quietly drop the
# profile gate or add coturn to the default stack.
#
# Uses `docker compose config` (the authoritative renderer). Skips cleanly when docker is
# absent (mirrors how the DB integration tests skip without DATABASE_URL), so it never
# false-reds an env without docker.
set -uo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
[ -d /opt/homebrew/bin ] && export PATH="/opt/homebrew/bin:$PATH"
cd "$ROOT"

if ! command -v docker >/dev/null 2>&1; then
  echo "[compose-check] docker absent — skipping (not a failure)"
  exit 0
fi

fail=0
default_services="$(docker compose config --services 2>/dev/null | sort | tr '\n' ' ')"
turn_services="$(docker compose --profile turn config --services 2>/dev/null | sort | tr '\n' ' ')"
echo "[compose-check] default services       : $default_services"
echo "[compose-check] --profile turn services: $turn_services"

# 1. coturn MUST be absent from the DEFAULT stack (Rule A / Rule 16).
if echo " $default_services " | grep -q ' coturn '; then
  echo "  ✗ FAIL: coturn is in the DEFAULT stack — the relay must stay profile-gated (Rule A/16)"; fail=1
else
  echo "  ✓ coturn absent from the default stack"
fi
# 2. coturn MUST appear under the turn profile (so it CAN be enabled when wanted).
if echo " $turn_services " | grep -q ' coturn '; then
  echo "  ✓ coturn present under --profile turn"
else
  echo "  ✗ FAIL: coturn missing under --profile turn — the relay can't be enabled"; fail=1
fi
# 3. The core stack (db/server/web) must always be in the default stack.
for s in db server web; do
  if ! echo " $default_services " | grep -q " $s "; then
    echo "  ✗ FAIL: core service '$s' missing from the default stack"; fail=1
  fi
done

[ "$fail" -eq 0 ] && echo "[compose-check] PASS" || echo "[compose-check] FAIL"
exit "$fail"
