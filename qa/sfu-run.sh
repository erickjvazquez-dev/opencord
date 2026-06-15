#!/usr/bin/env bash
# Boot a local OSS SFU (LiveKit) + an Opencord server wired to it, then run the SFU
# proof (qa/sfu.mjs): two browsers connect to the REAL LiveKit using Opencord-minted
# tokens and see each other. Proves the token format is accepted by a real server and
# the SFU forwards participants — without the (deferred) Railway demo instance.
#
# LiveKit runs in --dev mode (placeholder keys devkey/secret) with --node-ip 127.0.0.1
# so its WebRTC candidates are reachable from the host browser. Usage: bash qa/sfu-run.sh
set -uo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
[ -d /opt/homebrew/bin ] && export PATH="/opt/homebrew/bin:/opt/homebrew/opt/node/bin:$PATH"

LK_KEY=devkey
LK_SECRET=secret
LK_URL=ws://localhost:7880

cleanup() {
  echo "[sfu] tearing down…"
  kill "${SERVER_PID:-}" 2>/dev/null
  lsof -ti tcp:8080 -ti tcp:5173 2>/dev/null | xargs -r kill -9 2>/dev/null
  pkill -f "vite" 2>/dev/null
  docker rm -f oc-livekit >/dev/null 2>&1
  docker compose down >/dev/null 2>&1
}
trap cleanup EXIT

echo "[sfu] starting LiveKit (--dev, node-ip 127.0.0.1)…"
docker rm -f oc-livekit >/dev/null 2>&1
docker run -d --name oc-livekit -p 7880:7880 -p 7881:7881 -p 7882:7882/udp \
  livekit/livekit-server:latest --dev --bind 0.0.0.0 --node-ip 127.0.0.1 >/dev/null 2>&1
for _ in $(seq 1 30); do curl -sf -o /dev/null http://localhost:7880 && break; sleep 1; done

echo "[sfu] starting Postgres…"
docker compose down -v >/dev/null 2>&1
docker compose up -d db >/dev/null 2>&1
for _ in $(seq 1 30); do
  [ "$(docker inspect --format '{{.State.Health.Status}}' opencord-db-1 2>/dev/null)" = healthy ] && break
  sleep 1
done

echo "[sfu] building + starting Opencord wired to LiveKit (:8080)…"
lsof -ti tcp:8080 2>/dev/null | xargs -r kill -9 2>/dev/null
go build -o /tmp/oc-sfu-server ./cmd/server || { echo "[sfu] server build FAILED"; exit 1; }
DATABASE_URL='postgres://opencord:opencord@localhost:5432/opencord?sslmode=disable' \
  JWT_SECRET=sfuqa OPENCORD_ADDR=':8080' \
  OPENCORD_SFU_URL="$LK_URL" OPENCORD_SFU_KEY="$LK_KEY" OPENCORD_SFU_SECRET="$LK_SECRET" \
  /tmp/oc-sfu-server >/tmp/oc-sfu-server.log 2>&1 &
SERVER_PID=$!
for _ in $(seq 1 60); do curl -sf http://localhost:8080/healthz >/dev/null 2>&1 && break; sleep 1; done
curl -sf http://localhost:8080/healthz >/dev/null 2>&1 || { echo "[sfu] server didn't come up"; exit 1; }

echo "[sfu] starting Vite dev (:5173) so the REAL app drives the SFU path…"
( cd web && npm install --silent && npm run dev >/tmp/oc-sfu-vite.log 2>&1 ) &
for _ in $(seq 1 60); do curl -sf http://localhost:5173 >/dev/null 2>&1 && break; sleep 1; done

echo "[sfu] installing qa deps (livekit-client, playwright)…"
( cd qa && npm install --silent && npx --yes playwright install chromium >/dev/null 2>&1 )

echo "[sfu] running SFU E2E (real app over LiveKit)…"
QA_BASE_URL=http://localhost:5173 node "$ROOT/qa/sfu.mjs"
