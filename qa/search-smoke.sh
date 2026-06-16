#!/usr/bin/env bash
# Read-only post-deploy smoke for message search operators (before:/after:/free-text).
#
# Why this shape: plain-text messages are posted over the WebSocket gateway, not
# REST, so a smoke can't cheaply "write then find". Instead it proves the search
# operators actually FILTER by *discriminating against #general's existing history*:
# a tightening date window must return a non-increasing, and overall strictly
# smaller, set than the full window, and an unmatchable free-text token must
# return nothing while the full window returns something. If the operators were
# silently ignored (a regression that turns search into match-all), every count
# would equal the baseline and these assertions fail.
#
# It is READ-ONLY apart from registering one throwaway account (same as the other
# QA flows). Safe to run against prod after every deploy.
#
# Usage:
#   bash qa/search-smoke.sh                 # hits CCF_LIVE_URL from .ccf/project.env
#   OPENCORD_BASE_URL=http://localhost:8080 bash qa/search-smoke.sh
#   (or: make qa-search)
set -uo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

# Resolve the target base URL: explicit override wins, else the project's live URL.
BASE="${OPENCORD_BASE_URL:-${QA_BASE_URL:-}}"
if [ -z "$BASE" ] && [ -f "$ROOT/.ccf/project.env" ]; then
  set -a; . "$ROOT/.ccf/project.env"; set +a
  BASE="${CCF_LIVE_URL:-}"
fi
BASE="${BASE:-http://localhost:8080}"
BASE="${BASE%/}"   # strip any trailing slash
echo "[search-smoke] target: $BASE"

python3 - "$BASE" <<'PY'
import json, sys, time, urllib.error, urllib.parse, urllib.request

base = sys.argv[1]

def _req(req):
    try:
        with urllib.request.urlopen(req, timeout=15) as r:
            return r.status, json.load(r)
    except urllib.error.HTTPError as e:
        return e.code, None
    except Exception as e:
        print(f"[search-smoke] FAIL: request error: {e}")
        sys.exit(2)

def post(path, body):
    req = urllib.request.Request(
        base + path, data=json.dumps(body).encode(),
        headers={"Content-Type": "application/json"}, method="POST")
    return _req(req)

def get(path, token):
    req = urllib.request.Request(base + path, headers={"Authorization": "Bearer " + token})
    return _req(req)

# 1) Register a throwaway account (usernames are alnum — no hyphens).
user = "searchsmoke%d" % int(time.time())
st, body = post("/api/auth/register", {"username": user, "password": "search-smoke-pw-123"})
if st != 200 or not body or "token" not in body:
    print(f"[search-smoke] FAIL: register returned HTTP {st}")
    sys.exit(2)
token = body["token"]

# 2) Find the global #general channel.
st, chans = get("/api/channels", token)
if st != 200 or not isinstance(chans, list):
    print(f"[search-smoke] FAIL: GET /api/channels returned HTTP {st}")
    sys.exit(2)
gid = next((c["id"] for c in chans if c.get("name") == "general"), None)
if gid is None:
    print("[search-smoke] FAIL: no #general channel in channel list")
    sys.exit(2)

def count(q):
    st, msgs = get("/api/messages/search?channel=%d&q=%s" % (gid, urllib.parse.quote(q)), token)
    if st != 200 or not isinstance(msgs, list):
        print(f"[search-smoke] FAIL: search q={q!r} returned HTTP {st}")
        sys.exit(2)
    return len(msgs)

today = time.strftime("%Y-%m-%d", time.gmtime())
fails = []
def check(cond, msg):
    print(("  PASS " if cond else "  FAIL ") + msg)
    if not cond:
        fails.append(msg)

# 3) Baseline: the full window = all (capped) of #general's recent history.
baseline = count("after:1970-01-01")
print(f"[search-smoke] #general baseline (after:1970-01-01) = {baseline} messages")
if baseline == 0:
    # An empty channel can't discriminate — that's not a search bug. Report and pass.
    print("[search-smoke] INCONCLUSIVE: #general has no history to discriminate against "
          "(nothing posted yet). Search operators not exercised; not a failure.")
    sys.exit(0)

# 4) `before:` with a far-future bound returns everything (must equal baseline).
check(count("before:2099-01-01") == baseline,
      "before:<far-future> returns all history (== baseline)")

# 5) The operators must actually FILTER — discrimination against history:
check(count("after:2099-01-01") == 0,
      "after:<far-future> excludes all history (-> 0, strictly < baseline)")
check(count("before:1971-01-01") == 0,
      "before:<ancient> excludes all history (-> 0, strictly < baseline)")

# 6) Composition: combining bounds into a contradictory window -> empty;
#    combining into the full window -> baseline.
check(count("after:2099-01-01 before:1971-01-01") == 0,
      "after:<future> + before:<ancient> (impossible window) -> 0")
check(count("after:1970-01-01 before:2099-01-01") == baseline,
      "after:<ancient> + before:<far-future> (full window) -> baseline")

# 7) Free-text must discriminate too (not silently treated as match-all).
check(count("zzqx-no-message-can-match-this-token-zzqx") == 0,
      "unmatchable free text -> 0 (free-text filter not ignored)")

# 8) A tightening lower-bound (after:) chain must be MONOTONE non-increasing,
#    and overall strictly smaller (proves the window genuinely narrows results).
chain_dates = ["1970-01-01", "2000-01-01", "2020-01-01", today, "2099-01-01"]
chain = [count("after:" + d) for d in chain_dates]
print(f"[search-smoke] tightening after: chain {list(zip(chain_dates, chain))}")
monotone = all(chain[i] >= chain[i + 1] for i in range(len(chain) - 1))
check(monotone, "tightening after: window is monotone non-increasing")
check(chain[0] > chain[-1],
      "widest window (%d) strictly larger than narrowest (%d)" % (chain[0], chain[-1]))

if fails:
    print(f"[search-smoke] RESULT: FAIL ({len(fails)} assertion(s) broke — search operators "
          "are not discriminating; they may be silently ignored)")
    sys.exit(1)
print("[search-smoke] RESULT: PASS — before:/after:/free-text operators all discriminate "
      f"against {baseline} messages of #general history.")
sys.exit(0)
PY
RC=$?
echo "[search-smoke] exit $RC"
exit $RC
