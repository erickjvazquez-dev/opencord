# Opencord — Loop Improvements & QA Reflections

One entry per self-improve tick (newest first): what the loop learned about its own
QA, coverage, or process. Appended by `/self-improve-opencord` step 6.5 ("Reflect").

## 2026-06-22 (iter 225) — acted on the flake-watch: QA gate now self-diagnoses failures

Turned iter-224's logged watch ("if browser=1-with-no-✗ recurs, capture the output") into a permanent
fix instead of waiting for a recurrence — the "improve the rule, don't just note it" principle. `qa/run.sh`
now tees each suite to a per-suite log and, on a non-zero exit, re-prints just that suite's failure-
relevant lines (`✗ FAIL` / `[pageerror]` / crash / timeout / `QA: FAIL`) with the benign avatar-404
console noise filtered out. So a red gate now explains itself at the bottom instead of burying the reason
under hundreds of 404 lines. Verified both paths: extraction surfaces the right lines on a synthetic log;
a green full run stays silent (summary returns early on rc=0) and the tees don't break the green path.

**Decision discipline this tick (worth recording):** I considered three candidates and grep-checked each
before acting — manual presence status (online/idle/dnd/invisible) turned out ALREADY built (the
grep-before-building rule again stopped a re-implementation), the avatar-404 fix stayed correctly
deprioritized (cached, zero-UX-impact, per iter-221), so the diagnosability fix — directly actionable from
my OWN prior reflection — was the honest highest-value pick. On a mature product, "what did I already log
that I can now permanently fix?" is a strong tick-selection heuristic.

**Maturity steady-state (honest):** the product is feature-complete for MVP+ parity; ticks are now
keep-green + tooling-hardening + surfacing owner-decision items (header-icon design pass; the big epics:
cascaded SFU, tunneling, email, Cloud tier). Discipline: each tick must do GENUINE value (no churn); a
true no-op tick logs green WITHOUT committing.

**Possible next coverage target:** manual presence status (set yourself idle/dnd/invisible) exists but I
didn't find a browser-QA assertion that a *second* user sees your changed status — a candidate coverage gap.

**Component advanced:** infra/QA (loop-tooling — self-diagnosing failures). **Cadence:** shipped a fix +
the header-icon P1 still open → ACTIVE (1800s).

## 2026-06-22 (iter 224) — scroll-to-divider on open; "set intent at data-arrival, not the trigger"

Closed iter-223's follow-up: opening an unread channel now lands on the "New" line, not the bottom
(read-state parity complete: divider + scroll-to). Two takeaways:

**1. Effect-timing pattern — set scroll-intent where the DATA arrives, not where the trigger fires.** My
first attempt set the "scroll to divider" flag in the channel-SWITCH effect, but that effect fires FIRST
with the OLD channel's stale messages still in state (setMessages([]) hasn't re-rendered yet), so the flag
got consumed against stale data. The fix: set the flag in the WS HISTORY HANDLER — which runs exactly when
the NEW channel's messages + boundary arrive (no staleness) — and let a dedicated effect perform the
scroll once the divider renders, with the existing auto-scroll effect standing down via the flag. Codify:
when an action must happen on "the new data," hook the data-arrival callback, not the navigation trigger
that precedes it; intermediate renders carry stale state.

**2. AI-vision earned its keep on a scroll nuance tests can't see.** `scrollIntoView({block:'center'})`
passed the in-viewport assertion but the screenshot showed the divider jammed at the bottom with the
unread messages hidden below (only 2 unread → center clamps low). Switched to `block:'start'` (divider up,
unread below). A boolean "is it in the viewport" test would never have caught the bad PLACEMENT — only the
visual review did. Reinforces the Rule-14 AI-vision step for any scroll/layout change.

**QA-stability watch (logged, not yet acted):** one browser-QA run flaked to `browser=1` with NO printed
`✗ FAIL` (so a pageerror/timeout, not an assertion) and did not reproduce across two reruns. My divider
effect is defensive (optional-chaining, flag-gated) so it's an unlikely cause, but if `browser=1`-with-no-✗
recurs, capture the full browser output to find the silent failure (the QA should surface pageerrors in
the gate line, not just inline).

**Component advanced:** chat/UI (read-state parity now complete — divider + land-on-it). **Cadence:**
shipped a feature + the header-icon P1 still open → ACTIVE (1800s).

## 2026-06-22 (iter 223) — shipped the "New messages" divider end-to-end; reuse-existing-timing > new mechanism

A full-stack Discord-parity feature in one tick (backend + client + CSS + store test + two-client E2E +
AI-vision + deploy + rollout-verify): the red "New" line at the read→unread boundary. The key was reading
the existing mark-read timing FIRST — `markChannelRead` fires on channel LEAVE — which is EXACTLY what
makes the boundary work without any new mechanism: leaving freezes `last_read_id` at that point, and the
next open's WS history event carries it (captured before the open marks read). So the whole feature is
just `Store.LastReadID` + an `Event.lastReadId` field + a client divider — no schema change, no new
read-tracking. **Playbook: before building a feature, map the EXISTING lifecycle/timing it can ride; a
new mechanism is often unnecessary once you see when the data you need is already produced.**

**Two QA lessons this tick:**
1. **AI-vision screenshots must scroll the target into view.** The first divider screenshot auto-scrolled
   to the message-list bottom and missed the divider entirely (it was mid-list); the assertions passed but
   the vision frame was useless. Added `divider.scrollIntoViewIfNeeded()` before the shot. Generalize: a
   QA screenshot of an element NOT pinned to a viewport edge must scroll it into view first, or AI-vision
   reviews a frame that doesn't contain the thing under test.
2. **A negative assertion makes a state-dependent feature trustworthy.** Besides "divider appears when
   unread," the test also asserts "NO divider once caught up" — without it, a divider that ALWAYS rendered
   would still pass. State-toggling features need both the present AND absent assertions.

**Follow-up (GOAL):** scroll-to-divider on open (Discord lands you at the New line; Opencord lands at the
bottom, so on a big backlog you must scroll up to find it). Small frontend-only next tick.

**Component advanced:** chat (read-state parity — the in-channel unread divider). **Cadence:** shipped a
feature + open follow-ups (scroll-to-divider, header-icon pass) → ACTIVE (1800s).

## 2026-06-22 (iter 222) — shipped masked links (Rule 15); "secure by construction" beats validate-and-reject

Closed iter-221's queued follow-up: Discord-style `[text](url)`. The phishing surface (label ≠
destination) made it a Rule-15 feature, and the cleanest defense was to make the URL group `https?://`
IN THE REGEX — so `[x](javascript:…)`/`[x](data:…)` never even match and render as inert literal text.
Plus `title`=the real URL as an anti-spoof tell + target=_blank/rel=noopener. Adversarial vitest (incl.
the phishing shape href≠text, javascript:/data: stays inert, no on* extracted from a quoted URL) + browser
QA + AI-vision (the safe link shows the label; the javascript: one stays literal) + live rollout-verify.

**Security pattern to codify — "secure by construction" > "validate-and-reject".** Baking the safe scheme
into the matching regex (no link forms at all for a bad scheme) is stronger than matching any `(url)` then
checking/rejecting the scheme: there's no separate validation step that a later refactor can loosen or
bypass, and the adversarial tests lock the regex so broadening it (re-introducing javascript:) fails
loudly. Both URL→`<a>` paths (autolink + masked link) now share this shape. Prefer it for any future
"accept only X-shaped input" surface.

**Maturity signal (honest):** the message markdown subset is now at Discord parity (bold/italic/strike/
code/quote/lists/spoiler/autolink/emoji/headers/subtext/masked-links; only underline `__` deferred as
low-value + parse-conflicting). The loop has shipped ~10 productive ticks this session; the remaining
HIGH-value items increasingly need OWNER steering (the header-icon monochrome-line-icon design pass) or
are multi-tick epics with product/biz decisions (cascaded SFU, built-in tunneling, email accounts, the
Cloud tier). The loop can keep doing incremental coverage/parity polish, but the big moves want direction.

**Component advanced:** chat/UI (markdown parity — masked links) + security (the secure-by-construction
URL path). **Cadence:** shipped a feature + the header-icon P1 still open → ACTIVE (1800s).

## 2026-06-22 (iter 221) — shipped markdown headers (chat parity); grep-before-building killed a low-value chase

Started by investigating iter-220's "many 404s in QA" finding — but reading the Avatar component first
showed it already caches the 404 per user AND renders initials IMMEDIATELY (url starts null), so the 404
is a silent background request with ZERO UX impact; eliminating it needs a `has_avatar` field across the
API for no user-visible gain. Correctly **dropped it** (grep-before-building stopped a low-value chase —
the inverse of iter-218, where the same discipline redirected TO the real gap). Pivoted to a genuine
parity gap the same survey surfaced: the markdown subset had no headers. Shipped `#`/`##`/`###` + `-#`
subtext — pure formatting (no new security surface; header content reuses the XSS-safe `renderInline`),
with vitest + browser QA + AI-vision (clean Discord-like hierarchy) + live rollout-verify (CSS bundle
carries md-h*/md-subtext).

**Loop-process note — findings deserve a value check, not reflexive action.** A logged finding ("404
noise") isn't automatically worth a tick. The right move was a 2-minute code read to size its real impact
BEFORE building. Generalize: when picking up a prior tick's logged finding, first grep/read to confirm it
still matters and is worth the cost — a finding can be real yet not worth fixing (zero-UX-impact, or fix
cost ≫ benefit). Pairs with the existing grep-before-building rule.

**Follow-up logged (GOAL):** masked links `[text](url)` — higher-usage than headers but a PHISHING
surface (display text ≠ URL), so it's a Rule-15 tick (http(s)-only URL validation reusing the autolink
guard + a `title`=real-URL anti-spoof + the javascript:/data:-stays-inert adversarial cases). Underline
`__` deferred (collides with `_italic_`).

**Component advanced:** chat/UI (Discord markdown parity — headers). **Cadence:** shipped a feature + open
P1s remain (header-icon design pass, masked links) → ACTIVE (1800s).

## 2026-06-21 (iter 220) — proved LOSSLESS realtime across a reconnect (chat north star); test-anchor lesson

Advanced the chat north star's *verification*: the §1e reconnect QA checked connectivity recovery (banner
appears/clears, a post-reconnect message lands) but NOT message INTEGRITY across the drop. grep-before-
building confirmed the client's reconnect path is `setMessages(hist)` (replace) + live append — lossless
+ dupe-free *by construction* — so rather than a speculative fix I encoded the invariant: B posts a
message WHILE A is offline (into the gap), and after A reconnects the test asserts (1) A receives it —
**no loss** across the disconnect — and (2) the pre-outage + gap messages each appear EXACTLY once — **no
dupe** from history-replace. Full QA green.

**QA-process lesson (the real catch):** my first draft anchored the no-dupe count on `body` — a message
that is QUOTED in a reply earlier in the test — so `.message hasText: body` matched the original AND the
reply's snippet → count 2 → false-positive FAIL. The app was fine; the assertion was wrong. **Playbook
add: a "appears exactly once" count-by-text assertion needs a UNIQUE anchor that is never quoted/embedded
elsewhere (replies, search snippets, pins, jump-previews) — use a fresh dedicated marker message, or scope
the locator to the message body excluding reply-context.** This is the count-by-substring trap; the loop
hit it and should not again.

**Minor finding (logged, not chased):** the realtime QA console shows many `404 Not Found` for both
clients — almost certainly the Avatar component probing the image endpoint for users with no uploaded
avatar before falling back to initials (by-design fallback, benign, non-failing). A possible micro-
optimization (skip the fetch when there's no avatar) but low priority; noted for a future look.

**Coverage advanced:** chat/realtime (lossless-realtime invariant now pinned end-to-end). Next least-
tested realtime invariant to consider: message ORDERING after a reconnect, and presence-count correctness
post-recovery. **Cadence:** shipped a test + the header-icon P1 still open → ACTIVE (1800s).

## 2026-06-21 (iter 219) — due browser QA (green + polished); encoded a manual verification as a guard

The browser QA was due (iters 216–218 were backend/security/infra, no UI change → 3rd-tick cadence). Ran
the full suite (browser=0 realtime=0 voice=0 search=0) and AI-vision-reviewed the screenshots across
breadth (main chat, roles manager, mobile drawer, 3-way voice mesh). The product is mature + polished — no
P0, and the one real parity gap (header action icons are colorful EMOJI, not Discord's monochrome line
icons) is a whole-app visual decision the owner should steer, so I **logged it as a deferred P1 rather than
autonomously redesigning** mid-loop. That restraint is the right call: a hasty icon swap could make the
look worse and overrides an owner-owned design axis.

**Shipped (no-churn, real value):** a `qa/compose-profile-check.sh` regression guard that encodes the
iter-218 MANUAL coturn verification — asserts the optional TURN relay stays absent from the default
`docker compose` stack yet present under `--profile turn` (Rule A / Rule 16). Proved it's not a no-op with
a negative test (un-gating coturn → FAIL; restored → PASS), then wired it into `qa/run.sh` (RC5).

**Loop-process playbook add — "encode manual verifications as guards":** iter-218's off-by-default check
was a one-shot manual `docker compose config`. One-shot manual checks rot — the NEXT compose edit wouldn't
re-run it. Generalize: whenever a tick verifies an invariant by hand (a profile gate, a header that
shouldn't wrap, a default that must stay empty), leave behind a cheap automated guard so it can't silently
regress. This tick did that for coturn; apply it to future manual verifications too.

**Maturity-plateau note:** most remaining work is now either (a) owner-steered design (the header-icon
pass) or (b) multi-tick infra epics (cascaded SFUs). In this phase the highest-value loop behavior is:
keep QA green, harden/guard what's already shipped, and surface owner-decision P1s clearly — not invent
churn. Next tick: continue incremental hardening/coverage rotation, or the header-icon design pass IF the
owner greenlights it.

**Component advanced:** infra/QA robustness (a durable guard on the self-host TURN invariant) + flagged a
UI P1. **Cadence:** shipped a guard + an open P1 remains → ACTIVE (1800s).

## 2026-06-21 (iter 218) — rotated to AUDIO; bundled optional coturn — and caught a STALE-GOAL premise

Rotated off security (per iter-217's flag) to advance audio toward its thousands-scale north star. The
plan was "self-host TURN for hostile NATs" — but reading the CODE first revealed TURN was already fully
built (config + ephemeral HMAC creds + `ICEServersForUser`, unit-tested, iter 137). The real gap was that
the credential scheme was never paired with an actual **relay**: a self-hoster still had to BYO coturn.
So I shipped the relay itself — an optional `docker compose --profile turn up` coturn service (off by
default, Rule A preserved; `stack-guardian` APPROVE; free self-hosted OSS) wired to the same
`OPENCORD_TURN_SECRET`, plus the previously-missing README/.env docs for the ephemeral scheme. Verified at
the config level (profile gating: coturn absent from the default stack) AND by booting coturn 4.6.2 with
our exact flags (clean startup, no flag errors); a real symmetric-NAT relay needs a hostile-NAT client,
stated not faked.

**Loop-process playbook add (the real lesson):** the rotation target was set last tick from a STALE GOAL
note ("Next: optional self-host TURN") that lagged the code by ~80 iterations. GOAL/SPEC "Next:" prose
drifts behind what's actually built. **Codify: a component-rotation tick must `grep` the code for the
supposedly-missing capability BEFORE committing to build it** — exactly the map-before-act discipline from
the iter-216 security tick, now generalized to FEATURE ticks. It saved this tick from re-implementing TURN;
without it I'd have wasted the tick. (Same family as iter-216's "map coverage before probing.")

**Coverage note:** coturn relay behavior is config-verified + boots-clean, but there is NO end-to-end relay
test (the SFU path has `qa/sfu-run.sh` against real LiveKit; there's no equivalent forcing media through
coturn behind a simulated symmetric NAT). A future heavy QA slice could add one; logged, not built (needs
a NAT-simulation harness — disproportionate now).

**Next rotation:** audio is now mature (mesh + SFU + active-speaker + static/ephemeral TURN + bundled
relay); the only remaining north-star item (cascaded SFUs) is a multi-node infra epic that is NOT
loop-sized or loop-verifiable. So next tick should rotate to a **tick-sized** improvement — a chat/realtime
or infra/UX polish, or a QA coverage gap — not force more audio.

**Component advanced:** audio (free-to-self-host at scale — bundled the missing NAT-traversal relay).
**Cadence:** shipped a feature → ACTIVE (1800s).

## 2026-06-21 (iter 217) — finished the bidi class (all rendered names); security hardened → ROTATE next

Security tick 2 (within the ~2-tick cap): extended iter-216's Trojan-Source defense from message bodies
to EVERY other rendered display name — server/channel/global-channel/thread/category/group-DM names,
custom status + emoji, and custom role names (9 store write paths, via `stripBidiControls` or the shared
`validateRole`). Reproduced first (server name stored 9 controls verbatim) → fixed → re-attacked (clean) →
proved no collateral (Arabic + emoji name preserved) → regression (`TestBidiControlStrippingNamesIntegration`)
→ **live-probed the deploy** (created a server named "Guild‹RLO›HACK‹LRI›X" via REST → stored "GuildHACKX").
A nice scoping win from reproduce-first: it revealed **usernames were already safe** (auth's
`^[a-zA-Z0-9_]{3,32}$` regex), so I didn't waste a fix there — Rule 15's "reproduce before fixing" stopped
a non-fix.

**Component status — security is now in strong shape:** the Explore-agent coverage map (iter 216) showed
JWT/oversize/injection/traversal/rate-limit/channel-escalation all already tested; the only open gap was
bidi, now closed end-to-end. Per the rotation rule, **next tick must leave security** (2 ticks done).

**Highest-value next target = AUDIO (the component furthest from its north star).** UI is polished,
chat/realtime + security are solid, but audio's north star — *thousands* of participants — still needs the
documented next steps: **self-hosted TURN (hostile NATs)** and **cascaded SFUs**. That's a >3-file, new-
infra change → it must START with a SPEC (Rule 6) and run `stack-guardian` before adopting anything (Rule
16), and stay free to self-host (Rule A). So the next tick's first move is a SPEC slice (e.g. an optional
self-host coturn the one-command stack can point at), not code.

**Loop-process note (small):** the bidi fix is spread across 9 call sites with no structural guarantee a
FUTURE name-write path also sanitizes. Not worth a refactor now (over-engineering), but logged: if a 3rd
rendered-text field type appears, consider funneling name writes through one `sanitizeName` chokepoint.

**Component advanced:** security (hostile-input-proof — bidi class fully closed). **Cadence:** shipped a
fix → ACTIVE (1800s).

## 2026-06-21 (iter 216) — rotated to SECURITY; hardened Trojan-Source bidi spoofing (Rule 15, full cycle)

Acted on iter-215's rotation flag — left the voice/UI surface and advanced **security** (the
least-recently-touched component). Mapped the existing adversarial coverage with an Explore agent FIRST
(JWT/oversize/injection/traversal/rate-limit/channel-escalation all already COVERED — strong suite), then
targeted the one real GAP it surfaced: **Unicode bidi controls in message bodies** (Trojan Source,
CVE-2021-42574). Ran the full Rule-15 cycle: reproduced (integration test failed pre-fix — 9 controls
stored verbatim through `SaveReply`) → fixed at the store chokepoint (`stripBidiControls` on every write
path incl. `EditMessage` so an edit can't re-inject) → re-attacked (clean) → proved no collateral (emoji
+ZWJ, Arabic, CJK, LRM/RLM marks preserved) → regress (unit + integration + real-WS-ingest). **Live-probed
the deploy** (a Node WS client sent U+202E/U+2066 on the wire; the live server persisted "wire-live-clean")
— the real Rule-14 backend verification a bundle-grep can't give.

**Highest-value follow-up (GOAL.md):** the SAME spoofing class hits other rendered user text —
**usernames especially** (a U+202E username visually impersonates another user — higher impact than a
message body), plus channel/server names, custom status, thread/group-DM titles. Lift the sanitizer into a
shared helper and apply at each ingest; usernames warrant a stricter reject (identity field). Next tick =
security tick 2 (within the ~2-tick cap), then rotate.

**Loop-process playbook adds (two):**
1. **Map-before-probe for security ticks:** spawning an Explore agent to inventory existing adversarial
   tests vs. attack surfaces BEFORE picking a target stopped me from re-testing already-covered surfaces
   and pointed straight at the genuine gap. Codify: a security tick starts with a coverage map.
2. **Backend rollout-verify needs a live behavioral probe, not a bundle grep.** The skill's rollout-verify
   greps the SPA bundle for a shipped string — meaningless for a Go-only change. For backend ticks, hit
   the live endpoint and observe the new behavior (here: a live WS bidi probe). Added to the playbook.

**Component advanced:** security (hostile-input-proof — closed the bidi-spoofing gap). **Cadence:** shipped
a fix + an open security follow-up → ACTIVE (1800s).

## 2026-06-21 (iter 215) — shipped the deafen-mic-icon P1; flagging COMPONENT ROTATION (3 ticks on voice/UI)

Closed iter-214's own AI-vision P1: the panel 🎤 now strikes when `muted || deafened` so a deafened user
sees BOTH icons struck (Discord parity). Kept it display-only — `aria-pressed`/`data-muted` stay the real
mute toggle, a derived `data-mic-silenced` exposes the display state — so the click still flips `muted`
alone and un-deafen restores the prior mute. Browser QA proves deafen-ALONE strikes the 🎤 with
`aria-pressed` still false (isolating the derived-vs-toggle distinction) + clears on un-deafen; AI-vision
confirmed both icons struck. Shipped + railway + rollout-verified (live JS carries `data-mic-silenced`).

**Loop-process flag — ROTATE COMPONENTS next tick (owner's per-component-excellence directive, 2026-06-14:
"continuously and in rotation so no component stagnates").** Iters 213→214→215 were ALL voice-mute/deafen
(UI). That surface is now polished + well-tested; continuing to micro-polish it while other components wait
violates the rotation rule. Self-correction: next tick advance the component FURTHEST from its north star,
not the one I've been in. Candidate = **security** (hostile-input-proof) — a Rule-15 adversarial probe is
a listed Track-0 option and security is the least-recently-advanced surface (the loop has been on
audio/UI/chat parity for many ticks). Concretely next tick: pick ONE attack surface (WS frame validation,
JWT tampering/auth-bypass on a REST mutation, oversized/garbage body, or channel-access escalation),
reproduce the probe, prove it's blocked, and add a regression test (Rule 15's reproduce→fix→re-attack→
regress cycle). If already hardened, the probe becomes a permanent adversarial regression test — still net
coverage. **Playbook add: cap consecutive ticks on one component at ~2; the 3rd tick on the same surface
should trigger a deliberate rotation check.**

**Component advanced this tick:** UI (Discord-parity polish — mute/deafen-in-panel now feature-complete:
panel toggles → persist → apply-on-join (both flags) → deafen-strikes-mic). **Cadence:** shipped a fix →
ACTIVE (1800s).

## 2026-06-21 (iter 214) — closed the pre-call-deafen QA gap; AI-vision surfaced a deafen-mic-icon parity P1

Acted on iter-213's own logged gap (the loop fixing what it flagged): added a 2-client pre-call-DEAFEN
scenario to `qa/voice.mjs` proving BOTH effects deafen-on-join must have — (a) B's decoded RMS for A's
mic is ~0 (mic forced off) AND (b) A's inbound `<audio>` for B is `.muted` (incoming silenced) — then
un-deafen restores both (mic 0.0000→0.3143; incoming un-muted). The apply-on-join matrix now covers BOTH
persisted flags, not just mute. Full QA green (browser=0 realtime=0 voice=0 search=0).

**Two-effect insight:** deafen is the rare control with a SEND effect (mic off) AND a RECEIVE effect
(incoming muted). RMS only sees the send side (it reads the MediaStream, bypassing playback `.muted`), so
the receive side needs a DOM `.muted` assertion. A single-RMS check would have "passed" while silently
missing half the behavior. Playbook add (generalizing iter-213's note): **for a control with both a send
and a receive effect, assert each on its own channel — RMS for send, the `<audio>.muted` DOM prop for
receive.**

**First-run bug the QA caught in itself:** my initial deafen scenario re-ran `joinCall(b)`, but the
preceding mute scenario leaves B IN the call — so B has no "join voice" button and it timed out. Fixed to
only rejoin A (B stays; mesh re-offers). Lesson: when chaining scenarios that share browser contexts,
track the residual call state across scenario boundaries — each scenario inherits the previous one's
roster, it doesn't start clean.

**New P1 from AI-vision (GOAL.md, found this tick):** while deafened, the panel 🎧 icon shows the red
slash but the 🎤 mic icon does NOT — yet deafen silences your mic too. Discord struck-marks BOTH. Fix is
display-only: derive the mic-struck state as `muted || deafened` (don't touch the underlying `muted`
flag). Logged, not fixed this tick (the mute/deafen SPEC scoped in-call semantics as unchanged).

**Component advanced:** UI (Discord-parity polish) + the QA harness itself (coverage). No app code shipped
— a QA-only change, so committed + pushed but NOT redeployed (the served bundle is byte-identical; Rule 10
anti-churn). **Cadence:** shipped a real QA improvement + an open P1 remains → ACTIVE (1800s).

## 2026-06-21 (iter 213) — shipped mute/deafen-in-panel; the apply-on-join QA proves the mute path but NOT the deafen path

Implemented the queued SPEC end-to-end (slices 1–5): persisted `selfMute`/`selfDeafen` in
`voiceSettings.ts` (vitest), 🎤/🎧 panel toggles with a red-slash active state, `setMuted(on)` on the
VoiceTransport for a deterministic apply-on-join, and `joinVoice` seeding both from the persisted prefs.
Browser QA (panel toggle + localStorage persist + reload-survival + struck visual) and the 2-client voice
QA (B hears RMS 0.0000 on A's **pre-muted** join → 0.3043 after a panel un-mute) both went green; AI-vision
confirmed the panel + the in-call "· muted" consistency. Shipped + railway + rollout-verified (live JS
carries the aria-labels + `selfMute`; live CSS carries `sidebar-voice-btn`).

**Highest-value coverage gap (logged for next tick):** slice 3 applies BOTH self-mute AND self-deafen on
join, but the new voice QA only proves the **pre-call MUTE** path on the receiver's decoded RMS. The
**pre-call DEAFEN** apply-on-join is unproven end-to-end — when A joins with persisted self-deafen, the
session should (a) force A's mic off (B hears silence, like mute) AND (b) silence A's incoming audio
(A hears nothing from B). The browser QA toggles deafen but never measures the join-time effect. Next
tick: extend `qa/voice.mjs` with a 2-client pre-call-deafen scenario — set self-deafen in A's panel
before joining, then on join assert (a) B's RMS for A's mic is ~0 (mic forced off) and (b) A's inbound
`<audio>` for B is muted (deafen silenced incoming). This closes the apply-on-join matrix to both flags.

**Loop-process note:** the SPEC's slice-4 QA ask said "2-client voice QA proving B hears silence on A's
pre-muted join" — it scoped the *mute* proof but not the *deafen* proof, even though slice 3 ships both.
Playbook add: when an apply-on-join (or apply-on-X) change seeds MULTIPLE persisted flags, the QA slice
must cover EACH flag's runtime effect, not just the first/most-obvious one — one assertion per flag.

**Cadence:** shipped a real feature + an open follow-up coverage item → ACTIVE (1800s).

## 2026-06-21 (iter 212) — SPEC-first for mute/deafen-in-panel, then hand off to a fresh context (the right call after a long session)

The user kept the loop running (8th tick), signalling "do real feature work." The natural next parity
feature — voice mute/deafen in the user panel — is genuinely CROSS-COMPONENT (panel UI + persisted
voiceSettings + the live voice session's mute/deafen + apply-on-join), exactly what the loop flags for a
SPEC-first, fresh-context tick (Rule 6). Rather than build it in a very long context (degradation risk,
Step-0 judgment trigger), I did the correct first step: wrote an IMPLEMENTATION-READY SPEC (SPEC.md
"Voice mute/deafen in the user panel", 5 slices with exact file/line anchors + the 2-client voice-QA that
proves pre-call mute), queued it as a GOAL.md TOP-PRIORITY item, and proactively cleared context so the
NEXT tick implements it with a clean head. The `~/.claude/needs_clear` Stop-hook bridge is confirmed wired
+ safe (on-stop.sh; also auto-fires <20% ctx) and the pending ScheduleWakeup survives the clear, so the
loop continues uninterrupted — just fresher. **Lesson: "spec-now, build-next-tick-fresh" is the clean way
to start a big feature at the end of a long session — the SPEC is real progress AND the fresh context is
where the build belongs.**

## 2026-06-21 (iter 211) — 2nd green tick: confirmed maintenance plateau; next substantive work is a fresh-context big feature

Gate green (build/vet/test/live-health). Probed two more candidate small gaps — they're already done:
grouped-message hover-timestamp gutter (Discord parity) EXISTS (`.hover-time`, Chat.tsx ~3622), and
WS hostile/oversized/flood/access coverage is comprehensive (checked iter 210). Every contained item
I reach for is already shipped — the honest signal that the product has hit a maintenance plateau and
the remaining value is in LARGE features (mute/deafen↔voice integration; voice-channel/SFU evolution;
tunneling/accounts) that each need a SPEC-first tick from a fresh context (Rule 6), not a long one.
No code churn (Rule 10); idle_streak→2, cadence at the 3600s ceiling. The loop is correctly relaxing
to a light heartbeat while healthy; a specific user task or a fresh-context big-feature tick is the
right next mover. (Logged tersely on purpose — a green tick shouldn't grow the context it's conserving.)

## 2026-06-21 (iter 210) — GREEN maintenance tick: verified, no churn, widened cadence (the honest call after 5 ships in one long session)

Build/vet/test all green, live `/healthz` ok, the iter-209 deploy is live-verified, no P0/P1 open.
Checked the chat/realtime component's WS edge-case coverage before reaching for work: it's already
comprehensive — `TestServeWSHostileFrameHandling` (non-JSON garbage, type-confused field, empty/
whitespace/oversized body vs the 4 KiB `maxMessageSize`), `TestServeWSFrameSizeLimitIntegration` (a
>16 KiB frame rejected by `SetReadLimit`, distinct from the app-level body bound), the voice flood
guard, the hostile-kind-frame drop, and WS access control (garbage token 401, malformed channel 400).
No real WS coverage gap to close.

**Decision: a green tick is the correct output here, not invented work.** This session has shipped 5
substantive improvements (205 auto-load, 206 sidebar panel, 207+208 IDOR guards, 209 status-in-panel)
and the context is now very long. The remaining high-value work is LARGE features (mute/deafen needs
voice-session integration; group-DM/voice-channel evolution; tunneling/accounts) that each warrant a
fresh-context, SPEC-first tick (Rule 6) — exactly the kind of work a long, degrading context does
WORST. Per the loop's own rule ("a green iteration with nothing to fix is valid AND is the signal to
widen the cadence — log it, don't invent work to stay busy"), I logged green, made no code commit
(anti-churn, Rule 10), bumped `idle_streak`→1, and widened the next wake to 2700s. The right next
substantive tick starts from a FRESH context re-reading GOAL.md + this log.

**Loop-process note (reinforces 209):** the value of writing every decision here is exactly this —
after a `/clear`, the next tick reads "5 ships done, product green, do a big feature SPEC-first from a
fresh head" and picks up correctly with zero in-context memory. The durable log IS the loop's
continuity across context refreshes; a green tick that records WHY it was green is more useful to the
next tick than a forced micro-commit.

**Next-tick candidate (logged, fresh-context):** pick ONE large parity feature and do it SPEC-first —
strongest candidates: (a) voice user-panel mute/deafen quick-toggles (integrates the existing voice
DSP with an always-visible control), or (b) advance a non-UI component (audio→SFU is the big one but
needs stack-guardian per Rule 16; infra one-command-scale check). Start from a clean context.

## 2026-06-21 (iter 209) — Custom status in the user panel (rotated off security); + a RECURRING deploy-no-op finding the rollout-verify keeps catching

Rotated off the two security ticks (207/208) per plan, to a contained UI parity polish that builds on
the iter-206 sidebar user panel: the panel now shows the caller's **custom status + emoji under their
username** (Discord's user-panel layout). Used existing `myStatus`/`myStatusEmoji` state (already synced
from the member list) and the member-list status render pattern — hidden entirely when no status is set,
so it's zero-change for users without one. Verified: tsc + vitest 86/86, full browser/realtime/voice/
search QA green (extended the set-status flow to assert the panel shows it), AI-vision confirmed
"🚀 shipping presence" under the name, deployed + rollout-verified (bundle carries `self-chip-status`).

**LOOP-PROCESS finding (important, now confirmed TWICE — 206 and 209): the first `railway up` of a tick
silently no-ops.** Both ticks, the first deploy invocation (`eval "$CCF_DEPLOY_CMD" | tail -N`) returned
empty output and did NOT roll the bundle over (live stayed on the prior hash for 2+ minutes); a second,
DIRECT `railway up --service opencord --ci` printed full build logs + "Deploy complete" and rolled over.
The rollout-verify step (compare local vs live bundle hash) caught it BOTH times — `/healthz` was 200 on
the stale container the whole time, exactly the trap it guards. **This is why the loop's "never trust a
deploy you didn't rollout-verify" rule is non-negotiable.** Mitigation for future ticks (and a candidate
loop-rule tightening): after `railway up`, ALWAYS poll the bundle hash; if it hasn't changed within ~90s,
re-run `railway up` directly (not via the eval wrapper) and re-verify — don't assume the first one shipped.
Root cause not yet pinned (eval-wrapper vs direct invocation is the only obvious difference; possibly a
Railway CLI quirk when a build was recently triggered); worth a dedicated look if it recurs a third time.

**Next-tick candidate (logged):** continue the rotation — either advance a non-UI component (chat
realtime robustness, or an infra/one-command check), or the natural next user-panel parity step
(mute/deafen quick-toggles, which needs voice-session integration so it's a bigger, SPEC-first tick).
Avoid a 3rd straight UI-panel micro-polish; spread the work across components per the excellence rotation.

## 2026-06-21 (iter 208) — Finished the IDOR-scoping SWEEP: when one endpoint has an invariant gap, audit ALL its siblings in the same sweep

Executed the next-tick candidate I logged in 207: audited the SIBLING client-supplied-id read paths
for the same channel-scoping + `CanAccessChannel` invariant. Findings: all three message-read paths —
`RecentBefore` (history), `PinnedMessages` (pins), `SearchMessages` (search) — are uniformly gated by
`CanAccessChannel` in their handlers AND scoped by `m.channel_id = $1` in their store queries. The code
is safe everywhere. But only `RecentBefore` had an explicit cross-channel guard (207). Added
`TestSearchMessagesChannelScopingIntegration` for the highest-risk of the remaining two: **search**,
because it builds its WHERE clause DYNAMICALLY (free text + from:/has:/before:/after:), so it's the
most likely to regress — a new operator that dropped the leading `channel_id` AND-condition would leak
across channels with every single-channel test still green. Proven to catch it (Rule 15): forcing the
channel condition true → "free-text leaked a non-channel-A message"; restored → passes. Left `Pins`
without a dedicated test — its query is STATIC (`m.channel_id=$1 AND m.pinned`), the lowest refactor
risk; noted as verified-safe rather than manufacturing a low-value guard.

**Learning (logged): an IDOR-class finding is a SWEEP trigger, not a one-off fix.** When you discover
that an invariant (here: channel-scoping of a client-supplied id) needs a guard on endpoint X, the
same invariant almost always applies to X's siblings that share the access pattern. Audit them all in
the same arc and guard them by RISK (dynamic-SQL > cursor > static query), instead of guarding only
the endpoint that happened to surface the question. Two ticks (207 RecentBefore, 208 Search) closed
the message-read IDOR surface; Pins is verified-safe-by-construction. This "find-on-one → sweep-the-
siblings" rule generalizes to the next invariant class (e.g. admin-gating on the server-mutation
endpoints, block-symmetry on the DM paths).

**Process note (continued):** test-only tick again → committed + pushed, NO `railway up` (binary
byte-identical), NO browser QA (zero UI surface changed) — correct per anti-churn. Two consecutive
Track-0 security-coverage ticks balance the two prior feature ships (205/206); the suite is now
materially stronger on the read-path IDOR surface without any product churn.

**Next-tick candidate (logged):** rotate OFF security for a tick — either a UI/polish parity item
(e.g. surface mute/deafen in the new sidebar user panel, a natural Discord-parity add now that the
panel exists) or a fresh component per the per-component-excellence rotation (audio/chat/infra/UI).
The read-path IDOR sweep is done; don't keep mining the same vein past diminishing returns.

## 2026-06-21 (iter 207) — Track-0 security tick: IDOR guard on the pagination cursor; a global-id cursor is an attack surface even when the code is currently scoped

After two feature ticks (205 auto-load, 206 sidebar panel), spent this tick on Track 0 (the loop's
stated top priority — "do this MOST"): closed a real COVERAGE gap rather than shipping a 3rd feature.
Audited the iter-203 scroll-up cursor (`GET /messages?before=<id>` → `RecentBefore`) as a classic IDOR
surface: the cursor is a GLOBAL monotonic message id, so a natural attack is "feed channel A's endpoint
a `before` id from channel B and see if B's history leaks." The code is already safe (the query is
`WHERE m.channel_id = $1 AND m.id < $3`, and the handler gates on `CanAccessChannel`), but that
invariant was UNTESTED. Added `TestRecentBeforeChannelScopingIntegration` (interleaves ids across two
channels, pages A with B's cursors, asserts every row is A's). **Proved it catches a regression** (Rule
15, the full loop even for an already-secure path): temporarily dropped `m.channel_id = $1` → test
failed with "before=b3 leaked a non-channel-A message: id=118 channel=2"; restored → passed.

**Learning (logged): a cursor/id parameter is an attack surface to TEST, not just to read.** When an
endpoint accepts a client-supplied id that indexes a global/monotonic space (message ids, here),
encode the scoping invariant as a test even if today's query is correct — a future refactor that drops
the `WHERE channel_id` clause would otherwise silently turn it into a cross-tenant leak with all
happy-path tests still green. The proactive "break-it-to-prove-the-test-bites" step is cheap (one
revertible one-line patch) and is the only thing that proves the guard actually guards.

**Process note:** test-only tick → committed + pushed to source control but did NOT `railway up` (the
deployed binary excludes `_test.go`, so it's byte-identical; deploying would be pure churn, Rule 10).
The loop's "always railway up" is for APP changes; a pure test addition correctly skips it. Skipped the
every-3rd-tick browser QA too — zero UI surface changed and it ran fully green last tick (206).

**Next-tick candidate (logged):** audit the SIBLING client-supplied-id read paths for the same
channel-scoping + CanAccessChannel invariant and add guards where missing — pins (`?before`? no, but
`channel`), reactions, search (`SearchMessages`), thread history. If any read path scopes by something
other than the access-checked channel, that's the next IDOR test (or fix). One sweep, one guard each.

## 2026-06-21 (iter 206) — Bottom-left sidebar user panel (Discord layout); a RELOCATION is low-risk when QA selectors are class/aria-based, not position-based

Shipped the appearance slice-4 P2 fix the *right* way: instead of compacting the header `.meta`
cluster in place, RELOCATED it out of the header into a Discord-style bottom-left sidebar user panel
(`.sidebar-user`). This permanently fixes the group-DM / member-panel-open header wrap AND advances
Discord layout parity (the owner's TOP PRIORITY). Header is now a clean single row everywhere.
Verified: blast-radius guard PASS, tsc + vitest 86/86, full browser/realtime/voice/search QA green,
AI-vision confirmed the panel (desktop + mobile drawer) + the now-single-row group header, deployed +
rollout-verified (live bundle carries `sidebar-user`).

**QA-process learning: the blast radius of a UI MOVE is bounded by HOW the QA selects elements.**
~15 QA call sites touch the moved cluster (`user settings` button ×12, `log out` ×3, `.self-chip-pip`,
`.self-chip .avatar-self`, `/N online/`). Because they select by **role/aria-label/class** — not by
**DOM position** ("the button in the header") — moving the entire cluster to a different parent left
them ALL resolving unchanged. Only the THREE assertions that explicitly encoded *position* ("`.meta`
in the header", "reachable in the header") needed editing. **Lesson: aria/role/class-based QA
selectors make layout relocations cheap and safe; position-coupled selectors are the ones that break.**
This is the inverse of the iter-195 chat-header redesign, which was high-risk precisely because the QA
matched header buttons by visible TEXT in their header context. Bias the QA toward role/aria/stable-
class selectors so future relocations stay low-cost. (Confirmed by the blast-radius guard, which
classified each consumer as desktop-visible vs mobile-drawer and found zero breakage beyond the 3
position-coupled assertions.)

**Process note (the ONE real snag this tick):** the FIRST `railway up` returned empty output and did
NOT roll over (live bundle unchanged after 2+ min) — a silent partial/interrupted deploy. The
rollout-verify step (compare local vs live bundle hash) CAUGHT it; a re-run printed "Deploy complete"
and the hash matched. **Reinforces: never trust a deploy you didn't rollout-verify** — `/healthz` was
200 the whole time (the OLD container), exactly the trap the loop's bundle-hash check exists to catch.
The loop rule already mandates this; this tick is proof it earns its keep.

**Next-tick candidates (logged):** (1) the user panel could gain Discord's mute/deafen mic icons (we
have voice DSP + input-volume already — surfacing quick mute/deafen toggles in the panel is natural);
(2) message grouping (consecutive same-author messages within N min collapse the avatar/name header,
Discord-style) — a visible chat-density parity gap worth a look.

## 2026-06-21 (iter 205) — Auto-load-on-scroll (Discord parity); when a NEW path subsumes an old one, test the UNIFIED behavior, not the old discrete step

Shipped Discord-style auto-load-on-scroll for history: scrolling near the top of `.messages` now
pages older history in automatically, with the iter-204 "↑ Load older" button kept as a visible
fallback. Stale-closure-safe via `loadOlderRef`/`hasMoreHistoryRef` mirrors feeding the once-created
native scroll listener (the existing `messagesRef` callback-ref), guarded by `loadingOlderRef` +
`hasMoreHistory`. Verified: blast-radius guard PASS, tsc clean, vitest 86/86, full browser/realtime/
voice/search QA green (browser=0…), AI-vision confirmed the anchored mid-history view, deployed +
rollout-verified (live bundle byte-identical to the local build).

**QA-process learning (the real lesson): a discrete assertion can become unmeasurable once a new code
path subsumes the old one.** My first QA attempt kept the iter-204 "click the button → count grows"
assertion AND added a separate "scroll to top → count grows" assertion. It FAILED (`120 → 120`) — not
because auto-load was broken, but because **reaching the button requires Playwright to scroll it into
view, which now triggers auto-load**, so by the time the button-click resolved, the scroll-into-view +
button had already cascaded through ALL pages (the button test reported `50 → 120`, impossible from a
single 50-capped fetch — proof both paths fired). The old discrete button-click step is no longer
isolable. **Fix: test the UNIFIED behavior** — assert the fallback button is *present*, then assert
scroll-to-top auto-loads (the now-primary path) without yanking. When a feature merges two triggers
into one path, don't assert each trigger separately; assert the observable outcome once.

**Loop-process note:** this is the same family as "a test-trigger gap is often a product gap" (iter
202) but inverted — here a passing-looking change exposed that an EXISTING assertion had silently
become a tautology against the new behavior. Whenever a change touches a path an existing QA step
drives, re-read that step and ask "does this assertion still measure what it claims, or did my change
make it trivially true/false?" Added to the playbook.

**Next-tick candidate (logged):** the appearance slice-4 P2 — the header `.meta` cluster (online
count + self-chip + log out) still wraps to a 2nd row in group DMs / with the member panel open
(see 07l-group-header.png). The Discord-faithful fix is a bottom-left sidebar user panel (move user/
account controls out of the header), but it's >3 files + touches ~10 QA selectors matched by text
("log out", ⚙, "N online") → needs a SPEC'd, selector-migrated multi-slice tick (same shape as the
iter-195 chat-header compaction). Flagged, not started, to keep this tick surgical.

## 2026-06-21 (iter 204) — History pagination frontend; SEEDING GLOBAL STATE in QA pollutes other tests' assertions

Shipped the "Load older messages" frontend slice (prepend + scroll-anchor + skip-the-auto-scroll). It
WORKED first try in QA — the wins were process choices, and one real QA-architecture lesson.

**Lesson 1 — de-risk a fiddly interaction to its simplest shippable form.** The spec wanted auto-load-
on-scroll-to-top, which needs a scroll handler calling a state-dependent callback (stale-closure trap,
the iter-191 rabbit hole). On a saturated context I shipped an explicit "Load older" BUTTON instead: same
core value (older history accessible), none of the stale-closure/scroll-trigger complexity. Auto-load is
a clean future enhancement. Pick the version whose risk matches the context.

**Lesson 2 — seeding GLOBAL state to test feature A breaks feature B's tests.** To make the button appear
I needed 50+ messages, which WS rate-limits and REST won't do (needs a file), so run.sh SQL-seeds a
global `pgseed` channel (60 msgs). The pagination test passed immediately — but the realtime tab-badge
tests FAILED: a global channel with 60 messages nobody read is 60 UNREAD for every user, so B's tab
showed `● Opencord` when the test expected a clean title. The seed was correct for the feature under test
and wrong for an unrelated one sharing the same global namespace. Fix: have the affected test READ the
seeded channel first (conditional on its existence). **Rule: when a test fixture mutates shared/global
state (a global channel, a default workspace, a singleton), audit every OTHER test that reads that state
— unread counts, "list all X", totals, badges — and neutralize the fixture there. Prefer scoping the
fixture (a private/server channel) when possible; when it must be global, make the readers tolerant.**

**Lesson 3 — the anchor proof is visible, not just numeric.** "Count grew 50→60" proves paging; "not at
bottom after" proves the anchor didn't yank. But the AI-vision screenshot (older seeds in view + the
jump-to-present pill showing = stayed scrolled up) is what confirmed the *experience* is right, not just
the metrics.

## 2026-06-19 (iter 203) — A functional gap (fixed-window history) → backend pagination, which UNCOVERED a latent prod boot bug

Swept the FUNCTIONAL axis and found a real gap: history is a fixed 50-message window (no `before`
cursor), so a busy channel can never show older messages. Shipped the backend slice (RecentBefore +
`?before=`). But the headline was the bug the work flushed out.

**Adding a test that re-migrated AFTER an existing test exposed a production boot bug.** My new store
test runs `setup()` (which re-runs the full schema) AFTER `TestRenameGroupDMIntegration` creates two
group DMs named "Shared Name". The migrate then failed: the INTERMEDIATE recreate of
`channels_global_name_uniq` used `kind <> 'thread'` — it didn't exclude `'dm'`, even though the FINAL
index (and the naming feature) allow same-named group DMs. Because `db.Migrate` re-runs the whole schema
EVERY boot, once two same-named group DMs exist in prod, that intermediate CREATE fails with 23505 and
the server won't start. Prod wasn't broken yet (no same-named group DMs), but it was a latent trap.

**Lessons:**
- **A test that re-runs migrations against accumulated data is a cheap fuzzer for migration idempotency.**
  The bug was invisible to every existing test because none re-migrated after the duplicate existed;
  one new test in the right position caught a real prod-boot hazard. Worth having a dedicated
  "migrate twice on a DB that exercised every feature" guard.
- **When a feature loosens a uniqueness rule, grep for EVERY index/constraint on that column — including
  transient/intermediate migration steps.** Group-DM naming correctly fixed the FINAL index but left an
  earlier recreate inconsistent; both run every boot.
- Don't anchor on the first hypothesis: I assumed a `uniqueChannel()` UnixNano collision (fixed it too,
  a real latent flake), but the actual cause was the migration. Querying the DB for the real duplicate
  (it returned none under my assumed predicate) is what redirected me to read the index's true WHERE.

## 2026-06-19 (iter 202) — Error-state axis: a "Reconnecting…" banner; the failing test pointed at a real PRODUCT gap, not just a test gap

Continued sweeping fresh axes (now error-states): added a debounced "Reconnecting…" banner for a dropped
socket. The interesting part was the QA.

**The first banner test FAILED — and that failure was a real finding, not a flaky test.** Playwright's
`setOffline(true)` fires the browser `offline` event but does NOT close an open WebSocket, so `connected`
stayed true and the banner never showed. My first instinct was "the test can't simulate this." But the
SAME limitation is a real-world gap: a silent network drop (no close frame) leaves the app's socket
sitting "open" until the ~60s ping timeout — so the reconnect backoff AND the banner wouldn't start for
up to a minute on a real silent drop either. The fix served both: listen for the browser `offline` event
and proactively `ws.close()` the dead socket → backoff + banner start immediately → AND the test now
passes (setOffline fires `offline`). One change closed a test gap and a product gap together.

**Lesson — when a test can't trigger a state, ask WHY before declaring it untestable; the trigger gap
is often a real product gap.** "I can't make the socket close in the test" was the same fact as "the app
won't notice a silent drop for 60s." Treating the test difficulty as a signal (not an obstacle to route
around) surfaced the better fix. Also: a "banner clears after recovery" assertion is VACUOUS if the
banner never appeared — only meaningful once the "banner appears" assertion is a real pass first.

## 2026-06-19 (iter 201) — "Exhausted small-win surface" was premature: a high-frequency keyboard gap (Esc-closes-panel) was still open

After two green ticks I'd concluded the small-win surface was exhausted. Looking once more from a
DIFFERENT angle — keyboard parity rather than security/coverage/layout — surfaced a real, high-frequency
gap: Esc didn't close the open pins/search/members panel or exit a thread (a reflexive Discord habit).
Shipped it: a global keydown handler gated to skip while typing and to DEFER to any open modal/picker.

**Lesson — "no more small wins" is an angle-dependent claim, not an absolute.** My iters-199/200 sweep
checked security, test-coverage, and layout and found everything handled; I generalized that to "nothing
left." But I hadn't swept INTERACTION/keyboard parity. The honest version of "the surface is exhausted"
is "exhausted along the axes I checked" — before declaring equilibrium, enumerate the axes (security,
coverage, layout, **interaction/keyboard**, a11y, perf, error-states) and confirm each, rather than
extrapolating from three.

**The modal-deferral pattern, banked for reuse:** a new GLOBAL key handler must list every existing
overlay that owns the same key and bail when one is open (here: settings/new-DM/roles/profile/emoji/
reaction/PTT-rebind for Esc). The regression proof is cheap and strong: the pre-existing "Esc closes the
settings modal / profile card" tests must STILL pass alongside the new "Esc closes the panel" — if the
global handler fought them, one would flip. Green on both = the precedence is correct.

## 2026-06-19 (iter 200) — Maintenance EQUILIBRIUM reached; the remaining value is in big features that need a fresh-context design tick

Second consecutive verified-green tick (idle_streak → 2, cadence at the 3600s ceiling). Checked one more
edge this tick — WS auto-reconnect — already covered (realtime.mjs §1e). Across iters 199–200 the
small-win surface is exhausted: uploads, markdown XSS, self-chip truncation, header-redesign coverage,
and reconnect are all handled and tested.

**The honest read: this product's CURRENT scope is feature-complete + well-tested; the remaining value
is in the big `[ ]` backlog** — built-in free secure tunneling (the #1 North-Star differentiator),
real email accounts, roles hierarchy / granular permissions, federation. Each is a multi-tick effort
that starts with a SPEC (Rule 6) and, for tunneling, real research into free self-hostable approaches
(+ stack-guardian per Rule 16, + Rule A "degrade gracefully without a paid third-party"). That design
work is exactly what a 16-tick-saturated context does POORLY — a half-baked spec for a complex feature
misleads the implementer. **Lesson: when the small-win surface is exhausted AND the next work is
big-design, the highest-leverage move is a context refresh, THEN spec-first on a big feature — not
grinding marginal changes or rushing a spec while saturated.** Recommending /clear; the cloud 3h cron
is the real idle floor now.

## 2026-06-19 (iter 199) — A VERIFIED-GREEN maintenance tick: the honest output when a mature product has no high-value work

After the 3-slice header redesign (and 14 prior productive ticks this session), I went looking for the
next ship and genuinely found none worth doing. I checked four candidate areas and each was already
handled: file upload/serve (sniffed content-type, inline raster-only allowlist, HTML-as-download +
nosniff, path-traversal — all tested), markdown XSS (autolinker http(s)-only + the no-sink lint),
the header meta username (already max-width:120px + ellipsis), and the header-redesign regression
coverage (the existing narrow-header test already guards no-wrap + no-horizontal-overflow). The one
concrete remainder — header slice 4 (meta single-row when the member panel is OPEN) — is low value (the
2-row panel-open header is GRACEFUL, not broken) and carries Rule-18 risk (it would touch the deliberate
iter-90 flex-wrap fix late in a very long context).

So I ran the full QA as a real end-to-end regression check (green: browser/realtime/voice/search all 0)
and logged a green tick — no commit, idle_streak bumped so the cadence widens.

**Lesson — a verified-green tick is a valid, honest output, not a failure to ship; and recognizing it is
a skill.** The anti-pattern (Rule 10) is manufacturing a marginal change just to have a diff. The
discipline is: actually look (I checked four surfaces, didn't just assert "all good"), confirm green with
a real test run (not just the build gate), bank the cadence-widen signal, and — after a long session
where fresh wins are getting scarcer — recognize that a CONTEXT REFRESH is the highest-leverage next
move: a fresh window re-reading GOAL.md/SPEC.md/IMPROVEMENTS.md will surface backlog a saturated context
can't. "Nothing high-value to ship right now" + "long session" is itself the signal to widen and clear,
not to invent work.

## 2026-06-19 (iter 198) — Header redesign SLICE 3 (collapsible search): a state-gated element needs the QA to OPEN it first

Final functional slice: the always-open search box became a 🔍 toggle (expand on click, collapse on
Esc/empty-blur/channel-switch/clear). Action bar is now a single icon row; shipped + rollout-verified.
The 3-row → 2-row → (action-bar single-row) arc of the header P2 is essentially resolved for the common
(member-panel-closed) view; the only residue is the meta cluster wrapping when the panel is open (logged
optional slice 4).

**QA lesson — when an element becomes state-gated (only exists when expanded), the test must OPEN it
first, idempotently.** The search input now lives behind the toggle, so I added an `openSearch()` helper
that clicks `.search-toggle` IF present (collapsed) then waits for `.search-input`, and called it before
EVERY search fill — because `clearSearch` (and channel-switch) now collapse the box, so a fill after a
clear would hit a missing input. Idempotent-open-before-use is the clean pattern for any
expand/collapse UI (it's the same shape as "ensure the panel is open before asserting its contents").

**Also: know whether a surface is always-on or on-demand before judging "single-row".** The member-list
panel is TOGGLED, so the default server-channel header is full-width and genuinely single-row now; only
the panel-open case narrows the column enough to wrap the meta. Scoping the acceptance to the actual
default view kept me from over-chasing a narrowed edge case as if it were the common one.

## 2026-06-19 (iter 197) — Header redesign SLICE 2 (icon-ify): slice 1's de-risking made the re-skin a clean, single-fix tick

Re-skinned the header action bar from text labels to compact emoji icon buttons (aria-label + title).
Cut the server-channel header from THREE rows → TWO (AI-vision verified), shipped + rollout-verified.

**The payoff of staging:** because slice 1 (iter 196) had already moved every QA selector off button
text, this re-skin — which dropped ALL the visible labels — broke only TWO assertions, both state
read-outs that genuinely encoded state in text: the readonly toggle (text → aria-label) and the
voice-presence count (text → data-count). One QA run found both; fixed in one pass. Contrast the
iter-190 fear (an un-staged icon-ify breaking ~10 sites at once across 3 files). **Lesson: a re-skin's
blast radius collapses to "just the state read-outs" once the find/click selectors are decoupled first —
the staging investment (a whole prior tick) paid back as a near-zero-surprise change.**

**A nuance worth keeping:** when you move a control's label into an icon, any test that read the control's
STATE from its text must move to the same place the state now lives — aria-label (semantic, A11y-aligned)
for an on/off toggle, a `data-*` attribute for a numeric/value read-out. Decide per control: aria-label
when it's a human-readable state phrase, data-attr when it's a value the test compares.

## 2026-06-19 (iter 196) — Executed header-redesign SLICE 1 (decouple QA from button text); the spec paid off immediately

Ran the first slice of the iter-195 header spec: migrated all 10 text-based header-button selectors
(`getByRole('button',{name:'Join voice'/'pins'/…})`) to scoped `.chat-header .<class>` locators across
browser/sfu/voice QA. No UI change; full QA green. Now the upcoming icon-ify (slice 2) can rename/replace
the visible labels without breaking a single test.

**Why this validated the iter-195 defer-and-spec call.** Splitting "migrate selectors" into its own
shippable slice meant this tick was tiny, mechanical, and zero-risk — the exact opposite of the
multi-file regression a one-shot icon-ify on a long session would have been. Two refinements the actual
work surfaced that the spec didn't fully anticipate:
- **A toggle's STATE lived only in its text** (`make read-only` ↔ `allow everyone`). A pure class
  selector can't assert which state it's in, so I read the state via the class element's `textContent`
  (`.readonly-toggle` textContent includes 'allow everyone') — class-anchored for *finding*, text for the
  *state value*. Slice 2 will move the state to the icon/aria-label; for now this keeps the assertion
  meaningful without a text-name selector. **Lesson: "decouple from text" isn't uniform — a click/find
  selector decouples cleanly, but a state-readout that text encodes needs the text until the state has
  another home (icon/aria/data-attr).**
- **Scope class selectors to a container** (`.chat-header .voice-join`, not bare `.voice-join`): the old
  `getByRole(name)` was page-wide but unique-by-accident; a bare class could match a same-class button in
  the voice-channel view. Scoping to the header keeps it unambiguous.

## 2026-06-19 (iter 195) — Vision-audit tick → SPEC the deferred header redesign rather than start it on a long session

A Track-0 AI-vision audit of the rendered surfaces (mobile drawer, server-channel + member list, etc.).
Conclusion: the product is well-polished; the one real, recurring gap is the header P2 — confirmed by
eye that in a server channel with the member list open (the COMMON view) the header wraps to THREE rows.

**The discipline decision: on the 11th tick of a long session, I did NOT start the big multi-file header
redesign.** Per the blast-radius finding (it touches ~10 text-coupled QA sites across 3 files) and Rule
18 (a big change late in a long context is exactly when regressions slip in), the right move was to
write the SPEC (SPEC.md "Chat header — compact action bar") with a 5-slice staged plan whose FIRST slice
is "migrate the QA selectors from button-text to classes, no UI change" — so the next fresh-context tick
can build it without the test rework and the re-skin colliding. Rule 6 (spec before big changes) is also
the cheapest insurance: capturing the blast-radius + staging now means the next attempt doesn't
re-discover them the hard way.

**Also resisted a rabbit hole:** the member-list name truncates early ("alice72…") on long QA usernames;
the CSS (`.member-id` flex vs `.author`) is murky and the symptom is data-specific (real usernames are
shorter). Chasing it on a long session risked an iter-191-style spiral for a cosmetic, low-value issue —
noted, not chased. **Lesson: a vision audit's value is also in what it tells you NOT to do right now —
deferring-with-a-spec and explicitly-not-chasing are valid, honest tick outcomes, not a failure to ship.**

## 2026-06-19 (iter 194) — A small high-frequency feature (ArrowUp-edits-last) after a test run; deterministic-fixture QA + grep-by-hash rollout

A clean feature tick after several test/guard ticks: the Discord "↑ in an empty composer edits your
last message" shortcut (was missing — ArrowUp was only wired to @mention nav). ~10 lines reusing the
existing inline-edit UI; guarded so it only fires on an empty composer. Shipped + deployed +
rollout-verified.

Two small reusable QA techniques banked:
1. **Deterministic fixture over inferred state.** My first instinct was "ArrowUp edits the last own
   message — assert it shows the last message's body". But which message is newest depends on every
   prior QA step. Instead, SEND a fresh known message (`'arrow-up edit me'`) immediately before, so the
   target is deterministic and the assertion is `inputValue() === 'arrow-up edit me'`, not a fuzzy
   "includes". When a test needs "the latest X", create X in the test rather than reasoning about state.
2. **Rollout-verify by bundle HASH when there's no feature string.** Past features had a user-visible
   string to grep in the live JS ("Jump to present"). This one reuses startEdit — no new string. So I
   verified the rollout by asserting the live `assets/index-*.js` hash equals the local `npm run build`
   output: content-hashed bundles mean identical hash ⇒ identical bytes ⇒ the new code is serving. A
   clean rollout proof for stringless changes.

## 2026-06-19 (iter 193) — Backend Rule-15 gap: the WS transport frame cap was untested; "a close test can falsely pass on its own deadline"

Rotated to backend after a run of frontend ticks. Found a real coverage gap: the WS hostile-frame
battery tested the app-level body cap (maxMessageSize 4096 → oversized body dropped, connection lives)
but NOT the transport-level `SetReadLimit(maxFrameSize 16384)` — the guard that closes the connection
when one frame exceeds the limit, stopping a hostile client from streaming an unbounded frame to OOM
the server. Added `TestServeWSFrameSizeLimitIntegration` (send >16 KiB → assert close + nothing
persisted).

**Lesson — a "the connection should CLOSE" test can falsely pass on its OWN read deadline.** My first
version read in a loop with a 5s deadline and treated any read error as "closed". But if the guard were
broken (connection stays open), the 5s deadline itself fires a timeout error → the test passes for the
WRONG reason. Fix: distinguish the cause — `if ne, ok := err.(net.Error); ok && ne.Timeout()` → a
TIMEOUT means the connection stayed open = FAIL; only a real close/EOF passes. Proven by raising
maxFrameSize to 1 MiB (test FAILS "connection stayed OPEN"), then restoring 16384 (PASS). Same family as
the iter-191 instrument-don't-guess lesson: a green assertion is worthless until you've shown it goes RED
for the real defect — and for negative/"should-not-happen" tests, watch that your own timeout/teardown
isn't the thing satisfying the assertion.

## 2026-06-19 (iter 192) — XSS-by-construction audit → a whole-CLASS guard; and "vitest green ≠ build green"

Instead of adding the next per-field XSS render test from the ledger (server/channel name — all of
which are React-escaped, same proven mechanism), audited the WHOLE client and found it has zero
unsafe sinks (no dangerouslySetInnerHTML / innerHTML= / eval / new Function anywhere). Encoded that as
a single codebase-wide invariant lint (`check-no-unsafe-sinks.mjs`, wired into qa/run.sh) that fails if
ANY future code introduces a sink — adversarially verified with a probe file. **Higher leverage than N
per-field tests: guard the class, not each instance.** Reinforces the iter-189 "order a guard ledger
by blast radius" lesson — the very top of that ladder is "does an unsafe sink exist at all", and one
lint covers it forever.

**The real catch — "vitest green ≠ build green".** I first wrote this as a vitest test (`security.test.ts`)
using fs/path/process. `npx vitest run` passed 94/94 — but `npm run build` (`tsc -b`) FAILED: the app's
`tsconfig.json` includes `src/**`, so it type-checks test files too, and the browser tsconfig has no node
types. Had I trusted the green vitest run and committed, the next deploy's build would have broken. I
caught it because Rule-14 verify means running the PRODUCTION build, not just the test runner. Fix: move
the source-scan to a standalone node lint (its natural home — it's a lint, not a typed unit test), leaving
tsconfig untouched (no weakening of the existing tests' type-checking). **Lesson: a "test" that needs node
APIs does not belong in a browser app's type-checked source; and always run `npm run build`, not just
`vitest run`, before calling a test-tier change done.**

## 2026-06-19 (iter 191) — Smart auto-scroll + jump-to-present; INSTRUMENT-don't-guess turned a 6-rebuild rabbit hole decisive

Shipped a real UX-bug fix (the list stopped yanking a reader to the bottom on every new message while
they read history) + a Discord "↓ jump to present" pill. The build was easy; getting the QA green took
six rebuilds, and the lesson is in HOW it got unstuck.

**Two real gotchas, found by instrumenting — not guessing:**
1. **React 18's delegated `onScroll` never fired** for this scroll container. I burned two rebuilds
   swapping suppression logic and wheel-vs-programmatic scrolling on a hunch. What actually resolved it:
   adding `window.__ocAttach`/`__ocScroll` flags and reading them from the test. That printed
   `attach=messages scrollFires=0` — proving the listener was on the right element but NO scroll event
   reached it. Switched to a NATIVE `addEventListener('scroll')` via a CALLBACK ref (attaches exactly on
   mount, dodging the conditionally-rendered-container race a []-effect had).
2. **The failing assertion was the TEST, not the feature.** `scrollFires=0` with `scrollTop=0` meant the
   list was already at the TOP after the viewport shrink (the few messages didn't overflow at full
   height, so it was never auto-scrolled to the bottom) — so "wheel up" did nothing. Fix: wheel DOWN to
   pin first, then UP to reveal the pill. I'd assumed "shrink → at bottom"; the instrumentation proved
   otherwise.

**Lesson — when a UI test fails in a way that "should be impossible", STOP changing the code and add
two-line `window.__flag` instrumentation read from the test.** One decisive data point (`attach=messages
scrollFires=0`) replaced four speculative fixes. Guessing at React event quirks is a rabbit hole;
measuring the exact break point (ref attached? event firing? state set? element scrolled?) collapses it.
Also banked: a **programmatic `el.scrollTop=` does NOT fire a scroll event a listener sees** — drive
scroll tests with real `mouse.wheel` gestures.

## 2026-06-19 (iter 190) — Blast-radius check ABORTED a risky redesign mid-plan → pivoted to a clean additive win (PWA)

Came in planning to icon-ify the DM header (the iter-188 P2). Ran the blast-radius grep BEFORE editing
and found the QA matches header buttons by TEXT (`getByRole('button',{name})`) in ~10 call sites across
browser.mjs + voice.mjs + sfu.mjs, plus a header-line-count test. That's a multi-file QA rework, not a
one-tick re-skin — so I STOPPED, logged the precise scope into the GOAL P2, and pivoted to a clean,
zero-blast-radius, real-parity win instead: the **installable-PWA foundation** (branded SVG favicon,
manifest.json, theme-color, OG tags). Shipped + deployed + prod-rollout-verified (favicon→image/svg+xml,
manifest→application/json), browser-QA `0b` guards it, AI-vision verified the rendered icon.

**Lesson — the blast-radius check is a PRE-EDIT gate, not just a post-edit guard.** Running the
consumer grep before touching code turned a half-built risky redesign (which would've broken ~10 QA
assertions and likely overrun the tick) into a 2-minute "this is bigger than it looks → defer + spec"
decision. When a planned change's first grep lights up many text-coupled consumers, that's the signal
to convert it into a spec'd dedicated tick and ship something else clean this tick — don't push a big
change through on momentum.

**Also (small, reusable):** to vision-check a static SVG asset, render it to PNG via the already-installed
Playwright (`page.setContent(<svg>)` → `locator('svg').screenshot()`) and `Read` that — no new dep, and
it catches a malformed/blank icon the JSON/serve checks can't.

## 2026-06-19 (iter 189) — Rule-15 ledger advanced to the RICHEST surface (markdown autolinker); deferred the header redesign as spec-worthy

Continued the render-site XSS-guard ledger, but jumped to the **highest-leverage** entry instead of the
next plain-text one: the **markdown autolinker** — the only render path that turns user text into a
clickable `<a href>`, and thus the one place a careless future change (broadening the scheme regex)
could reintroduce real `javascript:`/`data:` XSS. Encoded 5 invariants as unit tests: javascript:/data:
never autolink, only http(s) does, every `<a>` carries `target=_blank` + `rel=noopener noreferrer`, the
parser never extracts attributes from a URL, and raw `<script>/<img onerror>` render inert. vitest 86/86.

**Ledger status:** guarded = message body (now incl. explicit autolinker + raw-HTML unit tests), About
Me (iter 141), group name (iter 187). Still unguarded (quick future wins): server name, channel
name/topic, status text — all plain-text (React-escaped), lower-risk than the autolinker, so rightly
deprioritised behind it. **Lesson — when working a guard ledger, order by BLAST RADIUS, not by list
order: harden the one surface that turns text into executable/clickable output before the N plain-text
ones that only ever render as escaped strings.**

**Deferred (correctly):** the iter-188 P2 — icon-ify the DM header's text-label controls / overflow
menu — is the real single-row fix, but it's a multi-element redesign (~10 buttons + a collapsible
search) that needs a SPEC.md entry (Rule 6) and careful staging so it doesn't regress the many header
QA flows. A partial slice would repeat the iter-188 "doesn't visibly fix it" trap. **Recommendation: a
dedicated tick that specs then builds it end-to-end**, rather than nibbling at it. It stays a P2 in
GOAL.md until then.

## 2026-06-19 (iter 188) — Header title truncation; AI-vision kept the fix HONEST (a green metric assertion ≠ the visual outcome)

Fixed the iter-187 P1: the chat header title now ellipsis-truncates (`max-width:min(36ch,55vw)`, the
`.channel-topic` pattern) with the full title on a hover `title` attr. Browser QA `07k4` sets an
~82-char name and asserts the title element is clipped (scrollW 676 > clientW 373) — green.

**The lesson is the vision pass, not the fix.** The `scrollWidth > clientWidth` assertion PASSED, which
on its own reads as "header layout fixed." But `Read`-ing the screenshot showed the header STILL wraps —
the title is now capped, yet the DM header's six text-label controls (pins/mute/add/rename/leave/voice)
+ a wide search box overflow the row independent of the title. Had I trusted the green metric, I'd have
marked "header single-row — DONE" and shipped a false claim. Vision caught the divergence: I fixed the
NAMED defect (title growth) and honestly logged the REMAINING half (control density → needs icon-only
buttons / a "⋯" overflow menu, Discord's approach) as a P2 instead of overclaiming.

**Rule (reinforces Step 4b-i): when the bug is a LAYOUT/visual symptom, a DOM-metric assertion proves
the MECHANISM, never the OUTCOME — you must vision-grade the rendered result and scope your "done"
claim to what the eye confirms, not what the metric passes.** A passing assertion that doesn't visibly
resolve the reported symptom means the symptom had more than one cause; find the others before claiming
the fix.

## 2026-06-18 (iter 187) — Rule-15 rotation: hardened the newest user-text render site (group name); vision caught a header-truncation P1

Rotated off group-DM features to a **security/Rule-15 pass**, applied to the *newest unhardened input*:
the group `name` shipped iters 184–186 is user-controlled text rendered in header/sidebar/welcome/
composer with no inert-render guard. Added browser QA that renames a group to
`<img src=x onerror="window.__ocGrpXss=1">` and proves it's inert (literal text, no `<img>` injected,
onerror never fires). The payload *discriminates*: if escaping ever breaks the flag flips and the test
goes red. Confirmed the backend is already safe (parameterized SQL, rune-based ≤100 cap) and the other
name sinks (document.title, Notification API) are text-only → React escaping is the sole defense, now
guarded.

**Lesson — keep a running "user-text render-site → XSS guard" ledger.** Guarded so far: message body
(escaped-script test), About Me (iter 141), **group name (this tick)**. Still UNGUARDED and worth a
future Rule-15 tick: **server name**, **channel name/topic**, **status text**. The rule: any tick that
ships a feature rendering NEW user text adds its inert-render assertion in the SAME tick (Rule 15 step
5) — don't let the ledger grow an unguarded entry.

**Vision caught a real P1 (logged to GOAL.md appearance-polish):** the adversarial 41-char name made the
chat HEADER wrap its actions to a second line — the header title doesn't ellipsis-truncate like the
sidebar does (iter 114). This bites NORMAL use too (a group with many members has a long member-list
title). Concrete next-tick fix: `min-width:0`+`overflow:hidden`+`ellipsis` on the header title, actions
`flex-shrink:0`, + a many-member-group QA assertion that the header stays one row. **Meta-lesson: an
adversarial input is also a free stress test of layout — vision-grade the screenshot for polish, not
just the security assertion.**

## 2026-06-18 (iter 186) — Closed the rename realtime gap; DM-membership realtime coverage is now COMPLETE → rotate component

Added realtime.mjs §16: A renames a shared group → B (viewing it, never reloading) sees the header,
sidebar row, welcome title, AND composer all relabel to the custom name LIVE, while the welcome
subtitle still lists the members (`dmMembersLabel`). AI-vision confirmed all four surfaces on B's
screen. This closed the exact gap I logged last tick — the "realtime relabel works" line is now a
two-client guarded fact, not an inherited assumption.

**The DM-membership realtime trio is now saturated:** add (§15, SendToUser push), leave (§14, channel
broadcast), rename (§16) are all two-client verified via the same `dm-membership` refetch path. The
marginal value of more group-DM tests is now LOW.

**Process lesson — reuse a prior section's fixture instead of re-scaffolding.** §16 reused §15's group
(A + B already on its WS) rather than registering+creating a fresh group, so the whole realtime relabel
proof was ~20 lines and added almost no wall-clock. When two flows share a precondition (a group both
clients are watching), chain them; don't rebuild it.

**Next tick: ROTATE off group DMs** (the standing GOAL.md rotation note + owner's UI-parity priority).
Group-DM membership + naming is feature-complete and fully realtime-tested. Prefer the component
furthest from its north star — strong candidates: **UI/appearance polish** (e.g. the user Settings
surface, still a bare card) or a **Rule-15 adversarial pass** on a less-hardened input surface. Do NOT
spend another tick on group-DM sub-slices unless the owner flags one.

## 2026-06-18 (iter 185) — Group DM naming slice 2 (client); the QA "set then CLEAR" pattern kept the later locators valid

Shipped the client side of group-DM naming (✏️ rename header action; `dmTitle` shows the custom name;
`dmMembersLabel` for tooltip/subtitle). The browser QA addition renames the group AND THEN clears the
name back to the member title before the existing leave step runs — because that leave step locates the
row by member text (`grpA`), and a lingering custom name would have made its locator find 0 rows (a
false-pass on `waitFor(detached)`). **Lesson — when a new QA step MUTATES a value that a later step
matches on (a title, a name, a count), restore it at the step's end, or the later locator silently
matches the wrong thing.** Same family as the iter-130 "a detour must restore context" rule, but for
DATA not navigation: the cleanup made the new step also exercise the clear-name path (free extra
coverage) instead of leaving a landmine for the leave assertion.

**Next-tick QA gap (logged):** the rename QA only verifies the ACTOR's own view. Add a two-client
realtime assertion (like realtime.mjs §15 for add-member): client A renames a shared group → client B,
viewing it, sees the title relabel LIVE via the dm-membership refetch, no reload. That would harden the
"realtime relabel already works" claim into a guarded fact rather than an inherited assumption.

## 2026-06-17 (iter 141) — Profile card from messages + Rule-15 XSS proof; the iter-130 "detour must restore context" lesson bit, the iter-139 grep saved a step

Shipped the **message-author profile card** (a public `GET /users/{id}/profile`) and, importantly,
ENCODED a Rule-15 guarantee: the QA now sets an `<img onerror>` in About Me and asserts the card
renders it as **literal inert text** (no element injected, `window.__ocXss` unset) — AI-vision shows the
raw `<img …>` as text. **Lesson — when a feature renders user-controlled text in a NEW place, the
adversarial test belongs in the SAME tick: a one-line XSS payload + an "is it inert?" assertion turns
"React escapes it, probably" into a regression-guarded fact.** This is cheap (reuse the existing render
test, just make the input hostile) and it's the Rule-15 step-5 "encode the exploit so it can't silently
return."

Two prior lessons showed up again — one caught, one bit:
- **iter-139 grep (CAUGHT):** before running QA I grepped for selector collisions with my new
  `.author-link` button (now username-named). The grep showed all QA username selectors are
  class-scoped (`.member-row` etc.), so no `getByRole(name:user)` clash — confirmed safe in 2s, no crash.
- **iter-130 "a detour must restore context" (BIT):** my new `07d7` step navigated to `#general` to test
  the message trigger and didn't navigate back, so the next server-channel step (`make read-only`)
  timed out 30s later. Same shape as iter-130. Fix: navigate back to the server channel at the step's
  end. **Meta-lesson — the "navigate-back" rule needs to fire at WRITE time for ANY step that switches
  channel/server/panel, not just reloads; I keep re-learning it per-trigger-type. Treat ANY
  `.click()` that changes the active channel/server as owing a restore-context line before the step
  ends.**

**Process tweak applied:** added "navigating away (channel/server/panel switch, not just reload) → the
step must restore the prior context before it ends" to the running QA-authoring checklist alongside the
label-collision grep.

## 2026-06-17 (iter 140) — Profile card; the iter-139 "grep before run" guardrail WORKED (caught a Save collision pre-run)

Shipped **About Me + pronouns + a profile card** (full stack). **Component: profiles.** The headline is
a process win: iter-139's reflection said "when a tick adds UI text that's a superstring of a common QA
target, run `grep -rn "name: '<word>'" qa/` as part of the build, not after a crash." This tick I added
a "**Save** profile" button — and the grep, run BEFORE the QA, immediately flagged that
`browser.mjs:715`'s status `getByRole(name:'Save')` (non-exact) would now match BOTH "Save" and "Save
profile". I fixed it to `exact:true` up front, and the QA passed first try. **The guardrail turned a
guaranteed ~6-minute crash-debug-rerun loop into a 2-second grep + a one-line fix.** Lesson confirmed and
now load-bearing: **a reflection becomes real only when a later tick executes it as a checklist step;
this one did, and it paid off the first time it could.**

Two reinforced patterns: (1) the centered-overlay-instead-of-positioned-popover choice kept the profile
card simple + robust (reused `.settings-overlay`/Esc-close, no viewport-clamping math) — when a feature
*can* be a centered card, prefer it over a positioned popover for a first slice. (2) The card reads the
already-loaded member-list data (no new GET endpoint), so it's trivially consistent and the existing
member-fetch covers it — same "derive from existing state" win as the tab badge (138) and mute (139).

Also a Go-edit note: router.go's deep tab indentation made `Edit` fail on whitespace twice; a small
python insertion (computing the indent from the anchor line + `gofmt` after) was the reliable fallback —
for tab-indented Go in deeply-nested closures, prefer an anchor-and-insert script over hand-matched
whitespace, then `gofmt -l` to confirm.

## 2026-06-17 (iter 139) — Per-channel mute; the substring-label collision bit AGAIN (and the guardrail that would've caught it)

Shipped **per-channel notification mute** (full stack: schema → store → endpoints → UI → QA).
**Component: notifications/chat.** It rode the existing unread pipeline (one `NOT EXISTS` clause makes
the sidebar dots, mention badges, AND tab badge all respect mute) — the iter-138 lesson ("a new feature
that's a new VIEW of existing state derives, doesn't re-model") applied again and kept it small.

But the **substring-label collision bit me a SECOND time** (iter-131 was the first, with
`getByLabel('camera')` vs `'camera preview'`): my new header button reads "🔔 mute", and voice.mjs's
`getByRole('button', { name: 'mute' })` (the mic button) is non-exact, so it suddenly matched BOTH →
strict-mode crash in a suite I didn't even edit. I'd written the lesson down twice and STILL re-hit it,
which means a note isn't enough — **the real fix is a habit at feature-build time, not at test-debug
time: whenever you add a button/label whose accessible name CONTAINS an existing one (mute ⊂ "🔔 mute",
camera ⊂ "camera preview"), grep the QA for `name: '<word>'` BEFORE running and either make the existing
selector exact/scoped or pick a non-overlapping label.** This tick I scoped the voice selector to its
unambiguous class (`.voice-mute`) — class/testid selectors are collision-proof in a way role-name
selectors aren't, so for buttons that share words, prefer a `data-testid`/class over `getByRole(name)`.

**Process improvement to apply going forward:** when a tick adds UI text that's a superstring of common
QA target names ("mute", "camera", "settings", "close"), run `grep -rn "name: '<word>'" qa/` as part of
the build (not after a crash) — it's a 2-second check that prevents a ~6-minute QA-run-debug-rerun loop.

Coverage note: the store integration test asserts the mute is per-USER (other's muted list excludes my
mute) — the kind of cross-tenant check that matters once a row is keyed by user_id (Rule B).

## 2026-06-17 (iter 138) — Tab badge + closed the voiceSettings coverage gap I flagged last tick (acting on my own notes)

Shipped the **browser tab unread/mention badge** AND closed the **`voiceSettings.ts` unit-test gap**
that iter-136's reflection explicitly named as "next." **Components: notifications/UI + test coverage.**
Two process wins worth recording:

1. **The loop's own IMPROVEMENTS notes are a backlog — act on them.** Iter 136 wrote "voiceSettings
   has ZERO unit tests despite being load-bearing for 4 features — add them next low-risk tick." This
   tick I did exactly that (9 tests: defaults, clamping on both set+get, the readBool('0') sentinel,
   partial-update isolation, corrupt-value fallback). **Lesson — the reflection step only pays off if a
   later tick actually picks up the flagged item; treat "Next QA-growth target" lines as a real queue,
   not journaling. Pairing a small feature with closing a previously-flagged coverage gap is a good
   shape for a low-risk tick.**

2. **Reuse existing state for a "new" feature → near-zero risk + free correctness.** The tab badge added
   NO new state or endpoint — it's a pure `useEffect` over the existing `unread` map (the same data the
   sidebar dots/mention badges already use), so it's automatically consistent with them and can't drift.
   **Lesson — when a feature is a new VIEW of state you already track, derive it (don't re-fetch/re-model);
   the derivation is trivially correct and the existing data's tests cover it transitively.**

QA-technique note: the tab title isn't screenshot-able, so AI-vision doesn't apply — the verification is
Playwright's `page.title()` asserting the exact strings (`● Opencord`, `(1) • Opencord`, `Opencord`)
across the existing two-client unread→mention→read sequence in realtime.mjs. **Lesson — not every UI
feature is visual; for `document.title`/`aria-live`/clipboard/focus, assert the DOM/API value directly
rather than forcing a screenshot.**

vitest is now node-only (no jsdom); for the localStorage round-trip I installed a tiny in-memory
`globalThis.localStorage` shim in `beforeEach` (the module reads the bare global), avoiding a jsdom dep.

## 2026-06-17 (iter 137) — Ephemeral TURN creds; rotating to the component furthest from its north star, + verifying a "gated-off-on-prod" change

Shipped **ephemeral HMAC TURN credentials**. **Component: audio/security (toward the audio north star:
reliable across hostile NATs).** After many UI ticks, the per-component-excellence rotation pointed at
the component FURTHEST from its bar — audio (thousands-scale, any-NAT). I picked the concrete,
self-contained, low-risk item on that path (`use-auth-secret`) rather than the big infra (cascaded
SFUs). **Lesson — when one area's obvious wins are done, the per-component rotation is the tiebreaker:
advance the component furthest from its north star with the smallest real step on its path, not the
flashiest one.**

A verification subtlety worth recording: **this change is gated OFF on prod (no `OPENCORD_TURN_SECRET`,
Rule A) so it has NO observable surface on the live deploy** — the live `/voice/token` returns the same
STUN-only payload on the old and new binaries. The loop's "grep the live JS bundle" rollout check
doesn't apply (it's Go, not JS), and the version string is static, so I can't distinguish builds by
behavior on prod. So I verified the FEATURE where it IS observable — a **local server booted with the
secret**, hitting the real `POST /voice/token` and confirming `username:"<exp>:<uid>"` + an HMAC
`credential` — and treat the prod step as "deploy + healthz + fresh container digest", explicitly
noting the feature is prod-gated-off (honest Rule 14, not a false "verified on prod"). **Lesson — for a
config-gated backend feature with no prod-observable surface, do the real E2E locally with the gate
ON, and don't pretend the prod healthz check verified the feature; state what was and wasn't checked.**

Also a small QA-mechanics catch: `/voice/token` is a **POST**, and my first E2E used GET → empty body
→ confusing JSON-parse error. When a curl E2E returns empty/garbage, check the HTTP method/status (`-i`)
before assuming the handler is broken.

**Next:** the audio north star still wants a deployed coturn for a real symmetric-NAT E2E (currently
only the cred-generation is tested, not actual relay traversal) — a future infra tick.

## 2026-06-17 (iter 136) — Auto-idle; choosing value/risk when the obvious wins are done, + a clean test hook for timers

Shipped **auto-idle presence** (online→idle on inactivity, restore on activity). **Component: presence
+ UI.** The interesting part this tick was the DECISION, not the code. After ~7 feature ticks the
TOP-PRIORITY UI list is substantially delivered, and the literal remaining sub-items were all poor
value/risk: input mic-gain (niche setting, a 2nd consecutive voice-pipeline refactor right after last
tick's transceiver bug), video slice 2 (niche screen+camera-at-once, changes the working `ontrack`
classification), and radius-normalization (measured 27 hardcoded radii — but unifying them would
visibly ALTER the established, fine-looking design = churn by my own iter-133 rule). **Lesson — "advance
the priority each tick" does NOT mean "force the next literal checkbox even when it's low-value or
risky." When the obvious high-value/low-risk wins in a priority area are done, the right move is the
highest value-per-risk REAL feature, and to write down WHY the riskier checkboxes were deferred (so the
deferral is a decision, not avoidance). Forcing a niche pipeline refactor just to tick a box is how you
ship regressions for no user benefit.**

QA-technique win: **a live-tunable threshold makes a long timer testable without faking time.** Auto-idle
fires after ~10 min — untestable directly. Instead of mocking timers, the code reads
`window.__ocIdleMs` on each re-arm, so the browser QA sets it to 1500ms, does ONE activity to re-arm,
stays quiet (Playwright's `waitFor` polls via CDP and generates NO input events, so "quiet" is real),
asserts the amber pip, then sets it huge again so later steps with their own waits never auto-idle.
**Lesson — for time-threshold features, expose the threshold as a live-read tunable (not a mount-time
constant) so a test can shorten it for one window and restore it; and lean on the fact that
Playwright's waiting is event-silent to simulate genuine inactivity.**

**Next QA-growth target:** `voiceSettings.ts` (localStorage layer under devices/DSP/output-volume/camera)
has ZERO unit tests despite being load-bearing for 4 features — add vitest for its defaults, clamping,
and the readBool('0') semantics next low-risk tick.

## 2026-06-17 (iter 135) — Video calling; the two-client QA caught a real WebRTC bug a single client never could

Shipped **mesh video calling (camera) — slice 1**, the biggest remaining Discord-parity feature, by
reusing the screen-share pipeline + a `kind` tag. **Component: UI + audio/realtime.** Two reusable
lessons, both about how the loop's QA earns its keep on hard features:

1. **The two-client voice QA caught a genuine WebRTC bug that NO single-client test or unit test could.**
   When A stops screen-share then starts camera, the peer connection REUSES the video transceiver, so
   B's `ontrack` does **not** re-fire — B never re-attached the video and saw no camera tile. Build was
   green, the camera worked on A's own screen, the Go relay test passed — every gate except the
   two-client E2E was happy. Only driving *two real browsers through the actual screen→camera switch*
   exposed it. Fix: the receiver retains the inbound video stream and re-attaches it on the
   `voice-screen` announce (the announce is the only signal when ontrack won't fire). **Lesson —
   realtime/peer features MUST be verified two-client through the actual state TRANSITIONS (not just the
   happy single-action path); the transitions (switch source, stop-then-start, reconnect) are where the
   transceiver/negotiation bugs live, and they're invisible to every non-two-browser gate.**

2. **Reuse the proven pipeline + an additive tag, not a parallel rewrite — it shrinks the risk surface.**
   Camera could have been a whole parallel video path (new frame, new peer fields, new ontrack branch).
   Instead it rides the EXISTING screen `addScreenTracksToPeer`/`voice-screen`/`ontrack` with one
   additive `kind` field — so the receive path (the scary part) was UNCHANGED and the existing
   screen-share QA proved no regression for free. The cost is a slice-1 limitation (screen ⊻ camera),
   documented and deferred. **Lesson — when a new feature is 90% an existing one, extend with an additive
   discriminator and keep the hot path byte-identical; the old feature's tests then guard the new one.**

**Adversarial note (Rule 15):** the new `kind` is attacker-controlled WS input, so the Go relay
whitelists it (`∈ {"", "screen", "camera"}` → else drop the frame) and a regression test fires a
`<script>` kind and proves the garbage frame never reaches the other client.

**Next QA-growth target (slice 2):** when screen+camera coexist (parallel streams), QA must assert BOTH
tiles render for one peer simultaneously (the case slice 1 explicitly can't do) — two-client, two video
streams from A, both visible on B.

## 2026-06-17 (iter 134) — Press feedback; the QA CAUGHT a CSS regression that would've shipped (and the cause is a reusable trap)

Shipped global **press (`:active`) feedback** and, more importantly, the browser QA **caught a real
regression my own change introduced before it could ship.** **Component: UI → polished.** I'd measured
the gap correctly (31 `:hover` rules, 0 `:active`) and reached for the intuitive press effect —
`transform: translateY(1px)` (a tactile nudge). Build was green, types fine. But the full browser-QA
run **crashed on the very first registration click** with a 30s `locator.click` timeout. Root cause:
**a `:active` transform moves the element during mousedown, and Playwright's click-stability check (and
real pointer hit-testing) treats the element as moving out from under the cursor → the click never
completes.** Fix: use a NON-positional press cue (`opacity`/`filter`), which dims without moving the
hit box.

Two durable lessons:
1. **Never use a positional `transform` for `:active` press feedback.** It breaks pointer clicks (test
   automation AND, more subtly, real fast clicks). Press cues must be paint-only: opacity, filter,
   background, box-shadow — never transform/margin/top that move the hit target mid-click.
2. **This is the strongest evidence yet that the full browser QA gate earns its ~3 min.** `go
   build`/`tsc`/`vitest`/`go test` were ALL green — a pure-CSS interaction regression is invisible to
   every non-browser gate. Only Playwright driving the real rendered UI caught it. Reaffirms the loop
   rule: a green non-browser suite is the floor, never the proof (Rule 14). I nearly had a "build's
   green, ship it" moment; the gate stopped a broken-registration deploy.

**Process improvement applied:** when a tick changes interaction CSS (`:hover`/`:active`/`:focus`/
`transform` on interactive elements), the browser QA is MANDATORY this tick (not the every-3rd-tick
cadence) — exactly because these regressions are invisible to the other gates. (This tick already did;
encoding it so a future tick doesn't skip browser QA after a "trivial CSS tweak".)

## 2026-06-16 (iter 133) — Focus-ring a11y; ground a "polish pass" in a measured gap + test the property robustly

Shipped the **global keyboard focus-ring** (Appearance pass, a11y slice). **Component: UI →
accessible.** After 4 ticks deep in one settings tab, I rotated to the owner's "Appearance polish"
item — but "polish" is the kind of vague directive that invites *churn* (Rule 10). The discipline that
made it safe: **don't polish by vibe — measure the gap first.** A 2-line audit (`grep -c focus-visible
styles.css` → only 6 hits, all on the brand-new settings components; `grep` for global resets → none)
proved objectively that the entire main chrome (sidebar/header/chat/member-list buttons) had no
visible keyboard focus indicator. That turned "make it feel nicer" into a single, defensible,
high-coverage change: one global `:focus-visible` rule. **Lesson — before any "polish/cleanup" tick,
run a quick metric (grep count, axe scan, a screenshot) to convert the vague ask into a specific
measured gap; if the metric shows no gap, it's churn — don't do it.**

QA-robustness lesson (continuing the fake-media theme): `:focus-visible` **only matches keyboard
focus, never a mouse click** — so the test MUST drive it with `page.keyboard.press('Tab')`, and a
programmatic `.focus()` would silently not match. And the *first* Tab can land on an input (which uses
a border, not an outline), so a naive "Tab once, assert outline" flakes by DOM order. Fix: **Tab in a
loop until `activeElement.tagName === 'BUTTON'`, then assert the outline** — robust to wherever the tab
order starts. **Lesson — when asserting a state that only some element types express (outline on
buttons vs border on inputs), drive toward the element type that expresses it rather than assuming the
first focusable does.**

**Next polish target:** hover/active-state consistency sweep — audit which interactive surfaces lack a
`:hover`/`:active` feedback rule (another grep-measurable gap), then unify them; AI-vision a hover state.

## 2026-06-16 (iter 132) — Output volume; split a feature along its RISK seam, and verify each layer where it lives

Shipped the **output (master) volume slider** (Voice & Video slice 3c) and explicitly **deferred** the
input/mic-gain slider to its own tick. **Component: UI + audio.** The reusable lesson is about
*scoping a listed feature*: GOAL.md bundled "input + output volume sliders" as one item, but they have
**very different risk profiles** — output volume is playback-only (`el.volume = peerVol * master`, can't
break the connection), while input gain needs a `GainNode` spliced into the capture chain that the
mute/PTT/device-hot-swap logic all mutate. Shipping them together would have put a risky pipeline
change on the same tick as a trivial one. **Lesson — when a backlog item bundles sub-parts, split it
along the RISK seam, not just the feature seam: ship the low-risk half now, isolate the invasive half
to a tick where it gets full attention + adversarial care.**

The QA half reinforced a coverage principle: this behavior **can't be tested in `browser.mjs`** (one
client → no peers → no peer `<audio>` to scale). So I verified it at the right layer for each claim:
the **math** in a vitest unit (`effectiveVolume`, incl. clamp + master-0), the **UI + persistence** in
`browser.mjs` (slider renders, `localStorage==='0.5'`), and the **live composition** in the two-client
`voice.mjs` (open settings mid-call → master 50% × per-user 40% → peer audio 0.2). **Lesson — match
each claim to the cheapest test that can actually exercise it; don't try to force a peer-dependent
assertion into the single-client suite (it'd silently never run).**

**Next QA-growth target (slice 3d):** input mic-gain — when built, QA it by asserting the capture
`GainNode.gain.value` tracks the slider AND that mute/PTT still gate correctly *through* the gain (the
exact interaction that makes it risky), two-client.

## 2026-06-16 (iter 131) — Camera preview; two QA-robustness bugs the *fake media* exposed

Shipped the **camera device + live preview** (Voice & Video slice 3b). **Component: UI → polished +
video groundwork.** The build was green and AI-vision was clean, but the FIRST browser-QA run failed
twice — both failures were in the *test*, not the feature, and both are reusable lessons:

1. **`getByLabel('camera')` matched two elements** — the `<select aria-label="camera">` AND the
   `<video aria-label="camera preview">`, because Playwright's `getByLabel` is **substring + case-
   insensitive by default**, and "camera" is a prefix of "camera preview". Strict mode → the whole
   script crashed. Fix: `getByLabel('camera', { exact: true })`. **Lesson — when two controls share a
   word in their accessible names, default `getByLabel`/`getByRole` name matching will collide; use
   `{exact:true}` (or disjoint labels) the moment a label is a substring of another.**

2. **The mic-test meter assertion flaked to 0** — it passed last tick (0.02) on the *same code* and
   failed this tick (0.00). Root cause: Chromium's fake mic **pulses** (beeps), and the assertion did
   `click → waitForTimeout(1200) → read once`. A single instantaneous read lands in a silent gap ~half
   the time. Fix: poll for the **peak** over a ~4s window and break on first non-zero. **Lesson — for a
   meter/level/animation driven by a periodic or noisy source, never assert a single instantaneous
   sample after a fixed sleep; poll for the peak (or a threshold) over a window.** A test that passes
   "usually" is a latent red that will burn a future tick at random.

**Highest-value meta-point:** headless **fake media** is great for exercising getUserMedia paths, but
its signal is *synthetic and timing-quirky* (pulsing audio, a fixed test-pattern video). Assertions
against it must be **shape-robust** (peak-over-window, `videoWidth>0`), never value-exact or
single-sample. Both fixes above make the suite deterministic regardless of where the fake tone is in
its cycle.

**Next QA-growth target (slice 3c):** input/output **volume sliders** — input gain needs a `GainNode`
spliced into the capture chain (more invasive than this preview), so QA it by asserting the slider
persists + the gain node's `.gain.value` tracks the slider, and AI-vision the control row.

## 2026-06-16 (iter 130) — Voice & Video tab; a reload-based persistence test would have wrecked downstream context

Shipped the **Voice & Video settings tab** (slice 3a: device pickers + DSP toggles + mic-test
meter, persisted to `voiceSettings.ts`). **Component: UI → polished/Discord-faithful + audio.**
The QA lesson came from a near-miss I caught at review, not run-time: my first draft verified the
toggle persistence with a full `page.reload()` → reopen-tab → assert-still-off. That's the
*intuitive* way to test localStorage, but in a long single-session Playwright script a reload
**silently resets the app to `#general`**, and the very next step (`07e` make-read-only) needs the
**server-channel** context the suite had navigated into 6 steps earlier. The reload would have
green-passed my new step and then failed `07e` with a confusing "button not found" three steps
later — a failure that reads as unrelated to the change that caused it.

**Highest-value lesson — in a stateful, sequential E2E script, prefer the *least-disruptive* proof
of a property over the most-thorough one.** I swapped the reload for: (a) a direct
`localStorage.getItem(...)` assertion (proves it persisted to storage — actually *stronger* than a
reload, which only proves the value survived), plus (b) a **modal close→reopen** that remounts the
component so its `useState(() => getAudioProcessing())` re-reads storage (proves the UI re-hydrates).
Same guarantee, zero context loss. A `page.reload()` mid-suite is only safe if the step **owns** its
navigation afterward (like `07i` avatar, which re-clicks `#general`) — otherwise it's a landmine for
every step that assumed the prior context.

**Loop-process improvement applied:** when adding an E2E step that needs a reload, either (1) make
the step restore the exact navigation context it found, or (2) prove the property without a reload
(direct storage assertion + component remount). Default to (2).

**Next QA-growth target (slice 3b):** input/output **volume sliders** + **camera device + live
preview** — the camera preview needs a `<video>` + `getUserMedia({video})` flow and an AI-vision
check that the preview tile renders (fake video device supplies frames headlessly).

## 2026-06-16 (iter 129) — User Settings modal; migrate QA selectors *in the same edit* as the UI move

Shipped the **User Settings modal** (GOAL TOP-PRIORITY slice 2). **Component: UI →
polished/Discord-faithful.** The risky part wasn't building the modal — it was that the
change *relocates* controls the browser QA already drives (avatar file input, status button,
presence `<select>`) out of the header and into the modal. A UI move like this silently
breaks every QA step that still looks for the old selector, and those failures only surface
~3 min into the boot. I migrated `qa/browser.mjs` in **lockstep** with the component edit
(status/presence/avatar steps rewritten to open the modal first; header `.status-edit`/
`.presence-pip` assertions dropped; `08b` mobile now checks the ⚙ chip not the presence
picker) and added a **new `07d0`** step asserting the ⚙ opens the modal and **Esc** closes it.

**Highest-value lesson — when a tick MOVES an element the QA already selects, treat the QA
update as part of the same change-set, not a follow-up.** The failure mode is asymmetric: a
moved selector doesn't error at edit time (tsc/vite are happy), only at QA-run time, and a
green build can lull you into shipping a broken QA. The cheap guard already exists (`run.sh`
`node --check`s every `.mjs` before boot) but that only catches *syntax*, not *stale
selectors* — so the discipline has to be manual: grep `realtime.mjs`/`voice.mjs`/`browser.mjs`
for every selector you're moving BEFORE running, which is exactly what caught that
realtime/voice don't touch these (so only `browser.mjs` needed edits).

**Loop-process improvement applied:** before moving any selector-bearing element, grep all QA
`.mjs` for that selector and update every hit in the same edit. This tick did that and the
full suite came up green first try (`browser=0 realtime=0 voice=0 search=0`).

**Next QA-growth target (slice 3):** the Voice & Video settings tab — when built, the
mic-test input-sensitivity meter + device pickers + toggles need a browser-QA flow (open tab
→ pick device → meter moves → toggle persists across reload) and AI-vision on the panel.

## 2026-06-16 (tick 101) — Invite expiry; chose the clean-default over the prompt-churn version

Shipped **invite expiry** (7-day default, enforced at redeem). **Component: security/servers
→ hostile-input-proof.** Spent most of the thinking on *scoping* — the cohesive feature is
"expiry + max-uses + customizable", but the only good UX for the customizable parts is a
small invite-options *form*, and bolting it on as window.prompt(s) would have churned the
two-dialog invite flow the browser QA reads codes from. Chose the **clean, complete,
zero-UI-churn slice**: a sensible default (auto-expire, like Discord) enforced server-side,
with customization + max-uses explicitly deferred to a form-based tick.

**Highest-value lesson — when the clean UX needs more UI than the tick affords, ship the
safe *default* and defer the *controls*, rather than ship the controls as prompt-clutter.**
A forced 7-day expiry is a real hygiene win on its own (a leaked code stops working) and
doesn't regress the common case (instant redeem). The alternative (prompt for expiry/max-
uses) would have added fragile two-dialog QA coupling for marginal extra value this tick.
Defaults are a legitimate, often-best first slice of a configurable feature.

**Backward-compat by design:** the new `expires_at` is nullable; legacy invites (NULL) stay
permanent, only new ones expire — the migration can't break existing invites. Worth stating
in the commit so a reviewer doesn't fear a mass-expiry.

- **Verification-boundary note (Rule 14):** the expired-rejection is integration-tested
  (force `expires_at` into the past), not reproducible on the live deploy in-context (can't
  age an invite 7 days). The live check is happy-path mint+redeem + healthz; a backend-only
  change with no instantly-observable new surface limits the live rollout signal — stated,
  not hidden.
- **Next:** the deferred invite-options form (expiry choice + max-uses) is a small UI tick;
  the bigger rocks remain built-in tunneling + audio mesh→SFU scale.

## 2026-06-16 (tick 100) — Search operators; a placeholder change caught the QA's brittle locator

Milestone tick 100. Shipped **search operators** (`from:`, `has:link/image/file`) on top
of the existing search — clean, no schema, dynamic-but-parameterized SQL. **Component:
chat → parity.**

**Highest-value lesson — UI-string changes are a silent QA-breaker; locate by structure,
not by copy.** Adding the operator *hint* meant changing the search placeholder from
"Search this channel…" to "Search… (try from:user has:link)". The browser QA located the
box via `getByPlaceholder('Search this channel')` — a substring match that the new copy no
longer satisfies, so the QA would have broken on a purely cosmetic edit. Fixed by switching
to the **`.search-input` class locator** (structure, stable across copy changes). General
rule applied: QA selectors should key on roles/test-ids/classes, not on display copy that
product iteration will churn. (Audit candidate: other `getByPlaceholder`/`getByText` on
churn-prone copy.)

**Security note (Rule B, done right):** the dynamic WHERE is assembled from fixed operator
fragments + bind-parameter *values* only — the store test encodes an injection in `from:`
(`from:' OR '1'='1`) and asserts it matches nothing. Dynamic SQL is fine when the *shape*
is fixed and only *values* vary through parameters.

- **Coverage:** the `has:image`/`has:file` distinction is store-tested (via
  `SaveWithAttachments` with image vs non-image content types); the browser test exercises
  `from:` (attachments aren't in that channel). Adequate split.
- **Milestone reflexion (90→100):** 11 ticks, every one verified end-to-end + deployed,
  spanning UI, security, chat realtime (kick/presence/status/unread/mentions/search), infra
  (per-user push), and audio (TURN) — plus two health/de-flake ticks. Furthest-from-north-
  star still: **built-in secure tunneling** (North-Star feature, untouched — needs a
  dedicated spec + relay-tech decision, likely multi-tick) and **audio mesh→SFU at true
  scale**. Those are the next big rocks.

## 2026-06-15 (tick 99) — Rotated to the most-neglected component (audio); shipped TURN config

After a chat/security/QA run (ticks 91–98), deliberately rotated to **audio** — the
owner's FIRST north star ("thousands/HD/no-drops/free") and the component with zero ticks
this session. **Advanced toward "no-drops":** configurable STUN + optional self-host TURN
for mesh WebRTC (the `voice.ts` comment literally said "TURN ... is a later slice" — this
was it). Replaced the hardcoded Google STUN (a soft Rule-A bake-in) with server-served,
env-driven iceServers.

**Highest-value lesson — be explicit about the verification boundary, and design the
feature so the testable part is meaningful.** TURN's whole point is symmetric-NAT
traversal, which can't be exercised in-context (needs a deployed coturn + a hostile NAT).
Rather than hand-wave, I split verification: (a) the *plumbing* is fully tested — config
builds the right iceServers, the endpoint serves them auth-gated, the mesh E2E still
connects (proving the new config path didn't break the happy path); (b) the *traversal*
is stated as needing real infra (Rule 14), not claimed. The design choice that makes (a)
meaningful: server-*served* iceServers (not client-hardcoded) means "the server decides
ICE" is a real, testable seam — a future managed-TURN cloud tier plugs in here with no
client change.

**Rule-16 check applied correctly:** this is config plumbing for a TURN the *self-hoster*
runs, not us adopting a TURN service — so no stack-guardian gate. The distinction
("enabling a self-hosted option" vs "adding a running service to our stack") is the right
test for when Rule 16 fires.

- **Component-rotation note:** with audio advanced, the recent coverage is now broad —
  chat (instant/lossless: unread+mentions), security (input bounds), infra (per-user push),
  UI (header/presence/status), audio (TURN). **Furthest-from-north-star now:** audio still
  (mesh→SFU→cascaded for true thousands-scale is the long pole) and **built-in secure
  tunneling** (Platform section, untouched — the other half of the "friends join without
  port-forwarding" promise). Good candidates for upcoming ticks.
- **QA coverage:** the TURN path's real behavior (relay allocation) has no automated test
  by necessity; a manual/staging checklist with a real coturn would be the only way to
  exercise it — noted as out-of-scope for the in-context loop.

## 2026-06-15 (tick 98) — Acted on last tick's own suggestion: proactively de-flaked the gate

A verification + loop-health tick. Ran the full browser QA (browser/realtime/voice all
green — the product is healthy after the iter-90→97 feature flurry) + AI-vision (core
chat/header/sidebar clean, no regression). Then **acted on the tick-97 reflection's own
suggestion**: audited the suite for the fixed-`Sleep`-then-assert pattern that false-red'd
twice (slowmode iter-91, rate-limit iter-97) and proactively converted the two remaining
*same-class* WS assertions to polls — the voice flood-guard's "legit burst relayed in
full" (lower-bound count) and the hostile-frame battery's "final-legit persisted" check.

**Highest-value lesson — the loop's own reflections are a backlog; work it, don't just
append to it.** Tick 97 flagged "convert remaining fixed-sleep patterns proactively." This
tick did exactly that instead of writing a new feature, and it's the right call: a flaky
health gate undermines every future tick's fix-first decision (a false P0 wastes a whole
tick on a non-bug). Distinguishing the genuinely-flaky pattern (fixed-sleep then assert a
**lower bound** / a specific result — flakes under load) from the safe one (assert an
**upper bound** / tolerant direction — load only makes it more true, e.g. flood-guard
Phase 2, left as-is) is the key judgment — don't poll-convert blindly.

- **Anti-churn note (correctly applied):** this is a **test-only** change — `_test.go`
  files aren't in the built binary, so the deployed artifact is byte-identical. Committed +
  pushed for source control, but **did not deploy** (no rollout-verify needed — nothing new
  serves). Deploying here would be pure churn (Rule 10/16).
- **Suite-flake status:** all four known timing-sensitive assertions (slowmode, rate-limit,
  voice flood-guard P1, hostile-frame final) are now poll-based or load-tolerant. Remaining
  `time.Sleep`s are poll *intervals* or the inherently-duration-based slowmode wait — no
  fixed-sleep-then-lower-bound-assert remains. Gate should be robust under load now.

## 2026-06-15 (tick 97) — Rotated to a security/QA pass after 6 feature ticks; found a real edge + a flake

After 6 straight Track-1/2 feature ticks, deliberately rotated to **Track-0 (security/QA —
"do this MOST")**. **Component advanced: security → hostile-input-proof.** A Rule-15 sweep
of the input surfaces found bodies all properly bounded (MaxBytesReader everywhere, incl.
pre-auth), but surfaced one real edge: **register with an over-long password → 500, not
400** (no password max; bcrypt rejects >72 bytes). Ran the full Rule-15 cycle (reproduce
the 500 → fix → re-attack 400 → happy path) and added the regression. Also caught a
**load-sensitive flaky test** (`TestServeWSRateLimitIntegration`, fixed-sleep) when the
full suite ran on a busy machine, and de-flaked it (poll, not sleep).

**Highest-value lesson — "all bodies are bounded" is necessary but not sufficient; check
what the bounded-but-invalid input does next.** The body cap (64 KiB) stopped a *huge*
password, but a merely-too-long-for-bcrypt one (73 bytes) sailed past validation into a
crash-shaped 500. The hardening question isn't only "is the input bounded?" but "for every
value still inside the bound, is there a downstream limit it can violate?" bcrypt's 72-byte
limit is exactly such a hidden downstream bound. **Rule for input validation: validate
against the *consumer's* limits, not just the transport's.**

- **Process note — rotate Track 0 deliberately.** Six feature ticks in a row drifted the
  loop toward Track 1/2; the explicit rotation (and the "do this MOST" reminder) paid off
  with a real find. Worth doing a dedicated security/QA tick every ~5–6 feature ticks, not
  only when something breaks.
- **Flaky-test watch:** two timing-flakes found now under load (slowmode iter-91, rate-limit
  iter-97), both fixed-sleep → poll. **Candidate loop improvement:** audit the suite for any
  remaining `time.Sleep(<fixed>)`-then-assert patterns and convert them to polls *proactively*
  before they false-red the gate — a good next Track-0 micro-tick.

## 2026-06-15 (tick 96) — Red mention badges; match the server detector to the client renderer

Shipped **mention-count badges** (red pill with a count), completing the iter-95
notification story. **Component advanced: chat → parity (the signal users act on).**
Clean extension of poll-based unread — `Unreads` now returns `{id, mentions}` per channel;
no new architecture.

**Highest-value lesson — when two layers must agree on a fuzzy rule, derive the
server's rule FROM the client's, and test the disagreement cases.** The client highlights
a mention as `@([A-Za-z0-9_]{2,32})` (case-insensitive) when the token equals your
username. A naive server `LIKE '%@alice%'` would count `@alice2` as a mention of `alice`
— the badge would disagree with what's highlighted in the message. I read the client's
actual regex first, then built the server match (`@(name|everyone|here)([^a-z0-9_]|$)`,
trailing boundary) to mirror it, and the store test pins the divergence cases explicitly:
`@alice2` ∌ alice, `@everyone` ∈, own-message ∉. **A count feature is only correct if it
counts the same things the UI shows** — so the test asserts the *boundary*, not just "> 0".

Two safety angles closed by construction (and noted in the spec): usernames are validated
`[a-zA-Z0-9_]{3,32}` → no regex metachars in the interpolated pattern; and the pattern is
a bind parameter → no SQL injection. Either alone is enough; both = defense in depth.

- **QA grown this tick:** the realtime suite now drives dot → **red badge** → clear (A
  @mentions B while away). **Coverage gaps (carried):** still no offline-member render
  test; and the mention test is store-level for the boundary — the *browser* test only
  asserts "a badge with a count appears", not the exact number, so a count-off-by-one in
  the UI mapping wouldn't be caught there (the store test guards the number).
- **Notification story status:** unread dots + mention badges done. Remaining notification
  parity (own follow-up ticks): per-channel/server **mute**, DM notifications, web push,
  and the **instant** (push-based) unread upgrade — all now have their primitives in place.

## 2026-06-15 (tick 95) — Unread indicators; shipped the right MVP (poll) over the fancy one (push)

Shipped **per-channel unread dots** — the most-requested Discord-parity gap, and the
feature the iter-94 per-user-push was meant to "unlock." **Component advanced: chat →
instant/lossless realtime (a step; still poll-refreshed, not push).** The judgment call:
I built **poll-based** unread, NOT the push-based instant version, even though I'd just
built the push primitive for it. Why: push-based instant unread needs the *co-member
observer set* per message (a DB query on the hot send path) — real complexity that
risks a rushed, half-verified tick. Poll-based (a ~10s `GET /unreads`) delivers the
whole user-visible value (dots appear/clear correctly, access-scoped) cleanly and fully
tested. The push upgrade is a clean follow-up. **Ship the MVP that's completable +
verifiable this tick; don't gold-plate just because you built the tool for the gold version.**

**Highest-value lesson — correctness of unread is all about *when you mark read*, and
the test had to pin the access-scoping.** Two subtle bugs avoided by design: (1) your
*own* messages must not self-unread (handled in SQL: `msg.user_id <> $1`), and (2) the
channel you're *viewing* must never show a dot AND must be marked read on *leave* (not
just open), or the next poll re-flags messages you saw — handled by the render filter +
WS-cleanup mark-read. The store test encodes the full lifecycle (unread→read→unread) and
a hostile case: a server channel you can't access **must never** surface in `/unreads`
(asserted), because an unread list is an information-disclosure surface (Rule B) — leaking
"channel X has activity" for channels you can't see would be a real bug.

- **QA grown this tick:** the realtime suite now drives a **poll-driven** cross-channel
  flow (A posts while B is away → B's dot appears within the poll window → clears on
  open) — the first QA that waits on a *background poll*, not a direct action. **Process
  note:** this adds ~5–10s to the QA run (waiting out the 10s poll). Acceptable, but the
  push-based follow-up would make it instant *and* faster to test — a nice alignment of
  product + test speed. **Coverage gap (carried):** still no offline-member render test,
  and no mention-count test (feature not built yet).

## 2026-06-15 (tick 94) — Paid the infra debt: per-user push (Hub.SendToUser) + live kick-notice

After flagging it 3 ticks, built the missing realtime primitive — **push to a user, not
just a channel** — and proved it on its first real consumer (live kick-notice: the kicked
user's client now drops the server + lands on #general instead of reconnect-looping on
403). **Component advanced: infra/realtime → "scales / complete."** Chose the *bounded*
consumer (kick-notice: target = one known user) over the tempting-but-complex ones
(scoped presence needs a co-member observer set; unread needs read-state) — the primitive
is the reusable part, and a small real consumer is enough to justify + test it (not
speculative, per Karpathy/Rule-6).

**Highest-value lesson — "notify then close" needed a concurrency fix, and the test had
to encode the ordering, not just the outcome.** Naively `SendToUser(notice)` then
`Evict(close)` could lose the notice: writePump's `select` could pick the Close frame
ahead of a pending data frame. Fix: writePump now *drains queued frames before* sending
Close. The ws test was strengthened to assert the kicked socket receives `server-removed`
(carrying the right serverId) AND THEN closes — i.e. it verifies the *order*, run -race
5x. An outcome-only test ("socket closed") would have passed even if the notice were
dropped. When ordering is the contract, test the ordering.

- **QA grown this tick:** realtime browser QA now drives the *kicked* side (B's sidebar
  drops the server + B lands on #general live), not just the kicker's view. **Coverage
  win:** the two-client suite now exercises a server→client *push* reaction, a pattern
  it couldn't before.
- **Debt retired / next up:** the per-user push unblocks the previously-stuck features.
  Highest-value next consumer is **cross-channel unread/mention badges** (push "new msg
  in channel Y" to a user on another channel) — a real flagship gap, now buildable; it
  still needs read-state storage + badge UI, so it's its own spec-first tick. Live
  presence (co-member-scoped) and live member-joined are the other two now-unblocked.

## 2026-06-15 (tick 93) — Custom status; chose the boring-clean feature over the risky infra

Shipped **custom status** (status line by the name). **Component advanced: Profiles →
parity.** The deliberate call this tick was *what NOT to do*: my last two reflections
pushed toward the hub per-user-event channel, but its clean consumers each carry a real
trap — live presence needs co-member scoping (a privacy leak if broadcast instance-wide)
and live kick-notice has a writePump send-vs-close race. Rather than ship a half-right
risky infra change autonomously, I picked a **complete, low-risk, fully-verifiable**
parity feature and shipped it end-to-end. Knowing when to defer the hard infra (it needs
a proper spec + careful concurrency design, not a rushed tick) is itself the right move.

**Highest-value lesson — reuse the project's own safety rails instead of re-deriving
risk.** Adding `status` meant another `ALTER TABLE users`, the exact operation that
deadlocked in iter-89. Instead of inventing a workaround, I leaned on the existing
mitigation (Migrate's lock_timeout+retry) and *re-ran its regression test under -race 3x*
to confirm the new ALTER is covered. The bar for "is this DDL safe?" is now "does
TestMigrateUnderConcurrentDML still pass with it?" — a concrete, repeatable gate, not a
judgment call each time.

- **QA grown this tick:** single-client browser flow sets a status and asserts it renders
  in the sidebar + header; store + router integration tests (trim/cap-128/clear, auth,
  member-list reflection). **Coverage gap to watch (carried + still open):** no QA yet
  for an *offline*/dimmed member render (all QA users are connected) and no test that a
  hostile `<script>` status is rendered inert (relying on React-escaping by construction,
  not asserted) — a vision/DOM check would lock it in.
- **Recurring infra debt (now flagged 3 ticks running):** the one-socket-per-channel
  model still has no per-user/instance-wide event push. Custom status, presence, and
  member-joined are all poll-refreshed (≤15s), not live. This is the next *real* infra
  investment and deserves a dedicated, spec-first tick — not another feature bolted onto
  the poll.

## 2026-06-15 (tick 92) — Per-member online presence; the hub-as-read-model pattern

Shipped **per-member online presence** (green dot in the member list), closing the
GOAL-flagged member-list follow-up. **Component advanced: chat/UI realtime →
"polished, native-feeling."** The clean bit: a *second* read-only hub query
(`OnlineUserIDs`, after iter-91's `EvictUserFromChannels`) confirmed the pattern —
**the hub is the in-memory read-model for "who's connected," and the HTTP layer
annotates store data from it on the hub goroutine** (no locks, reply-channel). Two
ticks running, reaching into the hub from a REST handler has been the right shape.

**Highest-value lesson — presence exposed an architecture limit worth naming.** The
member list refreshes presence only on its 15s poll (or an action-triggered refetch),
because Opencord uses **one WS socket per channel** — a client isn't subscribed to a
"presence" or "all-channels" stream, so there's no push path for "user X came online"
to reach observers in real time. iter-91's kick hit the same wall (kicked user can't
be told live). Both point at the same missing primitive: a **per-user / instance-wide
event channel** on the hub (broadcast to a user, or to "everyone who can see this
server"). That's the right next infra investment and would unlock: live presence, live
member-joined, live "you were removed," and cross-channel unread badges — all currently
blocked by the per-channel-only fan-out. Filed as the recurring theme; worth a focused
infra tick (with a spec) rather than bolting each on.

- **QA grown this tick:** ws integration test for `OnlineUserIDs` (connect→online,
  close→offline, -race); realtime browser QA asserts both connected users show
  `data-online="1"` + a green dot renders. **Coverage gap to watch:** no test yet that
  an *offline* member renders the grey/dimmed state (all QA users are connected); would
  need a third user who connects then disconnects before the member-list assertion.

## 2026-06-15 (tick 91) — Kick a member; the feature exposed a latent realtime access leak

Shipped **server kick** (Moderation parity) — owner/admin removes a member, full authz
matrix. **Component advanced: security → "hostile-input-proof."** The high-value find:
implementing the FIRST access-*revoking* op surfaced a latent leak — WS channel access
is checked only at connect (`ServeWS`→`CanAccessChannel`), so a kicked user's already
open socket would keep streaming the channel. A naive "kick = DELETE the membership row"
would have shipped a half-secure feature (Rule 15). Fixed by `Hub.EvictUserFromChannels`
(closes the kicked user's live sockets on the hub goroutine, mirroring the drop path),
proven by a `-race` ws integration test (5x) AND by the browser (online count drops 2→1
on kick — the eviction is observable end-to-end, not just in the unit test).

**Highest-value lessons (two):**
1. **A new mutation can expose a latent invariant gap elsewhere.** Before this, membership
   only ever GREW (join via invite), so "access checked at connect" was sufficient. Kick
   broke that assumption. Rule for new ops: ask "does this *revoke* something that an
   open connection still trusts?" — if so, the realtime layer must be told, not just the DB.
2. **A flaky pre-existing test surfaced under `-race` and must be fixed, not ignored.**
   `TestChannelSlowmodeIntegration` waited only 1100ms past a 1s window; under `-race`'s
   slowdown the server-measured `now()-created_at` could read <1s elapsed → intermittent
   red. Widened to 1600ms (separate surgical commit, Rule 10). Reinforces the tick-89 rule:
   run timing/concurrency assertions enough to trust them, and keep margins generous.

- **QA grown this tick:** a two-user kick flow in `qa/realtime.mjs` (owner kicks B → B
  vanishes from the panel + member-list sidebar; owner's own row has no kick button),
  plus store/router/ws integration tests. **Coverage gap to watch next:** the kicked
  user's *own* client has no live "you were removed" UX — it silently reconnect-loops
  (403). A targeted WS user-event would let the client drop the server cleanly; not yet
  QA'd because it isn't built. Also: no test yet for an admin (not owner) being unable to
  kick a fellow admin *through the HTTP layer with the admin's token* (covered at the
  store layer only).

## 2026-06-15 (tick 90) — AI-vision caught a header that text-wrapped; the FIRST regression test didn't catch it

Browser-QA AI-vision pass flagged a real **P1**: in a server channel (member-list
sidebar present → narrow column) the chat header wrapped its labels across lines —
"Join voice"→2, "1 online ·"→3, "log out"→2, brand→2 — looking broken. The plain
`#general` view has the full width so it never showed. Fixed with `flex-wrap` +
`row-gap` on `.chat-header`, `min-width:0` on the brand, `white-space:nowrap` +
`margin-left:auto` on the meta, `nowrap` on Join-voice. Header now flows into two
clean rows; AI-vision verified in both single- and two-user server views.

**Highest-value lesson — a regression test you haven't PROVEN against the broken
state can silently test the wrong thing.** My first assertion checked bounding-box
*overlap* and *horizontal overflow*. I ran it both ways (Rule 15) and it **passed
even with the fix reverted** — because the real defect was flex items shrinking to
min-content and wrapping their *text* (taller header), not boxes overlapping or the
bar overflowing. Without the prove-it-fails step I'd have shipped a green check that
never guards the bug. The correct signal was rendered **text-line count per control**
(padding/border-corrected `height / line-height`): each header label must be exactly
one line. That version goes red without the fix (brand/Join-voice/log-out = 2 lines)
and green with it.

- **Loop-process rule (apply every tick):** a UI regression assertion must be run
  against the **broken** state and observed to FAIL before it counts — same bar the
  iter-3 deadlock entry set for concurrency. "Exit code flipped red" isn't enough:
  confirm the *specific* new check failed, not unrelated flakiness (this run's
  reverted-CSS pass also showed flaky grouping/shift-enter/image/a11y failures that
  were green in the clean run — don't mistake harness churn for the regression).
- **QA gap to watch:** the header is the most control-dense surface and only the
  server-channel layout exercises its narrow width. Other dense surfaces under the
  member-list (e.g. the voice bar IN a server channel, the search-results header)
  aren't yet line-count-checked — candidate for a future tick.

## 2026-06-15 (parity) — Discord-style member-list sidebar (servers)

First Discord-parity tick under the new North Star ("match Discord's layout/design/features").
Shipped the **right-hand member list** for server channels — the iconic Discord column: members
grouped by role (Admins/Members) with avatars + role badges, hidden ≤900px (Discord behavior).
Reused the existing member-row/role-badge UI + `fetchServerMembers`; polls every 15s and syncs on
panel-open/role-change so joins/promotions appear without a manual refresh.

**Two bugs caught by QA before ship (the value of the two-client suite):** (1) the sidebar added a
SECOND `.role-badge.role-owner`, breaking browser.mjs's strict-mode locator — fixed by scoping that
assertion to `.member-row` (the panel). (2) the list was stale (loaded before B joined, never
refreshed) — fixed with the poll + on-fetch sync. Both surfaced only by driving the real two-client
server flow; the static build was green.

**Follow-ups (queued):** live `member-joined`/`member-left` WS broadcast (so the list updates
instantly, not on a 15s poll) + per-member online/idle presence (needs the presence protocol).

## 2026-06-15 (owner bug report) — "doesn't work when another person joins" → WS reconnect + UX

Owner: on the public deploy, a second person on another computer found voice worked but chat
didn't and a screen share never reached them. Diagnosed empirically (two sessions vs the LIVE
Railway URL): same-machine everything passed — so the code was right; the failure was
network/longevity. ROOT CAUSE: **no WebSocket reconnect** — `onclose` only set connected=false.
On a real/flaky network (or a proxy dropping an idle socket) the channel WS died and never came
back, so chat stopped and screen-share renegotiation couldn't signal, while already-established
P2P voice kept playing — matching the symptom exactly. Fixed with capped-backoff reconnect; the
server re-sends history on connect, so state re-syncs. **Verified live**: `context.setOffline
(true→false)` against the real deploy → reconnected and received a post-reconnect message.

Same session, owner feature asks delivered:
- **Screen view-size controls** — viewer-local small/medium/large tile sizing, fit↔fill toggle,
  and per-tile fullscreen. E2E + AI-vision (`voice-09-screenshare-sized.png`).
- **Invite/DM by username OR user id** — backend `LookupUserByIdentifier` (numeric→id, else
  username; email is a one-line branch once accounts store one), `/api/dms` accepts `identifier`,
  prompt updated. Go integration test for id + username.
- **North Star rewrite** (GOAL.md + CLAUDE.md): free local + built-in secure tunneling (no
  paywall), paid Cloud Opencord (24/7 hosting only), data-ownership, and **match Discord exactly**
  in layout/design/features + our improvements — the loop now advances Discord parity each tick.

**QA-process win:** added a real reconnect test (offline/online) to the two-client suite — network
resilience is now regression-guarded, not just happy-path. **Process note:** could NOT reproduce
the cross-network failure locally (same-machine always connects); the fix came from reasoning about
which features survive a dead socket (P2P voice) vs which don't (WS chat + renegotiation). TURN +
secure tunneling + accounts-email queued in GOAL.md "Platform & hosting".

## 2026-06-15 (owner request) — screen share (mesh) + screen-audio mixing controls

Owner asked to start developing screen share (4K@60-capable) with audio-level controls for BOTH
the sharer and the viewers. Built it end-to-end on the mesh path (the default, Rule A): capture via
getDisplayMedia (4K@60 constraints, contentHint detail, 8 Mbps, maintain-resolution), published to
every peer via perfect-negotiation, with a new Rule-B-bounded `voice-screen` WS frame so receivers
tell screen tracks from the mic. Three independent audio controls (the owner ask): sharer→viewers
outgoing gain (Web Audio GainNode), sharer→self local monitor (default 0, no echo), viewer→self
per-share volume. UI: share toggle + a stage of 16:9 video tiles with the right sliders per side.

**Verified (Rule 14):** extended `qa/voice.mjs` (3-way mesh) — A shares → preview + out/monitor
sliders → **B receives a live inbound video track** + per-share volume → both levels adjust → stop
clears both sides. Full `qa/run.sh` green; AI-vision on `voice-07/08-screenshare*.png`. Backend
`voice-screen` relay has a Go integration test (on + off). go+web build/vet/test green.

**Bug caught by QA (fixed before ship):** the first run failed "B no longer sees A’s screen tile"
after stop — I'd added `voice-screen` to the session's `handle()` but forgot to DISPATCH it from
Chat.tsx's `onmessage`, so the teardown signal never arrived (the tile had appeared only via the
video track). Fixed the dispatch + added a `track.onended` safety net. This is the value of driving
the real two-client UI: the unit pieces all passed; only the live flow exposed the missing wire-up.

**QA-process win:** confirmed headless Chromium CAN screen-capture with
`--auto-select-desktop-capture-source` — so screen share is now part of the autonomous browser QA,
not a manual-only feature. Guarded so it logs+skips (not fails) where capture is unavailable.

**Follow-ups (logged):** SFU-path screen share (LiveKit-native) for scale; true 4K@60 throughput
depends on hardware/network and wasn't measurable headlessly (the fake source isn't 4K).

## 2026-06-15 (iter 82) — Rule-C hardening: insecure-JWT-secret warning + config tests (security)

Rotated to **security** (north star: hostile-input-proof / secrets hygiene). Found a real Rule-C
gap: `config.Load()` AND `docker-compose.yml` both silently default `JWT_SECRET` to the public
`dev-insecure-change-me`, so a self-hoster who forgets it runs forgeable-token-open with NO signal —
and `internal/config` had **zero tests** (the only package without any). Closed both:
`Config.InsecureJWTSecret` flag → `main.go` logs a loud boot WARNING (warning, not hard-fail, so the
one-command stack stays zero-config — Rule A); new `config_test.go` (7 tests) covers defaults, PORT
precedence, env override, SFU opt-in, and the flag both ways.

**Verified (Rule 14, real boot — not just unit tests):** ran the actual binary; WITHOUT `JWT_SECRET`
the WARNING prints, WITH it set it's silent. `go build`/`vet`/`test` green. Docs-synced (README +
.env.example gained the warning note and the previously-undocumented `OPENCORD_SFU_*` vars).

**QA-process win:** the "which package has no tests?" scan (`internal/config` = 0 test files) is a
cheap, repeatable coverage probe — worth running each tick. Next under-covered target to eyeball:
`internal/db` (1 test file) and `cmd/server` (0, though it's mostly wiring).

**Process note:** backend/config change, no UI surface → browser QA correctly NOT triggered (iter 82,
82%3≠0); verification was the Go suite + a real-boot log check. **Not redeployed:** prod already sets
`JWT_SECRET` so the warning never fires there, and there's no frontend/API/bundle delta to serve or
rollout-verify — a `railway up` would rebuild an artifact with identical prod runtime behavior
(anti-churn, same reasoning as iter-80's QA-only no-deploy). Pushed to `origin` as the OSS deliverable.

## 2026-06-15 (iter 81) — @mention autocomplete (chat/UX polish)

Rotated off audio (now complete) to **chat/UX**. Shipped **@-mention autocomplete** — the composer
side of mentions (rendering already existed). Typing `@`+partial opens a suggestion listbox of
channel-active usernames (message authors minus self, capped 6); ↑/↓ move, Enter/Tab accept (insert
`@username ` + restore caret), Esc/send/channel-switch dismiss; click uses mousedown+preventDefault
to keep textarea focus. Pure `activeMention(text, caret)` helper detects the token; no new endpoint.

**Verified:** web build + tsc + vitest (6/6) green; `qa/realtime.mjs` (two clients) gains a flow —
A types `@bo` → dropdown suggests B → Enter inserts (and does NOT send) → Send delivers → B sees the
mention chip; full `qa/run.sh` (browser+realtime+voice) all PASS; AI-vision on
`rt-08-mention-autocomplete.png` confirms the highlighted suggestion sits cleanly above the composer.

**QA gap found (next tick):** the candidate source is "authors who've posted" — so autocomplete can't
suggest a silent channel member. For server channels we already fetch `serverMembers` on demand; a
follow-up could union those in. Logged for a future tick (not a regression — documented MVP scope).

**Process note:** app-code change → committed + pushed to `origin` AND `railway up` deployed +
rollout-verified (live bundle serves the new code), unlike iter-80's QA-only no-deploy.

## 2026-06-15 (iter 80) — close the PTT-on mobile-overflow QA gap (Track-0 self-improvement)

Closed the **P1 QA gap I logged last tick (iter 77)**: the voice-bar ≤640px overflow assertion ran
in the PTT-OFF state, so the densest layout (PTT-on adds `Hold to talk` + `key:`/rebind) was never
mobile-overflow-checked — exactly the kind of blind spot the meta-QA step exists to catch. Added a
PTT-on 390px block to `qa/voice.mjs`: asserts `scrollWidth <= clientWidth`, Talk + key controls
reachable, and saves `voice-06-ptt-mobile.png`. Full `qa/run.sh` (browser+realtime+voice) green;
AI-vision on the new shot confirms a clean wrap (every control readable/tappable, no clip).

**Process note:** this is the loop's "find a gap → log it → close it next tick" cycle working as
intended — a QA-only change (no app/bundle delta), so committed + pushed to `origin` but **NOT
redeployed** (anti-churn: nothing new to serve; a `railway up` would rebuild an identical bundle).

**QA gap found (next tick):** the mobile checks all run on client A only; tablet/landscape (e.g.
768px) and the PTT-on bar at that width are unverified. Low priority. Likely rotate to **infra** or
**UI** product work next tick — audio voice-controls + their QA are now complete.

## 2026-06-15 (iter 77) — voice global PTT hotkey (audio product feature)

Stayed on **audio** (per the iter-76 plan) — shipped the last open voice-control item, a
**rebindable global push-to-talk hotkey**: while in a call with PTT on, holding a bound key
(default `` ` ``/Backquote, stored by physical `KeyboardEvent.code` in localStorage so it survives
reloads) opens the mic from anywhere in the app; releasing or losing window focus closes it. A
window-level keydown/keyup listener gates `setTransmitting`; it **stands down when focus is in a
text field** (`isEditableTarget`) so holding it to talk never types into the composer and a chat
keystroke never opens the mic. A "key: X / rebind" control in the voice bar enters a capture mode
(next key press becomes the binding, Escape cancels). All reset on leave / PTT-off.

**Verified (real product, not just tests):** web build + tsc + vitest (6/6) green; extended
`voice.mjs` (3-way mesh) with 6 new checks — default Backquote, hotkey-down transmits, hotkey-up
stops, **suppressed while typing in the composer**, rebind-capture (→ KeyV), rebound key transmits;
full `qa/run.sh` browser+realtime+voice all PASS. AI-vision on `voice-05-ptt-talking.png` confirmed
the new `key: \`` control sits cleanly in the dense voice bar (no overlap/clip, single row).

**QA gap found (next tick):** the 390px mobile-overflow check on the voice bar runs BEFORE PTT is
toggled on, so the PTT-on controls (`Hold to talk` + `key:` button — extra width) are never
overflow-checked at ≤640px. Add a PTT-on mobile-width overflow assertion. Logged as a GOAL.md polish.

**Next:** audio voice-controls are now complete (mute, deafen, VAD, per-user volume, PTT + hotkey).
Rotate to **infra** (single-goroutine hub vs the "scales" north star) or **UI** polish next tick.

## 2026-06-15 (iter 76) — voice deafen (audio product feature, both transports)

Rotated to **audio** (per the iter-75 plan) — shipped **deafen**, the baseline Discord voice
control missing alongside mute/PTT. Transport-agnostic: `VoiceTransport.setDeafened(on)` in BOTH
the mesh `VoiceSession` and the SFU `SfuSession`. Deafen sets every remote `<audio>` element's
`.muted` (and mutes any peer/track that arrives while deafened) and adds a `!deafened &&` guard to
`applyMicState` so it also forces the mic off (Discord convention); un-deafen restores incoming +
the prior mute/PTT state. Playback-only muting means **speaking rings still show** while deafened.
UI: a deafen/undeafen toggle in the voice bar + "· deafened" on the self chip (resets on leave).

**Verified (real product, not just tests):** web build + tsc + vitest (6/6) green; `voice.mjs`
deafen flow in the **3-way mesh** asserts every remote audio element flips `.muted` true→false and
the self chip flags deafened; AI-vision on `voice-05-deafened.png` confirmed the bar renders clean
(undeafen highlighted danger, rings intact, no overflow). The existing 390px overflow check now
also covers the new button. Shipped to Railway + live-verified.

**Process win:** this was the planned product tick after two QA-only ticks — the rotation
(chat 73-74 → security 75 → audio 76) is keeping both the product AND its test net moving.

**Next:** audio still has *global PTT hotkey* (keyboard-driven Talk) open; or rotate to **infra**
(the single-goroutine hub vs the "scales" north star) / **UI** polish. Pick furthest-from-north-star.

## 2026-06-15 (iter 75) — adversarial WS-frame guard (security → hostile-input-proof)

Rotated off chat to **security**. The WS suite covered the *handshake* surface (auth 401,
garbage token, bad channel-id 400, non-member 403) + rate-limit + voice flood, but had **no
adversarial coverage of the inbound MESSAGE-frame path on a valid connection** — the exact
"every WS frame is attacker-controlled" surface (Rule B). Added `TestServeWSHostileFrameHandling`:
over one authenticated connection it fires non-JSON garbage, a type-confused field
(`{"body":12345}`), empty / whitespace / oversized (>4096) bodies, and a hostile `replyTo`
(bogus + negative id), paced ~600ms so the rate bucket refills and each frame is actually
processed (not rate-dropped). Asserts: the connection **survives the whole battery** (a final
legit message lands), **no hostile content persists** (size bound + empty-reject + unmarshal-drop
all hold), and the **bad replyTo is dropped** (message posts with ReplyTo nil) — proving the new
reply path is hostile-input-proof through the LIVE socket, not just the store unit test. No bug
found; now regression-guarded.

**Balance note (loop-process):** ticks 73→75 were feature(+tests) → test → test. Track 0 is the
top priority so QA-heavy ticks are legitimate, but next tick should advance a real **product**
feature (audio **deafen** or **global PTT hotkey**) so the product — not just its test net —
keeps moving. Per-component rotation: chat ✓(73-74), security ✓(75) → **audio** next.

**Next QA growth:** extend the adversarial test with a cross-channel `replyTo` over the live WS
(only unit-tested today) and an oversized FRAME (>16384 → triggers the SetReadLimit close path).

## 2026-06-15 (iter 74) — two-client LIVE reply propagation QA (Track 0)

Closed the coverage gap I logged last tick: replies were proven single-client (browser.mjs
3g) and at the store layer (TestReplyIntegration), but the **live two-client WS broadcast
path** for the denormalized reply preview was unproven. Added `realtime.mjs` 1c: B hovers A's
message → reply → "Replying to A" bar → send → **A sees the quoted preview appear live**, and
the assertions check A's render carries `replyToAuthor` (the original author) + `replyToBody`
(the original snippet) — i.e. the WS `message` Event ships the reply fields, not just history.
AI-vision on `rt-01c-reply-live.png` confirmed it renders cleanly; a **bonus** confirmation
also surfaced — because realtime.mjs runs in the same DB after browser.mjs (which deletes a
message), A's history showed a reply to that deleted message rendering `↰ author [deleted]`
live, end-to-end proof of the soft-deleted-target snippet path.

**Process note:** a QA-only change (qa/ isn't in the Docker image) is committed + pushed to
origin but **NOT deployed** — the product artifact is byte-identical, so a `railway up` would
be pure churn (Rule 10/16). The loop's "ship = deploy" step applies to product changes only.

**Next (rotate off chat):** the live `message-deleted` broadcast doesn't update reply previews
of OTHER messages that quote the deleted one (they only refresh to "[deleted]" on history
reload) — minor consistency polish. Bigger value: rotate to **audio** (deafen / global PTT
hotkey) or **security** (an adversarial WS-frame probe) next tick so no component stagnates.

## 2026-06-15 (iter 73) — message replies (chat) + de-flaked the voice flood guard

Component advanced: **chat**, toward "instant + lossless realtime" Discord parity — replies were
the last missing message primitive (pin/edit/delete/react/search/slowmode already shipped), and the
right rotation after ~5 voice + a11y ticks. A reply references an earlier message in the SAME
channel; the server validates + denormalizes the preview (author + 80-rune snippet) onto the message
so history AND the live WS broadcast render the quoted line with no extra round-trip. New
`Store.SaveReply` (Save delegates with nil → zero blast radius on 27 existing callers).
**Cross-channel / nonexistent refs are dropped server-side (Rule B/C)** — a client can't leak a
message it can't see through a reply preview. Verified: `TestReplyIntegration` (same-channel
populates · cross-channel dropped · bogus dropped · soft-deleted target → "[deleted]"), browser-QA
flow 3g (hover → reply → "Replying to" bar → send → `.reply-context` renders + clears), and
AI-vision on the two new screenshots (bar + quoted preview render cleanly, no clip/overlap).

**QA-process fix (the meta-improvement — highest-value this tick):** `TestServeWSVoiceFloodGuard`
was **flaky** — failing at a different frame each full-suite run (399 / 136 / 262; "close sent" /
"connection reset by peer"), green in isolation. Root cause: it treated ANY write error during the
500-frame flood as fatal, but the server legitimately closing/throttling a flooding client (whose
own fanned-back echoes outpace its drain) is the guard WORKING, not a bug. Fix: tolerate a write
error in the flood phase (the server defending itself only strengthens the "bounded" assertion);
keep it fatal for the 25-frame legit burst. Stable 15/15 after. **Lesson: a load test that asserts a
connection stays OPEN while it floods is racy — assert the OUTCOME (relay bounded), not the transport
staying up.** This is exactly the Step-6.5 mandate: a flaky test silently erodes the gate; fix the
QA, don't tolerate it.

**Next QA growth:** two-client live reply propagation in `realtime.mjs` (A posts → B replies → A
sees the quoted preview live); reply-to-a-reply (nested) rendering; reply preview refreshing to
"[deleted]" after the target is deleted (today it only refreshes on a history reload).

## 2026-06-15 (iter 72) — accessibility: axe-core in the QA + WCAG-AA contrast fixes (rotated to UI)

Component advanced: **UI**, toward "accessible" — the least-addressed word in its north star, and after ~10
audio ticks the right rotation. Integrated **axe-core** WCAG 2 A/AA scanning into the browser QA: it scans a
content-rich chat view and fails on any serious/critical violation. It found one rule (color-contrast, 7
nodes) — all real, all fixed: links/@mentions used the accent blue as *text* (3.2:1), green labels used the
presence green (4.33:1), and avatar initials were always white (2.8:1 on bright hues). Fixed with text-safe
`--link`/`--online-text` vars (keeping `--accent`/`--online` as button fills/dots) and a luminance-aware
`avatarTextColor` (black-on-bright / white-on-dark, backgrounds stay vibrant).

**Why an automated a11y gate beats a manual pass:** accessibility is the kind of thing that silently
regresses — a new muted color, a new icon button without a label — and a human won't re-audit every tick.
axe-core makes it a *gate*: the next contrast regression fails CI, not a future audit. This is the same
"verify-don't-assume" lever as the DB-test-gate (iter 61) and bundle-diff (iter 59b), now applied to a11y.

**The avatar fix is the interesting one:** "make initials white" is the obvious move and it's wrong for half
the hues. Computing the background's relative luminance and flipping the text color is the correct, general
solution — and it's pure logic, so it could be unit-tested by the vitest harness added last tick (didn't this
tick, but it's the natural home).

**Highest-value next item:** axe scans one content-rich view; it does NOT yet cover the **in-call voice bar**
(its own dense cluster of colored controls — Join/PTT/leave/volume) or the **server/members panels** (role
badges, which I fixed blind). Add an axe scan at those surfaces too (cheap — one `AxeBuilder().analyze()` each
in qa/voice.mjs + after the members panel opens). Also: keyboard navigation is wholly unaddressed (can you
operate the app with no mouse?) — a real gap toward the "accessible" bar, but a bigger, separate tick.

Component advanced: **audio**, toward "thousands without fidelity loss." The SFU client now connects with
`autoSubscribe:false` and pulls only the **top-N loudest** remote audio streams (`MAX_AUDIO_SUBSCRIPTIONS`=12)
— a 1000-person room can't mix 1000 streams, so this is the piece that turns "the SFU connects" into "scales."

**Closed a long-standing coverage gap on the way:** the web client had **zero unit tests** — voice.ts's
perfect-negotiation, markdown.tsx, all of it only ever E2E'd. The top-N picker is exactly the kind of pure
logic that's painful to E2E (you'd need >12 fake clients) but trivial to unit-test, so I added **vitest** (the
web's first test runner) and 6 cases for `selectAudioSubscriptions` (cap, active-priority, sticky recency,
quiet-room fill, stale-id filtering, no-overflow). That seam — extracting the decision into a pure function —
is the reusable trick: the at-scale behavior is now provable without a 1000-browser test.

**The verification trap I avoided:** with `autoSubscribe:false`, the *presence* roster still populates even if
the subscription logic subscribes to NOBODY — so the existing SFU E2E (roster-only) would have stayed green
while audio silently broke. Added an explicit "client actually subscribed to the peer's audio track" assertion
(SfuSession now attaches the audio element with `data-voice-audio`, like mesh). Lesson: when you flip a default
that gates a side effect (audio), assert the side effect, not the proxy (presence) that survives the bug.

**Highest-value next item:** the at-scale path (>12 participants) is unit-tested but never exercised against a
real LiveKit. A cheap, high-value harness upgrade: have `qa/sfu.mjs` spin up ~15 lightweight livekit-client
connections (the synthetic kind from iter 69, no UI) in one room and assert a 16th client subscribes to ≤12
audio tracks — proving the cap end-to-end against the real SFU without 15 full browser UIs. Target next.

Component advanced: **audio**, toward the thousands-scale north star. Shipped the client SFU path:
`web/src/sfu.ts` `SfuSession` over `livekit-client`, implementing the SAME `VoiceTransport` interface as the
mesh `VoiceSession`, so `joinVoice` just picks one on the `/api/voice/token` response and the voice-bar UI
(roster/mute/PTT/volume/devices/speaking) is transport-agnostic. The tick-69 harness paid off exactly as
intended: because token-acceptance + the LiveKit-docker ICE gotcha were already solved, building the client was
low-risk wiring, and the real-app E2E (`qa/sfu-run.sh` now boots vite too — two browsers Join voice over a
local LiveKit and see each other) passed without a networking rabbit hole.

**Two design wins worth recording:**
- **Defined `VoiceTransport` as the seam.** Instead of branching mesh-vs-SFU all over Chat.tsx, both sessions
  implement one interface and `voiceRef` holds the abstract type. The whole transport swap is *one ternary* in
  joinVoice; everything downstream (mute/PTT/volume/leave/roster) is untouched. The right abstraction made a
  scary-sounding "second voice stack" a small, contained change.
- **Lazy-imported livekit-client** (`await import(...)`) → Vite code-split it into its own 133 KB-gzip chunk,
  OUT of the 58 KB main bundle. A mesh-only self-hoster never downloads it. Verified in the build output, not
  assumed — the chunk is listed separately. (Matters for the "lean to self-host" ethos.)

**Highest-value next item:** the SFU connects, but the audio north star is *thousands without fidelity loss*,
which needs **active-speaker selection** (forward only the top-N loudest) — LiveKit supports this via
subscription/`setSubscribed` + the active-speakers event I already wire. Next tick: cap how many remote audio
tracks the client subscribes to (e.g. top 10 speakers), so a 1000-person room doesn't try to mix 999 streams.
Also a real follow-up logged in SPEC: browser-autoplay gesture handling (`room.startAudio()` + an enable-audio
prompt) for the blocked-autoplay case — invisible under the test's autoplay flag, but real for users.

Component advanced: **audio** (SFU scale path). De-risked slice 3 before building the big client refactor:
stood up a local LiveKit (`livekit-server --dev`, docker) and had **two real browsers** connect to it with
tokens minted by `/api/voice/token`, asserting each sees the other. **SFU PROOF PASSED** — closing iter-67's
explicitly-deferred "does a real LiveKit accept our token?" gap. Reusable `qa/sfu-run.sh` harness committed;
it's the E2E foundation for the client path.

**The bug the proof caught (and why de-risk-first was right):** the *first* run failed with "could not
establish pc connection" — but crucially **NOT** an auth/token rejection, so the token was already accepted;
the failure was ICE. Root cause: LiveKit in docker advertises its **container IP** (172.17.0.2) as the WebRTC
candidate, unreachable from the host browser. Fix: `--node-ip 127.0.0.1` so candidates point at the mapped
host port. Had I built the whole client transport first and hit this, I'd have debugged a "voice doesn't
connect" symptom across the entire new client stack instead of in a 70-line proof. **Lesson reinforced: when a
new external dependency is on the critical path, prove the smallest end-to-end slice against the real thing
before building on top of it** — the failure mode you learn is worth more than the code you'd have written.

**Highest-value next item:** SFU **slice 3** proper — the client transport. Build `web/src/sfu.ts`
(`SfuSession` over `livekit-client`, lazy-imported so the mesh-only bundle stays lean) exposing the SAME
surface Chat.tsx already drives (roster/mute/volume/PTT/speaking via callbacks); `joinVoice` fetches the
token and picks mesh vs SFU. E2E it by extending `qa/sfu.mjs` to drive the actual Opencord voice UI (Join
voice → SFU transport → two users hear each other) instead of the synthetic livekit-client connection.

Component advanced: **chat + security** (deliberate rotation — I'd done ~8 straight audio ticks, and the SFU
*client* path needs a real local LiveKit to E2E, which deserves its own focused tick). Shipped slowmode: a
per-channel post cooldown, admin-set, **enforced server-side** in the one `Store.Save` path so a client can't
bypass it (Rule B). Two correctness calls worth recording:
- **Measured the cooldown in SQL** (`now() - created_at < make_interval(...)`), not in Go — so it can't be
  fooled by app/client clock skew, and there's one source of time truth (the DB that also stamps `created_at`).
- **Mirrored the existing `postPolicy`/read-only pattern** end to end (sentinel error → WS error event →
  admin-gated PATCH → header badge), so the feature slotted into proven grooves instead of inventing new ones —
  fast and low-risk. The adversarial test (Rule 15) is the real proof: non-admin throttled, admin exempt,
  cooldown expires, non-admin can't *set* it (403).

**Rotation was the right call, but flag for honesty:** the audio SFU is still the component *furthest* from its
north star (no SFU = no thousands-scale). Rotating built breadth (chat parity) but didn't close that gap. The
loop should alternate, not abandon — **next tick goes back to SFU slice 3** (client path), and its first task is
the deferred decision: stand up a throwaway local LiveKit (`livekit/livekit-server --dev` in docker, NOT the
Railway demo) to (a) finally prove a real LiveKit accepts our minted token — closing iter-67's deferred gap —
and (b) E2E the client transport. That LiveKit-E2E harness is the single highest-value next thing; it unblocks
all remaining audio work.

Component advanced: **audio** (the SFU scale path). Built `POST /api/voice/token` + `internal/voice` token
minter + optional config. Two wins worth recording:

**Avoided a heavy dependency.** The SPEC said "mint via `server-sdk-go`", but a LiveKit access token is just
an HS256 JWT with a `video` grant — so I minted it with the **golang-jwt lib auth already uses**, pulling in
`server-sdk-go` (pion/webrtc + protobuf, a big tree) for *nothing*. go.mod unchanged. Lesson: before adding an
SDK, check whether the wire format is something you can already produce — "use the official SDK" is often
heavier than the actual need, and for a self-host-lean project that bloat is a real cost.

**Verified a deferred-backend feature honestly (Rule 14).** Can't prove a *real* LiveKit server accepts the
token (no SFU instance — deferred), so I verified what IS verifiable: the minted JWT decodes to the correct
LiveKit claims and is secret-bound (unit), the endpoint enforces the access gate (DB-integration: unauth 401 /
non-member 403 / member → token), and the live binary returns `{sfu:false}` unconfigured + a valid token when
the env is set. Stated the deferred bit explicitly rather than implying full E2E. Rule-15 pass landed: a
**non-member gets 403, never a token** (the token is the keys to the room — leaking it to a non-member would
bypass channel membership entirely).

**Highest-value next item:** SFU **slice 3** — the client SFU path. When `/api/voice/token` returns a token,
connect via `livekit-client` (publish mic, subscribe, use LiveKit's active-speaker events for the existing
ring) instead of building the mesh; mesh stays the fallback. This DOES add a client dep (`livekit-client`) and
is the first slice that needs a **real SFU to E2E** — so it also forces the deferred decision: stand up a
throwaway LiveKit (local docker, NOT the Railway demo) purely to E2E the client path, or gate slice 3's
"done" on that. Decide which at the top of next tick.

Component advanced: **audio**, toward its north star (thousands/call). A deliberately code-free tick: the next
audio leap is the SFU (mesh is N²-bound at ~4 peers), which is a Rule-16 adoption — so the right move was to
**decide before building**. Ran `stack-guardian`: **APPROVE LiveKit** (Apache-2, Go + first-class server SDK
= lowest integration cost here; genuinely free single-node self-host) over mediasoup (Node/C++, no Go API)
and Janus (GPLv3 + C). Wrote SPEC "mesh → OSS SFU (LiveKit) scale path": strictly opt-in (`OPENCORD_SFU_URL`
empty ⇒ mesh; one-command stack stays SFU-free — Rule A), server mints room=channel tokens from the verified
JWT gated by `CanAccessChannel` (Rule B/C), Railway demo instance DEFERRED (TCP-only + unbounded egress).

**Why no code this tick (and why that's the disciplined call):** adding a config field + status endpoint that
no client consumes yet would be a speculative half-feature (Rule 6 — "no speculative abstractions"). The SFU
is a multi-slice change touching go.mod (server-sdk-go), a new endpoint, and a whole client transport path;
each slice should land complete. The decision + SPEC IS the gate everything else depends on — real artifact,
not churn.

**Highest-value next item:** build SFU **slice 2** — the server token endpoint. Add optional SFU config (all
default empty), pull `github.com/livekit/server-sdk-go`, and `POST /api/voice/token?channel=<id>` minting a
room=channel JWT from the verified user, `CanAccessChannel`-gated, returning `{sfu:false}` when unconfigured.
Adversarial pass (Rule 15): a non-member must get 403, not a token; a forged/tampered JWT must not mint one.
DB-integration test for the access gate. No client change yet (mesh stays) — keep the slice complete + testable.

Shipped push-to-talk — the last non-trivial voice-control gap. Component advanced: **audio**, toward the
owner's Discord-parity/"crisp" bar (voice now has mute, device auto-follow + picker, active-speaker ring,
per-user volume, and PTT). The design choice that paid off: a **single `applyMicState()`** deriving the mic
track's `enabled` from one rule (PTT on → live only while transmitting; PTT off → live unless muted) instead
of scattering `track.enabled = …` across mute/PTT/device-swap paths. mute, PTT, device switching, and the VAD
ring all read from that one function, so they can't disagree. Chose a **press-and-hold button** over a
keyboard hotkey: works on desktop + touch and sidesteps the key-vs-message-composer conflict (global hotkey
noted as a later nicety).

**QA-growth note:** driving press-and-hold from Playwright is just `locator.dispatchEvent('pointerdown'|'pointerup')`
against the React `onPointerDown/Up` handlers, with `data-transmitting` mirroring state — clean and
deterministic, no mouse-coordinate math. Reused the "snap a screenshot at the transient state" trick
(`voice-05-ptt-talking.png`) from the speaking-ring tick.

**Highest-value next item:** voice controls are now broad but the audio north star is **thousands per call**,
which mesh can't reach — every participant uploads to every other (N² fan-out). The real next leap is the
**OSS SFU** (LiveKit candidate). That's a Rule-16 adoption → must run `stack-guardian` first and stay free to
self-host. Next tick: *scope* it — `stack-guardian` on LiveKit (self-host cost/footprint), and a SPEC for the
mesh→SFU switch with graceful mesh fallback when no SFU is configured (Rule A). Don't adopt yet; decide first.

Last tick I predicted the dense in-voice bar would "wrap badly ≤640px" and queued a redesign (move volume into
a popover). Did the AI-vision pass at 390px first: it wraps **cleanly** — title + your chip, peer chip + volume
slider, mic selector, output selector + mute/leave each stack onto their own readable, tappable row; an
objective `scrollWidth <= clientWidth` check confirms **zero horizontal overflow**. So the right move was the
opposite of what I queued: **don't redesign** (Rule 10 anti-churn), just add the missing coverage. `qa/voice.mjs`
now resizes to 390px mid-call and asserts the bar renders, leave is reachable, a peer chip is visible, and the
bar doesn't overflow — plus `voice-04-mobile.png` for future vision passes.

**Loop lesson:** a queued "this will probably be bad, redesign it" item must be **re-verified before acting** —
I almost spent a tick rebuilding a layout that was already fine. Predictions decay; check the artifact first.
The anti-churn rule and the verify-first rule pulled the same direction here.

**Highest-value next item:** push-to-talk — the last non-trivial voice-control gap toward the owner's parity
bar. Plan: a press-and-hold "Talk" button (works on desktop + touch, no key-vs-composer conflict) gating the
mic via `track.enabled`, with PTT/mute mutually exclusive in the UI; reflect `transmitting` in a `data-` attr
so the E2E can assert the hold→transmit→release flow. Its own tick (real state interactions with mute + VAD).

Shipped two things: **per-user volume** (a local-only slider per peer chip → `HTMLAudioElement.volume`;
never signaled, so it can't be abused to boost yourself for everyone — Rule B) and the **browser-QA voice
entry guard** I flagged last tick. `qa/browser.mjs` (single client) never touched voice — voice lived
entirely in the 2+ peer `qa/voice.mjs`. Now the single-client run launches with fake media and asserts the
"🎙 Join voice" control renders, is enabled once connected, joins solo (in-voice bar + your chip), and that
leaving removes the bar. So a regression that breaks the *entry point* (button gone, getUserMedia broken) is
caught by the cheap single-client run, not only the heavier multi-peer one.

**Testing note worth reusing:** driving a React **controlled `range` input** from Playwright needs the native
value-setter trick (`Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value').set` + dispatch
`input`) — a plain `el.value=…` doesn't fire React's onChange. Encoded in the volume assertion.

**Highest-value next item:** the voice bar is getting dense (roster + speaking ring + volume slider + 2 device
selectors + mute/leave), and on a phone viewport it will wrap badly — **no mobile-viewport check covers the
voice bar yet**. Next tick: add a ≤640px AI-vision pass on the in-voice bar (does it stay usable/readable when
wrapped?), and if it's cramped, move per-peer volume into a click-to-open popover (Discord's pattern) rather
than an always-visible inline slider. Product-wise, push-to-talk is the next voice-polish step.

Shipped the speaking ring (owner's "close the gap Discord has"): client-side Web Audio VAD — one
`AudioContext`, an `AnalyserNode` per stream (local mic + each remote), a 120 ms RMS sampler with 250 ms
hysteresis so the ring doesn't flicker. No new signaling: each client detects remote speakers from the audio
it already receives (nothing to trust/rate-limit — Rule B). `VoicePeer.speaking` + a local-speaking callback
→ a green `.speaking` ring on the voice-bar chips. E2E + AI-vision verified (the fake mic's tone lit voxb's
ring in the screenshot).

**Nice QA win:** Chromium's fake mic emits a periodic tone, so the *same* fake-media flags that let voice
connect headlessly also exercise VAD for free — `qa/voice.mjs` asserts a `[data-speaking="true"]` chip
appears. The vision check needed a screenshot captured *during* a loud phase (the tone pulses), so the test
now snaps `voice-03-speaking.png` the instant it first sees the ring rather than at a fixed point — a small
pattern worth reusing for any time-varying visual.

**Highest-value next item:** the browser QA (`qa/browser.mjs`, single-client) still never touches voice at
all — voice is only covered by `qa/voice.mjs`. That's fine (voice needs ≥2 peers), but the **single-client
browser QA should at least assert the "🎙 Join voice" control renders + is gated** (disabled until WS
connect), so a regression that removes/breaks the entry point is caught even without the multi-peer run.
Target next tick. Product-wise, **per-user volume sliders** are the next voice-polish step toward the bar.

Fixed the iter-60 P1: the health-gate `go test ./...` ran without `DATABASE_URL`, so every
`*Integration` test (`t.Skip`ped without a DB) silently no-op'd — the gate reported green while never
running the auth/channel/DM-access/role/moderation/voice-flood security tests. `scripts/test.sh` now boots
the compose Postgres (when `DATABASE_URL` is unset and docker is up), runs the full suite so the integration
tests execute, and tears the DB down; `make test` and `CCF_TEST_CMD` point at it. Verified: 20+ `*Integration`
tests that were skipping now RUN and pass. CI was already correct (sets `DATABASE_URL`), so this is
local/loop ↔ CI parity, not new coverage in CI.

**Reflection — the meta-lesson across iters 59b→61.** Three ticks in a row the failure mode was the same
shape: *a green signal that wasn't actually exercising the thing.* 59b: a `git push` that didn't deploy but
looked like it did. 60: a flood test that crashed because the gate never ran integration tests. 61: the gate
itself skipping silently. The through-line: **a skip/200/"pushed" is not a pass — verify the work actually
happened.** The loop now (a) diffs the live bundle after deploy, (b) runs integration tests against a real
DB. **Highest-value next item:** the loop's Step 1 wording still says "`eval $CCF_TEST_CMD` (go test)" and the
skill text shown at fire-time lags the on-disk file — but more concretely, the next *product* gap is voice
**polish toward the owner's "crisp" bar**: a voice-activity/speaking indicator (active-speaker highlight),
which also lays groundwork for the SFU's active-speaker selection. Target that next tick.

Hardened the voice signaling surface I shipped last tick (Rule 15: adversarial pass on a new input). Two
backend fixes: a **dedicated voice rate bucket** (voice frames were rate-limit-*exempt* → an unthrottled
flood is fanned out to the whole channel = amplification DoS) and — the bigger find — a **send-channel
close-race panic**. Writing the flood test crashed the server: `Hub.emitToChannel` did `close(c.send)` to
drop a stuck consumer, but `ServeWS` (history) and `readPump` (error reply) also write to `c.send`, so a
concurrent close panics with "send on closed channel". A flood reliably triggered it. Fix: **never close
`c.send`** — close a per-client `done` channel (hub goroutine only, ≤once); producers `sendSafe` via
`select{case send<-d: case <-done:}`; writePump exits on `<-done`. `go test -race` clean; full E2E green.
Reproduced the panic first, fixed, re-attacked → bounded + no crash (Rule 15 cycle).

**Highest-value loop-process gap (fix next tick): the health-gate `go test ./...` runs WITHOUT a DATABASE_URL,
so every DB/WS integration test SKIPS.** I only caught the panic because I manually exported `DATABASE_URL`
and ran the ws tests against a booted Postgres — the loop's Step-1 gate has been green every tick while
silently skipping `TestServeWS*` (rate limit, voice signaling, voice flood, DM access control, …). The
browser/realtime/voice E2E in `qa/run.sh` covers the path end-to-end, but the Go integration tests that assert
specific security logic never run in the gate. **→ GOAL P1:** Step 1 should boot the compose DB and run
`DATABASE_URL=… go test ./...` so integration tests actually execute (or CCF_TEST_CMD should). A gate that
green-lights while skipping its security tests is a false floor — exactly the "verify, don't assume" lesson
from iter 59b, one layer down.

**Secondary smell (logged, not yet fixed):** the consumer-buffer-overflow drop penalizes the *victim* — when
A floods, it's B (or A's own echo) whose 32-frame buffer overflows and gets dropped, not the attacker. The
new per-sender voice bucket throttles at the right layer (the source), but the buffer-drop backstop drops the
wrong party. Fine as a backstop; worth revisiting if voice scales.

## 2026-06-14 (iter 59b, owner-directed) — 3-peer N-way mesh test + the deploy was never actually verified

Owner asks this turn: (1) add a 3rd peer to the voice test, (2) "always push it to railway opencord, it
looks it wasn't last tick."

**(1) 3-peer mesh.** `qa/voice.mjs` now drives THREE fake-media contexts and asserts each participant holds
**2 connected links** (a genuine N-way mesh, not just a pair), each roster lists the other two, two live
inbound audio tracks each, and — the new correctness check — **a partial leave collapses only its own
links**: B leaving drops B from A's and C's rosters while the A↔C link survives; then C leaving empties A.
Caught a self-inflicted bug doing it: a local `const join = …` helper **shadowed `path.join`**, so the
screenshot got a Promise as its path (`path.lastIndexOf is not a function`). Renamed to `joinCall`. Lesson:
in these test files, never name a local after an imported util.

**(2) The deploy was genuinely broken — and my first diagnosis was wrong, which is the real lesson.** I
*assumed* the `opencord` Railway service auto-deployed from GitHub on push (a `repo` field in the project
JSON + a false-positive `grep` made me believe voice was already live), and I wrote that into the skill,
`.ccf`, and memory. Then I actually **diffed the live bundle**: `grep -o 'Join voice'` on the served JS
returned **0** — voice was NOT live, and the latest Railway deployment was still the pre-voice one from
17:41Z. My two pushes had created **zero** deployments. Truth: **Railway here is NOT GitHub-wired; a `git
push` does not deploy.** The deploy is CLI — `railway up --service opencord --ci` (the original memory note
had it right; I'd "corrected" it wrongly). Deployed that way → live bundle now has voice, verified against
the running site, and a 3-peer mesh E2E passes against the **live** URL (proving WSS signaling through
Railway's proxy, not just localhost). **Fixes:** `.ccf` `CCF_DEPLOY_CMD='…railway up…'` +
`CCF_DEPLOY_TARGET=railway` + `CCF_LIVE_URL`; Ship (step 5) now commits→pushes→`railway up`→**verifies the
live bundle actually serves the new code** (not just `/healthz`=200 — the *old* build also 200s, which is
exactly what fooled me); scope guard narrowed to ContextForge's Railway so Opencord's own is verifiable.
**Two process lessons:** (a) *verify the rollout by diffing what's served, never by assuming a push deployed
or trusting a 200* — silence and a healthy old build both read as success; (b) *don't bake an unverified
assumption into config/docs* — I shipped the wrong mechanism into three files before testing it; test first,
then encode.

## 2026-06-14 (iter 59) — Voice slice 2 shipped (mesh WebRTC client + device intelligence); the WS-race the E2E nearly hid

Built voice **slice 2**: `web/src/voice.ts` (`VoiceSession`, one RTCPeerConnection per peer,
**perfect-negotiation** glare handling, crisp DSP + ~96 kbps Opus, **device auto-detect** following the
OS default with a manual mic/output picker — the owner's mid-tick directive), wired into `Chat.tsx`, and
a two-context fake-media E2E (`qa/voice.mjs`) proving a real `connectionState === "connected"` mesh link
+ live remote audio on both sides. Full harness green: `browser=0 realtime=0 voice=0`.

**The lesson — a green-looking E2E that was actually a silent product/test race.** The *first* full-harness
run failed at "A joins voice" (`.voice-bar` never appeared). Root cause wasn't the WebRTC code: `joinVoice`
guards on `wsRef.current?.readyState === WebSocket.OPEN`, and the test clicked "Join voice" the instant
after register — before the channel WS finished opening — so the guard silently returned and nothing
happened. **Two process gaps this exposes:**
1. **QA gap (fixed this tick):** any action that depends on the WS being OPEN must first wait for the
   connected signal (`.dot.online`). I added that wait; voice.mjs is now deterministic. The browser/realtime
   suites happen to avoid this because their first WS action is slower-arriving, but the pattern is latent
   there too — a good audit target.
2. **Product polish (fixed this tick):** `joinVoice` *silently no-ops* when the WS isn't open yet — a real
   user clicking Join voice in the first second after load got nothing, no feedback. Fixed: the Join-voice
   button is now `disabled` until `connected` (with a "Connecting…" title + a `.link:disabled` style).
   Silent guards that eat a user action are a UX bug, not just a test race — worth a broader sweep for
   other controls that fire before the WS is ready.

**Highest-value next-tick item:** the biggest remaining voice coverage gap is that **`qa/voice.mjs` only
covers the happy 2-peer path** — add a **3rd peer** (proves the mesh is genuinely N-way, not just pairwise,
and exercises the simultaneous-join glare path that perfect-negotiation exists to handle) and a
mute-audibility assertion. Then start voice slice 3 (active-speaker / SFU) groundwork.

## 2026-06-14 (iter 58) — Voice signaling shipped; clearing context for the WebRTC client build

Shipped voice **slice 1** (WS signaling relay: `voice-join`/`voice-leave`/`voice-signal`, dumb relay,
16 KiB frames for SDP, two-client test) and recorded the owner's **audio north star** (thousands /
HD / free, via OSS SFU later) + the **per-component excellence** directive (GOAL + loop skill + memory).

**This tick: clearing context (judgment trigger).** Slice 2 — the mesh WebRTC **client** (getUserMedia,
RTCPeerConnection-per-peer, STUN, offer/answer/ICE, join/leave UI, audio playback) plus a fake-media
two-context E2E — is a major new subsystem, and this session is ~58 ticks deep. The disciplined call
(framework rule for sustained deep work; I flagged it last turn) is to build it in a **fresh context**
off the now-complete SPEC, not in a long degrading one. Durable handoff is all on disk:
- SPEC "Voice channels — MVP" has the full design (mesh, the 3 signal frames, STUN, **client peer
  discovery = answer incoming offers, existing-members-offer-to-joiner, ties by user id**, trickle ICE).
- GOAL voice item `[~]` with the north star; memory `project_opencord` voice section.
- Signaling relay is committed + green; client is the only remaining slice for a testable call.

**Next tick (fresh context):** build `web/src/voice.ts` (`useVoice` hook) + wire into `Chat.tsx`
(join/leave + roster + route WS voice events) + a Playwright test launched with
`--use-fake-device-for-media-stream --use-fake-ui-for-media-stream` asserting two contexts reach an
`RTCPeerConnection` `connected` state. Then iterate audio toward the north star (OSS SFU via
`stack-guardian`).

## 2026-06-14 (iter 57) — Markdown lists; the delimiter ambiguity that needed care

Added `- `/`* ` and `1. ` lists to the renderer. The interesting bit is the `*` ambiguity: `* item`
(bullet) vs `*italic*` (emphasis). Requiring a trailing space on the bullet rule (`/^[-*]\s+/`)
disambiguates them, and I probed both to confirm `*italic*` still renders as `<em>`, not a one-item
list.

Reflections:
- **When a new rule shares a delimiter with an existing one, prove the OLD behavior still holds.** The
  risk in adding `*`-bullets wasn't the new feature — it was silently breaking `*italic*`. The probe's
  `italic-not-a-bullet` case is the regression guard for that interaction; I wrote it before trusting
  the rule.
- **block vs inline is the right axis for this parser.** Lists/blockquotes are line-grouped in
  `renderBlocks`; bold/italic/code/links/mentions are span-level in `renderInline`. Keeping that split
  clean meant lists slotted in without touching any inline logic.
- **This likely closes the markdown arc.** bold/italic/strike/code/fence/blockquote/spoiler/lists/
  mentions/links is a complete-enough set; further markdown (tables, headers) would be diminishing
  returns. Next ticks should move to a *different* surface (e.g. moderation, presence, account) rather
  than keep enriching one renderer — watch for that drift.

## 2026-06-14 (iter 56) — Pins panel UI; reused the panel pattern, kept panels exclusive

Shipped the pins panel (header "pins" button → list of the channel's pins), completing pinned
messages end to end.

Reflections:
- **Reusing the search-results panel made this a tiny diff** — same container/markup, just a different
  data source (`fetchPins`) and header. The expensive part of a panel (layout/CSS/message rendering)
  was already built; the new feature is mostly state + a fetch. Recognizing "this is the search panel
  with different data" is what kept it small.
- **Three mutually-exclusive overlays need explicit exclusivity, or they fight.** members / search /
  pins all render over the message list. I made opening any one clear the others (and a channel switch
  clear pins), and ordered the render conditions by priority — so you never get two panels stacked or a
  stale one re-appearing when you close another. Enumerated the interactions rather than hoping.
- **Pinned messages is now a complete vertical feature** across 4 ticks (iter 53 backend → 54 UI → 55
  list endpoint → 56 panel), each slice independently shipped, tested, and (for UI) AI-vision-checked —
  a good template for medium features in a long session: small, verifiable increments over one big PR.

## 2026-06-14 (iter 55) — Pins list endpoint; correctness over the cheap in-memory filter

Added `GET /messages/pins` so a future pins panel sees ALL pins, not just those among the loaded
last-50 messages. Backend slice (panel UI next).

Reflections:
- **Resisted the cheap-but-wrong shortcut.** A pins panel could just filter the in-memory `messages`
  (`m.pinned`), which needs no backend — but it would silently miss any pin older than the loaded
  window. Spent the extra ~30 lines on a dedicated query/endpoint so the feature is *correct*, and
  logged the reasoning. "It mostly works for recent data" is a bug waiting to confuse someone.
- **New read endpoints are nearly free when you mirror the existing one.** `HandlePins` is `HandleRecent`
  with a different store call — same `ChannelIDFromQuery`, same `CanAccessChannel` gate, same shape —
  so the access-control story is identical and obviously-correct, and the test is a near-copy.
- **Still backend-first** at iter 55 to keep long-context risk low; the panel UI (reusing the existing
  search-results-panel pattern) is the next clean tick.

## 2026-06-14 (iter 54) — Pin UI; mirrored the server authz rule in the client gate

Completed pinned messages: pin/unpin hover action, 📌 badge, live `message-pinned` WS handler.
Two-tick slice done (backend iter 53 → UI iter 54).

Reflections:
- **The client's "can I do this?" gate should mirror the server's authz, not guess.** `canPin =
  !activeServerChannel || canModerate` is the exact client-side reflection of the store rule (admins
  in server channels, any member elsewhere). The server still enforces it (the UI gate is just UX), but
  matching them means the button only shows when the action will actually succeed — no misleading
  controls that 403.
- **Lean on the existing live-update path instead of optimistic state.** Pin calls the API and lets the
  `message-pinned` broadcast update the flag for everyone — same pattern as delete. One source of truth
  (the broadcast), and the sender sees the same update as everyone else, so no optimistic/rollback code.
- **The pin badge sits outside the (group-collapsed) message head** so a pinned message still shows it
  even when grouped under the same author — a small correctness check the screenshot confirmed.

## 2026-06-14 (iter 53) — Pinned messages (backend); reused the moderation authz model

Shipped pin/unpin backend: `messages.pinned`, `SetMessagePinned`, `PUT/DELETE /messages/{id}/pin`
with a broadcast, flag surfaced in `Recent`. Backend-first (UI next), matching the channel-topics
two-tick pattern.

Reflections:
- **A new action should reuse the project's *existing* authorization shape, not invent a new one.**
  Pinning's gate mirrors moderation-delete: resolve the message's `server_id`, require server-admin
  for server channels, fall back to channel-access for serverless ones. Same query, same error
  sentinels (`ErrMessageNotFound`/`ErrForbidden`), same 404-no-leak behavior. Consistency = fewer
  surprises for clients and a smaller test surface.
- **Backend-first keeps long-context risk low and the slice independently verifiable** — schema +
  tested endpoint now; the UI (pin action + pinned panel/badge, handle the `message-pinned` WS event)
  is a clean separate tick. The `message-pinned` event ships now but is harmlessly ignored until the
  client handles it.
- **Round-trip the new flag through a real read path in the test** — I asserted pinned=true/false via
  `store.Recent` (the same query the app serves history with), not just the UPDATE's return, so the
  new SELECT column is proven wired, not assumed.

## 2026-06-14 (iter 52) — Autolink URLs; security-by-construction over sanitization

Clickable `http(s)` links in messages (a chat table-stakes gap — pasted URLs were dead text). One
new inline rule; XSS-safe because only `https?://` is matched (href can't be `javascript:`/`data:`)
and React escapes the attribute — plus `rel="noopener noreferrer"` for tab-nabbing.

Reflections:
- **The allowlist regex *is* the security control — no separate sanitizer needed.** Rather than match
  any `scheme:` and then filter dangerous schemes (a blocklist that rots), the rule only ever matches
  `https?://`, so an unsafe href is unrepresentable. Probed the `javascript:`/`data:` cases explicitly
  to prove they fall through to plain text. Prefer "can't express the unsafe thing" to "remember to
  strip it."
- **Edge cases are where autolink earns trust:** trailing `).` punctuation, underscores inside the URL
  (must not become italics), and URLs inside inline code (must stay literal). The earliest-match parser
  handled the last two for free; the punctuation trim was the one deliberate touch. Probed all three.
- **Verification cost stayed ~zero:** folded the URL into the existing markdown QA message (no extra WS
  send → no rate-limit pacing) and reused the react-dom/server probe. The ladder keeps paying off.

## 2026-06-14 (iter 51) — Channel-topic UI; completed the two-tick vertical slice

Shipped the frontend half from last tick: the header shows a server channel's topic and admins
get an "edit topic" control. Channel topics is now a complete, E2E-verified feature.

Reflections:
- **Backend-first, then UI in the next tick, is a clean way to size risk in a long session.** The
  backend slice (iter 50) was pure data + a tested endpoint; this tick was a contained UI change
  against an already-proven API. Each half was small and independently verifiable — better than one
  big cross-layer change deep in a long context.
- **Reused the project's own UI patterns instead of inventing.** The "edit topic" admin control mirrors
  the existing read-only toggle (same `canModerate` gate, same optimistic `setServerChannels` update,
  same `window.prompt` affordance as channel creation). Matching local conventions kept the diff tiny
  and the QA wiring trivial (the harness already drives prompts via `promptAnswer`).
- **The QA harness's existing seams made the new test almost free:** set `promptAnswer`, click, assert
  `.channel-topic`. Investments in test infrastructure compound — the Nth UI feature costs almost no
  incremental QA effort.

## 2026-06-14 (iter 50) — Channel topics (backend slice); a schema change done safely

Shipped a real parity item's backend: server channels get a `topic` (admin-set, ≤1024 chars,
returned in the API). New schema column, `SetChannelTopic`, and `PATCH /channels/{id}` extended
to take optional `postPolicy` and/or `topic`. UI follows next tick (backend-first pattern).

Reflections:
- **Extend an endpoint without breaking its existing clients: make new fields optional pointers.**
  PATCH used to require `postPolicy`; switching both fields to `*string` means each is applied only
  when present, so the read-only-channel UI's `{postPolicy}` requests still work unchanged. I added a
  backward-compat test asserting exactly that — the cheapest insurance against a silent break.
- **A schema change is safe here because two earlier invariants hold:** the ALTER is idempotent
  (`ADD COLUMN IF NOT EXISTS … DEFAULT ''`) and `Migrate` is serialized by the advisory lock (iter 40),
  so the new column adds cleanly even under concurrent boots/test packages. Past hardening paid off.
- **Verified on a *fresh* DB** (`docker compose down -v`) so the new column actually exercises the
  first-creation path CI will hit — the iter-40 lesson, applied by habit now.
- **Sized to the context:** still backend-only (no frontend) on a long session; the visible header UI
  is a clean separate tick.

## 2026-06-14 (iter 49) — @everyone/@here highlighting; a feature tick (not more tests)

After several test/QA ticks, deliberately advanced a user-visible parity item instead of adding
the Nth test: `@everyone`/`@here` now render as highlighted all-mention chips (they ping you too),
with a `mention-all` DOM hook. Pure renderer + React elements (no new CSS, XSS-safe). GOAL Mentions
TODO shrank.

Reflections:
- **Watch for safe-busywork drift.** I'd done many low-risk test ticks; another would have been
  borderline churn (Rule 10). The honest move when the product is green is to *advance* it — a small,
  fully-verifiable feature — not manufacture coverage to look busy. Picked a bounded renderer change
  with the same verification rigor (react-dom/server probe → browser QA → AI-vision).
- **Reuse the verification ladder you built.** Probe the pure unit (1s) → fold the assertion into an
  existing QA message (no extra WS send, dodges the rate limiter) → eyeball one screenshot. Each new
  renderer feature now costs almost no incremental QA effort because the rungs are in place.
- **Keep `[~]` honest:** this is rendering only — no delivery/notifications — so Mentions stays `[~]`
  with the remaining work spelled out, not optimistically closed.

## 2026-06-14 (iter 48) — Auth input-hardening audit: confirmed bounded, then guarded it

Rule-15 audit of the auth input surface: register/login already bound the body
(`MaxBytesReader` 64 KiB) and validate username/password — so no vuln, but the handler
rejection paths had no test (only the regex was unit-tested). Added DB-free guards: malformed
/non-JSON bodies, bad username, short password, and a >64 KiB body all → 400 without a DB call.

Reflections:
- **An audit that finds the code is *already* safe still earns a regression test.** "It's
  bounded" was true but unguarded — a future refactor of `decodeCreds` could drop the
  `MaxBytesReader` and nothing would fail. Pin the safety property, not just fix the unsafe ones.
- **Test at the cheapest layer that still proves the property.** These run with a nil DB pool
  because every rejection happens before the DB call — so they're fast, always-on in CI, and
  don't need the Postgres service. Find the layer where the invariant lives and test there.
- **Context discipline:** 10th tick in one session — kept it backend-only + DB-free again
  (lowest blast radius). Strongly recommending a `/clear` before the next feature-sized tick;
  all loop state is durable (GOAL/IMPROVEMENTS/heartbeat/wakeup/memory) so it resumes clean.

## 2026-06-14 (iter 47) — Deployed to Railway; then guarded the deploy-critical routing

Out-of-band this session: shipped a single-binary prod build (Go serves the embedded SPA) and
**deployed Opencord to Railway** — live at opencord-production-1b00.up.railway.app, verified
in-cloud (register/login/SPA/asset/401 + a browser screenshot of the login screen). This tick
added the missing tests for that new code.

Reflections:
- **Ship the guard for what you just shipped — especially deploy-path code.** The `/*` SPA
  catch-all and the embed handler went out with the deploy but had no tests; a refactor could
  silently make `/api/bogus` serve index.html (masking real 404s) or break the SPA. Added a
  webui unit test + an httpapi guard asserting the catch-all serves client routes but never
  shadows the API. New code that reaches production gets a test in the very next tick.
- **A good test taught me real `net/http` behavior:** `http.FileServer` 301-canonicalizes
  `/index.html`→`/`. My first assertion was wrong (expected 200); the failure was the test
  being naive, not the handler. Fixed the test, kept the meaningful cases. Tests that fail on
  framework semantics are still teaching you something — read the failure, don't paper over it.
- **Deploy gotchas logged in memory, not just here:** Railway's managed-DB plugin returns
  "Unauthorized" (CLI token lacks DB-create scope; `railway login` can't run in the session's
  non-interactive `!` shell). Worked around with a Postgres Docker-image service. CAVEAT: no
  volume yet → data is ephemeral. (See [[project_opencord]].)
- **Next:** add a persistent volume to the cloud DB (offered to owner); a CI `docker build` of
  the root Dockerfile would guard the image build itself (the one deploy-path step CI doesn't
  cover — go+web are built separately but not the combined image).

## 2026-06-14 (iter 46) — Tested the rate limiter (the abuse defense I only knew worked by accident)

Added `TestServeWSRateLimitIntegration`: one connection floods 30 messages; the test asserts the
per-connection token bucket throttles most (saved < 30) while allowing the burst (observed 5/30,
matching `rateBurst`). Backend-only tick (deliberately low-risk given a long session).

Reflections:
- **A security feature you only confirmed by accident has no guard — write it.** I learned the rate
  limiter existed in iter 44 because it silently dropped the QA bot's sends. "It clearly works" was
  true but untested; one refactor of the bucket math could have weakened it invisibly. Convert every
  *observed* behavior into an *asserted* one. The flood test now pins burst+throttle behavior.
- **Make adversarial assertions timing-robust.** The exact pass-count depends on send speed vs refill,
  so the test asserts a wide band (`>=3 && <30`), not `==5` — it proves the invariant (throttling
  happens, burst survives) without being flaky on a slow CI runner.
- **Match the tick's risk to the context's age.** Eight ticks deep in one session, I chose a
  self-contained backend test over a cross-cutting frontend change — less blast radius if my context
  is fraying. (Offered the user a `/clear`; they kept going, so I kept the work conservative.)

## 2026-06-14 (iter 45) — @mention rendering; applied last tick's lesson proactively

Shipped the first @mention slice: `@user` chips with the viewer's own mention highlighted
distinctly (amber `mention-me` vs accent). Client-only, XSS-safe, GOAL Mentions now `[~]`.

Reflections:
- **Applied iter-44's "isolate the unit" lesson *before* the slow path, not after.** Rather than
  build → run the ~2-min full browser QA → debug, I first proved the renderer via react-dom/server
  (`@alice`→mention-me, `@bob`→mention, `foo@bar`→`@bar` quirk) in ~1s, *then* ran the E2E once with
  confidence. A debugging lesson is only banked when it changes the *next* tick's order of operations.
- **Paced the new send by default.** Knowing the rate-limiter trap (iter 44), the mention step waits
  for the bucket before sending — no flaky discovery this time. The meta-QA gap noted last tick (steps
  accumulate WS frames) is now handled per-step; a future cleanup could factor a `sendPaced()` helper.
- **Logged the scope honestly:** rendering only (no resolution/notifications/autocomplete) and the
  `foo@bar` email quirk are in SPEC + the commit, so the `[~]` is truthful, not optimistic.

## 2026-06-14 (iter 44) — Markdown blockquote + spoiler; isolate the unit when E2E fails

Completed the Markdown subset: `> ` blockquotes (block-level line grouping) and `||spoiler||`
(stateful click-to-reveal component), still React-elements-only (XSS-safe). GOAL's Markdown item
is now `[x]`. But the real lesson was the debugging.

Reflections:
- **When an E2E test fails, isolate the unit before touching it.** The browser QA timed out waiting
  for a blockquote, which *looked* like a renderer bug. Instead of poking the renderer blind, I
  transpiled `markdown.tsx` with esbuild and rendered it through `react-dom/server`:
  `"> q\n||s||"` → `<blockquote>q</blockquote><span class="spoiler">s</span>`. The renderer was
  **provably correct**, which redirected the hunt to the test/runtime — saving a wrong "fix" to good
  code. **Keep this technique: prove the pure unit in isolation to bisect unit-vs-integration.**
- **The actual cause was the product working as designed: the rate limiter.** Diagnostics (compose
  value correct, draft cleared on Enter, yet no message) showed the WS frame was *sent* but *dropped*
  server-side — the per-connection token bucket (`rateBurst=5`, `+2/s`, every message AND typing
  frame costs a token) had emptied under the bot's rapid sends. An automated client sends far faster
  than a human and trips real abuse limits. **Fix the test (pace it), not the limit.**
- **Layered diagnostics beat guessing:** body-HTML dump → "message never sent"; compose-value
  before/after Enter → "sent but dropped"; each step removed half the search space. Add the
  diagnostic, don't theorize.
- **Meta-QA gap to watch:** the browser QA accumulates WS frames across steps; future message-send
  steps should pace by default (or the suite should reset the connection) so they don't randomly trip
  the limiter as more steps are added.

## 2026-06-14 (iter 43) — Multi-line composer; "check the input path can type the syntax"

Set out to finish Markdown (blockquote/spoiler) and discovered the composer was a single-line
`<input>` — so blockquotes and multi-line code blocks were **untypeable**, and there were no
multi-line messages at all. Fixed the foundation first: the composer is now an auto-growing
`<textarea>` with Enter=send / Shift+Enter=newline (the Discord convention). Verified E2E
(browser QA types two lines via Shift+Enter, asserts they land in one message and that the first
line wasn't sent alone) + AI-vision of `03d-multiline.png`.

Reflections:
- **A renderer for syntax users can't enter is dead code — check the INPUT path before adding
  output features.** Last tick's "next" list had blockquote/spoiler; building them onto a
  single-line input would have shipped Markdown nobody could trigger. The fix was the composer,
  not more parse rules. **Rule: verify the whole user path (type → send → render), not just the
  render half, before extending a feature.**
- **Foundational UX unblocks several backlog items at once:** multi-line input now enables
  blockquote, multi-line code blocks, and just plain paragraph messages — one change, several
  features unblocked. Prefer the enabling fix over a cosmetic one.
- **Next (now genuinely unblocked):** Markdown blockquote (`> `) + spoiler (`||x||`, click to
  reveal); both are typeable now and worth a focused tick.

## 2026-06-14 (iter 42) — Closed the iter-39 edit/reaction HTTP gap; caught a self-made phantom gap

Two things this tick. (1) Closed a *real* logged gap: the message **edit** and **reaction** REST
endpoints had no HTTP-layer test despite carrying authz — added cases to `router_integration_test.go`
(edit author-only → 404 for others; empty body 400; reactions channel-access gated → non-member 403;
invalid emoji 400; add/remove 200). (2) The item I'd queued as "next" — *add a web CI job* — turned
out to be **already done**: `ci.yml` has gated `web (typecheck · build)` since the first CI commit.

Reflections:
- **A "gap" found by a partial grep is not a gap — verify against the whole source before logging
  it.** Last tick I grepped `ci.yml` for `DATABASE_URL|go test|postgres`, saw only the server job,
  and declared the web ungated. Reading the full file this tick showed a `web` job that passes every
  run. **Rule: confirm a missing-thing claim by reading the file / `git show HEAD:<file>` and the
  live CI job list, not by a keyword search that can miss a sibling block.** Cheaper than a tick spent
  "fixing" what exists. (Corrected the false note in the iter-41 entry above.)
- **Honesty over tidiness:** struck the wrong claim in place rather than deleting it, so the loop's
  record shows what it learned, not a clean rewrite.
- **Next real gaps (verified by reading, not grepping):** message edit/reaction over **WS** (not just
  REST); finishing Markdown (blockquote/spoiler, still `[~]` in GOAL); a new parity feature
  (mentions / pinned messages) for a Track-2 tick.



After two backend test-coverage ticks, advanced the *product*: a Discord-like Markdown
subset in messages (**bold**, *italic*, ~~strike~~, `inline`/```fenced``` code). Built it
security-first — `renderMarkdown` returns **React elements, never an HTML string**, so it's
XSS-safe by construction (raw `<script>` renders as literal text). Ran the **full browser QA
stack** for the first time this session and reviewed `03c-markdown.png` by eye: bold/code
render, the `<script>` is escaped. browser + realtime QA both green.

Reflections:
- **CI does not build/typecheck the web client — a real coverage gap.** ~~`ci.yml` only runs
  the Go job…~~ **[CORRECTED iter 42 — this was WRONG.]** `ci.yml` has had a `web (typecheck ·
  build)` job (`npm ci && npm run build`) since the first CI commit (`aca1f80`), and it passes
  every run. I concluded "no web job" from a partial `grep` that only matched the server job's
  lines instead of reading the whole file. The frontend IS gated. See iter 42's lesson.
- **Rendering user content is an attack surface — pick a structurally-safe design, not a
  filter.** Returning React elements (no `dangerouslySetInnerHTML`) means there's no HTML sink
  to sanitize and no blocklist to keep current; the safety is in the shape of the code. Prefer
  that over "escape then inject" every time.
- **Grow the QA with the feature (Step 5b):** added a markdown+XSS assertion to `browser.mjs`
  in the same commit, so the guard ships with the feature, not later.
- **Balance the tracks:** three straight QA ticks would drift from the North Star (Discord
  parity); a feature tick that *still* ships its own QA + vision check keeps both moving.

## 2026-06-14 (iter 40) — WS access-control suite + a CI race adding tests exposed

Shipped the WebSocket gateway's first access-control test (`internal/ws/serve_integration_test.go`):
the realtime path is the most attacker-exposed surface (Rule B), and `ServeWS` rejects
401/403/400 *before* the upgrade — now proven, including a **real gorilla/websocket
handshake** (member connects via the `?token=` browser path and receives history; a
non-member is refused 403, no leak before upgrade).

But adding integration tests to a *third* package turned CI red — and the lesson is the
bigger deliverable:
- **A new test package can change the concurrency profile of `go test ./...`.** Go runs
  package binaries in parallel, so chat + httpapi + ws all called `db.Migrate` at once on
  CI's fresh DB. `CREATE … IF NOT EXISTS` is **not** atomic against simultaneous creation
  → catalog duplicate-key (SQLSTATE 23505 on pg_class/pg_type). Fixed at the root with a
  `pg_advisory_lock` in Migrate (+ `TestMigrateConcurrent` regression guard).
- **A green local `go test ./...` ≠ green CI when the local DB is pre-migrated.** The race
  only fires on *first* concurrent creation; my warm dev DB already had the schema, so it
  hid the bug. **Process fix (apply every DB tick): verify on a FRESH DB** — `docker
  compose down -v` before the run — to match CI, not the warm dev DB.
- **Rule 15 done right even when the first repro misses:** a single `go test ./...` didn't
  reproduce the timing race, so I forced it deterministically (8 goroutines from a barrier,
  fresh schema each run) — 3/3 FAIL without the lock, 3/3 PASS with it, then full suite +
  CI green. Don't conclude "can't reproduce" from one non-overlapping run.

## 2026-06-14 (iter 39) — HTTP-layer authz coverage: testing router.go, not just the store

`internal/httpapi/router.go` (the entire REST surface) had **zero** test files while
`auth`/`chat`/`ws` all had suites. The store layer's authorization was well covered by
`chat/store_integration_test.go`, but the *HTTP wiring* — JWT middleware, the inline
`IsServerMember`/`IsServerAdmin` gates in `mountServerRoutes`, and the `error → HTTP
status` mapping — was untested. A bug there (wrong code, a missing gate) is a security
hole the store tests can't see. Added `router_integration_test.go`: a `DATABASE_URL`-gated
suite that drives the *real* chi router via `httptest` and walks an attacker through every
boundary (401 unauth · 403 non-member/non-admin · 404 no-existence-leak on moderation ·
400 bad role/policy/id · 2xx for the legitimate owner/admin). Verified against a local
postgres (7/7 subtests) and runs in CI's postgres service.

Reflections:
- **"Tested" must name the layer.** The store being green hid that the HTTP layer mapping
  it to status codes had no guard at all. Coverage gaps live at seams between packages —
  the next audit target is the **WS layer's** REST-adjacent access checks (`ServeWS`
  channel-access gating) tested through a real upgrade, not just `ws` unit tests.
- **Reuse the established pattern, don't reinvent.** Mirrored the store suite's
  skip-without-`DATABASE_URL` convention so `go test ./...` stays green locally and the
  test is a real CI guard — not a second bespoke harness.
- **Next QA growth:** extend this suite to the message edit/reaction endpoints (PUT/DELETE
  reactions, PATCH body) and assert the read-only `Save`→403 path through HTTP once a
  message-send REST route exists (today messages are sent over WS).

## 2026-06-14 (iter 38) — Read-only channel UI: finishing the last feature's front end

Completed the read-only-channel feature shipped backend-only last tick. The client now
gets each channel's `postPolicy` (added to the Channel JSON), so: an admin sees a
"make read-only / allow everyone" header toggle + a 🔒 badge; a non-admin in a read-only
channel gets a disabled composer ("read-only — only admins can post"); and the WS `error`
frame is finally surfaced (alert). Single-client QA toggles it and grades the badge by eye.

Reflections:
- **A backend permission needs its state on the resource for the UI to reflect it.** Just
  like roles needed `Server.role`, read-only needed `Channel.postPolicy` on the channel
  list — the UI can't disable a composer for a policy it can't see. Pattern across this
  whole arc: enforce server-side, then expose the deciding field on the thing the client
  already fetches.
- **Known limitation, logged not hidden:** toggling read-only doesn't live-propagate to
  other connected clients (no `channel-updated` broadcast) — they see it on next load. The
  enforcement is still correct (server-side); only the *composer-disable hint* lags. A
  `channel-updated` WS event is the fix when it matters; noted, not silently shipped.
- **UI-hint vs server-gate, again:** the composer disable + Send-disable are affordances;
  `Save`'s `ErrForbidden` (iter 37) is the real gate. Three features now follow this split
  (moderation, read-only, post-policy) — it's the house pattern for permissions here.
- **Roadmap is now 100% built AND fully wired through the UI.** Every parity item from
  GOAL.md is shipped, verified, and usable in-app. Next work is genuinely net-new (threads,
  attachments, voice, read state) — bigger, fresh-context features, not roadmap cleanup.

## 2026-06-14 (iter 37) — Read-only channels: per-channel posting policy (last roadmap slice)

The final roadmap item: `channels.post_policy` ('everyone'|'admins') — an admins-only
channel is a read-only announcement channel. Gated the **WS send path** (the new attack
surface) by folding the check into `store.Save` (every caller covered, defense in depth),
returning `ErrForbidden`, which `readPump` turns into an `error` frame to the sender.
`PATCH /api/channels/{id}` sets the policy (server admins only). Backend-only; the
set-read-only UI is the follow-up.

Reflections:
- **Gate the write at the store, not the handler, when there are multiple entry points or a
  hot path.** Messages only flow through one WS handler today, but putting the policy check
  in `Save` means a future REST-post, an import tool, or a bot can't bypass it. The cost is
  one indexed lookup per message — acceptable, and correctness on a permission check beats a
  micro-optimization.
- **A dropped WS frame needs an out-of-band "no" or it reads as a bug.** The send path used
  to `continue` on any Save error (rate limit, etc.) — fine for those, but a *forbidden*
  post silently vanishing looks broken. Mapped `ErrForbidden` → an `error` frame so the
  sender learns why. (The client doesn't surface it yet — that's the UI slice, and no user
  can reach a read-only channel until the set-policy UI ships anyway.)
- **Verifying a WS *rejection* needs a listening client, not just a status code.** The live
  check connected a member socket, sent, and asserted both the `error` frame AND that the
  message never persisted (GET /messages) — a 2xx/4xx wouldn't exist for a WS frame.
- **The roadmap is essentially complete.** v0.2 + v0.3 are built and verified end-to-end.
  Remaining are UI polish (set-read-only control, error toasts) and net-new features beyond
  the original parity list (threads, attachments, voice) — all good fresh-context work.

## 2026-06-14 (iter 36) — Moderation UI: the admin delete button (role-aware client)

Surfaced last tick's moderation backend in the UI. The client now needs to know its role
per server, so `ListServers` (and CreateServer/RedeemInvite) carry the requesting user's
`role`; the chat computes `canModerate` for the active server channel and shows the delete
button on *others'* messages when I'm owner/admin (edit stays author-only). Verified E2E in
the two-client realtime QA: B posts → A (owner) clicks delete → B sees `[deleted]` live.

Reflections:
- **A backend permission only becomes a feature once the client can ask "what am I here?"**
  Moderation was enforced server-side last tick, but invisible until the client knew its
  role. Cheapest answer: fold the viewer's role into the resource it already fetches
  (`/servers`), rather than a separate "my permissions" call per channel.
- **Authorize on the server, *hint* on the client.** The delete button is shown via
  `canModerate`, but the actual permission is still enforced in `DeleteMessage` (iter 35).
  The UI flag is a convenience/affordance, never the gate — a hand-crafted DELETE still
  hits the server check. Keep that split explicit so a UI bug can't become a security bug.
- **No new pixels, no new screenshot — but say so.** This tick added a *conditional* on an
  existing, already-vision-graded button (delete) and reused the verified `[deleted]`
  render. The assertion (B sees `[deleted]` live) covers behaviour; I logged that there's no
  new visual surface rather than silently skipping the vision step.
- **Roles & permissions is now feature-complete bar per-channel overrides** — the last
  remaining slice is channel-level permission overrides (e.g. a read-only announcement
  channel), which is a bigger model change and a good fresh-context candidate.

## 2026-06-14 (iter 35) — Message moderation: server admins can delete others' messages

`DeleteMessage` now allows the author OR an admin of the message's channel's server (the
moderation core). Backend-only this tick — the admin delete *button* is the next UI slice.
Full Rule-15 cycle live: a plain member deleting another's server message → 404; the owner
(and a promoted admin) → 204; public/serverless channels are unaffected (non-author → 404,
the original behaviour).

Reflections:
- **Widening a permission means re-checking the rows that relied on it being narrow.** The
  old delete was a one-line `UPDATE ... WHERE id AND user_id`. Loosening it (author OR
  admin) required a SELECT-then-authorize-then-UPDATE — and the existing
  `TestDeleteMessageIntegration` (non-author → not-found) had to keep passing. It did,
  because for a *public* channel there's no server admin path; I verified that explicitly
  rather than assuming the refactor preserved it.
- **Return the same error for "not allowed" as for "not found" on a delete** — a member
  probing message ids shouldn't learn which exist in a channel they can moderate-but-not.
  Kept `ErrMessageNotFound` for the unauthorized path (→ 404) rather than a distinct 403,
  matching the existing non-leak behaviour.
- **Edit deliberately left author-only.** Moderation is about *removing* abuse, not
  rewriting someone's words — admins can delete but not edit others' messages. Scoping a
  permission tightly is itself a security decision.
- **Next:** the moderation UI (show the delete button on others' messages when I'm an admin
  of the active channel's server — needs the client to know its role per server, via a
  `role` on the servers list), then per-channel permission overrides.

## 2026-06-14 (iter 34) — Roles UI: members panel + the owner's promote control

Made last tick's roles backend usable in-app: `GET /servers/{id}/members` + a members
panel (avatar · name · role badge), with an owner-only make-admin/demote toggle per
non-owner member. Verified two ways: single-client browser QA (owner sees the OWNER
badge) and a **two-client** realtime test — A (owner) opens the panel and promotes B
(member) → B's badge flips to ADMIN live (`rt-07-roles.png`).

Reflections:
- **The panel pattern paid off a third time.** Search, then... actually the members panel
  reuses the exact "replace the message area + ✕ close" structure as search — same
  `.search-results` shell. Three overlays (search, members) now share it; a shared
  `<Panel title onClose>` component is the obvious next refactor if a fourth appears.
- **CI-coverage gap caught:** the members list is exercised by the browser/realtime QA,
  which DON'T run in CI (they need a live browser). So `ListServerMembers` would have had
  zero CI coverage — extended `TestServerRolesIntegration` to assert it (owner-first
  ordering + the promoted admin). Rule: anything only the Playwright QA touches still needs
  a Go integration test, or CI is blind to it.
- **Roles is now usable end-to-end** (create server → invite → join → owner promotes in the
  UI → admin can create channels). Remaining roles slices: message moderation (admins delete
  others' messages — needs the client to know its role in the active channel's server) and
  per-channel permission overrides.

## 2026-06-14 (iter 33) — Server roles, first slice (the big v0.3 feature, bounded)

Began roles & permissions with a deliberately small, Discord-shaped slice instead of the
whole thing: `server_members.role` (owner|admin|member), channel creation gated to admin+
(members can't — the Discord default), and owner-only promote/demote
(`POST /servers/{id}/roles`). Backend only; per-channel overrides, message moderation, and
a UI are later slices. Full Rule-15 cycle live: member create-channel → 403 until the owner
promotes them → admin → 201; member self-promote → 403; invalid role → 400.

Reflections:
- **A huge feature is a sequence of small verified slices, not one commit.** "Roles &
  permissions" sounds like a monolith; the first useful, shippable unit is one role enum +
  one gated action + one management endpoint. Each slice ships green and de-risks the next —
  better than a giant branch, especially deep in a long session.
- **Tightening an existing gate is a regression risk — check who relied on the old rule.**
  Channel-create went member→admin. The QA flows create channels only as the server *owner*
  (an admin), so they stayed green — but I verified that explicitly rather than assuming.
  When you narrow a permission, grep every caller/test that exercised the looser one.
- **Roles QA is a two-actor story** — the next slice's QA should be: owner promotes B in the
  UI, B (now admin) can create a channel; before promotion B has no "+ channel". That needs
  the roles UI first, so it's coupled to the UI slice.
- **Next:** roles UI (show role, owner's promote control) + message moderation (admins delete
  others' messages); then per-channel permission overrides.

## 2026-06-14 (iter 32) — In-channel search, with LIKE-wildcard hardening

Added message search within a channel: `GET /api/messages/search?channel&q`, a header
search box, and a results panel (count + matches, ✕ clear). Access-gated like every read
path. Verified at three layers: store test, browser-QA (find + clear) + vision, and a live
HTTP adversarial run (member 200, empty q 400, non-member 403).

Reflections:
- **A LIKE/ILIKE search is an injection surface even when parameterized.** The query is a
  bound parameter (no SQL injection), but `%`/`_` are LIKE *wildcards* — a user typing `%`
  would otherwise match-all. Escaped them (`\ % _` → `\\ \% \_`) so the term matches
  literally; encoded the exact case as a test (`%` → only the literal-`%` message) and a
  live check (`%25` → 0 matches). Rule B isn't just "bound params"; it's "what does each
  metacharacter mean in the sink?"
- **Self-inflicted process slip (caught, no harm):** the standalone live-search probe failed
  ECONNREFUSED because the *previous* qa/run.sh had torn the DB down and I didn't re-boot it.
  Reminder for ad-hoc live checks: they don't inherit the harness's stack — boot db + server
  + assert /healthz first (the same fail-loud lesson from the iter-25 harness fix, applied to
  one-off probes).
- **Next:** roles & permissions (the big v0.3 item — spec-first, fresh context); later,
  channel-spanning search (search all channels you can access, grouped by channel).

## 2026-06-14 (iter 31) — Two-client server flow: invite → redeem → live chat (the deferred QA)

Closed the two-client server QA I'd logged pending for three ticks. realtime.mjs now,
after the DM flow: A creates a server + channel, mints an invite (code captured from the
shown prompt), posts a message; B redeems the code, auto-lands in the server channel,
reads A's history, and then receives A's *next* message live (`rt-06-bob-server.png`).
This is the realtime, through-the-UI proof of the whole servers+invites stack — the
backend/HTTP checks couldn't show the cross-user live path. Pure QA (only realtime.mjs)
— no production risk.

Reflections:
- **A flow that prompts more than once per page needs per-page mutable answers.** The DM
  step had hard-wired `a.on('dialog', d => d.accept(userB))`, which would have answered
  the server-name prompt with a username. Refactored to an `ans = {a, b}` holder set
  before each action, plus `aDefault` to capture the invite code the prompt pre-fills.
  Same pattern as browser.mjs — now both harnesses share it.
- **Redeem auto-navigates, so the QA didn't need to click the channel** — `joinServerPrompt`
  selects the server's first channel after redeem, so B just lands there. Testing the
  real handler's side effects (not re-implementing navigation in the test) kept it short.
- **Coverage milestone:** every members-only surface (DM, server channel) now has BOTH a
  backend adversarial "outsider is blocked" test AND a two-client "member gets in and
  chats live" UI test. That pairing is the template for the next private feature.
- **Next:** roles & permissions (the big v0.3 item — spec-first, backend then UI) and search.

## 2026-06-13 (iter 30) — Server invites: a feature that closes a gap I shipped

The first servers slice (iter 27) shipped an **open** `POST /servers/{id}/join` — anyone
could join any server by guessing its sequential id, defeating the members-only access
control. Replaced it with **invite codes**: a member mints an unguessable 8-char code
(crypto/rand), redeeming it admits you; the open join endpoint is gone. Full Rule-15
cycle, all live-verified: old join → 404 (gap closed), non-member read → 403, non-member
mint → 403, bogus code → 404, real code → 200 then read → 200. Plus a store integration
test and a browser-QA invite-button check.

Reflections:
- **A feature can be the fix for a gap you shipped.** The "members-only" servers weren't
  actually private until joining was gated. When you add access control, audit *every*
  way in — I gated reads/WS but left an open join door for three ticks. New private
  resources need a "how does someone get IN, and is THAT gated?" check, not just "can an
  outsider read it?".
- **QA pattern — capturing a prompt's value:** the invite code is shown via
  `window.prompt(msg, code)`. To assert it in browser QA, the dialog handler records
  `d.defaultValue()` into a node var, and the test polls it (the prompt fires after an
  async round-trip, so a poll, not a bare check). Reusable for any "we showed the user a
  generated value" flow.
- **Still pending (now 3 ticks):** the two-client server flow (A invites → B redeems →
  B chats live) — the realtime analogue of today's single-client + HTTP checks.

## 2026-06-13 (iter 29) — Mobile-responsive layout; a transition-vs-screenshot QA lesson

First v0.3 item: at ≤640px the sidebar collapses into an off-canvas drawer behind a
header `☰` toggle, the chat goes full-width, a backdrop dims it, and selecting a channel
closes it (`selectChannel` helper). Pure CSS media query + a little drawer state — no
data/access changes. Verified E2E (toggle opens/closes the drawer) + by eye (`08-mobile-
closed` full-width chat; `08-mobile-open` drawer over a dimmed chat).

**QA lesson — screenshots can catch an animation mid-flight.** The first mobile shots
looked broken: "closed" showed the sidebar half-on, "open" showed it half-off. Not a CSS
bug — the 0.2s slide `transition` was still animating when the screenshot fired (the QA
resizes desktop→mobile mid-session, which animates the transform; a real mobile load
renders hidden from the start, no flash). Fix: `waitForTimeout(350)` to let the transition
settle before the shot. Lesson: **when vision-checking anything with a CSS transition,
wait for it to settle first**, or the artifact lies. Don't "fix" a transition artifact by
changing correct CSS.

Reflections:
- **The AI-vision pass earned its keep again** — the assertions were green (the drawer
  *did* open/close), but only the screenshot revealed the mid-transition capture. A
  text/DOM-only QA would have shipped misleading artifacts.
- **Close-on-select via one helper:** routing all three sidebar lists' clicks through
  `selectChannel(id)` (set channel + close drawer) kept the mobile behaviour in one place
  instead of sprinkling `setSidebarOpen(false)` across handlers.
- **Next:** the two-client server flow (still pending from iter 28) and a mobile
  vision-check of the *open DM/serverlist* drawer states, not just channels.

## 2026-06-13 (iter 28) — Servers UI: a sidebar accordion over the existing chat machinery

Shipped the servers UI: a "Servers" sidebar section listing each server (name + id badge)
with its channels indented (accordion), plus create-server / join-by-id / create-channel.
Selecting a server channel reuses the existing chat machinery unchanged — the header,
composer, history, send, reactions, and grouping all "just work" because a server channel
is still a channel. Verified single-client E2E (create server → add channel → post) and
by eye (`07-server.png`: three clean sidebar sections, accordion, active server channel).

Reflections:
- **A multi-source dialog QA needs answer routing.** browser.mjs had one fixed prompt
  answer; the server flow has two prompts (server name, channel name) with the *same*
  message text as the existing channel prompt, so message-text routing wouldn't work. Made
  the dialog handler read a mutable `promptAnswer` set before each action — the clean
  pattern for a flow that prompts more than once. Reuse it for future multi-prompt flows.
- **"It's still a channel" kept the UI tiny.** Because DMs and server channels are both
  just channels with access control, three different sidebar sources (global, DM, server)
  all feed the *same* `channelId` → one chat view. Resolving the active channel's display
  name across all three sources (`current ?? activeServerChannel`, plus `activeDM`) was the
  only header/composer change. Modeling the access layer in the backend paid off in the UI.
- **Next QA growth:** a two-client server flow — A creates a server, shares its id, B joins
  and sees the channel + a live message (the realtime analogue of the single-client test);
  and a non-member-can't-see-it isolation check through the UI.
- **Polish (P1, logged):** the Servers section is dense at the bottom of a tall sidebar; a
  Discord-style left server rail is the eventual shape. Fine for the MVP; not a P0.

## 2026-06-13 (iter 27) — Servers/guilds backend; an idempotent-migration bug the suite caught

Shipped the servers/guilds backend foundation (servers + members + server-scoped
channels, per-server channel names, REST create/list/join, members-only access). Built
additively so global channels still work (a channel with `server_id IS NULL` stays a
global room). Verified at all three layers: store (`TestServersIntegration`), live HTTP
(owner 200 / outsider 403 on a server's channels + message read), and live WS (outsider
403, owner 101, join→101); global list excludes server channels.

**The bug the test suite caught — a non-idempotent migration.** Allowing each server its
own `#general` meant replacing the table-wide `UNIQUE(channels.name)` with partial unique
indexes. But the default-channel seed used `INSERT ... ON CONFLICT (name)`, which has no
arbiter once the constraint is dropped — so the *second* `db.Migrate` (every test re-runs
it) failed with `42P10`. Caught because the integration tests each migrate a shared db, so
the 2nd test failed loudly. Fix: a constraint-independent guarded seed
(`INSERT ... SELECT ... WHERE NOT EXISTS`). Re-verified by an 8×-migrate run on a fresh db.

Reflections:
- **A migration must survive running twice — test that explicitly.** The suite caught it by
  luck (shared db, sequential migrates). A dedicated "migrate twice, assert no error" test
  would catch this class deterministically; worth adding for any schema that drops/renames.
- **Blast radius of a uniqueness change:** dropping `UNIQUE(name)` silently broke
  `DefaultChannelID` (its `WHERE name='general'` became ambiguous across per-server
  generals). Grepped every `name =` lookup and scoped it to `server_id IS NULL`. When you
  change a constraint, grep every query that relied on it.
- **Node 23 `fetch` can't set `Connection: Upgrade`** (undici rejects it) — for WS-status
  probes use `curl` (it worked for both the DM and server WS 403 checks). Node's built-in
  `WebSocket` is for real WS sessions, not status-only probes.
- **Next:** the servers **UI** (server rail + per-server channel list + create/join), then
  a two-client server-isolation browser flow.

## 2026-06-13 (iter 26) — Closed the DM reaction-access gap (full Rule-15 cycle)

Closed the security follow-up logged when DMs shipped: reactions weren't access-checked,
so a non-member could react to a private DM message by guessing its id (ids are
sequential). Ran the complete Rule-15 loop: **reproduced** (a new test failed —
`non-member AddReaction err = <nil>`), **fixed** (`requireChannelAccess` gate in
`AddReaction`/`RemoveReaction` → `ErrForbidden` → HTTP 403), **re-attacked at both layers**
(store test passes; live server: carol PUT *and* DELETE → 403, member → 200), proved no
regression (members + public reactions still work, full suite green), and **encoded** it as
`TestDMReactionAccessControlIntegration`.

Reflections:
- **Every guessable-id endpoint on a private resource needs its own gate.** The read/connect
  gate (iter 24) wasn't enough — *write* paths that take an id (react, and later: pin, edit
  others', report) each need the same `requireChannelAccess` check. New private surfaces
  should get an access-control test per mutating verb, not just per read.
- **Node 23's built-in `WebSocket` unblocked live WS-layer adversarial checks** with zero
  deps — used it to send a real DM message, then curl-react as a non-member. Worth keeping in
  the toolkit for any WS-only flow the curl/HTTP harness can't reach.
- **Verified at two layers on purpose:** the store test proves the authorization logic; the
  live HTTP run proves the handler maps `ErrForbidden`→403. A thin mapping is still a layer
  that can be wrong.

## 2026-06-13 (iter 25) — DM UI + a QA-harness P0: the harness was testing STALE code

Shipped the DM UI (sidebar "Direct Messages" section, "+ New DM" flow, DM-aware
header/composer) — a DM just selects its channel, so the existing chat machinery
(history/send/edit/react/typing) works unchanged. Verified two-user E2E: A opens a DM
with B, sends; B reloads, opens it from the sidebar, reads it (`rt-04`/`rt-05`, mirror
views, both titled with the *other* user).

**The real find — a QA-harness P0 that had been silently lying.** The DM browser QA
failed at first; the server log showed `:8080 bind: address already in use`. Root cause:
`qa/run.sh` started the server with `go run ./cmd/server`, which spawns a **temp child
binary**; the cleanup `pkill -f "cmd/server"` killed the `go run` parent but not the
child, so an **orphaned server held :8080 across runs**. Every subsequent `run.sh` failed
to bind, and the QA silently tested the *stale* backend. It went unnoticed for ticks 20–23
only because those changes were frontend/QA-only; the DM UI was the first to need *new
backend*, which the stale server lacked.

Fixes (Track 0, high value): `run.sh` now (a) frees :8080/:5173 before starting, (b)
**builds a real binary and runs that** (killable by PID, no orphan), and (c) **fails loud**
if `/healthz` isn't up — never again silently runs against a stale server.

Reflections:
- **"Tests pass" can mean "tests ran against the wrong build."** A health check on the
  thing-under-test (is OUR server actually up?) belongs in every harness, not just checks
  on the feature. The loop's iter-14-style "verify on the live thing" must include "verify
  it's the *current* live thing."
- **`go run` is a QA footgun** — its orphan child outlives naive `pkill`. Build+exec, or
  kill by port, in any throwaway-server harness.
- **Next:** live DM-list updates (today B must reload to see a new DM — push a WS event on
  DM creation); a three-user UI isolation check once a 3rd context is worth the cost.

## 2026-06-13 (iter 24) — Direct messages: backend slice + the first access-control surface

First big v0.2 feature, built backend-first (reactions pattern). A DM is a new channel
**kind** with a `channel_members` table; the genuinely new piece is **per-channel access
control** (`CanAccessChannel`) gating both the WS upgrade and REST history. Verified two
ways before "done": DB integration test (`TestDirectMessagesIntegration`) on real Postgres,
AND a live-server adversarial pass — carol (non-member) hitting the DM's REST history and
WS both returned **403**, while alice (member) got 200 (Rule 14 + Rule 15).

Reflections:
- **The first private surface changes the QA shape.** Until now every channel was public,
  so QA never tested *authorization*. From here, every read/write path needs a
  "non-member is denied" probe — I added one at the HTTP/WS layer this tick; the browser QA
  should grow a two-client DM-isolation flow when the UI lands (A and B DM; C must not see
  it). This is the access-control analogue of the per-channel-isolation gap from iter 21.
- **Test helper as shared infrastructure:** extending `setup()` to also return the pool (so
  tests can register multiple users) unlocked the multi-user tests DMs/roles/membership all
  need. The 5 call-site updates were mechanical; worth it — future membership tests reuse
  `regUser(t, pool)`.
- **Known follow-ups (logged in SPEC):** (a) react/edit/delete on DM messages aren't yet
  membership-gated (read path is, and IDs aren't exposed to non-members); (b) CreateOrGetDM
  has no cross-process lock — a concurrent double-open could duplicate a DM channel.
- **Next:** the DM **UI** slice (sidebar DM list + "message @user" + open a DM), then the
  two-client DM-isolation browser QA.

## 2026-06-13 (iter 23) — Guard the grouping *break* cases (the likely regression site)

Last tick shipped grouping with a single "it groups" assertion. The more likely
regression is the *inverse* — grouping failing to BREAK — so this tick added both
break cases: (a) browser QA, after deleting the message above a grouped follow-up,
asserts the follow-up un-groups and its avatar returns; (b) realtime QA, B replies
after A and asserts a different author is NOT grouped (keeps its avatar). No product
code changed — pure Track-0 coverage.

Reflections:
- **One screenshot proved every rule at once** (`rt-01b-different-authors.png`): a
  `[deleted]` row, a same-author message that un-grouped *because the row above it was
  deleted*, then two different-author rows — i.e. the exact same "second line" body that
  GROUPS in the single-client test correctly UN-groups here under different context. That
  is the strongest kind of QA evidence: the same code, opposite-but-correct outcomes.
- **Test the inverse of every new conditional.** A feature that hides UI under a
  condition needs a test that the UI *returns* when the condition flips — assert both
  edges, not just the one you built toward.
- **Next QA growth (still open from iter 21):** per-channel isolation across two clients
  — A in #general, B in a second channel; a message in one must NOT appear in the other.

## 2026-06-13 (iter 22) — Closed the message-grouping P1 the loop's own QA raised

The loop's AI-vision QA flagged (iter ~19) that consecutive same-author messages
repeat the avatar + name — un-Discord-like. This tick closed it: consecutive
same-author messages within 5 min collapse into a tight block (avatar gutter kept via
a spacer so bodies align; name/time/avatar hidden). A deleted message breaks the run.
Verified by eye in `03a-grouped.png` — clean, aligned, tight.

Reflections:
- **The find→fix loop worked as designed:** a P1 *surfaced by AI-vision* one tick became
  a *closed + regression-guarded* item a few ticks later. The screenshot review is
  earning its keep — it catches polish gaps no assertion would.
- **Blast-radius win:** relocating the hover action toolbar out of the (now-conditional)
  message header into a floating absolute element could have broken edit/delete/react.
  The existing browser-QA flow caught that risk for free — all three still PASS — which
  is exactly why the harness drives real clicks, not DOM assertions. New assertion added:
  "consecutive same-author message is grouped / hides the repeated avatar."
- **Next QA growth:** assert a group *breaks* correctly — a different author, or a
  >5-min gap, or a deleted message in the middle must start a fresh (avatar-bearing) row.
  That's the inverse of what I tested and the more likely regression site.

## 2026-06-13 (iter 21) — Two-client realtime QA (the fan-out the single client can't prove)

The whole point of Opencord is *realtime* — yet every QA so far drove a **single**
browser, so live fan-out between users was never actually proven (the unchecked
"## Now: two users, live message" item sat open). Added `qa/realtime.mjs`: two
independent browser contexts (alice + bob), wired into `run.sh` after the single-client
suite (combined exit code). It asserts, with no reload:
- A sees **2 online** after B joins (presence fan-out).
- B receives A's message live; B sees A's reaction chip live.
- Cross-client `mine` correctness — A's chip is accent/"mine", B's identical chip is the
  neutral non-mine style (the count-only broadcast carries `mine=false`). Confirmed by
  **eye** in `rt-02` (B, neutral) vs `rt-03` (A, accent, count 2).
- B reacts too → the count climbs to **2 for both, live**, and A's "mine" survives the
  count-only update.

Reflections:
- **A single-client harness structurally cannot test a realtime app.** This is the
  highest-leverage QA shape for Opencord; future realtime features (typing seen by the
  *other* user, read state, DMs) should get a two-context assertion here, not a solo one.
- **Bonus catch (vision):** `rt-02` also shows B's live "alice is typing…" — typing
  fan-out works across clients, now captured by a screenshot for future regression eyes.
- **Next QA growth:** per-channel isolation across two clients (A in #general, B in a
  second channel — a message in one must NOT appear in the other); reconnect/replay
  after a dropped socket.

## 2026-06-13 (iter 20) — Reactions UI shipped for the orphaned backend

The reactions **backend** shipped last tick (`7e8ecfd`: add/remove, per-viewer counts,
live WS) but had **zero web UI** — the feature was only reachable via curl. Wired it
into the client (chips + quick-emoji palette + optimistic toggle) and grew the browser
QA to drive it (react → assert highlighted chip + count → survives edit).

Reflections:
- **Real bug the new QA caught (AI-vision + assertion):** the `message-edited` WS
  handler replaced the whole message object, so editing a message **wiped its
  reactions**. The "reaction survives the edit" assertion + `04-edited.png` proved the
  fix (merge `reactions` instead of clobbering). A static render alone wouldn't have
  found this — it took *driving* the edit after a reaction.
- **Loop-process gap → rule:** the loop should scan for "**shipped backend, missing
  frontend**" gaps every tick (a whole feature was usable only via curl). Cheap check:
  `grep` a backend route family in `web/src/` and flag any with no client caller.
- **`mine` is client-owned:** the live `reaction` broadcast is count-only (viewerID 0 →
  `mine=false`), so the UI seeds "mine" from the viewer-scoped history and overlays it
  on count updates. Document this so a future refactor doesn't trust broadcast `mine`.
- **Next QA growth:** two-client reaction propagation (A reacts → B sees the count tick
  up live, the realtime path the single-client QA can't prove); per-channel reaction
  isolation in the UI.

## 2026-06-13 — Browser QA + AI manual test introduced (owner ask)

The loop had only ever tested the **backend** (curl/WS scripts) — never the real
rendered UI. Added:
- `qa/browser.mjs` (Playwright) — drives the actual UI like a user: register → send
  → avatar → edit → create/switch channel → delete, **logging every click** and
  screenshotting each step into `qa/qa-screenshots/`.
- `qa/run.sh` / `make qa-browser` — boots a dev stack, runs the QA, tears down.
- **AI manual test** = the loop `Read`s the screenshots and grades them by eye.

Reflections from the first run:
- **P1 (AI-vision finding):** consecutive messages from the same author repeat the
  avatar + name; Discord groups them into one block. → tracked under GOAL.md
  "timestamp grouping".
- **Process fix (applied):** QA must start from a **fresh DB** (`docker compose
  down -v`), or stale data breaks assertions (two `(edited)` elements). Done.
- **Next QA growth:** add reaction-chip assertions when the reactions UI ships; add
  a two-context typing-indicator check; assert per-channel message isolation in the UI.

## 2026-06-15 — File/image attachments shipped + a meta-QA fix

Shipped message file/image attachments end-to-end (composer 📎 → multipart upload →
local-disk store → access-gated serve → inline image / download chip), Rule-15
hardened (9 adversarial sub-tests: path traversal, oversized, XSS-as-download, DM
access bypass, auth) and browser-QA + AI-vision verified.

**Meta-QA fix (the highest-value lesson this tick):** the first browser-QA upload
assertion used a **1×1 PNG**. Every check passed (`isVisible`, `naturalWidth > 0`)
— but the AI-vision screenshot showed *nothing* (a 1-pixel dot is invisible), so the
mandatory interaction-vision review (Step 4b-i) couldn't actually confirm the image
renders. A test that's mechanically green but produces no human-visible artifact is a
**QA blind spot**, the exact class GAP-026 warns about. Fix: `qa/browser.mjs` now
generates a real **240×140 solid-colour PNG** (`makePng` via `zlib.deflateSync` +
`zlib.crc32`) so the rendered image is plainly visible and vision-gradeable. Re-ran →
the blurple rectangle renders inline cleanly (`06c-attachment-image.png`).

- **Loop-process rule (apply every tick):** any browser-QA flow whose correctness is
  judged BY EYE must upload/produce a **visibly-sized** artifact, never a degenerate
  fixture that passes the DOM check but is invisible to vision. Prefer a generated
  fixture with real dimensions/colour over a minimal blob.
- **Next QA growth:** two-client attachment propagation (A uploads → B sees the image
  live over the WS, the realtime path the single-client QA can't prove); a
  non-image (download-chip) render assertion; an oversized-file UI-error vision check.

## 2026-06-15 (tick 2) — Two-client attachment QA + a false-positive I caught & fixed

Grew the QA to prove the **realtime** path of attachments (Step 5b follow-through on
last tick's ship):
- `qa/realtime.mjs` now drives **A uploads an image → B sees a NEW image render
  inline live** (the WS-broadcast path the single-client `browser.mjs` can't prove);
  B fetches the bytes with B's OWN token, proving the access-gated serve works
  cross-client.
- `qa/browser.mjs` gained a **non-image download-chip** assertion (a `.txt` renders
  as a `.attachment-file` chip with a size, not inline).
- Extracted the PNG generator into a shared **`qa/fixtures.mjs`** (`makePng`), used by
  both suites (DRY).

**Meta-QA catch (highest-value lesson):** the FIRST version of the realtime check
used `.attachment-image` **`.last()`** — but the realtime suite shares `#general`
with the prior `browser.mjs` run, so a **stale** image was already in history. The
check passed by matching that stale image even though it never verified A's *new*
upload arrived — a **false positive**, the exact QA blind-spot class the loop exists
to kill. AI-vision review of the screenshot (the new orange image wasn't at the
bottom) is what surfaced it. Fix: the check is now **count-based** — it records B's
`.attachment-image` count before the upload and requires it to **grow by one**, so a
pre-existing image can't satisfy it. Re-ran → genuinely green, and the screenshot now
shows B's new orange image live at the bottom.

- **Loop-process rule (apply every tick):** in a **shared-DB / shared-channel** QA
  suite, never assert on "the last/any matching element" — a prior step or a prior
  suite may have seeded one. Assert a **delta** (count grows, or match the specific
  new content), and **AI-vision the screenshot to confirm the NEW artifact is the one
  rendered**, not a stale look-alike. A green check whose screenshot doesn't show the
  expected new thing is a false positive until proven otherwise.
- **Next QA growth:** attachment in a DM (two members) renders for the other side;
  an oversized-file UI error path vision-checked; multi-file (2–3 at once) staging +
  render.

## 2026-06-15 (tick 3) — Uploaded avatars + a deadlock the feature exposed

Shipped uploaded avatars (image upload, local-disk store reusing the attachment
helpers, access-gated serve, an `Avatar` component that blob-fetches by userId and
falls back to initials, swapped in at every avatar site, header click-to-upload).
Rule-15 hardened (8 adversarial sub-tests) + browser-QA + AI-vision verified (the
purple test avatar renders as a circle in the message list + header after upload).

**Highest-value lesson — a schema change caused a DB deadlock, found by the parallel
test suite, NOT by the feature's own tests.** Adding `ALTER TABLE users ADD COLUMN`
made every `Migrate` take an ACCESS EXCLUSIVE lock on the heavily-FK-referenced
`users` table. Under `go test ./...` (8 packages migrating + running DML at once)
that DDL raced concurrent INSERTs into FK-referencing tables (messages,
server_members, reactions) into a Postgres deadlock (40P01) — and the *innocent
test DML* was sometimes the victim, which nothing retries. It was **non-deterministic
about which test failed**, so a single green run would have masked it.

Fixes (both, layered):
1. `Migrate` now runs the schema in a transaction with `SET LOCAL lock_timeout =
   '500ms'` — below Postgres's 1s deadlock-detection threshold — so a contended ALTER
   *yields* (55P03) and retries instead of ever deadlocking the DML. The migration
   always loses the race; live DML always wins. This also hardens real rolling
   deploys (a new instance migrating while the old one serves traffic).
2. A regression test (`TestMigrateUnderConcurrentDML`) drives migrators + DML writers
   from a barrier and asserts nothing errors. **Proven to catch it:** neutering the
   lock_timeout makes it fail with "writer insert: deadlock detected" 2/3 runs; with
   the fix it's green.

- **Loop-process rule (apply every tick):** a flaky/concurrency failure must be run
  **N times (≥5)** before declaring green — one pass proves nothing for a race. And a
  schema/DDL change is a concurrency change: think about the locks it takes and what
  DML it races, not just whether the column appears.
- **Next QA growth:** two-client avatar — A sets an avatar, B sees it on A's messages
  after a reload; avatar in the member-list sidebar renders the image.

---

## 2026-06-16 — tick 102: ban members (server moderation parity)

**Shipped:** ban/unban — the stronger form of kick (removes the member AND blocks
rejoining via `server_bans` + an `ErrBanned`→403 guard in `RedeemInvite`, until an
owner/admin unbans). REST `POST/DELETE/GET /servers/{id}/bans`, members-panel ban
button, admin "Banned (N)" section with unban. Adversarial integration test
(authz matrix + rejoin-blocked then unban-restores, 3× green) + two-user browser
E2E (upgraded the realtime.mjs terminal kick step into a ban step — a strict
superset: every removal assertion holds, plus Banned-section + unban + live drop)
+ AI-vision of the ban/unban renders. Live-verified on Railway.

**Highest-value lesson — a QA-script syntax error cost a full ~3-min stack boot.**
The first `qa/run.sh` re-run died because the new ban step redeclared `bRow`
(already a `const` earlier in `realtime.mjs`). `qa/run.sh` boots Postgres + builds
the Go server + starts Vite + installs Playwright *before* it ever `import`s the
.mjs — so a one-character syntax slip isn't caught until ~3 minutes in, and only on
the crashing script (the others had already burned the boot). A `node --check` on
each `qa/*.mjs` takes milliseconds and catches this class instantly.

**Improvement (shipped this tick, not just noted):** `qa/run.sh` now `node --check`s
every `qa/*.mjs` up front and aborts before booting the stack if any fails to parse.
Fast-fail before the expensive boot. (Loop-process rule going forward: `node --check`
every edited `qa/*.mjs` before `bash qa/run.sh`.)

**Next QA growth:** the dedicated two-user *kick* UI flow in realtime.mjs is now a
ban flow; kick's authz is still covered by `TestRouterKickMemberIntegration` and the
shared button-render check, but a lightweight standalone kick-UI assertion could be
re-added if kick and ban ever diverge in the UI.

---

## 2026-06-16 — tick 103: member timeout (temporary mute)

**Shipped:** timeout — completes the kick/ban/timeout moderation triad. An owner/admin
mutes a member for a duration (`server_members.timeout_until`); a server-side post-guard
in `SaveReply`/`SaveWithAttachments` (`ErrTimedOut`, enforced on BOTH the WS and HTTP
send paths) blocks posting until it expires or is cleared. Duration clamped ≤28d.
`POST/DELETE /servers/{id}/timeouts`, members-panel timeout/unmute button + ⏳ muted
badge, composer disabled for the muted viewer. Adversarial integration test (mute
enforced at the store guard then lifted on clear; duration clamp) + two-user browser
E2E (new timeout/unmute step in realtime.mjs) + AI-vision verified. Live on Railway.

**Process win — the tick-102 `node --check` gate paid off immediately.** This tick's
realtime.mjs edit parsed clean on the first try; the up-front parse-check ran in
milliseconds and gave confidence before the ~3-min stack boot. The QA-harness
improvement from last tick is already compounding (exactly the intent).

**Verification lesson — test the post-guard at the layer that enforces it.** Messages
post over the WS, not HTTP, so an HTTP-only integration test can't drive a real send.
The right move was to assert the guard at the STORE boundary (`store.SaveReply` →
`ErrTimedOut`) where both the WS and HTTP paths converge — proving the mute regardless
of transport, then proving it lifts after clear. Cheaper and more faithful than trying
to script a WS send + error round-trip in the integration harness.

**Next QA growth / P1 polish:** the members-panel row now carries demote/kick/ban/
timeout(+unmute) — 4 action links plus 2 badges. It reads fine but is getting busy; a
Discord-style kebab/context menu is the future polish. Also: a browser assertion that a
muted member's composer actually disables (needs the ~10s member-list poll, so it wasn't
worth the wall-clock this tick — the backend guard + the placeholder logic cover it).

---

## 2026-06-16 — tick 104: channel categories (collapsible groups)

**Shipped:** Discord-style channel categories — `channel_categories` table + nullable
`channels.category_id` (ON DELETE SET NULL), admin-gated create + member list endpoints,
optional `categoryId` on channel create with a Rule-B cross-server guard
(`ErrCategoryNotFound`). Sidebar renders uncategorized channels first, then collapsible
category groups with a per-category "+". Adversarial integration test + browser E2E
(create → nest → collapse/expand) + AI-vision verified. Live on Railway.

**Highest-value lesson — a new QA step that changes shared UI state broke a LATER,
unrelated test.** My category step created a channel and (via the create-channel handler)
switched the active channel to it. The search test runs 15 lines later and searches the
ACTIVE channel — which was now the new empty category channel, not the one holding the
posted message → two search assertions failed with `browser=1`. The category assertions
themselves all passed; the failure was pure test-ordering coupling through the shared
"active channel" state. Fix: the new step restores the active channel before yielding.

**Loop-process rule (apply when adding a browser-QA step):** a step that mutates shared
app state (active channel, open panel, selected server, draft) must **leave that state as
it found it** (or the very next step inherits a surprise). Treat browser.mjs steps like
tests sharing a fixture: clean up the global UI state you changed. When a QA run goes red,
check whether a *just-added* step changed state a *later* step depends on before assuming
the feature itself is broken — here the feature was fine; the harness coupling wasn't.

**Next QA growth / follow-ups:** category reorder/drag, move-an-existing-channel between
categories, and "keep the active channel visible even under a collapsed category" (Discord
behaviour) are deferred polish — each wants its own assertion when built.

---

## 2026-06-16 — tick 105: jump-to-message (reply / pin / search → original)

**Shipped:** clicking a quoted reply preview, a pinned message, or a search result now
scrolls to + briefly flashes (accent highlight) the original message in the channel.
Two code paths: the *inline* case (reply preview — list already on screen, jump via
requestAnimationFrame) and the *close-a-panel* case (pin/search — close the panel, then a
useEffect retries the jump once the message list is rendered). Frontend-only. Reply-jump
+ search-jump E2E + AI-vision verified (the flash highlight is clearly visible).

**Highest-value lesson — a useCallback in a dependency array must be DECLARED above the
effect that lists it.** I first defined `doJump`/`jumpToMessage` next to the panel
handlers (~line 835) but referenced `doJump` in an effect's dependency array at ~line 414.
The dep array is evaluated *during render*, so `doJump` (a `const`) was in its temporal
dead zone there → a ReferenceError at render, before any QA could run. tsc/vite still
*built* (TDZ is a runtime error, not a type error), so the build was green but the app
would have crashed on mount. Fix: hoist `doJump` above the effect. Caught it by reasoning
about hook ordering before running QA — but it's a reminder that **a green `tsc`/`vite`
build does NOT prove the component mounts** (Rule 14: the browser QA, which actually
mounts and drives the app, is what proves it).

**Loop-process note:** for hooks, define a value before the first effect/callback whose
dependency array references it; "group related functions together" can silently create a
TDZ when one is used in an earlier effect. The browser QA (real mount) is the guardrail —
keep adding assertions that exercise newly-wired handlers.

**Next QA growth / follow-ups:** pins-panel jump has the same code path as search-jump
(covered); fetch-older-messages-on-jump (when the target is outside the loaded window) and
a pins-panel jump assertion are deferred.

---

## 2026-06-16 — tick 106: delete category (lifecycle completion)

**Shipped:** `DELETE /servers/{id}/categories/{catId}` (admin-gated) + a "✕" on each
category header. Completes the category lifecycle (tick 104 shipped create-only, so a
typo'd category was permanent). The FK's `ON DELETE SET NULL` already orphans channels
safely — deleting a category leaves its channels as uncategorized, never deletes them.
Adversarial test (authz + cross-server-delete-via-path guarded by `AND server_id`, 3x) +
browser E2E (delete → group gone, channel survives uncategorized) + AI-vision.

**Note (deliberately a tight tick):** this was the 6th feature ticket in one session;
chose a small, lifecycle-completing item with minimal blast radius (1 store fn + 1 route +
1 button, all additive) over a large feature, to stay surgical under deep context. The
frontend updates `serverCategories` + clears the affected channels' `categoryId` locally
(no refetch) so the UI matches the server's `SET NULL` without a round-trip.

**Follow-ups still open for categories:** rename, reorder/drag, move-an-existing-channel
between categories, "keep active channel visible under a collapsed category".

---

## 2026-06-16 — tick 108: invite management (list + revoke)

**Shipped:** the members panel (admin) gained an **Invites** section — lists a server's
active codes (expiry + creator), a **+ New invite** mint, **copy**, and **revoke** (kills
a leaked code so it stops redeeming immediately). Backend: `ListInvites` + `RevokeInvite`
(DELETE scoped by `server_id`, Rule-B cross-server guard) + admin-gated `GET`/`DELETE
/servers/{id}/invites[/{code}]`. Adversarial integration test (admin-gating, cross-server
revoke blocked, revoked code → 404, re-revoke → 404) + browser E2E + AI-vision verified.

**Highest-value lesson (loop-process) — a new UI element must NOT borrow an existing
element's CSS class; shared classes are also QA + behavioural selectors, and reuse
collides silently.** I styled the invite rows/header by reusing `member-row` and
`bans-head`. Both passed `go test` + `tsc` + `vite` green, then **failed the browser QA
twice in a row**, each time in a DIFFERENT, invite-unrelated place:
1. `.member-row` reuse → the realtime QA's `locator('.member-row', {hasText: userA})`
   (member-by-name lookup) ALSO matched the invite row whose creator label says
   "by <userA>" → strict-mode crash at the ban step.
2. `.bans-head` reuse → the realtime QA waits on `.bans-head` as the signal that "the ban
   refreshed the panel"; my always-present "Invites" header also matched it, so the wait
   resolved *before* the ban and three ban assertions raced ahead and failed.

Root cause both times: an invite row/header **masquerading** as a member/ban row. Fix:
dedicated classes throughout (`invite-row`, `invites-head`, `invite-info`, `invite-by`,
`invites-empty`) with the borrowed styling replicated in CSS. **The full browser QA (real
two-client mount) caught what the unit tests + type-check could not** — exactly its job.

**Rule to carry:** when adding UI that *looks like* an existing component, give it its own
classes and replicate the styles; never reuse a class that a `.locator()`/`getByRole`
anywhere (QA or app) selects on. Grep the new classes against `qa/*.mjs` before shipping.
A green `tsc`/`vite` build proves types, not that the rendered DOM keeps existing selectors
unambiguous (Rule 14).

**Also fixed (separate commit):** a *pre-existing* flaky realtime selector —
`getByRole('button', {name: /userA/})` for the DM-open matched both the DM `.channel-item`
and a `.reply-context` "jump to userA's message" button (a render race since tick 105).
Scoped it to `.channel-item`. Surfaced by this tick's full QA timing, not caused by it.

**Next QA growth / follow-ups:** invite **max-uses** + a uses counter is the clean next
slice (schema `max_uses`/`uses` + atomic redeem guard + a column in the panel). A QA
assertion that greps new CSS classes against `qa/*.mjs` would have caught both collisions
pre-run — worth a tiny lint step in `qa/run.sh`.

---

## 2026-06-16 — tick 109: invite max-uses (cap a code's joins)

**Shipped:** invites can now carry an optional max-uses cap (1–1000; blank = unlimited).
`server_invites.max_uses`/`uses`; `RedeemInvite` enforces + counts the cap in ONE
transaction (guarded `UPDATE ... WHERE uses < max_uses` then the member INSERT, so a
failed join rolls back the use and concurrent redeems of the last slot can't overshoot),
and short-circuits an existing member so a re-redeem burns no use. Panel shows `N/M uses`.
Adversarial integration test (3 subtests) + browser E2E (creates unlimited + a 0/5 cap) +
AI-vision. `CreateInvite` kept as the unlimited shorthand → zero churn to its 8 callers.

**Highest-value note — last tick's lesson held: dedicated CSS classes from the start →
a clean first-try QA (browser=0 realtime=0 voice=0), zero collisions.** Tick 108 burned
two QA iterations on `.member-row` / `.bans-head` reuse; this tick I used `invite-*`
classes throughout and added the new assertions before running, so the full suite passed
on the first attempt. The discipline (new UI element ⇒ its own classes; grow the QA in the
same change) is paying off. Also pre-empted the harness collision the new flow could cause:
the panel's "+ New invite" now opens a `window.prompt` for the cap, so the browser QA sets
`promptAnswer` ('' then '5') before each click — a feature that adds a prompt MUST update
the QA's dialog handling in the same change, or the auto-answer feeds garbage to validation.

**Next QA growth / follow-ups:** (1) a two-client browser test that actually **exhausts** a
maxUses=1 invite through the UI (A mints a 1-use code → B joins → C's redeem is refused),
not just the HTTP-layer exhaustion already covered. (2) A small `qa/run.sh` pre-flight that
greps newly-added `className` literals in `web/src` against hardcoded selectors in
`qa/*.mjs` would have caught both tick-108 collisions before the 3-min boot — worth it if
the false-positive rate on intentionally-shared classes (`link`, `message`) can be kept low.
(3) invite-links / temporary-membership are the remaining Discord invite gaps.

---

## 2026-06-16 — tick 110: prove max-uses is race-safe (Track-0 hardening)

**Shipped (test-only):** a store-level concurrency test for invite max-uses — 25 goroutines
released simultaneously redeem a maxUses=5 code; asserts EXACTLY 5 succeed, 20 get
ErrInviteExhausted, the stored `uses` counter is exactly 5 (no overshoot), and exactly 5
members were admitted. PASS x3 under `-race` (clean). ~2.4s without -race, so the gate
(`go test ./...`, no -race) barely notices it.

**Why this tick (not a 3rd feature):** tick 109's max-uses used a guarded
`UPDATE ... WHERE uses < max_uses` inside a tx *specifically* to be race-safe, but the
integration test only redeemed sequentially — it asserted the feature works, never that the
safety property it was built for actually holds under contention. A claimed invariant with
no test that can fail when it's violated is an unguarded invariant. After two back-to-back
features, the highest-value move was to *prove* the safety claim rather than pile on more
surface area — and to do it as a pure test (no product/UI change), the lowest-risk way to
keep advancing in a deep session.

**Loop-process note:** when a change's whole justification is a concurrency/atomicity
property (tx, CAS, guarded UPDATE, lock), the same PR should add a test that actually races
it — a sequential test passes whether or not the guard exists. Carry this: "did I test the
property the design exists for, or just the happy path?"

**Deploy note:** test-only change → committed + pushed to origin, but NO `railway up` (the
served binary + web bundle are byte-identical; deploying would rebuild the same artifact —
Rule 10 anti-churn). Deploys are for app changes; a test isn't one.

**Next QA growth / follow-ups:** still open — a 2-client UI exhaustion test (panel-mint a
1-use code → a second browser joins → the code disappears from the admin list); invite-links
/ temporary-membership remain the Discord invite gaps.

---

## 2026-06-16 — tick 113: server settings (rename + delete) + a vision-found polish gap

**Shipped:** the two missing "Server Settings" actions. `RenameServer` (owner/admin,
live `server-renamed` sidebar relabel) and `DeleteServer` (owner-only; one tx deletes the
server's messages — `messages.channel_id` has no `ON DELETE CASCADE` — then drops the
server, cascading members/channels/invites/categories/bans; global `#general` survives;
members live-evicted via the existing `server-removed` path). UI: a ⚙ Server settings
section atop the members panel. Verified end-to-end on the live Railway deploy: the new
CSS bundle serves `.delete-server-btn`, and a prod API round-trip returned PATCH 200 /
DELETE 204 / GET `[]` / re-DELETE 404 / blank-name 400.

**QA grown:** new store + HTTP authz-matrix tests (rename admin+, delete owner-only;
member/admin/stranger 403; unknown 404) + a cascade test (messages/members/channels gone,
`#general` intact), and a browser E2E flow (create → rename → delete a throwaway server,
vision-checked).

**Highest-value improvement found (this is the reflection):** the AI-vision pass on the
new screenshots surfaced a *pre-existing* polish gap the feature didn't cause but exposed —
`.server-name` has no `text-overflow: ellipsis` / `min-width: 0`, so a long server name
(my 19-char "qa settings renamed") overflows the fixed 220px sidebar. Every prior browser
QA only ever created the 9-char "qa server", so the truncation path was never exercised —
a coverage blind spot. Logged as a GOAL.md polish item (truncate + `title` tooltip; extend
QA to create a long-named server and vision-check the row stays one line). Did NOT fix it
in this commit (Rule 10 — unrelated to the feature; it gets its own repro + test next).

**Loop-process note:** QA fixtures should deliberately use *boundary-sized* inputs (long
names, long status, long topic), not just convenient short ones — a short happy-path string
hides every truncation/overflow bug. Carry this: "did my fixture stress the width, or just
the behaviour?" (cf. tick-110's "did I test the property the design exists for?").

**Deploy note:** real app change → committed, pushed origin, `railway up` deployed, live
rollout confirmed by fetching the served bundle (not just `/healthz`).

---

## 2026-06-16 — tick 114: closed the tick-113 finding (sidebar truncation) + a QA-assertion lesson

**Shipped:** sidebar long-name truncation. Server/channel/DM names now ellipsis-truncate
(`.item-name`/`.server-name-text` get `min-width:0` + `overflow:hidden` + `text-overflow:
ellipsis`; the `#id` badge, avatar, hash, and unread dot stay `flex-shrink:0`), with the
full name in a `title` tooltip — a long name can no longer overflow the fixed 220px sidebar.
This directly closes the polish gap tick-113's AI-vision surfaced.

**QA grown:** the browser server-settings flow now creates a *boundary-width* server name
("qa settings srv long enough to overflow the sidebar") and asserts the name element clips
(`scrollWidth > clientWidth`) and stays within the sidebar's right edge — exactly the
fixture-stresses-width lesson from tick-113, now encoded.

**Highest-value learning (the reflection): a failing UI assertion is not proof the code is
broken — reconcile it against the screenshot first.** My FIRST assertion checked the whole
`.server-list` container's `scrollWidth > clientWidth`. It FAILED — but the AI-vision pass
on the same screenshot showed the name truncating perfectly with a clean sidebar. The fix
was correct; the *assertion* was wrong: a container's horizontal overflow is confounded by
every sibling in it (the +channel/+category/invite action row), so it can't isolate "did
the name overflow?". Switching to an element-level check (does THIS name element clip, and
is its right edge inside the sidebar?) made it precise and green. Lesson: assert the
specific element's property, never a broad container metric many things contribute to — and
when an assertion and the rendered pixels disagree, trust the pixels and fix the test (Rule
14 is why the vision pass runs *alongside* the automated checks, not instead of them).

**Deploy note:** frontend-only change → committed, pushed origin, `railway up`, live rollout
to be confirmed by fetching the served bundle.

---

## 2026-06-16 — tick 115: leave server (the join counterpart) + reusing test state cleanly

**Shipped:** `LeaveServer` — voluntary self-removal for any non-owner member (`POST
/servers/{id}/leave`). The owner can't leave (403 — must delete/transfer, Discord's rule);
non-member/unknown → 404; the leaver's own live sockets are evicted. The members-panel
⚙ Server settings section now renders for every member: owner sees "delete server",
non-owners see "leave server". Closes the leave-server TODO left by tick-113.

**QA grown:** the leave flow lives in `realtime.mjs` (browser.mjs's bot always *owns* its
servers, so it can never exercise leave). The new step reuses the two-client state already
built up — after the ban/unban sequence, B (now unbanned, a non-owner) rejoins via the
still-valid invite and leaves, asserting the panel shows "leave server" (not "delete
server") and the server drops from B's sidebar. AI-vision confirmed B's post-leave sidebar
is empty and B fell back to #general.

**Highest-value learning (reflection): pick the QA harness whose actors can actually reach
the code path.** Leave is owner-forbidden, so the single-client owner-bot in browser.mjs
structurally *cannot* test it — only realtime.mjs has a non-owner member (B, who joined via
invite). The cheap move was to extend the existing two-client narrative (its ban/unban
already leaves B as a rejoinable non-owner) rather than stand up a fresh fixture. Carry
this: before writing a UI test, check the harness's actors hold the role the path requires
— a test in the wrong harness either can't be written or quietly tests the wrong branch.

**Deploy note:** real app change → committed, pushed origin, `railway up`; live rollout to
be confirmed by fetching the served bundle for the new `.leave-server-btn` class.

---

## 2026-06-16 — tick 116: transfer ownership completes the server-management lifecycle

**Shipped:** `TransferServerOwnership` (`POST /servers/{id}/transfer`) — the owner hands the
server to another member in one tx (target → owner, old owner → admin, `servers.owner_id`
updated). Owner-only; can't transfer to self/non-member. A "make owner" button on each
non-owner member row (owner-only). With this, the server-management cluster is
MVP-complete: create · join (invite) · rename · delete · leave · kick · ban · timeout ·
roles · transfer.

**QA grown:** a transfer round-trip (A→B→A) in `realtime.mjs`, asserting the role badges
swap in *both* directions, then the existing leave test runs (B ends as admin — still a
non-owner — so it stays leave-able). AI-vision confirmed the post-transfer panel: B=OWNER,
A=ADMIN, and A's Server settings controls correctly switched from "delete server" to
"rename/leave server" now that A is no longer the owner.

**Highest-value learning (reflection): a round-trip test both proves both directions AND
restores state for the next test.** Transfer is owner-only and one-way per call, so a naive
one-shot test (A→B) would leave B as owner and break the *subsequent* leave test (B could no
longer leave). Doing A→B→A in one flow verified the swap symmetrically *and* returned the
fixture to a leave-able state — composing tests by restoring state beats standing up a fresh
isolated server for each. Carry this: when a test mutates shared state a later test depends
on, prefer an inverse operation that doubles as extra coverage over a throwaway fixture.

**Also vision-verified incidentally:** role changes correctly re-gate the UI live — the
demoted owner's panel swapped delete→leave without a reload, confirming the role-driven
control rendering from ticks 113/115 holds under a role change.

**Deploy note:** real app change → committed, pushed origin, `railway up`; live rollout to
be confirmed by fetching the served bundle for `.transfer-owner-btn`.

---

## 2026-06-16 — search `before:`/`after:` date operators

**Shipped:** day-exclusive `before:<YYYY-MM-DD>` / `after:<YYYY-MM-DD>` message-search
operators (combine into a window, compose with `from:`/`has:`/free text); strict UTC parse
so a malformed/hostile date falls through to inert free text; bind-param SQL (Rule B).
Integration test + browser QA `07c3` + SPEC note. Pushed origin + `railway up`; live rollout
verified (served bundle carries the new `before:2024-01-31` placeholder).

**Highest-value learning (reflection): verify a query/filter feature on prod by asserting it
*discriminates* against existing channel history — never by post-then-find.** This tick's
first live probe tried to register → POST a probe message → search for it, and hit HTTP 400:
`POST /api/messages` is the **attachment-upload** path (requires ≥1 file); plain text messages
only travel over the **WS gateway**, so there is no one-line REST way to seed a text message
for a live smoke. The probe that actually proved the feature instead **counted #general's
existing 7 messages across four date bounds** (`before:2099`→7, `before:2000`→0,
`after:2099`→0, `after:2000`→7) — no posting needed, deterministic on whatever history exists,
and it proves the bound genuinely filters. Plus malformed/injection dates → HTTP 200 (inert),
the Rule-15 assertion, live.

**Carry this (loop-process improvement):** for any future search/filter/sort operator, the
Step-5b live check should be a **read-only discrimination probe** (same query family, two
bounds that must return different counts against current data) rather than a write-then-read
flow — it sidesteps the WS-only message path and can't be defeated by an empty fixture.
*Actionable follow-up (GOAL):* a tiny `qa/search-smoke.sh` (register → 4 bounded searches on
#general → assert counts strictly decrease as the window tightens) the loop can run post-deploy.

---

## 2026-06-16 — status emoji (Users/Profiles component)

**Shipped:** optional **status emoji** before the custom status — `users.status_emoji`
(capped 16 runes), same `PUT /me/status` payload, rendered React-escaped in the header +
both member-list panels. Store test (set/clear/caps/emoji-only) + browser QA `07d2` (asserts
🚀 renders before the text in member list AND header) + SPEC note. Pushed origin + `railway
up`; live round-trip verified (member list returns the emoji; oversized → 16-rune cap on prod).

**Highest-value QA-harness improvement (Step 4b-ii — advance the QA PROCESS): the browser-QA
dialog handler can now disambiguate sequential `window.prompt`s by message content.** Before,
a single `promptAnswer` string answered *every* prompt, so a flow with two prompts in a row
(emoji → text) was untestable — both would get the same answer. The handler now routes by
`d.message()` (`/emoji/i` → `statusEmojiAnswer`, else `promptAnswer`), which is backward-
compatible (no existing prompt mentions "emoji", so they still get `promptAnswer`) and unlocks
testing ANY future multi-prompt affordance. This is the reusable capability, not just a
one-off assertion.

**Carry this (QA-process rule):** when a UI flow issues multiple `window.prompt`s, the QA
handler must answer each by matching `d.message()` — never assume one global answer covers a
sequence. Component rotation: advanced **Users/Profiles** this tick (chat/search last tick,
servers before that) — next, rotate to presence (idle/DnD) or a Messaging item.

---

## 2026-06-16 — presence states (idle/dnd/invisible) + a replace_all near-miss

**Shipped:** four-state presence (online/idle/dnd/invisible) — manual picker,
`users.presence_state`, pure-unit-tested effective-presence rule (others see
invisible/disconnected as offline; you see your own true state), `PUT /me/presence`,
member-list dot colored green/amber/red/grey in both panels. Pure + http + browser tests;
pushed origin + `railway up`; live round-trip verified (dnd/idle/invisible reflected;
bogus→online on prod). Component rotation: **presence** this tick.

**Highest-value learning: `replace_all` silently replaced only ONE of two "identical" dot
renders because they sat at different JSX nesting/indentation — and reported "All occurrences
replaced," giving false confidence.** The two presence-dot spans (members-management panel vs
the visible right-sidebar member list) differed only in leading whitespace, so my replace_all
matched the exact-indentation string once and left the VISIBLE one on the old `mb.online`. All
Go unit tests + the backend probe passed (backend was correct); only the rendered dot was
wrong. **Browser QA caught it pre-commit** — the dnd dot never appeared — exactly the class of
bug HTTP/unit tests miss. Fixed the second spot, re-ran, green.

**Carry this (process rules):**
1. **`replace_all` only hits EXACT-text matches.** When "the same" code is duplicated at
   different nesting depths, it will quietly miss the differently-indented copies. After any
   replace_all on duplicated render logic, **grep the symbol to confirm the count** (`grep -n
   'presence-dot' …` → expect N) rather than trusting the "all occurrences" message.
2. **This is why browser QA runs before commit, not after.** A backend-correct, unit-green
   change can still render wrong; only driving the real UI + AI-vision catches it. The gate
   worked — keep it mandatory for every UI-touching tick.

---

## 2026-06-16 — responsive QA for new header controls (caught a real overflow)

**Lean tick (context-hygiene):** after 3 feature ticks, advanced the QA PROCESS instead of a
4th feature. Added browser QA `08b`: at 390px the always-shown user controls (presence picker
+ status + log out) must be reachable AND the header must have zero horizontal overflow.

**It immediately caught a real bug:** 149px of horizontal overflow on mobile — the `.meta`
control row is a `nowrap` flex group, and the presence picker added last tick pushed it past
the viewport. It was latent because only the DESKTOP header was overflow-checked (false
confidence). Fixed: `.meta` wraps onto multiple lines at ≤640px + a tighter status max-width →
0px overflow, AI-vision verified the controls wrap cleanly. Shipped + live-verified.

**Carry this (QA-process rule): any new always-visible header/toolbar control gets a
mobile-width (≤640px) no-overflow assertion in the SAME tick it's added — not a tick later.**
A desktop-only layout check passes while the mobile layout silently overflows; the two are
independent and both must be guarded when a persistent control is added. Component/dimension
rotation: advanced the **responsive** QA dimension (Step 4b-ii) this tick.

---

## 2026-06-16 (tick 122) — Closed the last QA/loop-tooling backlog item: qa/search-smoke.sh

After a heavy 3-feature+1-fix prior session, took a **lean Track-0 tick**: closed the one
unchecked item in GOAL.md "### QA / loop tooling" — a read-only post-deploy smoke for the
message-search operators (`before:`/`after:`/free-text). **Component: chat (search) → QA
coverage.** No product code changed (zero Go/web diff), so nothing new is *served* — pushed
to `origin`, deliberately **skipped `railway up`** (deploying a byte-identical server image
is exactly the churn Rule 10 forbids; the smoke is a harness script, not served code).

**The interesting design constraint:** plain-text messages are posted over the WS gateway,
not REST, so a smoke can't cheaply "write then find" on prod. The smoke instead proves the
operators genuinely FILTER by **discriminating against `#general`'s existing history**: the
full window (`after:1970-01-01` = 7 on prod) must strictly exceed a future/contradictory
window (→ 0), a tightening `after:` chain must be monotone non-increasing, and an unmatchable
free-text token must return 0. If search ever regressed to ignore operators (match-all), every
count would equal the baseline and the assertions break.

**Proved the guard fires (Rule 15):** ran the exact assertion battery against a simulated
match-all `count()` → **5 of 7 assertions trip**, exit non-zero. A green run on prod (7
messages of history) confirms the happy path. Also handles an **empty `#general`** honestly:
reports INCONCLUSIVE + exit 0 rather than false-red, since an empty channel can't discriminate
and that's not a search bug.

**Carry this (QA-process rules):**
1. **Read-only prod smokes should discriminate against *existing* state, not mutate it.** When
   the write path isn't reachable over the smoke's transport (WS-only here), assert *relative*
   invariants (subset monotonicity, full>narrow, unmatchable→0) that hold for any data
   distribution with ≥1 row — robust without seeding.
2. **A smoke that can't discriminate (empty data) must say INCONCLUSIVE, not PASS.** A silent
   pass on no-data is a false-green; exit 0 with a clear "not exercised" line instead.
3. **A new guard isn't done until you've shown it fails on the regression it targets** —
   simulate the broken behavior and confirm the assertions trip (did it here, 5/7).
Component/dimension rotation: advanced **search/chat QA coverage** this tick (distinct from
the prior presence + responsive ticks).

---

## 2026-06-16 (tick 123) — Browser-QA tick: all-green + vision pass; wired search-smoke into the local gate

Ran the full browser QA (`bash qa/run.sh`) — **browser=0 realtime=0 voice=0**, every assertion
green including the two-client voice + screen-share flows. AI-vision pass over the screenshots
(search, presence/DnD member list, mobile header at 390px): all clean, **no P0/P1** — search
result card, the DnD red dot + 🚀 status, and the wrapped mobile header all render polished and
Discord-like. The last session's mobile-header overflow fix holds.

**QA-process improvement this tick:** wired `qa/search-smoke.sh` into `qa/run.sh` as a 4th gated
step (`search=$RC4`) against the local API, so search-operator discrimination is now verified on
**every** browser-QA run, not just manually against prod. Verified by re-running the full suite:
the smoke found 14 messages of #general history (posted by the QA bots) and passed all
discrimination assertions → `browser=0 realtime=0 voice=0 search=0`. On a fresh/empty channel it
self-reports INCONCLUSIVE (exit 0), so it can never false-red the gate.

**Highest-value lesson — verify the parse logic before filing a "bug" from a surprising number.**
The local smoke showed `after:<today>` = 0 even though all 14 messages were posted *today* (real
clock 2026-06-16 21:1x UTC). My first instinct was "after: date-boundary bug." Reading
`parseSearchQuery` showed `after:` does `d.AddDate(0,0,1)` — `after:2026-06-16` means *strictly
after the whole day* (≥ 2026-06-17), the **documented day-exclusive** semantic. So excluding
today's messages is CORRECT, not a bug. A surprising QA number is a prompt to read the code, not
a bug to file — the smoke's robust assertions (which don't depend on `after:today`) stayed green
throughout. Component/dimension rotation: advanced **interaction depth / result-region** QA
(wired a real discrimination check into the gate + AI-vision over result cards).

---

## 2026-06-16 (tick 124) — Rule-15 HTTP-handler hardening tests for PUT /me/status + /me/presence

Rotated off QA-tooling (2 prior ticks) to the **security** component. Adversarially probed the
recently-added presence/status endpoints. **Finding: the handlers are already robust** — status
capped to 128 runes (rune-safe slice), emoji to 16 runes, body bounded to 4 KiB
(`MaxBytesReader`), `NormalizePresence` coerces any hostile value → `online`, all parameterized +
JWT-derived. Confirmed live on prod: malformed JSON → 400, oversized (>4 KiB) body → 400, bogus
presence → 204(online), no 500, nothing stored.

**Gap closed: those Rule-15 bounds weren't encoded as regression tests** at the HTTP layer. The
existing `TestRouterStatusIntegration` / `TestPresenceEndpointIntegration` covered unauth/set/
clear/bogus but not the hostile-body paths — exactly the handler-level class that produced the
**iter-97 over-long-password 500**. Added, through the real router: malformed body → 400,
oversized (>4 KiB) body → 400, and the length caps end-to-end via HTTP (status → 128 runes, emoji
→ 16 runes), each with a guard that the rejected body left nothing stored.

**Proved the guard fires (Rule 15 step 3):** transiently widened the status `MaxBytesReader`
4 KiB → 1 MiB → the oversized body decoded → 204 → the new assertion FAILED (`status = 204, want
400`); reverted, green again. A bound without a test that fails when the bound is removed is an
unguarded bound.

**Carry this (process rule): when an adversarial probe finds a surface already hardened, the work
isn't "nothing to do" — it's "encode the bound as a regression."** A correct-but-untested bound
silently regresses (a future refactor bumps `1<<12` and no test notices). The deliverable of a
clean security audit is the test that pins the bound, not just the green probe. Component
rotation: advanced **security / hostile-input-proof** this tick.

---

## 2026-06-16 (tick 125) — Doc-sync: README front door was stale vs the shipped product (Rule 14)

After 3 test/QA ticks, did a **docs-sync** pass (Rule 14). The README — the open-source project's
front door — had drifted hard: the "Working today" list omitted presence states (idle/DnD/
invisible) + custom status, the whole **roles / moderation (kick·ban·timeout) / invites / channel
categories / pinned messages / read-state + @mention badges** feature set, and the **API surface
table listed 16 of the now-51 endpoints** (missing `PUT /me/status` + `/me/presence`, kick/ban/
timeout, leave/transfer, server rename/delete, invite list/revoke, categories, pins, unreads,
voice/token). The Configuration table was missing the shipped STUN/TURN vars.

Fixed all three sections. **Verified accuracy, not vibes:** wrote a Python cross-check that parses
every `r.{Get,Post,…}("…")` decl from the router and every README table row, normalizes both to
`METHOD path`, and asserts the sets are EQUAL — `actual 51 == readme 51`, zero invented endpoints,
zero missing. Also verified each documented payload (`{maxUses}`, `{userId,durationSeconds}`,
`{userId,reason}`, `{userId}`, `{postPolicy?,topic?,slowmodeSeconds?}`) against the real handler
structs rather than guessing field names. Docs-only → push origin, no redeploy (README isn't served).

**Highest-value lesson — docs drift silently and fastest on the highest-traffic surface.** The
README accumulated ~35 endpoints and several feature families of drift while every tick shipped
"the feature" without the doc delta. The fix is a **machine-checkable invariant**: a route-vs-README
diff is cheap and could be a CI guard so the API table can never silently fall behind again (logged
as a follow-up QA idea). Carry: when a tick adds/renames an endpoint or user-facing flow, the README
delta ships in the same push (Rule 14) — and a doc table that enumerates code should be diff-verified
against the code, not eyeballed.

---

## 2026-06-16 (tick 126) — Automated the route-vs-README guard (closed the doc-drift class)

Closed the follow-up logged last tick: turned the one-off Python route-vs-README cross-check into
a **permanent Go test** (`TestREADMEAPITableMatchesRoutes`). It builds the real chi router, `chi.Walk`s
its registered routes, parses the README "API surface" table, and asserts the two sets are EQUAL —
failing if a registered route is undocumented OR the table lists a route that no longer exists. So
the API table can never silently fall behind the code again (Rule 14, enforced not hoped).

Design choices that matter: (1) walks the **real router**, not a regex over source, so it can't be
fooled by registration style; (2) **DB-free** — route registration never calls the store/hub, so
zero-value deps suffice and it runs in 0.00s as a unit test (no Postgres needed); (3) excludes the
SPA `/*` catch-all + OPTIONS/HEAD (not API endpoints). **Proved it fires both ways (Rule 15):**
deleting the `/me/presence` row → "registered but MISSING from README"; adding a fake `/ghost`
row → "lists … but no such route is registered"; reverted → green.

**Browser-QA cadence note:** tick 126 was a `%3` browser-QA tick, but the served bundle
(`index-Bni0IEyi.js`) is byte-identical to tick 123's full green run — ticks 124/125 changed only a
Go test file + README, zero web/server code. A 3-min stack re-boot would re-confirm guaranteed-green
results, so I **skipped the redundant full browser QA** (anti-churn, Rule 10) and verified UI
liveness cheaply (SPA still serves) instead. **Carry: the "every 3rd tick" browser-QA floor is for
catching drift while shipping UI; when the rendered bundle is provably unchanged since the last
green run, a cheap serve-check + the next UI-touching tick's full run is the right call — don't
re-boot a stack to re-verify identical bytes.** Component/dimension: advanced **QA-process /
doc-coherence** (a new machine-checkable invariant in the gate).

---

## 2026-06-17 (tick 128) — UI sprint slice 1: login/register redesign (owner UI-parity priority)

First slice of the owner's UI/UX Discord-parity push. Redesigned the bare login card (`Auth.tsx` +
`styles.css`) to Discord's bar: brand wordmark, mode-aware heading/subtitle, uppercase field labels,
**password show/hide toggle**, inline min-length hint, a submit **spinner** + disabled-until-valid
button, accessible labels + `role="alert"` error. **Component: UI → polished.**

**Key discipline that kept the blast radius zero:** the two-client realtime/voice/search QA drives
auth via `getByPlaceholder('username'/'password')` and the exact button names `Create account` /
`No account? Register`. I **preserved every one of those selectors** (kept placeholders + button text,
added labels alongside), so a full UI rewrite of the login touched nothing the other suites depend on
— `browser=0 realtime=0 voice=0 search=0` first try, no QA edits needed beyond the new toggle assertion.
**Carry: when redesigning a surface other QA flows pass through, treat its existing test selectors as a
contract — keep them, add to them; don't rename them.** Grew QA with a Show/Hide-toggle assertion +
`01b-auth-register.png`; AI-vision verified both login and register modes (polished, no P0/P1).

**Deploy:** this slice changed `web/` (new bundle hash), so unlike the prior 5 docs/test ticks it
**deploys** — `railway up` + rollout verification (live bundle is the new hash). Going forward every UI
slice deploys; docs/test ticks stay push-only. Next slice: the Discord-style User Settings modal
(My Account tab).

## 2026-06-17 (tick 143) — slice 3d: input (mic) volume + a NEW QA capability (decoded inbound RMS)

Shipped the Voice & Video **Input Volume** slider (mic gain spliced into the live capture chain). The
high-value QA advance this tick: `qa/voice.mjs` now measures the **decoded RMS of a peer's inbound mic
stream** via an `AnalyserNode` on the receiver — proving a send-side change actually alters what the
other browser HEARS, not just a DOM property. Slice 3c could only assert `au.volume` (a playback
property); there was **no way to prove a capture/encode-side change reached the receiver**. The new
`measureRms(page, audioId)` helper closes that — it proved input 0% silences A on B while C stays
audible (isolation), the real end-to-end proof Rule 14 wants for audio.

**Component advanced:** audio → toward its north star (faithful, controllable, free) + QA-process
(decoded-audio measurement is now a reusable tool).

**Highest-value NEXT improvement (QA blind spot the new helper can now close):** mute / PTT / deafen
are currently verified only via the **self-chip label** and `track.enabled` — NOT by confirming the
RECEIVER actually gets silence. With `measureRms` we can now assert, two-client, that when A mutes (or
releases PTT), B's decoded inbound RMS for A drops to ~0 — and rises again on unmute. That would turn
the most safety-critical voice behaviors (am I really muted?) from "the UI says muted" into "the other
person provably hears nothing." Open as the next Track-0 QA item.

**Carry (loop-process):** a large, careful single-feature tick on the core voice path landed clean
because the change reused an in-file proven pattern (`buildScreenSendAudio`) and the two-client QA
exercised every adjacent flow (mute/PTT/deafen/screen/camera/hot-swap) — the blast-radius proof. When
touching the core audio path, lean on the existing send-gain pattern and let the full voice QA be the
regression guard, rather than hand-reasoning about each interaction.

## 2026-06-17 (tick 144) — mute proven at the RECEIVER (closing the GAP I opened tick 143)

Acted on tick 143's reflection: the mute QA only checked the self-chip label, never that the peer
stops hearing you. Reused the new `measureRms` helper to prove it two-client — A mutes → B's decoded
inbound RMS for A goes **0.2412 → 0.0000**; unmute → **0.0000 → 0.3079**. The "am I really muted?"
guarantee is now verified where it matters (the other person's ears), not in the UI. Bonus: it
reconfirms slice 3d's capture gain node didn't break mute (a disabled source still feeds true silence
through the node). **Component advanced:** security/robustness (a safety-critical guarantee is now
provable) + QA-process (receiver-RMS is now an established primitive).

**Highest-value NEXT improvement:** finish the trio with the SAME pattern — PTT (release Talk → B hears
silence; hold → audible) and deafen (deafen forces A's mic off → B hears silence). These are the last
two audio-gating behaviors checked only by UI state; the helper makes them cheap. After that, the
receiver-RMS primitive could guard future audio features (noise gate, per-peer input) for free.

**Carry (loop-process):** find → reflect → open a GOAL item → next tick close it. The tick-143 reflection
became a one-line GOAL item that made tick 144 obvious and surgical. Keep that find→item→close cadence —
it turns vague "improve QA" into a concrete queue.

## 2026-06-17 (tick 145) — receiver-silence trio complete (mute/deafen/PTT) + UI vision pass

Finished the GOAL item from tick 143: deafen + PTT now also prove peer-side silence via `measureRms`
(deafen 0.31→0.0000→0.30; PTT idle 0.0000→held 0.31). All three audio-gating guarantees are verified
where it matters — the listener's ears — not just a DOM flag. The receiver-RMS primitive (built tick
143) paid off across three ticks: input-volume, mute, deafen, PTT all proven with one helper.

**Also this tick:** AI-vision pass on the actual UI (main chat, server+member-list, mobile drawer)
looking for the owner's "feels like Discord" polish gaps — found NONE worth fixing. The member-list
truncation that looked aggressive is correct responsive ellipsis (only the 12-char QA names truncate; a
normal username fits). **Held the line on Rule 10** — did not manufacture cosmetic churn; pivoted to the
objective QA completion instead.

**Highest-value NEXT improvement (ROTATION):** three ticks on voice/QA — voice is now well-hardened.
Next tick should rotate to **visible Discord parity** (the owner's TOP priority): advance **Video slice
2** (parallel `cameraStream` so screen + camera coexist — the next TOP-PRIORITY partial) or pick a
high-visibility parity feature. The per-component rotation says favor the component furthest from its
north star; audio/QA is now strong, UI-features is the gap.

**Carry (loop-process):** an AI-vision pass that finds nothing is a VALID, valuable tick outcome — it
prevents churn. "Looked, it's clean, did the objective thing instead" is the right call, not a failure
to find work.

## 2026-06-17 (tick 146) — rotated to visible parity: Discord date dividers

Acted on tick 145's rotation note: moved off voice/QA to a visible Discord-parity feature. Added
calendar-day **date dividers** ("Today" / "Yesterday" / full date) to the message list — client-only,
no backend, low-risk. Extracted `dayLabel` to its own `dates.ts` so it's unit-testable in isolation
(no heavy Chat import); tested the Today/Yesterday/month-boundary branches. Shipped + railway up +
rollout-verified (live bundle carries it). **Component advanced:** UI (Discord parity + readability).

**Highest-value NEXT improvement:** the natural follow-up small polish — **hover timestamp on grouped
messages** (Discord shows a faint left-margin time on the grouped continuation rows on hover; ours hide
the time entirely). Tiny, visible, client-only. Bigger parity items still open: custom emoji (server
:name:, self-hostable like avatars), Video slice 2 (screen+camera coexist). Keep rotating components.

**Carry (loop-process):** when adding a small pure helper inside a giant component, extract it to its
own module so the test doesn't drag in the whole component tree — cheap testability, clean blast radius.

## 2026-06-17 (tick 147) — hover timestamp on grouped messages (parity polish)

Shipped tick 146's noted follow-up: grouped continuation rows now reveal a compact gutter time
("8:53 AM", no seconds) on hover, Discord-style. Added `shortTime` to the same `dates.ts` module
(dayLabel's neighbour) — the extraction from tick 146 keeps paying off (two tested date helpers, no
Chat import in tests). Browser QA proves the opacity 0→1 reveal + HH:MM format; AI-vision verified the
revealed gutter time against the real render. Shipped + railway up + rollout-verified.

**Highest-value NEXT improvement:** the message HEADER still shows full time with seconds
("8:53:23 AM") via `toLocaleTimeString()` — now inconsistent with the new compact `shortTime`. Discord
shows "Today at 8:53 AM". Tiny follow-up: use `shortTime` (and a relative-day prefix) in the header too.
Bigger parity items still open: custom emoji (server :name:, self-hostable like avatars), Video slice 2.

**Loop-process note (context):** 5 substantive ticks this session (143–147), all clean, no regressions,
state tracked accurately in GOAL.md/IMPROVEMENTS.md/heartbeat. Not degrading yet, so NOT clearing — but
watching for any slip next tick (per Step-0 judgment-trigger). Durable state means a clear is cheap if needed.

## 2026-06-17 (tick 148) — Discord-style header timestamp (messaging-polish thread complete)

Shipped tick 147's noted follow-up: message headers now read "Today at 9:31 AM" via `messageTimestamp`
(= dayLabel + shortTime), replacing the seconds-y `toLocaleTimeString()` ("8:53:23 AM"), across the
main list + search results + pins. Unit-tested + browser QA asserts the format + AI-vision. This
completes a coherent 3-tick messaging-polish thread: **date dividers (146) → hover time (147) → header
time (148)** — all built on the one `dates.ts` module (now 3 tested helpers).

**Highest-value NEXT improvement (bigger feature, rotate up):** the small-polish vein is mined out;
time for a meatier parity feature. Recommend **custom emoji** (server-uploaded `:name:`, self-hostable
on local disk like avatars — Rule A clean, high-visibility) as the next build; **Video slice 2**
(screen+camera coexist) is the alternative if favouring the voice component.

**Loop-process note (context):** 6 clean ticks this session (143–148), no regressions. Checked the
on-stop.sh auto-clear bridge: it fires `/clear` at ctx<20% AND re-arms `/loop` immediately (wiping the
ScheduleWakeup) — so a *forced* judgment-clear would break the 30-min idle pace. Since I'm not
degrading and the <20% safety net is wired, NOT forcing a clear; the mechanical floor is handled
automatically. Verified the cadence/clear interaction so future ticks don't mis-fire it.

## 2026-06-17 (tick 149) — "start of channel" intro (message-area parity now rich)

Shipped the Discord welcome block atop every channel/DM scrollback (round #/@ icon + "Welcome to
#general!" + start-of-channel subtitle; DM variant). Composes with the date divider + header
timestamp + hover time from 146–148 — the message area now reads distinctly Discord. Browser QA +
AI-vision verified. Kept the tick small/low-risk on purpose given the longer session.

**Highest-value NEXT improvement — commit to the big one:** message-area polish is now rich enough;
build **custom emoji**, BACKEND-FIRST as slice 1 so it's self-contained + testable even mid-session:
DB table (custom_emoji: server_id, name, uploader, image), store CRUD with validation (name unique
per server + format, image type/size — Rule 15 adversarial: non-member upload, oversized/non-image,
name path-traversal), HTTP upload/list/serve/delete endpoints, Go integration tests, curl-verify. Then
slice 2 = client `:name:` markdown render, slice 3 = picker + upload UI. Spec-first in SPEC.md (Rule D).

**Loop-process note (context):** 7 clean ticks, no degradation. Deliberately did 2 small wins (148/149)
while cautious about a big feature in a long context — but that caution shouldn't stall the roadmap, so
next tick commits to custom-emoji slice 1 (a backend slice is safe to do as a contained unit). Not
forcing a clear (re-arm is fragile; the <20% auto-clear is the safety net).

## 2026-06-17 (tick 150) — custom emoji BACKEND (slice 1), built via delegated agent + reviewed

Committed to the big feature. Per Rule 17, used an Explore agent to map the avatar/schema/auth/test
patterns (kept my context lean), then a general-purpose agent to implement+test the backend slice in
its own context, then I REVIEWED the diff, hardened, and independently verified. Shipped: server_emoji
table + store CRUD + 4 auth-gated routes (admin upload/delete, member list, public serve), Rule-15
hardened (sniffed image allowlist → no SVG/XSS, opaque keys → no traversal, size cap, scoped delete).
`TestServerEmojiIntegration` (9 cases) witnessed passing on real Postgres; prod-verified the migration
applied (authed GET /api/emoji/999999 → 404, not 500).

**Carry (agentic delegation works, but REVIEW is non-negotiable):** the impl agent verified with
build/vet/test — which DON'T check formatting — and left router.go gofmt-dirty (mixed tab indentation).
My review caught it; `gofmt -w` fixed it before commit. Lesson: when delegating code, always run the
checks the agent's own gate omits (gofmt, lint) and read the security-sensitive files yourself. Net:
delegation saved my context on a 666-line change while I still owned correctness + security.

**Highest-value NEXT improvement:** custom emoji slice 2 — CLIENT renders `:name:` as the emoji image
(fetch a channel/server's emoji list, extend `markdown.ts` to replace `:name:` with an <img> from
/api/emoji/{id}, cache the name→id map per server). Then slice 3: emoji picker (insert `:name:`) +
server-settings upload/delete UI. Slice 2 is client-only + testable via browser QA + AI-vision.

**Loop-process note (context):** heaviest tick yet (2 agents + big review + deploy); context now heavy.
NOT forcing a clear (re-arm wipes the ScheduleWakeup + is fragile; would disrupt the 30-min cadence the
session is paced on). Relying on the wired ctx<20% auto-clear as the net; still executing cleanly
(delegated well, caught the gofmt issue, verified independently + on prod).

## 2026-06-17 (tick 151) — custom emoji CLIENT render (slice 2); feature usable end-to-end

Shipped the client half: `:name:` renders as an inline emoji image for the active server's emoji
(per-server cached map; literal in #general/DMs). Built via delegated general-purpose agent (kept my
context lean again); I reviewed the XSS-critical `markdown.tsx` change (React img, numeric-id src,
map-gated, literal fallback — safe), independently ran build+vitest (39/39) + full QA (no markdown
regression across bold/spoiler/mention/blockquote) + AI-vision the render, and prod-verified. Custom
emoji now works end-to-end (backend upload + client render); only the UI to upload (slice 3) remains.

**Delegation is the context-management lever:** ticks 150–151 were big features (backend + client) but
delegating the implementation to agents kept MY context from ballooning while I owned review + security
+ verification. This is how the loop sustains long sessions without a risky forced-clear — fan the
file-heavy implementation out, keep the main loop as reviewer/verifier/shipper (Rule 17).

**Highest-value NEXT improvement:** slice 3 — server-settings **emoji manager** (list + upload form +
delete, admin-only, in the Settings modal or a server-settings surface) + an **emoji picker** that
inserts `:name:` into the composer (extend the existing reaction quick-palette pattern). Delegate the
impl; review + browser-QA + AI-vision. That completes the custom-emoji feature.

**Loop-process note (context):** 10 ticks; delegation kept this one lean-ish. Still not forcing a clear
(re-arm fragile); the <20% auto-clear remains the net. Continue delegating big slices.

## 2026-06-17 (tick 152) — custom emoji COMPLETE (slice 3a manager UI)

Shipped the admin emoji manager (list/upload/delete in the members/server-settings panel) with live
cache invalidation (`:name:` resolves immediately after a UI upload — no reload). Delegated impl;
reviewed (admin-gating, multipart, cache refresh) + independently verified (build, vitest 39/39) +
AI-vision (Discord-like, integrated) + prod-verified. **Custom emoji is now done end-to-end across 3
slices** (150 backend → 151 render → 152 manager), each delegated, each reviewed+verified+shipped by me.

**3-slice delegation retro:** breaking a big feature into backend/render/manager slices, each
delegated to a fresh agent context while the main loop stays reviewer/verifier/shipper, worked
extremely well — shipped ~1400 lines of feature across 3 ticks without my context ballooning or a
single regression. This is the template for big features in a long session.

**Highest-value NEXT (rotate to a fresh area — emoji is done):**
- **Custom-emoji reactions** — natural extension: let users REACT with the custom emoji we now have
  (reactions currently store unicode strings; would need to carry an emoji id). Medium.
- **Video slice 2** (screen+camera coexist) — the remaining TOP-PRIORITY partial. Complex (WebRTC).
- **Emoji picker** (slice 3b) — minor convenience (insert `:name:` from a palette).
Pick per per-component rotation; emoji-reactions builds on fresh momentum, video advances TOP PRIORITY.

**Loop-process note (context):** 11 ticks; FEATURE-COMPLETE checkpoint (cleanest possible stopping
point). Delegation kept per-tick growth modest + no degradation, so continuing with the 30-min cadence
rather than a disruptive forced-clear; the <20% auto-clear net has room to fire between ticks if needed.

## 2026-06-17 (tick 153) — composer emoji picker (slice 3b); custom emoji FULLY complete

Shipped the composer 🙂 picker (insert `:name:` at the caret; shown only when the server has emoji;
reaction palette untouched). Delegated impl; reviewed caret logic + verified (build, vitest 39/39, full
QA, AI-vision) + prod. Custom emoji is now fully complete across 4 slices (backend/render/manager/picker).

**QA gap found this tick (do NEXT — Track 0):** the QA emoji FIXTURE is a degenerate 1px PNG, so the
inline emoji + manager + picker tiles render tiny/ambiguous in screenshots — I couldn't AI-vision-confirm
the *rendered* emoji unambiguously (picker tiles did visibly load, and the DOM assertions prove the
correct `img.emoji-inline src=/api/emoji/{id}`, so the feature IS correct — but the vision check is
weak). Fix: replace the QA emoji fixture with a clearly-visible small PNG (e.g. a solid 64×64 colored
square) so every emoji screenshot definitively shows whether emoji render to the eye. Small, high-value
(makes the AI-vision step meaningful for the whole emoji feature). This is the loop's "improve the QA
process itself" work.

**Highest-value NEXT (rotate off emoji):** (a) the visible-fixture QA fix above (quick, do first); then
(b) **video slice 2** (screen+camera coexist) — the remaining TOP-PRIORITY partial — or custom-emoji
reactions. Per-component rotation: emoji is done; video advances TOP PRIORITY.

**Loop-process (context):** 12 ticks; still trusting the wired ctx<20% auto-clear (purpose-built, fires
+ re-arms when actually low) over a mistimed forced-clear. Delegation keeps per-tick growth modest.

## 2026-06-17 (tick 154) — clarified emoji fixture is fine; spec'd video slice 2 (de-risk)

Two things: (1) Re-examined the emoji QA fixture concern from tick 153 — it's a NON-issue: `makePng`
builds a real visible 240×140 PNG (the picker tiles visibly render it), so the inline emoji DO render
(small blurple rect; my tick-153 read was a misinterpretation). No fix needed. Lesson: before queuing a
"fix", confirm the suspected defect is real — I almost spent a tick fixing a fixture that was already
correct. (2) Investigated the mesh video path (Explore agent) and wrote a concrete sub-sliced SPEC for
"screen + camera coexist" (video slice 2) — the last TOP-PRIORITY item.

**Judgment surfaced (not buried):** video slice 2 is NICHE (few users screen+camera at once) and the
HIGHEST-blast-radius change in the codebase (core voice mesh). Spec is ready, but I recommend either a
dedicated implementation tick OR deferring it for higher-ROI broadly-used parity — flagged in GOAL.md
for the owner. This respects the owner's TOP-PRIORITY list (advanced it via investigation+plan) while
making the risk/value tradeoff explicit rather than rushing a risky core-voice change.

**Highest-value NEXT:** custom-emoji REACTIONS — broadly used, natural extension of the just-completed
emoji feature, medium risk (reactions store unicode TEXT; add an optional emoji-id path). Recommend this
over video slice 2 unless the owner wants coexist. Also viable: role colors in member list + messages.

**Context:** running on the 1M-context model — 14 ticks is well within budget (the <20% auto-clear
hasn't fired because there's huge headroom). Earlier "heavy context" worry was overblown; continuing.

## 2026-06-17 (tick 155) — caught + fixed a 4-tick-old P1: emoji images never loaded; shipped reactions

Set out to ship custom-emoji reactions (done). While AI-visioning, my nagging "is that a broken image?"
doubt led me to ADD a `naturalWidth>0` QA assertion — which FAILED: the inline emoji image never
actually loaded. Root cause: raw `<img src="/api/emoji/{id}">` against an AUTH-GATED endpoint → 401
(img tags can't send the bearer token) → broken image, across the whole emoji feature for 4 ticks.
Fixed via a new `EmojiImg` fetch+blob component (mirrors `Avatar.tsx`, the established pattern), wired
into every emoji render site. naturalWidth>0 now PASSES; AI-vision shows the emoji as a visible image
(was broken). Shipped fix + reactions together; prod rollout verified.

**THE BIG QA LESSON (highest-value carry):** element-existence + src-attribute checks + ambiguous
AI-vision of tiny fixtures MISSED a total render failure for FOUR ticks. The product-level truth is
"does the image DECODE" — `naturalWidth>0`. Generalize: every auth-gated image the QA renders must be
LOAD-checked, and the client must use fetch+blob (not `<img src>`) for token-auth'd images.

**Highest-value NEXT (find the bug CLASS, not just the instance):** AUDIT every other auth-gated image
surface for the SAME 401-on-img-src bug — especially **message ATTACHMENTS** (inline images via
`/api/attachments/{id}`): does `AttachmentList` use raw `<img src>` (BROKEN) or fetch+blob? Add a
`naturalWidth>0` assertion to the attachment QA step; if broken, fix with the same EmojiImg/Avatar
pattern. Avatars are already fine (Avatar.tsx always used fetch+blob — which is why ONLY emoji broke).

**Process note:** trusting the "that looks off" instinct + converting it into an assertion (not a guess)
is exactly how the loop should self-correct. Don't sign off "renders fine" on an image without a decode check.

## 2026-06-17 (tick 156) — image-bug-class audit (clean) + desktop notifications

Audited every auth-gated image surface for the tick-155 401-on-img-src bug: avatars + attachments
ALREADY use fetch+blob AND already have naturalWidth decode-check QA; emoji was the sole offender
(fixed 155). Bug class contained + guarded everywhere — no code change. Refined lesson: the decode-check
rigor already existed; the emoji slices just didn't mirror it. (An audit that finds it's already-clean is
a valid, valuable tick — confirms no further instances without manufacturing churn.)

Then shipped desktop notifications (Web Notifications API, Rule-A graceful, opt-in via a new Settings →
Notifications tab): notify on DMs/@-mentions while the tab is backgrounded. Pure shouldNotify/mentionsMe
(21 vitest cases) + a real stub-based browser E2E (forced document.hidden, 2nd user @mentions → assert a
Notification was constructed). Delegated impl, reviewed (pure logic, regex-escape, graceful no-ops) +
verified + prod.

**Highest-value NEXT (rotate):** options, in rough ROI order — (a) **friends / friend requests /
blocking** (Discord core social; medium-big — new backend social graph + UI); (b) **server-level mute**
(small clean extension of per-channel mute — mute a whole server); (c) **role colors** (limited, roles
are fixed owner/admin/member); (d) **threads** (big). Lean friends OR server-level-mute next.

**Process note:** the loop is in a healthy groove — investigate (Explore agent, keep context lean) →
delegate impl → review the security/correctness bits myself → verify (gate + naturalWidth/decode +
AI-vision) → ship → prod-verify. The tick-155 P1 catch shows the verification depth is paying off.

## 2026-06-17 (tick 157) — user blocking, backend slice 1 (DM enforcement)

Shipped the block API (block/unblock/list) + SYMMETRIC DM enforcement via the central `CanAccessChannel`
gate (so block flows through open/send/react/history) + `CreateOrGetDM`→ErrBlocked + `ListDMs` filter.
Investigated first (Explore agent → slice plan), delegated impl, REVIEWED the central-gate change myself
(the kind='dm' branch only; server/global untouched), independently ran the blocking + regression tests
(DM/server access still pass — no collateral), and PROD-verified the live block flow (block→204,
/me/blocks lists, unblock→204, unauth→401, migration applied). Rule-15 adversarial cases live in the
integration tests.

**Carry — modifying a central security function:** the highest-care change of the epic was the one-branch
edit to `CanAccessChannel` (every channel access flows through it). The discipline that made it safe:
(a) scope the change to the DM branch ONLY, (b) keep an explicit regression guard test asserting
server-channel/#general access is unaffected, (c) review the exact SQL myself, (d) run the existing
DM+server access tests and confirm green. When touching a central gate, the regression guard is as
important as the new behavior.

**Highest-value NEXT — blocking slice 2 (client):** block/unblock button on the profile card + member
row (reuse the profile-card surface), a blocked-users list in Settings (a "Privacy" or extend the
account area), and HIDE/collapse a blocked user's messages in server channels (the slice-2 effect — hook
the message history/render: `Recent`/`SearchMessages` server-side filter OR a client collapse). Then the
friends/requests social graph is a separate later epic.

## 2026-06-17 (tick 158) — user blocking COMPLETE (client slice 2)

Shipped the client half: Block/Unblock on the profile card, a Settings → Privacy blocked-list, and
blocked authors' messages hidden in every channel (pure `visibleMessages` filter applied before the
divider/grouping pass; live WS auto-hidden). Two-author browser E2E proves block→hide→unblock→reappear;
vitest for the filter; AI-vision the card button + Privacy tab + the message actually vanishing.
**User blocking done end-to-end** (backend DM-enforcement slice 1 + client slice 2), prod-verified.

**Highest-value NEXT (rotate):** options — (a) **friends / friend requests** — completes the
relationships/social area started with blocking (shares the relationship concept; a real Discord surface:
send request → accept/decline → friends list → friends-only DM gating). Medium-big epic, backend-first.
(b) A **consolidation / "feels like Discord" polish + broad QA pass** across the ~15 ticks of new
surfaces (emoji, notifications, Privacy, profile card) to ensure cohesion — lighter, lower-risk. (c)
Threads or custom colored roles (big epics). Lean friends (natural continuation) OR a polish pass to
consolidate the feature burst.

**Process note:** the investigate→delegate→review-the-risky-bits→verify(gate+AI-vision+prod)→ship loop
is humming — 3 multi-slice epics (custom emoji, blocking) + 2 features (notifications) shipped cleanly
with the one P1 (emoji-load) caught and fixed by tightening the QA (naturalWidth). Verification depth is
the throughline.

## 2026-06-17 (tick 159) — consolidation + polish pass (GREEN: product is cohesive, no churn)

After ~5 rapid feature ticks (custom emoji, desktop notifications, user blocking), did a deliberate
consolidation/design-vision pass to serve the owner's "UI/UX Discord parity POLISH" north star: full QA
green (browser=0 realtime=0 voice=0 search=0), then AI-vision-reviewed the surfaces most prone to
post-burst rough edges — the DENSEST settings tab (Voice & Video: input/output device + input/output
volume + mic test + DSP + camera, all cleanly sectioned), the now-4-tab Settings rail
(Account/Voice/Notifications/Privacy — cohesive), and the mobile drawer. All clean + consistent (dark
Discord theme, uppercase section labels, consistent buttons/sliders/tabs). The recent features were each
AI-visioned clean as they shipped. **No P0/P1; deliberately NO commit (Rule 10 — don't manufacture
churn).** A green polish-confirmation tick after a feature burst is valid + disciplined.

**Cadence note:** kept ACTIVE (not widened) — there IS a queued parity backlog (this was a chosen
review tick, not genuine idleness), and I ship a feature next tick.

**Highest-value NEXT (decided plan, ROI-ordered for open-DM Opencord):**
1. **Group DMs** — clean extension of the existing 2-member DM model to N members (create-group flow,
   member add, naming); medium. Good ROI/scope.
2. **Custom colored roles** — every Discord server uses them; currently roles are fixed
   owner/admin/member. High value, big epic (role CRUD + assignment + color in member list/authors).
3. **Threads** — high parity value (heavily used), big/complex.
4. **Friends/requests** — a social surface, but lower functional ROI here (DMs are already open, so it
   gates nothing; blocking already provides privacy).
Lean GROUP DMS next (best ROI/scope), or colored roles if favouring a bigger visible win.

## 2026-06-17 (tick 160) — group DMs slice 1 (backend) shipped + rollout-verified

Advanced the tick-159 plan's #1 item (group DMs, best ROI/scope). Backend-first slice (mirrors the
blocking epic): generalized the 2-member DM to N members with NO schema change — the win was recognizing
`channel_members` + the per-channel WS hub were already N-member, so the work was purely loosening the
hardcoded `COUNT=2` assumptions (create/list/access) + a new `POST /api/dms/group`. Verified end-to-end
on a REAL running server (not just `go test`): sorted `users`, per-viewer `ListDMs`, member-200/
non-member-403, 404/400 guards, 1-other delegating to the idempotent 1:1 — then `railway up` +
rollout-proof (the new route returns 401 not 404, vs a genuinely-unknown path still 404).

**Highest-value loop improvement this tick (QA + process):**
1. **QA gap (will close in slice 2):** there is NO automated coverage of the N-member DM **fanout** —
   the integration test proves membership/access, and the live E2E proves the HTTP surface, but neither
   asserts that a message sent in a group is actually *received* by all N members over the WS. Slice 2
   should add a **3-client WS test** (A sends → B and C both receive) to `qa/` — the per-channel hub
   makes this free, but "free" is exactly the kind of thing that silently breaks later. This is the one
   real verification I could not run in-context this tick (stated per Rule 14).
2. **Process win to keep:** caught TWO env traps that could have produced a false-red/false-green —
   (a) a **stale `oc-local` dev server already on :8090** made the first E2E hit an OLD binary (the new
   route 404'd) → always pick a *known-free* port for loop E2E and confirm the build under test is mine;
   (b) **`GID` is a read-only special var in zsh** — assigning to it aborts the script. Cheap, recurring
   foot-guns; noting them so the next E2E tick uses `CID`/a free port from the start.

**Cadence:** shipped a real feature with a follow-on slice queued → stay ACTIVE (1800s).

## 2026-06-17 (tick 161) — group DMs slice 2 (client) shipped + rollout-verified

Completed core group DMs: a Discord-style "New Direct Message" chips modal (replacing the old double
`window.prompt`), group rendering across the DM list/header/welcome/composer via a pure, vitest-tested
`dm.ts`, and the 3-client WS fanout test I flagged last tick (closed). Full QA green incl. a new
create-group browser flow; AI-vision verified the modal + rendered group. Shipped + rollout-verified
(live bundle byte-identical, carries the new strings). **Component advanced: chat/UI** toward the
"polished, Discord-faithful" north star (a real modal, not a prompt; faithful group title/avatar).

**Highest-value loop improvement this tick (process — caught + fixed a real false-red):**
The browser QA passed but **realtime QA went red (RC2=1)** because `qa/realtime.mjs` still drove the
DM-create via the removed `window.prompt`. The lesson is a concrete loop rule, now internalized: **when a
UI affordance changes, grep ALL QA scripts (browser.mjs AND realtime.mjs AND voice.mjs) for the old
selector/flow before declaring done — not just the one you're editing.** The multi-suite QA caught it
(good), but I should have anticipated it from the diff. The fix migrated realtime to the modal flow
(1 chip → "Create DM"), and both suites are now green. Reinforces tick-160's env-trap lesson: the gate
is only trustworthy because it runs the REAL multi-client UI — a unit-test-only gate would have shipped
a broken DM-create flow.

**Coverage note:** group DMs now have backend (integration), realtime (3-client WS fanout), and browser
(create + render) coverage — well-covered. Least-tested area surfacing next: the group-DM *edge* paths
have no UI yet (add/remove member, leave) — but those are unbuilt, so not a gap yet. Next genuine
target: a colored-roles epic (tick-159 plan #2) or threads — both large; pick per ROI next tick.

**Cadence:** shipped a feature (core group DMs complete) → ACTIVE (1800s); next tick starts a new epic.

## 2026-06-17 (tick 162) — custom colored roles slice 1 (backend) shipped + rollout-verified

Started the tick-159 ROI #2 epic (colored roles). Key design decision that kept blast radius low:
treat colored roles as a COSMETIC layer (`server_roles` + `member_roles`) entirely SEPARATE from the
existing owner/admin/member permission tier (`server_members.role`) — so the proven permission/kick/ban
logic is untouched and the member-list grouping still works. The top-role color is surfaced on
`ServerMember.color` via one correlated subquery (Discord's highest-position rule). Full admin-gated
CRUD + assignment, validated (hex color + name length, Rule B/15). Verified end-to-end on a REAL server
(CRUD + color resolution + recolor + cascade + adversarial 403/400/404), then `railway up` + rollout-proof
(new route 401 not 404). **Component advanced: UI/parity** (groundwork for colored names — a near-universal
Discord feature).

**Highest-value loop improvement this tick (process — recurring E2E foot-gun, now a rule):**
The live E2E first run FAILED to fetch user ids: I assumed a `/api/me`-style id endpoint, but the id is
returned in the **register/login response** itself (`{token, user:{id,...}}`). Two ticks running, the
E2E scripting (not the product) has been the fragile part — tick 160 `GID` zsh var, tick 161 realtime
selector, tick 162 id source. **Rule for next E2E tick: derive identity from the auth response
(`.user.id`/`.token`) — never assume a separate `/me` lookup — and prefer non-reserved var names + a
known-free port.** The CRUD/adversarial assertions that DIDN'T need ids all passed first try, so the
product was right; the harness scripting was the cost. A small reusable bash helper (register→{tok,id})
would remove this entirely — noted for a future QA-tooling tick.

**Coverage note:** colored roles now have backend (integration) + live-E2E coverage. The gap is the
SAME as group DMs at this stage: no UI yet, so no browser/AI-vision coverage — that lands with slice 2.

**Cadence:** shipped a feature with slice 2 queued → ACTIVE (1800s).

## 2026-06-17 (tick 163) — custom colored roles slice 2a (client) shipped + rollout-verified

Completed core colored roles: a `RolesManagerModal` (create/recolor/delete with color picker + preset
swatches) from the members-panel server settings, per-member assignment via toggle chips in the
ProfileCard, and member names rendered in their top-role color across the member list + panel + profile.
One small backend tweak (ServerMember.roleIds) gave the client per-member assignment state. Full QA green
incl. a new flow that creates a role → assigns it → asserts the member name's inline color style; AI-vision
confirmed the manager, the colored name (3 surfaces), and the assigned chip. **Component advanced:
UI/parity** — colored names are a near-universal Discord feature, now live.

**Highest-value loop improvement this tick (the E2E-harness fix from tick 162 PAID OFF):**
Last tick I codified "derive identity from the auth response, use non-reserved vars + a known-free port"
after 3 ticks of E2E-scripting foot-guns. This tick had ZERO scripting mis-steps — the browser-QA roles
flow ran clean first try, AND I caught the assignment-UI-needs-per-member-state gap during DESIGN (added
`roleIds`) rather than after a failed E2E. The lesson held: **invest the design thought before writing the
QA, and the loop's own rules compound.** The remaining genuine gap is honest scope, not a miss:
**message-author coloring is NOT done** (a member's color shows in the member list/profile but NOT on their
chat messages) — that needs author role-color in the message history + WS payload (a backend change),
explicitly deferred to slice 2b and logged in GOAL.md so it isn't silently dropped.

**Coverage note:** colored roles now have backend (integration) + live-E2E + browser + AI-vision coverage
— well-covered for the shipped surface. The untested surface is exactly the deferred one (message-author
color), which has no code yet.

**Cadence:** shipped a feature with slice 2b queued → ACTIVE (1800s).

## 2026-06-17 (tick 164) — colored roles slice 2b (message-author coloring) — FEATURE COMPLETE

Closed the honest gap from tick 163: message authors now render in their top-role color (member list +
profile already did). `Message.authorColor` joined into the read paths via a shared `authorColorSQL`
subquery + an `authorColor()` helper on the live Save/Edit paths; the client tints the author at the head/
search/pins sites. Colored roles is now FULLY complete. Component advanced: **UI/parity**.

**Highest-value loop improvement this tick (a real QA bug I found + fixed, twice):**
The new browser-QA step failed TWICE before passing — and each failure taught a reusable rule about
**testing realtime-fetched UI**:
1. **Re-selecting the SAME channel is a no-op** → no WS reconnect → no history refetch, so a value that
   only changes server-side (the author color, computed at fetch time) won't update. Fix: force a real
   channel CHANGE (hop via #general) to trigger the refetch. **Rule: to verify a server-computed field
   updated, navigate AWAY and BACK, never re-click the current channel.**
2. **My step closed the members panel mid-flow and broke the NEXT panel-dependent steps.** Fix: restore
   the panel before yielding. **Rule: a QA step must leave the app in the state the next step assumes —
   if you change context (close a panel, switch channels), restore it (or the step belongs at the end).**
These are exactly the kind of bug the real-UI gate exists to catch — a unit test would never have surfaced
either. The backend was correct first try (integration test green); the cost was entirely in faithfully
exercising the *rendered, realtime* UI.

**Coverage note:** colored roles is now fully covered (backend integration + live-E2E + browser create/
assign/colored-name/colored-message + AI-vision). Next epic (per ROI): threads (big), or a polish/
hardening rotation since 4 feature epics shipped in a row (group DMs, blocking earlier, colored roles).

**Cadence:** shipped a feature; colored roles complete, no unchecked P0/P1 left in the active epic →
this is effectively a clean SHIP. Still ACTIVE (1800s) — a queued parity backlog remains (threads, etc.).

## 2026-06-17 (tick 165) — security hardening rotation (Rule 15) on the new v0.6/v0.7 surfaces

After 5 feature ticks (group DMs ×2, colored roles ×3), did a deliberate adversarial pass instead of
starting another feature — the "security → hostile-input-proof" component was furthest from its bar
(brand-new input surfaces). Attacked group DMs (`/dms/group`) + colored roles (`/custom-roles`,
assign/unassign) on a LIVE server: authz bypass, cross-server IDOR, member-cap bypass, oversized/
malformed/injection/CSS-injection input, XSS role name, non-member read/post. **All repelled — no vuln.**
The surfaces are hostile-input-proof BY DESIGN (identity from the JWT not the payload; IDOR closed by
WHERE-scoped queries `WHERE id=$role AND server_id=$path`; hex/length-validated input; 4–64 KiB-bounded
bodies; React-escaped output). Encoded the probes as permanent route-level tests
(`TestCustomRolesAuthorizationIntegration` + `TestGroupDMAuthorizationIntegration`) — the HTTP-layer
coverage these endpoints lacked. Component advanced: **security**.

**Highest-value loop improvement (process rule, now codified):**
The real gap wasn't a vuln — it was that new endpoints shipped with STORE-level tests but no ROUTE-level
(HTTP auth/IDOR) tests, so the security boundary was only verified by ad-hoc curling at ship time, not a
permanent regression. **New rule: when a slice adds an HTTP endpoint, add its route-level `wantStatus`
auth/IDOR test (401/403/404 boundaries) in the SAME tick — don't defer the HTTP-layer security coverage.**
Had I followed this during slices 1/2a, this tick's gap wouldn't have existed. Also reinforced tick-162's
E2E lesson: two probe "failures" this tick were curl-harness artifacts (multipart reads `channelId` as a
FORM field not `?channel=`; `-F @/etc/hostname` doesn't exist on macOS → 000), not product bugs — always
confirm an unexpected status is a real reject (did the side effect happen?) before calling it a finding.

**Coverage note:** the new surfaces now have store + route + live-adversarial coverage. Least-tested area
next: WS frame fuzzing (oversized/garbage/wrong-type frames) beyond the existing checks, or start threads.

**Cadence:** shipped real coverage (no runtime change → no deploy, Rule 10). Backlog remains → ACTIVE 1800s.

## 2026-06-17 (tick 166) — full-product QA/AI-vision pass (green) + threads backend slice 1 shipped

Two tracks. **Track 0:** on the every-3rd-tick cadence (last tick was test-only), ran the full browser QA
(green) and AI-vision-reviewed a broad sample — mobile drawer (group DM glyph clean), mobile message view
(XSS escaped, mentions/replies/attachments/code render), member list (role badges + typing indicator),
colored names — **all cohesive, no P0/P1**. A valid green polish-confirmation after the feature burst.
**Track 1:** started the biggest remaining parity gap — **threads** — backend slice 1. The design win
(same as group DMs): a thread = `kind='thread'` channel with `parent_id` that copies the parent's
server_id, so access/posting/history/WS-fanout reuse the existing channel-id-scoped infra with ZERO new
message code. Only real subtlety caught during design: threads carry a name + server_id, so they'd collide
with the per-scope channel-name UNIQUE indexes — recreated those to exclude `kind='thread'`. Full
integration + live E2E (incl. adversarial 403/400) + rollout-verified. Component advanced: **UI/parity**.

**Highest-value loop improvement (the recurring-design-pattern is now explicit):**
Three epics in a row (group DMs, colored roles assignment, threads) shipped fast BECAUSE the data model
reused an existing N-scoped table instead of inventing a new mechanism — group DMs reused `channel_members`,
threads reused the channel/message/hub infra. **Codified design heuristic for new features: before adding a
new table/mechanism, ask "can this be a new `kind`/row on an existing N-scoped table?" — the channel +
channel_members + per-channel-hub primitives already cover DMs, groups, and threads for free; reusing them
keeps each feature a thin slice and inherits all the access/realtime/test coverage automatically.** This is
why threads slice 1 touched only 6 files with no new message/WS code. Also re-confirmed the schema-migration
discipline: a `DROP INDEX IF EXISTS` + recreate is needed when CHANGING an existing partial-unique-index
predicate (CREATE ... IF NOT EXISTS alone won't update a live index).

**Coverage note:** threads slice 1 has store + route-implicit + live-E2E coverage; the gap is a 3-client WS
thread-fanout test + browser coverage — both land with slice 2 (the thread UI).

**Cadence:** shipped a feature with slice 2 queued → ACTIVE (1800s).

## 2026-06-18 (tick 167) — threads slice 2 (client) shipped — THREADS MVP COMPLETE

Completed threads: a 🧵 threads panel (header button, mirrors the pins panel) with create + open, a
thread message-hover action, and a thread view (the message view reused on the thread's channel id) with
a "← 🧵 name" header + back-link. Plus the 3-client WS thread-fanout test from the plan. Full QA green
incl. a new create/open/post/list flow; AI-vision verified the view + panel. Threads (the biggest
remaining parity gap) is now an end-to-end MVP — create, open, chat, realtime. Component: **UI/parity**.

**Highest-value loop improvement (the channel-id-reuse strategy is now PROVEN across the stack):**
Threads slice 2 was a thin client slice ONLY because slice 1 modeled a thread as a channel — so the
client reused `selectChannel` (WS reconnect + history), the message view, the composer, and the message
broadcast with ZERO new realtime/message-render code. The whole client slice was: 2 api helpers, a panel
(copied from pins), and `activeThread` tracking for the header name (the one thing a thread needs that a
listed channel gets for free). **This validates the tick-166 heuristic end-to-end: model a new feature as
a new `kind`/row on the channel/`channel_members`/hub primitives and BOTH the backend AND the client
become thin slices.** The only thread-specific client cost was that threads aren't in any channel list,
so the header name needs separate tracking — worth noting as the general "reused-channel" tax (also true
for group DMs, which track `activeDM`). 

**Coverage note:** threads now have store + route + 3-client-WS-fanout + browser + AI-vision coverage —
fully covered for the MVP surface. Untested = the deferred slice-3 surface (anchored threads / system
message / unread counts), which has no code yet.

**Cadence:** shipped a feature; threads MVP complete. No unchecked P0/P1 in the active epic, but the
parity backlog still has items (voice channels as entities, audit log, role hoisting). ACTIVE (1800s).

## 2026-06-18 (tick 168) — threads slice 3 (message-anchored, discoverable) shipped

Closed the real usability gap in the threads MVP: threads were only reachable via the header panel.
Now a thread can be anchored to the message it was started from (the "thread" hover action), and that
message shows a clickable 🧵 chip — Discord's discoverability model. Reused the proven reply-validation
pattern (anchor must be a non-deleted message in the parent, else dropped — Rule B/C) and the existing
read-path JOIN approach (same shape as the authorColor subquery from slice 2b). Full QA green incl. a
start-from-message → chip → open flow; AI-vision verified the chip. Component: **UI/parity**.

**Highest-value loop improvement (a concrete reuse pattern is now a named tool):**
This slice added a per-message derived field (`threadId/threadName`) by LEFT-JOINing the read paths —
EXACTLY the same mechanism as `authorColor` (slice 2b). Two ticks apart, the same "annotate every message
with a derived field via a join/subquery in Recent+Pins+Search" pattern. **Named heuristic for future
per-message metadata (thread refs, author color, future: reaction-of-the-day, pin-count, etc.): add the
field to Message, JOIN/subquery it into the THREE read paths (Recent, PinnedMessages, SearchMessages) +
set it on the live Save/Edit path if it must appear instantly; the WS history reuses Recent for free.**
Knowing the read paths are exactly those three (and the live paths are Save*/Edit) makes each such field a
mechanical, low-risk change. Logged so the next per-message field doesn't re-discover the surface.

**Coverage note:** threads now fully covered (store + anchor + route + WS-fanout + browser create/open/
anchor-chip + AI-vision). Threads is feature-complete for parity; remaining thread items (system message,
unread counts, archive) are niche polish. Next epic candidates: voice channels as entities, role hoisting.

**Cadence:** shipped a feature; threads complete. Parity backlog remains → ACTIVE (1800s).

## 2026-06-18 (tick 169) — role hoisting shipped (colored roles fully complete)

Completed colored roles with Discord's role hoisting (member list grouped by hoisted role). Key design
choice that made it LOW-RISK on a tested surface: ADDITIVE — hoisted-role members get pulled into role
sections ABOVE the existing Admins/Members grouping, so with no role hoisted (the default + every existing
server) the member list renders byte-identically to before. No backend `ListServerMembers` change: the
client already had `member.roleIds` + the roles list, so it computes the grouping itself. AI-vision
confirmed the "QA-MOD" hoisted section. Considered voice-channels-as-entities first but deferred it — the
defining feature (live participant list) needs persistent voice-presence in the HUB (the single-goroutine,
most-concurrency-sensitive code), which deserves a dedicated tick, not a quick add.

**Highest-value loop improvement (a concrete QA-harness rule, hit live this tick):**
The browser-QA `.check()` on the hoist checkbox FAILED: "Clicking the checkbox did not change its state."
The cause: it's a CONTROLLED checkbox whose `checked` is driven by an async server round-trip (toggle →
PATCH → refetch → re-render), so the state doesn't flip synchronously on click — and Playwright's
`.check()` asserts a synchronous flip. **Rule: for a controlled input whose value updates via an async
server round-trip, use `.click()` (which doesn't assert the resulting state) + an explicit
`:checked`/value `waitFor`, NEVER `.check()`/`.fill()`-with-implicit-assert.** This is the input-side
analogue of the tick-167 "navigate away+back to refetch a server-computed field" rule — both stem from
the same root: client state that only reflects truth after a server round-trip. Logged so the next
async-controlled-input QA doesn't re-discover it.

**Coverage note:** colored roles now fully covered (create/assign/recolor/delete/cascade + colored names
in 3 surfaces + hoisting + route-level security from tick 165). Genuinely complete. Next epic candidate:
voice channels as entities (needs the hub voice-presence work) or audit log.

**Cadence:** shipped a feature; colored roles complete. Parity backlog remains → ACTIVE (1800s).

## 2026-06-18 (tick 170) — voice presence shipped (audio component advanced)

After ~10 UI/chat ticks, rotated to the AUDIO component (furthest from its north star per the owner rule).
Shipped voice presence: the hub tracks who's in the voice call per channel and broadcasts it, so a channel
shows a live "🔊 N in voice" indicator without joining. Key correctness property: the voiceMembers map is
mutated ONLY on the hub's single Run goroutine (the same lock-free discipline as the client set) — every
mutation point (voice-join/leave event, unregister, evict) is inside Run, and the HTTP-safe query pattern
(OnlineUserIDs) is reserved for slice 2. Verified with a real two-client WS test (join→presence-includes-A,
leave→drops) + a 3-client browser E2E (B, not in the call, sees "1 in voice") + AI-vision.

**Highest-value loop improvement (a discovery, not a process gap):**
While picking the work I found the AUDIO component is FAR more advanced than the loop's recent focus
implied — `web/src/sfu.ts` already has a full LiveKit SFU transport (stack-guardian APPROVED 2026-06-14,
opt-in via OPENCORD_SFU_URL), with active-speaker selection, and mesh↔SFU both work E2E. The scale north
star is largely BUILT; what was missing was the everyday UX around voice (who's in a call), which is what
this tick added. **Lesson for the loop's component rotation: "furthest from north star" must be judged from
the CODE, not from which component the recent ticks happened to touch — I'd mentally ranked audio as
"behind" when its hard scale-problem was already solved and its real gap was presence/UX.** Re-read the
component's GOAL.md NORTH STAR note + grep its code before assuming it's the laggard.

**Coverage note:** voice presence has WS-unit + 3-client-browser + AI-vision coverage. The untested next
surface is the cross-channel sidebar presence (slice 2) which needs the HTTP query — not built yet.

**Cadence:** shipped a feature; voice-presence slice 1 done with slice 2 queued → ACTIVE (1800s).

## 2026-06-18 (tick 171) — voice presence slice 2 (cross-channel sidebar 🔊 badges) shipped

Extended voice presence from the active-channel header (slice 1) to the WHOLE sidebar: a 🔊 N badge on
each server channel with a call. The architectural constraint was real — the client's WS is scoped to ONE
channel, so it can't see other channels' calls live; resolved with an HTTP query (lock-free
`Hub.VoiceMembersFor`, mirroring `OnlineUserIDs`) the client polls per server (15s) + refreshes instantly
on any voice-presence WS event near you. Verified the hub query inside the WS test, the route auth via the
httpapi suite, AND the visual badge via a clever browser-QA trick: a raw WS opened as the logged-in owner
(token from `localStorage['opencord.token']`) sends voice-join on the server channel → badge appears →
close the WS → badge clears. Component advanced: **audio/UI**.

**Highest-value loop improvement (a reusable browser-QA technique, now named):**
Testing realtime presence in the browser usually needs a SECOND participant, which is heavy. The trick
that made this tick's QA tractable: **drive a raw `WebSocket` from inside `page.evaluate` using the
logged-in user's own token (`localStorage['opencord.token']`), kept open on `window.__qaVoiceWS`, to
simulate a second presence-bearing connection — then assert the UI reacts, and close it to assert the
teardown.** This is far lighter than launching a second browser context, works for any WS-driven state
(voice presence, typing, future read-receipts), and tests BOTH the appear AND the clear-on-disconnect
paths. Logged as the go-to pattern for "the UI should react to another connection's realtime event."

**Coverage note:** voice presence now has WS-unit + route-auth + browser-visual (appear+clear) + AI-vision.
Well-covered. Next: dedicated kind='voice' channels (slice 3) reuse this presence query under each channel.

**Cadence:** shipped a feature; slice 3 queued → ACTIVE (1800s).

## 2026-06-18 (tick 172) — voice channels as entities (v0.9 slice 3a, backend) shipped

Made a dedicated voice channel a first-class `kind='voice'` server channel — the backend foundation
for Discord's core 🔊 click-to-join channel (slice 3b). Lowest-blast-radius design: a new
`CreateServerChannelOfKind` (existing `CreateServerChannel`/`InCategory` keep their signatures and
delegate with "public"); `ListServerChannels` surfaces `kind` but normalizes `'public'→""` so every
existing text channel's JSON stays byte-identical (only voice carries `"kind":"voice"`). A blast-radius
guard sub-agent (Rule 18) confirmed PASS before the edit: no Go consumer of `ListServerChannels` reads
`kind` (all only pull `.ID`), and the client's `Channel.kind` is already optional. Verified store + route
integration tests, then **live E2E on the deploy** (201 voice / unchanged text / 400 bad-kind / 403
non-admin) — proving the rolled-out binary really carries the new validation (the old build had no `kind`
field and would have 201'd `kind=stage`).

**Highest-value loop improvement (a QA gap to close next tick, now named):**
This was a backend-only slice, so it shipped WITHOUT a browser/AI-vision pass — correct for this tick
(there's no UI yet), but it leaves a standing rule for slice 3b: **the browser QA must gain a
create-AND-join-a-voice-channel flow.** Concretely, `qa/browser.mjs` should, in slice 3b: (1) create a
channel via a UI control that sends `kind='voice'` (the create-channel modal needs a type picker), (2)
assert the sidebar row renders a 🔊 (not #) icon, (3) click it → assert the voice bar appears and the
participant list shows self, (4) AI-vision the rendered voice-channel row + participant list. Until 3b
lands that UI, the only coverage for voice channels is the Go integration tests + live API E2E — which
is the right floor for a dormant-but-ready backend, but is explicitly NOT enough once the UI exists.
Logged so 3b can't ship the UI without its browser+vision coverage (the GAP-026 result-region lesson:
a feature isn't QA-covered until the rendered RESULT is exercised by eye, not just the API).

**Coverage note:** voice-channel CREATE/LIST now has store-integration + route-integration + live-API
adversarial coverage. The CLICK-TO-JOIN + participant-list-beneath UI is uncovered until slice 3b.

**Cadence:** shipped a feature; slice 3b queued → ACTIVE (1800s).

## 2026-06-18 (tick 173) — voice channels in the client (v0.9 slice 3b, UI) shipped

Made `kind='voice'` channels visible + usable: a "+ voice" create button, a 🔊 sidebar row with
participants listed beneath it (Discord-style), and a centered join-to-talk main view (composer +
message list hidden) with Join/Disconnect reusing the existing in-call voice bar. A blast-radius guard
(Rule 18) confirmed PASS before shipping: `channelButton` now returns a wrapper `<div>` for voice
channels but the bare `<button>` for text (unchanged), the composer/intro/message-list gates only add
`&& !activeChannelIsVoice` (false for DMs/threads/text), and no test asserts `.server-channel` is a
direct button. Drove the REAL UI via browser QA (create → 🔊 row → join view → Join with the fake mic →
in-call voice bar → Disconnect) and **AI-vision-graded three screenshots** — the join view, the in-call
view, and a sidebar element-shot showing the participant beneath the 🔊 row. All clean, no P0.

**QA process improvement this tick (the meta-QA, owner's top Track-0 ask):** grew `qa/browser.mjs` with
a full **create-AND-join-a-voice-channel** interaction flow — exactly the standing rule slice 3a logged
("3b must not ship the UI without its browser+vision coverage"). The flow asserts not just that the row
renders but that the RESULT is right by eye: the join view shows, the composer is GONE (`.composer`
count === 0 — a negative assertion the harness didn't have before), joining surfaces the in-call bar,
and disconnecting returns to Join. This extends the GAP-026 result-region discipline to a brand-new
interaction (join/leave a call) and adds element-shots of the result region for vision grading.

**Highest-value loop improvement (a QA gap the vision pass surfaced, now logged):** AI-vision caught a
P1 the automated checks missed — the channel HEADER still shows `#` + text-channel actions (pins,
threads, edit-topic, make-read-only, slowmode, search) for a voice channel, which are inapplicable.
This is the vision pass earning its keep: text+DOM assertions all passed, but the rendered header is
off for a voice channel. Logged as slice 3c. Next tick should ALSO add a browser assertion that a voice
channel's header hides those text-only actions, so the fix is regression-guarded.

**Coverage note:** voice channels now have store+route integration (3a) + a full browser create/join/
leave flow + AI-vision (3b). Uncovered: the header-polish state (3c) and auto-join-on-click (3d).

**Cadence:** shipped a feature; slice 3c queued → ACTIVE (1800s).

## 2026-06-18 (tick 174) — voice-channel header polish (v0.9 slice 3c) shipped

Closed the P1 the slice-3b AI-vision pass surfaced: a voice channel's header showed `#` + text-channel
actions (make-read-only, slowmode, edit-topic, pins, threads, message search) that don't apply to a
call. Now the brand shows `🔊 <name>` and those actions (plus the redundant header Join-voice/N-in-voice
buttons) are hidden, all behind `!activeChannelIsVoice` (text/DM/thread headers byte-identical). This is
the vision→fix→regression-guard loop closing: the P1 vision found last tick became this tick's fix +
a browser assertion so it can't silently return.

**QA process improvement this tick:** grew `qa/browser.mjs` with a header regression guard — on the open
voice channel it asserts the brand contains 🔊 AND that `.readonly-toggle/.slowmode-edit/.topic-edit/
.pins-open/.threads-open/.search-form` are all absent (count===0). Negative assertions ("this chrome is
GONE") are a class the harness was thin on; they're how you regression-guard a "hide X in context Y" fix.

**Highest-value loop improvement (a QA-robustness gap, logged for next tick):** the in-call screenshot
this tick did NOT show the voice roster beneath Disconnect (last tick it did) — a timing race between
the voice-join and the ~15s `serverVoice` presence poll: the screenshot fired before presence
round-tripped. The feature is fine (3b proved it), but the QA evidence is non-deterministic. FIX next
tick: before the in-call screenshot, `waitFor` the `.voice-channel-roster-item` (or assert it shows
self) so the roster is deterministically present — turning a flaky visual into a real end-to-end
presence assertion inside the voice-channel view (stronger than a screenshot, and stable).

**Coverage note:** voice channels now have store+route (3a) + create/join/leave browser flow (3b) +
header regression guard (3c) + AI-vision. Gap: the in-call ROSTER inside the voice view isn't asserted
yet (the timing-race fix above closes it). Auto-join-on-click (3d) remains uncovered/unbuilt.

**Cadence:** shipped a fix; voice channels feature-complete → still ACTIVE (1800s), but the next tick
can rotate to another component (group-DM sub-slices, appearance polish, or audio→SFU north star).

## 2026-06-18 (tick 175) — voice-channel live roster + presence assertion (v0.9 slice 3d) shipped

Closed the QA-robustness gap I logged last tick — but at the ROOT, not by papering over it. The flaky
in-call roster screenshot was a symptom: the voice view listed participants from the ~15s polled
`serverVoice`, so the roster lagged a join by up to 15s. Fix: the open voice channel IS the active
channel, so its roster now reads the LIVE `voicePresence` (updated on every WS voice-presence event),
falling back to the polled map only until the live list arrives. The QA then became a real assertion:
`waitFor` the `.voice-channel-roster-item` containing the logged-in user → assert SELF is listed. This
turned a non-deterministic screenshot into an end-to-end presence check INSIDE the voice view (the one
part of voice channels that wasn't asserted) AND made the screenshot deterministic. Process lesson:
when a screenshot is flaky, ask whether the product is racy — fixing the data source beat adding a
longer test timeout.

**Highest-value loop improvement (next-tick QA gap, logged):** the voice-channel-view roster is only
SINGLE-client tested (self joins → self appears). The existing `qa/voice.mjs` does two-client MESH
roster, but the new voice-CHANNEL-VIEW roster (the `.voice-channel-roster` UI) isn't cross-client
tested — a second participant joining a voice channel should appear in the FIRST viewer's
voice-channel-view roster. Next QA growth: a two-client voice-channel-view assertion (reuse the raw-WS
second-presence trick from slice 2: open a raw WS as a second identity, voice-join the SAME voice
channel, assert the first client's `.voice-channel-roster` lists BOTH). This proves cross-client
presence in the new view, not just self.

**Rotation note (per-component excellence):** voice has had 4 consecutive ticks (3a–3d) and is now
complete + polished + well-covered. To avoid stagnating other components, NEXT tick should rotate —
highest-value candidates: a Rule-15 adversarial pass on the new voice-channel create/kind surface
(hostile kind, non-member join, oversized), a group-DM sub-slice (naming / add-remove-member / leave),
or the appearance-polish spacing sweep. Pick the one furthest from its north star.

**Cadence:** shipped a fix → ACTIVE (1800s).

## 2026-06-18 (tick 176) — Rule-15 hardening: voice channels are voice-only (v0.9 slice 3e) shipped

ROTATED component (voice/UI had 4 ticks → security this tick) with a Rule-15 adversarial pass on the
brand-new voice-channel surface — and it found a real hole. The UI hid the composer for voice channels
(slices 3b/3c), but the BACKEND never checked `kind` on the post path: `CanPostInChannel` gated only
post-policy + membership, so a hostile client could bypass the hidden composer and POST text to a
`kind='voice'` channel via the raw WS `message` frame or the REST attachment path. The messages were
stored + broadcast to people in the call but never displayed — a data-integrity inconsistency and an
unbounded-write abuse vector in a channel with no moderation UI. A second instance: `CreateThread` didn't
reject a voice parent. Fixed both at their chokepoints; voice channels are now voice-only at the data
layer, consistent with the UI.

Followed the full Rule-15 cycle (the discipline, not just "add a test"): (1) **reproduced the break
first** — wrote the guard, ran it RED (`Save` to a voice channel succeeded today); (2) fixed
`CanPostInChannel` (the single chokepoint BOTH `SaveReply`/WS and `SaveWithAttachments`/REST call, so
one change closes both); (3) **re-attacked on the LIVE deploy** with a raw Node WebSocket sending a
`{body}` frame to a voice channel → got `{"type":"error"}`, no broadcast, not persisted on reconnect =
BLOCKED; (4) proved legit use intact (text channels still post + thread); (5) the test
`TestVoiceChannelRejectsMessages` encodes the exploit in the gate.

**Highest-value loop improvement (a standing QA rule, logged):** the lesson is "UI-hides-X is not
backend-rejects-X." Whenever a slice HIDES an action for a channel/entity type in the UI (composer,
threads, pins, a button), the loop must also verify the BACKEND rejects that action via the raw
WS/REST — the UI is not an enforcement boundary (Rule B). Next-tick QA candidate: audit the other
"hidden in the UI for context Y" actions (read-only channels, DM-only flows, non-member views) for the
same UI-only-gate gap, and add raw-path adversarial probes where the backend is the only real boundary.

**Coverage note:** the voice-channel surface now has store+route (3a), UI flow (3b), header (3c), live
roster (3d), AND adversarial post/thread rejection with a live-verified exploit (3e). Well-hardened.

**Cadence:** shipped a security fix → ACTIVE (1800s).

## 2026-06-18 (tick 177) — leave a group DM (v0.2 DM sub-slice) shipped

ROTATED to UI Discord-parity (the owner's TOP PRIORITY): the highest unchecked item is Group DMs, whose
biggest functional gap was that you couldn't LEAVE one (a group DM you can't leave is a trap). Shipped a
"🚪 leave group" header action (groups only) → `POST /api/dms/{id}/leave`; remaining members refresh
live via a `dm-membership` WS broadcast; the leaver's socket is evicted. Mirrored the existing
server-leave/kick eviction pattern, so this reused proven realtime plumbing. Full vertical slice
(store + route + WS event + client + types + docs + test + QA), all additive (221 insertions, 0
deletions).

**The blast-radius guard earned its keep again (Rule 18):** it caught TWO breaks BEFORE the gate that I
would otherwise have hit — (1) the new `dm-membership` WS type wasn't in the client's `ServerEvent.type`
union → tsc TS2367; (2) the new route wasn't in the README API table → `TestREADMEAPITableMatchesRoutes`
would fail (the docs-sync guard). Fixed both pre-gate. Lesson reinforced: a new WS event type needs the
client union updated, and a new route needs README — both are enforced by tests, so the guard running
FIRST turns two red-gate cycles into zero.

**Highest-value loop improvement (a QA gap, logged):** the browser leave-group test is SINGLE-client
(the leaver leaves → gone from THEIR sidebar). The "remaining members see them gone LIVE via
dm-membership" path is covered by the store test + the WS broadcast logic, but NOT asserted in a
two-client browser flow. Next-tick QA candidate: in realtime.mjs (which already runs 2 contexts), add a
group-DM leave assertion — A+B+C in a group, A leaves, assert B's DM header member list updates live
(the dm-membership refetch) without a reload. This would prove the live-refresh path end-to-end through
the real UI, closing the one untested edge of this feature.

**Coverage note:** leave-group now has store-integration (group/1:1/non-member/unknown) + live-API E2E
(204 + leaver-drops/others-keep + re-leave 404) + single-client browser flow + AI-vision. Gap: the
remaining-members live refresh (two-client). Other group-DM sub-slices (naming, add/remove member,
stacked avatars) remain unbuilt.

**Cadence:** shipped a feature → ACTIVE (1800s).

## 2026-06-18 (tick 178) — two-client realtime test for leave-group live refresh

Closed the coverage gap I logged last tick: the "remaining members see the leaver gone LIVE" edge of
leave-group was only covered by the store test + the WS-broadcast logic, never asserted through two real
browsers. Added section 14 to `qa/realtime.mjs` — A creates a group (B + a third member C registered via
API), B opens it (→ connected to its WS), A clicks "leave group", and B (without reloading) sees A drop
from the group title LIVE via the `dm-membership` refetch (title goes "alice, carol" → "@carol", and the
"leave group" button correctly disappears since it's now a 2-member 1:1). AI-vision confirmed B's view.

**QA-hygiene lesson (process improvement, the real win this tick):** my first attempt put the new section
in the MIDDLE of realtime.mjs (right after the 1:1-DM test) and it broke two LATER, order-dependent
assertions ("reading everything clears B's tab badge" / "a muted channel does not badge the tab title")
— my new group DM left B with an unread/extra-channel state those tab-title checks didn't expect.
Diagnosed via the captured log (my own checks passed; the failures were downstream). FIX + rule: a new
multi-step realtime test that creates channels/DMs/unreads must run **LAST** in the two-client suite (or
fully reset state), so it can't perturb earlier order-dependent assertions. Moved the section to the end
→ full suite green (browser=0 realtime=0 voice=0 search=0). This is a reusable rule for extending any
stateful two-client QA file: append, don't insert.

**Coverage note:** leave-group is now covered at store + live-API + single-client browser + **two-client
realtime** + AI-vision levels — fully verified across every edge including the live remaining-members
refresh. Next group-DM work is feature (add/remove member, naming, stacked avatars), not coverage.

**Cadence:** real QA coverage added (no product change → no deploy) → ACTIVE (1800s); more group-DM
sub-slices queued.

## 2026-06-18 (tick 179) — group DM stacked member avatars (v0.2 UI polish) shipped

A contained, visible Discord-parity slice (deliberately low-risk given a long session): the DM-list group
row now shows a STACK of the first two members' avatars (`.dm-group-stack`, two 15px Avatars offset
top-left/bottom-right, ringed in the sidebar bg) instead of a single generic people-glyph, so a group
reads as "several people" at a glance. Frontend-only, reuses the existing Avatar component (img-or-
initials → each member a distinct coloured circle). Updated the browser-QA assertion that previously
checked the removed `.dm-group-avatar` glyph to assert `.dm-group-stack` + exactly 2 `.dm-stack-avatar`.
tsc/vitest/go green; browser QA green; AI-vision confirmed the stack in the sidebar; rollout-verified.

**Highest-value loop improvement (a consistency gap the vision pass surfaced, logged):** AI-vision caught
that while the SIDEBAR row now stacks avatars, the WELCOME-INTRO big icon (the 68px circle at the top of
an empty group conversation) still shows the generic 👥 emoji — a visible inconsistency now that the
sidebar is richer. It's a separate element (channel-intro-icon), so out of this slice's scope, but logged
as the next polish: render the same stacked avatars (or the members' avatars) in the intro icon for a
group DM. General rule reinforced: when you upgrade one representation of an entity (the sidebar group
icon), audit the OTHER places that entity renders (header, welcome, member panel) for the same upgrade.

**Coverage note:** group DMs now have create + render (now stacked avatars) + leave (full multi-level
coverage). Unbuilt: group naming, add/remove member; the intro-icon consistency follow-up.

**Cadence:** shipped a UI feature → ACTIVE (1800s).

## 2026-06-18 (tick 180) — group-DM welcome-intro stacked avatars (consistency follow-up) shipped

Closed the consistency P1 that LAST tick's AI-vision pass surfaced: the sidebar group row stacked the
first two members' avatars, but the 68px welcome-intro icon still showed a generic 👥 emoji. Now the
intro icon stacks the same two avatars (`.channel-intro-stack` / `.intro-stack-avatar`, the big-icon
counterpart of `.dm-group-stack`), so a group reads consistently across the sidebar and the welcome.
Browser QA asserts the intro shows exactly 2 `.intro-stack-avatar`; AI-vision confirmed; rollout-verified.

This directly executed the rule I logged last tick ("when you upgrade one representation of an entity,
audit its other render sites for the same upgrade") — vision found the gap → this tick closed it →
encoded a regression assertion. The find→fix→guard loop on a UI-consistency issue, in two ticks.

**Highest-value loop improvement (logged):** the remaining group-DM work is FUNCTIONAL, not cosmetic —
**add/remove member** (add someone to an existing group; remove/leave) and **group naming** (needs a
non-UNIQUE name column + a rename UI + a realtime relabel). Both are meatier backend+frontend+realtime
slices (the leave-group pattern is the template: store mutation → route → WS broadcast → client + a
two-client realtime test). These are best started on a FRESH context (this session is 9 ticks deep), so
next tick should either take ONE of them deliberately or rotate to another component's north star
(e.g. an audio/SFU reliability slice). Flagging the scope so the next tick picks intentionally.

**Coverage note:** group DMs are now create + render (sidebar + intro stacked) + leave (store + live-API
+ single & two-client browser + vision) — well-covered + visually consistent. Unbuilt: add/remove member,
naming.

**Cadence:** shipped a UI consistency fix → ACTIVE (1800s).

## 2026-06-18 (tick 181) — add a member to a group DM (v0.2 DM sub-slice) shipped

Completed group-DM membership management by mirroring leave-group: a "➕ add" header action adds a
member via `POST /api/dms/{id}/members`. In Discord you ADD people and only ever LEAVE yourself (no
"remove other"), so add+leave is the full set — group DMs are now functionally complete. Reused the
leave-group template (store mutation → route → WS notify → client + refetch), which made this a fast,
low-risk slice: store `AddGroupDMMember` with the full guard matrix (actor-member-first per Rule B,
group-only, 10-cap, already-member, blocked), a route that resolves the identifier + does BOTH a
channel broadcast (existing members) AND a `SendToUser` push (so the NEW member — not on the channel —
still sees the group appear live). Verified at store + blast-radius + browser + **live-API adversarial**
(204 / new-member-sees-it / 409 re-add / 403 non-member) + AI-vision levels.

The blast-radius guard + the proactively-added README row meant ZERO red-gate cycles this tick — I've
now internalized the two recurring gate-breakers (new WS type → client union; new route → README) and
handle them up front. That's the loop teaching me: a guard that catches the same class twice becomes a
pre-flight checklist item.

**Highest-value loop improvement (next-tick QA gap, logged):** add-member's REALTIME edge — "the added
member sees the group appear LIVE via the SendToUser dm-membership push, without reloading" — is proven
by the live-API E2E (dave's /api/dms lists it) but NOT through two real browsers. Mirror the leave-group
realtime test (now realtime.mjs section 14): A adds C to a group while B (a member) is viewing it and C
is on #general → assert BOTH B's title grows AND C's sidebar gains the group, live. Append it LAST
(the section-14 hygiene rule). This closes the last edge of group-DM membership the same way leave was
closed one tick after shipping.

**Coverage note:** group DMs are now create + render (stacked, consistent) + leave + add — fully built
and well-covered. Remaining (lower priority): group naming (schema + rename UI + realtime relabel).

**Cadence:** shipped a feature → ACTIVE (1800s).

## 2026-06-18 (tick 182) — two-client realtime test for group-DM add (SendToUser push)

Closed the realtime edge I logged last tick: add-member's "the added member sees the group appear LIVE,
without reload" — the genuinely novel part (a user NOT connected to the channel getting pushed a new DM
via SendToUser). Added realtime.mjs §15 with a THIRD browser context (D): A makes a fresh group (B + E,
E via API for a uniquely-matchable title), B opens it, a brand-new D logs in on #general, then A adds D
→ asserts BOTH B (viewing, via the channel broadcast) sees D in the title live AND D (on #general, via
the SendToUser push) sees the group materialize in their sidebar live — no reload. AI-vision confirmed
D's sidebar gained the group (stacked avatars and all). Both pass; full QA green.

This is the leave-group coverage cadence repeated: ship the feature with store + live-API + single-client
(tick N), then the two-client realtime edge (tick N+1). Group-DM membership management (add + leave) is
now covered at EVERY level — store, live-API adversarial, single-client browser, two-client realtime,
AI-vision. That's the bar for a "done" realtime feature; logging it as the template: a realtime mutation
isn't fully covered until a SECOND client proves the live propagation through a real browser.

**Test-harness technique reused + worth naming:** to test a push to a user who isn't on the mutated
channel, spin a SHORT-LIVED extra browser context inside the section (create → use → close), rather than
adding a permanent top-level context — keeps the new test self-contained and the harness lean. (Same
"append §N, don't insert" hygiene as §14.)

**Highest-value loop improvement (logged):** group-DM membership is complete + fully covered. The only
remaining group-DM item is **naming** (a non-UNIQUE name column on dm channels + a rename header action +
a realtime relabel push, like server-rename) — a backend+frontend+realtime slice for a future tick. Next
tick could take naming OR rotate to a different component's north star (e.g. an audio/SFU reliability
slice, or a Rule-15 adversarial pass on a not-recently-probed surface).

**Cadence:** real QA coverage added (no product change → no deploy) → ACTIVE (1800s).

## 2026-06-18 (tick 183) — Rule-15 security audit of upload/serve/WS surfaces + inline-allowlist guard

ROTATED to security (after a long UI/group-DM run) with a Rule-15 adversarial pass on the surfaces most
likely to harbor a hostile-input hole. Finding: the product is genuinely well-hardened AND behaviourally
tested across all of them — the prior security discipline paid off:
- **Attachment upload/serve**: files stored under an OPAQUE random key (client filename never a path →
  no traversal), `sanitizeFilename` for the display name, content-type SNIFFED (not trusted), size-bounded
  (8 MiB/file, 40 MiB/req, 10 files), served with `nosniff` + `inline` ONLY for a raster allowlist (else
  download), access-gated. Already tested: traversal-filename, HTML-not-inline, oversize, 403, 404.
- **Avatar upload**: image-allowlist-only, ≤2 MiB, JWT-scoped (can't set another's), nosniff.
- **WS frames**: `SetReadLimit(16 KiB)`, malformed-JSON dropped, body >4 KiB dropped, rate-limited.
  Already tested (serve_integration_test.go §519): non-JSON garbage, type-confused field, oversized →
  connection survives + a valid frame after still works.

The ONE real gap closed: the `inlineImageTypes` allowlist (which gates inline-vs-download for
attachments + avatars + emoji) had NO test on its membership — only behavioural payload tests. Added
`TestInlineImageAllowlistIsRasterOnly` (internal unit test): the allowlist must contain ONLY
png/jpeg/gif/webp; a scriptable type (image/svg+xml, text/html, *xml) fails it AT THE SOURCE. **Proved
it by mutation** — injected `image/svg+xml` → test FAILED with the actionable message → reverted → PASS.
This catches a future well-meaning "SVG is an image, add it to the allowlist" mistake that NO behavioural
test would catch (SVG sniffs to text/xml, so behaviour is unchanged — only the constant betrays the risk).

**Loop-process lesson (logged):** when a Rule-15 audit finds a surface already hardened + behaviourally
tested, the highest-value move isn't a redundant payload test — it's a guard on the security-critical
CONSTANT/invariant that the behavioural tests can't see (here: the allowlist membership). "Test the
invariant, not just the payload" — added to the security-pass playbook.

**Coverage note:** upload/serve/WS surfaces fully hardened + tested + now an allowlist-invariant guard.
Least-probed-next: search operator injection (LIKE-escaping), JWT expiry/tamper across endpoints, and the
voice signaling relay under a hostile peer — candidates for a future security tick.

**Cadence:** security guard added (no product runtime change → no deploy) → ACTIVE (1800s).

## 2026-06-18 (tick 184) — group DM naming, backend slice 1 (schema + rename route) shipped

After a security audit that found search ALSO hardened + tested (parameterized everywhere; the `%`-literal
wildcard-escaping is already a regression test), I advanced the owner's priority feature instead of
hunting non-existent gaps: group-DM naming. Took it BACKEND-FIRST (slice 1) — the proven cadence for a
schema-touching feature on a deep session — keeping it contained (~5 files, no frontend, no QA-selector
risk since unnamed groups render exactly as today). De-risked the schema change first: it's the EXACT
thread precedent (recreate the global name partial-unique index to also exclude kind='dm'), idempotent +
self-applied; verified it applies cleanly (the db package re-ran green) AND on prod (live rename
succeeded with no unique-constraint error, two groups can share a name). Full vertical-but-backend slice:
schema + store RenameGroupDM (full adversarial matrix) + DMChannel.Name + ListDMs + PATCH route +
dm-membership relabel + live E2E (rename→204, all members see it, clear→"", non-member→403).

**Loop-process note (logged):** when the highest unchecked priority item touches the SCHEMA, splitting it
backend-first (slice 1) lets you ship + verify the migration on prod BEFORE the frontend depends on it —
the migration is the riskiest part, so isolating + proving it live de-risks the whole feature. Same
cadence as voice channels 3a. Added to the playbook: "schema-touching feature → backend slice first,
verify the migration live, then the client."

**Highest-value next step (slice 2, logged):** the client — a "rename" group-DM header action (member-only,
prompt for the name, PATCH) + `dmTitle` uses `dm.name` when set (falls back to the member list when
empty). The realtime relabel already works (the dm-membership refetch covers it), and a two-client
realtime test would mirror §14/§15 (A renames → B sees the new title live). Slice 2 is pure frontend +
QA, no backend — a clean fresh-context tick.

**Coverage note:** group-DM naming backend has store-adversarial + live-API E2E. The client + its
two-client realtime relabel are slice 2. Group DMs after slice 2: create, render, leave, add, name — fully
complete.

**Cadence:** shipped a backend slice + migration → ACTIVE (1800s).
