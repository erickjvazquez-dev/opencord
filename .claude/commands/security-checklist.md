---
description: Pre-launch security checklist — the framework's single source of truth for hardening before shipping.
---
# /security-checklist — Pre-Launch Security Checklist (framework SSOT)

The **canonical 20-item launch-security bar** every framework project must satisfy
before — and while — it is live. This is the single source of truth referenced by
`/self-improve` (Security step), `/critical-checks` (pre-commit subset),
`/gap` (`--area security`), and the daily **`<project>-security-audit` cloud routine**.
The items map to OWASP Top-10 + the standard SaaS pre-launch bar.

**Do not re-type this list anywhere else — every consumer derives from this file.**

## Usage

```
/security-checklist              # full pass (static + live if $CCF_LIVE_URL set)
/security-checklist --fast       # static P0 subset only (the pre-commit set)
/security-checklist --live       # live-surface probes only (needs $CCF_LIVE_URL)
```

Config-driven and portable: it reads `.ccf/project.env` (`CCF_PROJECT_DIR`,
`CCF_LIVE_URL`, `CCF_CLIENT_DIRS`) and degrades gracefully. **A SKIP is not a FAIL** —
an item with no applicable surface (e.g. no live URL, no client bundle) logs `SKIP`,
never a false red.

## Severity

- **P0** — blocks commit AND launch. A P0 FAIL is a hard gate: `/self-improve` must not
  push, `/critical-checks` must not pass, a PR must not merge.
- **P1** — fix-next. Logged to `GOAL.md` "## TOP PRIORITY" as a `SEC-*` item.

## The 20 items

| # | Item | Severity | Automatable |
|---|------|----------|-------------|
| 1 | Hide API keys — no real secret shipped in repo or client bundle | P0 | static |
| 2 | Purge Git secrets — history carries no leaked token/key | P0 | static |
| 3 | Public/restricted DB key — client never holds the admin/service key | P0 | static |
| 4 | Row-level security — per-user/record data isolation | P0 | live-authed |
| 5 | Encrypt sensitive data — keys/PII hashed or encrypted at rest, never plaintext | P0 | static |
| 6 | Server-side auth — auth decided server-side, constant-time secret compare | P0 | live-authed |
| 7 | Lock record access — no IDOR; unknown-owner records fail **closed** | P0 | live-authed |
| 8 | Block field tampering — client cannot set privileged fields (tier/admin/quota) | P0 | live-authed |
| 9 | Secure session cookies — HttpOnly + Secure + SameSite | P1 | static+live |
| 10 | Hash passwords — any password/secret auth uses bcrypt/argon2/scrypt | P0 | static |
| 11 | Rate limit auth — brute-force protection on signup/login | P1 | live |
| 12 | Bot protection — PoW/captcha on signup, **enforced in prod** (not dark-shipped) | P1 | live |
| 13 | Parameterize queries — no string-built SQL with user input | P0 | static |
| 14 | Validate all input — bodies validated + size/shape limits; hostile input → 4xx not 500 | P1 | static+live |
| 15 | Escape user content — user/LLM content via textContent/escape, never raw innerHTML | P0 | static |
| 16 | Restrict file uploads — type + size limits enforced | P1 | live |
| 17 | Trim API responses — no internal paths/stack/raw-exception text leaked | P1 | static+live |
| 18 | Security headers — CSP, HSTS, X-Frame-Options, X-Content-Type-Options, Referrer-Policy | P1 | live |
| 19 | Force HTTPS — HTTP→HTTPS redirect / HSTS; no mixed content | P1 | live |
| 20 | Scan dependencies — SCA in CI, **blocking on critical** (not advisory-only) | P1 | static |

## Automatable checks (run these)

