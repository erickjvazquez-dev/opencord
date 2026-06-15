#!/usr/bin/env bash
# Run the FULL Go test suite with a Postgres available, so the DB/WS integration
# tests actually execute. Those tests `t.Skip()` when DATABASE_URL is unset — so a
# bare `go test ./...` silently runs only the unit tests and reports green while the
# integration suite (auth, channels, DM access control, voice flood guard, …) never
# runs. CI sets DATABASE_URL + a Postgres service; this gives the same coverage
# locally and in the self-improve loop's health gate.
#
# Behaviour:
#   • DATABASE_URL already set (e.g. CI)        → use it as-is, just run the tests.
#   • unset + docker available                  → boot the compose `db`, run, tear it
#                                                 down (only the db we started).
#   • unset + no docker                         → run anyway (integration tests SKIP),
#                                                 but say so loudly so it's not silent.
#
# Usage: bash scripts/test.sh [extra go test args...]   (or: make test)
set -uo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
[ -d /opt/homebrew/bin ] && export PATH="/opt/homebrew/bin:$PATH"

DB_URL_LOCAL='postgres://opencord:opencord@localhost:5432/opencord?sslmode=disable'
started_db=0

cleanup() {
  if [ "$started_db" = "1" ]; then
    echo "[test] tearing down the test Postgres…"
    docker compose down >/dev/null 2>&1
  fi
}
trap cleanup EXIT

if [ -z "${DATABASE_URL:-}" ]; then
  if command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then
    echo "[test] booting compose Postgres so integration tests run…"
    docker compose up -d db >/dev/null 2>&1
    started_db=1
    for _ in $(seq 1 30); do
      [ "$(docker inspect --format '{{.State.Health.Status}}' opencord-db-1 2>/dev/null)" = healthy ] && break
      sleep 1
    done
    export DATABASE_URL="$DB_URL_LOCAL"
  else
    echo "[test] WARNING: docker unavailable and DATABASE_URL unset —"
    echo "[test] the DB/WS integration tests will SKIP (unit tests only)."
  fi
fi

echo "[test] go test ./... (DATABASE_URL=${DATABASE_URL:+set})"
go test ./... "$@"
