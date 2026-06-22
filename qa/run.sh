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

# Free the ports from any stale process first. A previous run's `go run` child
# binary can survive and hold :8080, silently making QA test STALE backend code.
echo "[qa] freeing ports 8080/5173 from any stale process…"
lsof -ti tcp:8080 -ti tcp:5173 2>/dev/null | xargs -r kill -9 2>/dev/null

echo "[qa] building + starting Go server (:8080)…"
# Build a real binary and run THAT (not `go run`, whose temp child survives pkill
# and leaks the port). $SERVER_PID is then the actual server, killable by PID.
go build -o /tmp/oc-qa-server ./cmd/server || { echo "[qa] server build FAILED"; exit 1; }
DATABASE_URL='postgres://opencord:opencord@localhost:5432/opencord?sslmode=disable' \
  JWT_SECRET=qa OPENCORD_ADDR=':8080' /tmp/oc-qa-server >/tmp/oc-qa-server.log 2>&1 &
SERVER_PID=$!

echo "[qa] starting Vite dev (:5173)…"
( cd web && npm install --silent && npm run dev >/tmp/oc-qa-vite.log 2>&1 ) &

cleanup() {
  echo "[qa] tearing down…"
  kill "$SERVER_PID" 2>/dev/null
  lsof -ti tcp:8080 -ti tcp:5173 2>/dev/null | xargs -r kill -9 2>/dev/null
  pkill -f "vite" 2>/dev/null
  docker compose down >/dev/null 2>&1
}
trap cleanup EXIT

echo "[qa] waiting for server + web…"
for _ in $(seq 1 60); do curl -sf http://localhost:8080/healthz >/dev/null 2>&1 && break; sleep 1; done
for _ in $(seq 1 60); do curl -sf http://localhost:5173 >/dev/null 2>&1 && break; sleep 1; done
# Fail loud if our server isn't actually up — never silently run QA against a stale
# server (the bug that hid the DM backend behind an orphaned :8080 listener).
curl -sf http://localhost:8080/healthz >/dev/null 2>&1 || {
  echo "[qa] server did not come up on :8080 — see /tmp/oc-qa-server.log"; exit 1; }

# Seed a dedicated global channel with >50 messages so the history-pagination "Load older" button
# has something to page (iter 204). Direct SQL — the WS path is rate-limited and the REST path
# requires a file, so neither can cheaply seed messages. A separate channel keeps #general clean.
# 120 messages = THREE pages (newest 50 on open, +50 via the button, +20 via auto-load-on-scroll)
# so the browser QA can exercise BOTH the click fallback and the scroll-to-top auto-load (iter 205).
echo "[qa] seeding the pgseed channel with 120 messages for history pagination…"
docker exec opencord-db-1 psql -U opencord -d opencord -q -c "
  INSERT INTO users (username, password_hash) VALUES ('pgseed-user', 'x') ON CONFLICT (username) DO NOTHING;
  INSERT INTO channels (name) SELECT 'pgseed' WHERE NOT EXISTS (SELECT 1 FROM channels WHERE name='pgseed' AND server_id IS NULL);
  INSERT INTO messages (channel_id, user_id, body, created_at)
  SELECT (SELECT id FROM channels WHERE name='pgseed' AND server_id IS NULL),
         (SELECT id FROM users WHERE username='pgseed-user'),
         'history seed ' || g, now() - (interval '1 second' * (600 - g))
  FROM generate_series(1, 120) g;" >/dev/null 2>&1 || echo "[qa] pgseed seed failed (pagination test will skip)"

# Parse-check every QA script BEFORE the expensive boot — a one-char syntax slip in a
# .mjs otherwise isn't caught until ~3 min in (Postgres + Go build + Vite + Playwright
# all run first), and only on the crashing script. node --check is milliseconds.
echo "[qa] syntax-checking QA scripts…"
for f in "$ROOT"/qa/*.mjs; do
  node --check "$f" || { echo "[qa] SYNTAX ERROR in $f — aborting before boot"; exit 1; }
done

# Rule 15 — XSS-by-construction guard: fail the QA if any unsafe HTML/eval sink was introduced in
# the client (dangerouslySetInnerHTML / innerHTML= / eval / …). Cheap source scan, pre-boot.
echo "[qa] scanning client for unsafe HTML/eval sinks (Rule 15)…"
node "$ROOT/web/scripts/check-no-unsafe-sinks.mjs" || { echo "[qa] unsafe sink found — aborting"; exit 1; }

echo "[qa] installing Playwright (first run only)…"
( cd qa && npm install --silent && npx --yes playwright install chromium >/dev/null 2>&1 )

echo "[qa] running browser QA (single client)…"
QA_BASE_URL=http://localhost:5173 node "$ROOT/qa/browser.mjs"; RC1=$?

echo "[qa] running realtime QA (two clients)…"
QA_BASE_URL=http://localhost:5173 node "$ROOT/qa/realtime.mjs"; RC2=$?

echo "[qa] running voice QA (two clients, mesh WebRTC)…"
QA_BASE_URL=http://localhost:5173 node "$ROOT/qa/voice.mjs"; RC3=$?

# Search-operator discrimination smoke (read-only) against the local API directly.
# By now browser/realtime QA have posted to #general, so it has history to
# discriminate against; on an empty channel the smoke reports INCONCLUSIVE (exit 0),
# so it never false-reds the gate.
echo "[qa] running search-operator smoke (read-only, local API :8080)…"
OPENCORD_BASE_URL=http://localhost:8080 bash "$ROOT/qa/search-smoke.sh"; RC4=$?

# Compose-profile guard (iter 219): the optional coturn TURN relay must stay OFF by
# default (Rule A / Rule 16). Cheap + docker-only; skips cleanly without docker.
echo "[qa] running compose-profile guard (coturn stays off-by-default)…"
bash "$ROOT/qa/compose-profile-check.sh"; RC5=$?

echo "[qa] browser=$RC1 realtime=$RC2 voice=$RC3 search=$RC4 compose=$RC5"
[ "$RC1" -eq 0 ] && [ "$RC2" -eq 0 ] && [ "$RC3" -eq 0 ] && [ "$RC4" -eq 0 ] && [ "$RC5" -eq 0 ]
exit $?