```bash
# ── Resolve project config (portable; SKIP-not-FAIL) ──────────────────────────
ROOT=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
set -a; [ -f "$ROOT/.ccf/project.env" ] && . "$ROOT/.ccf/project.env"; set +a
DIR="${CCF_PROJECT_DIR:-$ROOT}"; URL="${CCF_LIVE_URL:-}"
CLIENT_DIRS="${CCF_CLIENT_DIRS:-extension public web client static}"
P0FAIL=0; P1FAIL=0
fail0(){ echo "  [P0 FAIL] $1"; P0FAIL=$((P0FAIL+1)); }
fail1(){ echo "  [P1 FAIL] $1"; P1FAIL=$((P1FAIL+1)); }
ok(){ echo "  [OK] $1"; }; skip(){ echo "  [SKIP] $1"; }

# 1/3/5 — secrets in repo or client bundle (env/test/demo excluded)
HITS=$(grep -rInE '(secret|api[_-]?key|token|password|private[_-]?key)\s*[:=]\s*["'\''][A-Za-z0-9/_+\-]{16,}' \
  "$DIR" --include='*.py' --include='*.js' --include='*.ts' --include='*.json' --include='*.env*' 2>/dev/null \
  | grep -ivE 'environ|getenv|process\.env|os\.env|example|placeholder|your-|test|mock|demo|fake|dummy|sample|xxx|redact' | head -5)
[ -z "$HITS" ] && ok "1/3/5 no committed secret literals" || { fail0 "1/3/5 possible secret literal(s):"; echo "$HITS" | sed 's/^/      /'; }
# service/admin key shipped to a client dir → P0
for cd in $CLIENT_DIRS; do
  [ -d "$DIR/$cd" ] && grep -rIlE 'service[_-]?role|admin[_-]?key|SUPABASE_SERVICE|ADMIN_KEY\s*=\s*["'\'']' "$DIR/$cd" 2>/dev/null \
    | head -1 | grep -q . && fail0 "3 admin/service key referenced in client dir $cd"
done

# 2 — git history secret spot-check (recent window; gitleaks if present)
if command -v gitleaks >/dev/null 2>&1; then
  (cd "$DIR" && gitleaks detect --no-banner -q 2>/dev/null) && ok "2 gitleaks clean" || fail0 "2 gitleaks found history secrets"
else
  H=$(cd "$DIR" && git log -p -n 60 2>/dev/null | grep -iE '^\+.*(secret|api_key|token|password)\s*=\s*["'\''][A-Za-z0-9]{20,}' \
    | grep -ivE 'environ|getenv|example|placeholder|your-|test|mock|demo|fake' | head -3)
  [ -z "$H" ] && ok "2 no secrets in last 60 commits (run 'gitleaks detect' for full history before launch)" || { fail0 "2 secret-like additions in history:"; echo "$H" | sed 's/^/      /'; }
fi

# 13 — string-built SQL with interpolation
SQLI=$(grep -rInE '(execute|executemany|cursor\.execute|query)\s*\(\s*(f["'\'']|["'\''][^"'\'']*["'\'']\s*[%+]|.*\+\s*\w)' \
  "$DIR" --include='*.py' 2>/dev/null | grep -iE 'select|insert|update|delete|where|from' | grep -ivE 'test|#' | head -5)
[ -z "$SQLI" ] && ok "13 no string-interpolated SQL" || { fail0 "13 possible SQL injection:"; echo "$SQLI" | sed 's/^/      /'; }

# 15 — raw innerHTML / dangerouslySetInnerHTML with a non-constant value
XSS=$(grep -rInE '\.innerHTML\s*[+]?=\s*[^"'\'' ]|dangerouslySetInnerHTML|v-html|insertAdjacentHTML\([^,]*,\s*[a-z_$]' \
  "$DIR" --include='*.js' --include='*.ts' --include='*.jsx' --include='*.tsx' --include='*.vue' --include='*.html' 2>/dev/null \
  | grep -ivE 'escapeHtml|_cfEsc|sanitize|DOMPurify|=\s*["'\''`]\s*["'\''`]|innerHTML\s*=\s*["'\''`]' | head -5)
[ -z "$XSS" ] && ok "15 no unescaped innerHTML sinks" || { fail0 "15 possible XSS sink (escape before assign):"; echo "$XSS" | sed 's/^/      /'; }

# 10 — weak/plaintext password hashing (only if password auth exists)
if grep -rIlE 'password' "$DIR" --include='*.py' 2>/dev/null | head -1 | grep -q .; then
  WEAK=$(grep -rInE '(md5|sha1)\(.*password|password\s*==\s*' "$DIR" --include='*.py' 2>/dev/null | grep -ivE 'test|#|hashlib.sha256' | head -3)
  [ -z "$WEAK" ] && ok "10 no weak/plaintext password compare found" || { fail0 "10 weak password handling:"; echo "$WEAK" | sed 's/^/      /'; }
else skip "10 no password auth surface"; fi

# 20 — SCA present AND blocking in CI
CI=$(ls "$DIR"/.github/workflows/*.y*ml 2>/dev/null)
if [ -n "$CI" ]; then
  grep -qiE 'safety scan|pip-audit|npm audit|trivy|snyk' $CI && {
    grep -qiE '(safety scan|pip-audit|npm audit)[^|]*\|\|\s*true' $CI \
      && fail1 "20 SCA present but ADVISORY-only (|| true) — gate it on critical findings" \
      || ok "20 SCA present in CI"
  } || fail1 "20 no dependency scanner (safety/pip-audit/npm-audit) in CI"
else skip "20 no CI workflows"; fi

# ── LIVE probes (only if $CCF_LIVE_URL) ──────────────────────────────────────
if [ -n "$URL" ]; then
  H=$(curl -sS -D - -o /dev/null "$URL" --max-time 12 2>/dev/null)
  # 18 — the five headers
  MISS=""
  for hdr in strict-transport-security content-security-policy x-frame-options x-content-type-options referrer-policy; do
    echo "$H" | grep -qi "^$hdr:" || MISS="$MISS $hdr"
  done
  [ -z "$MISS" ] && ok "18 all 5 security headers present" || fail1 "18 missing header(s):$MISS"
  # 19 — HTTP→HTTPS
  HURL=$(echo "$URL" | sed 's|^https|http|'); CODE=$(curl -sS -o /dev/null -w '%{http_code}' -I "$HURL" --max-time 10 2>/dev/null)
  echo "$CODE" | grep -qE '30[1278]' && ok "19 HTTP→HTTPS redirect ($CODE)" || fail1 "19 no HTTP→HTTPS redirect (got $CODE)"
  # 9 — session cookie flags (if any Set-Cookie)
  SC=$(echo "$H" | grep -i '^set-cookie:')
  if [ -n "$SC" ]; then
    echo "$SC" | grep -qi httponly && echo "$SC" | grep -qi secure && echo "$SC" | grep -qi samesite \
      && ok "9 session cookie has HttpOnly+Secure+SameSite" || fail1 "9 session cookie missing a flag"
  else skip "9 no Set-Cookie on landing (verify on the auth route)"; fi
else skip "18/19/9/live probes — no \$CCF_LIVE_URL set"; fi

echo "SECURITY-CHECKLIST: P0_FAIL=$P0FAIL P1_FAIL=$P1FAIL  ->  $([ "$P0FAIL" -eq 0 ] && echo 'P0 CLEAN' || echo 'P0 BLOCK')"
```

## Manual / live-authed items (not fully automatable — cover in the E2E/adversarial suite)

Items **4, 6, 7, 8, 11, 12, 14, 16, 17** need a live authed probe. They belong in the
project's Rule-15 adversarial E2E suite and the daily routine's Fable pass:

- **4 RLS / 7 IDOR** — sign up two keys A,B; upload as A; assert B gets 403/404 fetching A's
  resource by id. **Unknown-owner records MUST fail closed** (a record with no owner recorded
  is NOT readable by any key — the fail-open trap).
- **6 server-side auth** — call a protected route with no / forged / another user's credential → 401/403; secret comparisons use `secrets.compare_digest` (constant-time).
- **8 field tampering** — POST `tier=admin` / `quota=999999` / `is_admin=true` on a self-serve
  route → server ignores; privileged mutations are `Depends(admin)`-gated.
- **11 rate limit** — N rapid auth calls from one IP → 429 (XFF-forge-proof IP resolution).
- **12 bot protection** — signup without a valid PoW/challenge → rejected. **Verify enforced
  in prod** (a feature gated behind an unset env var is dark-shipped = not protected).
- **14 input validation** — malformed/oversized/lone-surrogate body → 4xx, never a 500 stack.
- **16 upload hardening** — oversized / disallowed-type upload → 413/415; disable-limits flag
  never honored in production.
- **17 response trim** — a 500 body and success responses expose no server filesystem path,
  stack, or raw upstream/exception text (map to first-party copy).

## How it's wired (consumers)

- **`/self-improve` Security step** — runs `/security-checklist` (+ `.ccf/hooks/security.sh`);
  a **P0 FAIL blocks the tick's push** exactly like a red test gate. `SECURITY:` in the Step-6
  report is computed from this (`P0 CLEAN` / `N P1 open` / `BLOCK`), never asserted unbacked.
- **`/critical-checks`** — Block 4 runs the **static P0 subset** (`--fast`) pre-commit.
- **`/gap --area security`** — grades the diff/posture against these items; findings → `SEC-*`.
- **Daily `<project>-security-audit` cloud routine** — full pass + a Fable-5 live adversarial
  pass; P0 → GOAL.md TOP PRIORITY + `owner-todos.md`; P1 → GOAL.md as `SEC-*`.
- **Inheritance** — this file lives in the framework `commands/`, auto-pushes to the CCF repo,
  fans out to every project via `setup.sh`, and is seeded into new projects via `scaffold/`.

## Historical incidents this bar prevents

- Bot protection **dark-shipped** behind an unset env var (all code live, one env flag off →
  zero protection) — item 12's "enforced in prod" clause.
- Record ownership **failing open** (unknown-owner id readable by any key) — item 7.
- Response **over-exposure** (absolute server path / raw exception text in a JSON body) — item 17.
- SCA **advisory-only** (`safety scan || true`) — never fails CI, so a critical CVE ships — item 20.
- Unpinned dependency lower bounds pulling a breaking/compromised major (supply-chain) — item 20.
