#!/usr/bin/env bash
# Boot a local Opencord dev stack (Postgres + Go server + Vite dev), run the
# browser QA against it, then tear everything down. Returns the QA exit code.
#
# Usage: bash qa/run.sh    (or: make qa-browser)
set -uo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

# On this dev machine the default go/node are ancient; prefer Homebrew if present.
[ -d /opt/homebrew/bin ] && export PATH="/opt/homebrew/bin:/opt/homebrew/opt/node/bin:$PATH"

echo "[qa] starting Postgres (fresh volume for deterministic QA)…"
docker compose down -v >/dev/null 2>&1   # wipe prior QA data so assertions are deterministic
docker compose up -d db >/dev/null 2>&1
for _ in $(seq 1 30); do
  [ "$(docker inspect --format '{{.State.Health.Status}}' opencord-db-1 2>/dev/null)" = healthy ] && break
  sleep 1
done

echo "[qa] starting Go server (:8080)…"
DATABASE_URL='postgres://opencord:opencord@localhost:5432/opencord?sslmode=disable' \
  JWT_SECRET=qa OPENCORD_ADDR=':8080' go run ./cmd/server >/tmp/oc-qa-server.log 2>&1 &
SERVER_PID=$!

echo "[qa] starting Vite dev (:5173)…"
( cd web && npm install --silent && npm run dev >/tmp/oc-qa-vite.log 2>&1 ) &

cleanup() {
  echo "[qa] tearing down…"
  kill "$SERVER_PID" 2>/dev/null
  pkill -f "cmd/server" 2>/dev/null
  pkill -f "vite" 2>/dev/null
  docker compose down >/dev/null 2>&1
}
trap cleanup EXIT

echo "[qa] waiting for server + web…"
for _ in $(seq 1 60); do curl -sf http://localhost:8080/healthz >/dev/null 2>&1 && break; sleep 1; done
for _ in $(seq 1 60); do curl -sf http://localhost:5173 >/dev/null 2>&1 && break; sleep 1; done

echo "[qa] installing Playwright (first run only)…"
( cd qa && npm install --silent && npx --yes playwright install chromium >/dev/null 2>&1 )

echo "[qa] running browser QA (single client)…"
QA_BASE_URL=http://localhost:5173 node "$ROOT/qa/browser.mjs"; RC1=$?

echo "[qa] running realtime QA (two clients)…"
QA_BASE_URL=http://localhost:5173 node "$ROOT/qa/realtime.mjs"; RC2=$?

echo "[qa] browser=$RC1 realtime=$RC2"
[ "$RC1" -eq 0 ] && [ "$RC2" -eq 0 ]
exit $?
