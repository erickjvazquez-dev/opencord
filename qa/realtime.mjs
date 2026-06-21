// Opencord realtime QA — TWO browser contexts (two users) proving live fan-out
// that the single-client browser.mjs can't: user A acts, user B sees it WITHOUT
// reloading. This is the two-user realtime check in GOAL.md "## Now" (Rule 14):
//   • B sees A's message live  • B sees A's reaction live (count-only, not "mine")
//   • B reacts too → the count climbs to 2 for BOTH, live  • presence shows 2 online
//
// Assumes a running stack at QA_BASE_URL (default http://localhost:5173); boot one
// with qa/run.sh, which runs this after browser.mjs.
import { chromium } from 'playwright'
import { mkdir } from 'node:fs/promises'
import { join } from 'node:path'
import { makePng } from './fixtures.mjs'

const BASE = process.env.QA_BASE_URL || 'http://localhost:5173'
const SHOTS = process.env.QA_SHOTS || join(import.meta.dirname, 'qa-screenshots')

let failed = 0
const step = (s) => console.log('  → ' + s)
const check = (cond, msg) => {
  console.log((cond ? '  ✓ ' : '  ✗ FAIL: ') + msg)
  if (!cond) failed++
}
const rcount = async (msg) => (await msg.locator('.reaction .rcount').first().textContent())?.trim()

async function register(page, user) {
  await page.goto(BASE, { waitUntil: 'domcontentloaded' })
  await page.getByPlaceholder('username').waitFor({ timeout: 15000 })
  await page.getByRole('button', { name: 'No account? Register' }).click()
  await page.getByPlaceholder('username').fill(user)
  await page.getByPlaceholder('password').fill('hunter2')
  await page.getByRole('button', { name: 'Create account' }).click()
  await page.getByPlaceholder(/Message #/).waitFor({ timeout: 15000 })
}

async function main() {
  await mkdir(SHOTS, { recursive: true })
  const browser = await chromium.launch()
  const a = await (await browser.newContext({ viewport: { width: 1100, height: 820 } })).newPage()
  const b = await (await browser.newContext({ viewport: { width: 1100, height: 820 } })).newPage()
  for (const [who, pg] of [['A', a], ['B', b]]) {
    pg.on('pageerror', (e) => {
      console.log(`  [pageerror ${who}] ` + e.message)
      failed++
    })
  }
  // Per-page mutable prompt answers (set before an action that prompts); aDefault
  // captures a prompt's pre-filled value (e.g. the invite code A is shown).
  const ans = { a: '', b: '' }
  let aDefault = ''
  a.on('dialog', (d) => {
    if (d.type() === 'prompt') aDefault = d.defaultValue()
    d.accept(d.type() === 'prompt' ? ans.a : undefined)
  })
  b.on('dialog', (d) => d.accept(d.type() === 'prompt' ? ans.b : undefined))

  const sfx = String(Date.now()).slice(-7)
  const userA = 'alice' + sfx
  const userB = 'bob' + sfx

  step(`register two users (A=${userA}, B=${userB}) in #general`)
  await register(a, userA)
  await register(b, userB)

  // Presence — A must see B's join reflected live (2 online).
  step('presence reflects 2 online for A after B joins')
  await a.getByText(/2 online/).waitFor({ timeout: 8000 })
  check(await a.getByText(/2 online/).isVisible(), 'A sees "2 online" after B connects')

  // 1 — A sends a message; B sees it live (no reload).
  const body = 'live ping from A ' + sfx
  step("A sends a message → B receives it live")
  await a.getByPlaceholder(/Message #/).fill(body)
  await a.getByRole('button', { name: 'Send' }).click()
  await b.getByText(body).waitFor({ timeout: 8000 })
  await b.screenshot({ path: join(SHOTS, 'rt-01-b-sees-message.png') })
  check(await b.getByText(body).isVisible(), "B receives A's message in real time")

  // 1b — B replies: a DIFFERENT author must NOT group under A (the inverse of the
  // same-author grouping rule) — B's message keeps its own avatar.
  const reply = 'reply from B ' + sfx
  step('B replies → different author is not grouped (avatar shown for both)')
  await b.getByPlaceholder(/Message #/).fill(reply)
  await b.getByRole('button', { name: 'Send' }).click()
  await a.getByText(reply).waitFor({ timeout: 8000 })
  const aReply = a.locator('.message', { hasText: reply }).first()
  await a.screenshot({ path: join(SHOTS, 'rt-01b-different-authors.png') })
  check((await aReply.locator('.avatar').count()) === 1, "B's reply (different author) shows its avatar")
  check(
    !(await aReply.evaluate((el) => el.classList.contains('grouped'))),
    "B's reply is not grouped under A",
  )

  // 1c — Reply feature across two clients (LIVE broadcast): B replies to A's message;
  // A must see the new message carry a quoted preview of the original. This proves the
  // WS `message` broadcast carries the denormalized reply fields (replyToAuthor /
  // replyToBody) — not just single-client history (which the browser QA already covers).
  const bSeesA = b.locator('.message', { hasText: body }).first()
  step("B replies to A's message → A sees the quoted preview live")
  await bSeesA.hover()
  await bSeesA.getByRole('button', { name: 'reply' }).click()
  check(
    (await b.locator('.reply-bar strong').textContent())?.trim() === userA,
    'B\'s "Replying to" bar names A (the original author)',
  )
  const replyBody = 'replying to A live ' + sfx
  await b.getByPlaceholder(/Message #/).fill(replyBody)
  await b.getByRole('button', { name: 'Send' }).click()
  await a.getByText(replyBody).waitFor({ timeout: 8000 })
  const aSeesReply = a.locator('.message', { hasText: replyBody }).first()
  await aSeesReply.locator('.reply-context').waitFor({ timeout: 8000 })
  await a.screenshot({ path: join(SHOTS, 'rt-01c-reply-live.png') })
  check(
    (await aSeesReply.locator('.reply-context .reply-author').textContent())?.trim() === userA,
    'A sees the reply quote the original author (live broadcast carries replyToAuthor)',
  )
  check(
    (await aSeesReply.locator('.reply-context .reply-snippet').textContent())?.includes(body),
    'A sees the reply quote the original message body (live broadcast carries replyToBody)',
  )

  // 1d — @mention autocomplete: typing "@" + a partial offers matching usernames
  // active in the channel; Enter ACCEPTS the suggestion (inserts "@username ") rather
  // than sending. Both A and B have posted by now, so B is a candidate for A.
  step('@mention autocomplete: typing @ offers a matching user; Enter inserts it')
  const composer = a.getByPlaceholder(/Message #/)
  await composer.click()
  await composer.fill('hey @bo')
  const acMenu = a.locator('.mention-autocomplete')
  await acMenu.waitFor({ timeout: 8000 })
  check(
    (await a.locator(`.mention-option[data-mention-option="${userB}"]`).count()) > 0,
    `autocomplete suggests ${userB} for "@bo"`,
  )
  await a.screenshot({ path: join(SHOTS, 'rt-08-mention-autocomplete.png') })
  await composer.press('Enter') // accept the highlighted suggestion — must NOT send
  check(
    (await composer.inputValue()) === `hey @${userB} `,
    'Enter inserts "@username " into the composer',
  )
  check((await acMenu.count()) === 0, 'autocomplete closes after accepting')
  check(
    (await a.locator('.message', { hasText: 'hey @' + userB }).count()) === 0,
    'accepting the suggestion did NOT send the message',
  )
  step('the accepted @mention sends and renders as a highlighted chip for B')
  await a.getByRole('button', { name: 'Send' }).click()
  const bMention = b.locator('.message .body .mention', { hasText: '@' + userB }).first()
  await bMention.waitFor({ timeout: 8000 })
  check(await bMention.isVisible(), 'the sent @mention renders as a highlighted chip for B')

  // 1e — WebSocket auto-reconnect: a dropped socket (flaky network / sleep / a proxy
  // closing an idle connection) must re-establish so chat keeps working — the gap
  // behind "chat stops working / screen share never reaches the other person".
  step('WS auto-reconnect: A drops offline → comes back → still receives live messages')
  await a.context().setOffline(true)
  await a.locator('.dot.online').waitFor({ state: 'detached', timeout: 8000 }).catch(() => {})
  // A "Reconnecting…" banner appears once the drop persists past the grace window (iter 202).
  const bannerShown = await a
    .locator('.reconnect-banner')
    .waitFor({ timeout: 8000 })
    .then(() => true)
    .catch(() => false)
  check(bannerShown, 'a "Reconnecting…" banner appears while the socket stays down')
  await a.context().setOffline(false)
  const reconnected = await a
    .locator('.dot.online')
    .waitFor({ timeout: 20000 })
    .then(() => true)
    .catch(() => false)
  check(reconnected, 'A’s connection re-establishes after going back online')
  // …and the banner clears once the socket is back.
  const bannerGone = await a
    .locator('.reconnect-banner')
    .waitFor({ state: 'detached', timeout: 10000 })
    .then(() => true)
    .catch(() => false)
  check(bannerGone, 'the reconnecting banner clears after the socket recovers')
  const afterReconnect = 'after-reconnect ' + sfx
  await b.getByPlaceholder(/Message #/).fill(afterReconnect)
  await b.getByRole('button', { name: 'Send' }).click()
  check(
    await a.getByText(afterReconnect).waitFor({ timeout: 12000 }).then(() => true).catch(() => false),
    'A receives a live message sent after the reconnect (socket truly recovered)',
  )

  const aMsg = a.locator('.message', { hasText: body }).first()
  const bMsg = b.locator('.message', { hasText: body }).first()

  // 2 — A reacts 👍; B sees the chip + count live, and it is NOT "mine" for B
  // (the broadcast is count-only — viewerID 0 — so B's chip must not highlight).
  step('A reacts 👍 → B sees the chip live (count 1, not highlighted as mine)')
  await aMsg.hover()
  await aMsg.getByRole('button', { name: 'react' }).click()
  await a.locator('.emoji-picker').getByRole('button', { name: '👍' }).click()
  await bMsg.locator('.reaction').waitFor({ timeout: 8000 })
  await b.screenshot({ path: join(SHOTS, 'rt-02-b-sees-reaction.png') })
  check(await bMsg.locator('.reaction').isVisible(), "B sees A's reaction chip live")
  check((await rcount(bMsg)) === '1', 'B sees reaction count 1')
  check((await bMsg.locator('.reaction.mine').count()) === 0, "B's chip is not 'mine' (B didn't react)")

  // 3 — B reacts 👍 too; the count climbs to 2 for BOTH, live.
  step('B reacts 👍 too → count becomes 2 for both, B\'s chip becomes mine')
  await bMsg.locator('.reaction').first().click()
  await a
    .waitForFunction(
      () => document.querySelector('.message .reaction .rcount')?.textContent.trim() === '2',
      { timeout: 8000 },
    )
    .catch(() => {})
  await a.screenshot({ path: join(SHOTS, 'rt-03-a-count-2.png') })
  check((await rcount(aMsg)) === '2', 'A sees the count climb to 2 after B reacts (live)')
  check(await bMsg.locator('.reaction.mine').isVisible(), "B's chip becomes 'mine' after B reacts")

  // 3c — Attachments propagate live: A uploads an image in #general → B sees a NEW
  // image render inline WITHOUT reloading (the realtime broadcast path the
  // single-client browser QA can't prove). B fetches the bytes with B's OWN token
  // (access-gated). Count-based so a stale image already in history (the suite shares
  // a channel with browser.mjs) can't pass this — we require the count to GROW by one.
  step('A uploads an image → B sees a NEW image render inline live (no reload)')
  const bImgBefore = await b.locator('.message .attachment-image').count()
  const aImgBefore = await a.locator('.message .attachment-image').count()
  await a.waitForTimeout(3000) // rate-limit refill before the send
  await a.locator('.composer input[type=file]').setInputFiles({
    name: 'rt-pic.png',
    mimeType: 'image/png',
    buffer: makePng(220, 130, [235, 110, 75]),
  })
  await a.locator('.pending-file-name').waitFor({ timeout: 8000 })
  await a.getByRole('button', { name: /Send|Sending/ }).click()
  // A's own echo: its image count must grow by one.
  await a
    .waitForFunction(
      (n) => document.querySelectorAll('.message .attachment-image').length > n,
      aImgBefore,
      { timeout: 12000 },
    )
    .catch(() => {})
  check(
    (await a.locator('.message .attachment-image').count()) === aImgBefore + 1,
    'A sees its own uploaded image echo back',
  )
  // B receives the NEW image live over the WS broadcast (count grows by one).
  const bGrew = await b
    .waitForFunction(
      (n) => document.querySelectorAll('.message .attachment-image').length > n,
      bImgBefore,
      { timeout: 12000 },
    )
    .then(() => true)
    .catch(() => false)
  const bImg = b.locator('.message .attachment-image').last()
  await b.screenshot({ path: join(SHOTS, 'rt-03c-b-sees-attachment.png') })
  check(bGrew, "B sees a NEW image arrive live (WS broadcast carries the attachment)")
  check(
    bGrew && (await bImg.evaluate((el) => el.complete && el.naturalWidth > 0)),
    "B's copy of the new image decoded — fetched with B's own token (access-gated serve)",
  )

  // 4 — Direct messages: A opens a private DM with B and sends a message; B reloads,
  // finds the DM in their sidebar, and reads it. Proves the DM UI end-to-end.
  step('A opens a DM with B (+ New DM modal) and sends a private message')
  await a.getByRole('button', { name: '+ New DM' }).click()
  await a.locator('.group-modal').waitFor({ timeout: 4000 })
  const dmChip = a.locator('.group-chip-input')
  await dmChip.fill(userB)
  await dmChip.press('Enter')
  // One member → the modal's button reads "Create DM" and resolves to the idempotent 1:1.
  await a.locator('.group-modal').getByRole('button', { name: /Create DM/ }).click()
  await a.locator('.group-modal').waitFor({ state: 'detached', timeout: 8000 })
  const dmComposer = a.getByPlaceholder('Message @' + userB)
  await dmComposer.waitFor({ timeout: 8000 })
  check(
    (await a.locator('.brand .channel').textContent())?.includes('@' + userB),
    'A header shows @' + userB + ' for the DM',
  )
  const dmBody = 'private hello ' + sfx
  await dmComposer.fill(dmBody)
  await a.getByRole('button', { name: 'Send' }).click()
  await a.getByText(dmBody).waitFor({ timeout: 8000 })
  await a.screenshot({ path: join(SHOTS, 'rt-04-alice-dm.png') })
  check(await a.getByText(dmBody).isVisible(), 'A sees the message in the DM view')

  step('B reloads, opens the DM from the sidebar, and reads the private message')
  await b.reload({ waitUntil: 'domcontentloaded' })
  // Target the DM's sidebar channel button specifically. `getByRole('button', {name:
  // /userA/})` is too loose — once #general renders a reply whose preview is labelled
  // "jump to <userA>'s message", that reply-context button also matches userA's name and
  // strict mode sees two buttons (a render race). A reply-context button is not a
  // .channel-item, so scoping to .channel-item resolves to exactly the DM.
  await b.locator('.channel-item', { hasText: userA }).first().click()
  await b.getByText(dmBody).waitFor({ timeout: 8000 })
  await b.screenshot({ path: join(SHOTS, 'rt-05-bob-dm.png') })
  check(await b.getByText(dmBody).isVisible(), 'B opens the DM and reads the private message')

  // 5 — Servers across two users: A creates a server + channel, mints an invite, B
  // redeems it, lands in the channel, reads history, and receives a live message.
  step('A creates a server, a channel in it, and an invite code')
  ans.a = 'team ' + sfx
  await a.getByRole('button', { name: '+ New server' }).click()
  await a.locator('.server-name', { hasText: 'team ' + sfx }).waitFor({ timeout: 8000 })
  const srvChan = 'sc' + sfx.slice(-5)
  ans.a = srvChan
  await a
    .locator('.server-group', { hasText: 'team ' + sfx })
    .getByRole('button', { name: '+ channel' })
    .click()
  await a.getByRole('button', { name: new RegExp(srvChan) }).waitFor({ timeout: 8000 })
  aDefault = ''
  await a
    .locator('.server-group', { hasText: 'team ' + sfx })
    .getByRole('button', { name: 'invite' })
    .click()
  for (let i = 0; i < 50 && aDefault.length < 6; i++) await a.waitForTimeout(100)
  const inviteCode = aDefault
  check(inviteCode.length >= 6, 'A mints an invite code to share')

  const srvMsg1 = 'server msg one ' + sfx
  await a.getByPlaceholder(new RegExp('Message #' + srvChan)).fill(srvMsg1)
  await a.getByRole('button', { name: 'Send' }).click()
  await a.getByText(srvMsg1).waitFor({ timeout: 8000 })

  step('B redeems the invite → lands in the server channel → reads history')
  ans.b = inviteCode
  await b.getByRole('button', { name: 'Join server' }).click()
  await b.getByText(srvMsg1).waitFor({ timeout: 10000 })
  await b.screenshot({ path: join(SHOTS, 'rt-06-bob-server.png') })
  check(await b.getByText(srvMsg1).isVisible(), 'B joins via invite and reads the server history')

  step('A posts again → B sees it live in the shared server channel')
  const srvMsg2 = 'server msg two ' + sfx
  await a.getByPlaceholder(new RegExp('Message #' + srvChan)).fill(srvMsg2)
  await a.getByRole('button', { name: 'Send' }).click()
  await b.getByText(srvMsg2).waitFor({ timeout: 8000 })
  check(await b.getByText(srvMsg2).isVisible(), 'B receives a live message in the shared server channel')

  // 5b — Moderation: B posts, A (owner) deletes B's message via the delete button.
  step("B posts → A (owner) moderates it away → B sees [deleted] live")
  const modMsg = 'please moderate me ' + sfx
  await b.getByPlaceholder(new RegExp('Message #' + srvChan)).fill(modMsg)
  await b.getByRole('button', { name: 'Send' }).click()
  const aModRow = a.locator('.message', { hasText: modMsg }).first()
  await aModRow.waitFor({ timeout: 8000 })
  await aModRow.hover()
  await aModRow.getByRole('button', { name: 'delete' }).click() // confirm auto-accepted
  await b
    .locator('.message', { hasText: '[deleted]' })
    .first()
    .waitFor({ timeout: 8000 })
  check(
    (await b.locator('.message', { hasText: '[deleted]' }).count()) > 0,
    'owner moderates B\'s message → B sees it [deleted] live',
  )

  // 6 — Roles: A (owner) opens the members panel and promotes B (member) to admin.
  step('A opens members and promotes B to admin')
  await a
    .locator('.server-group', { hasText: 'team ' + sfx })
    .getByRole('button', { name: 'members' })
    .click()
  const bRow = a.locator('.member-row', { hasText: userB })
  await bRow.getByRole('button', { name: 'make admin' }).click()
  await bRow.locator('.role-badge.role-admin').waitFor({ timeout: 8000 })
  await a.screenshot({ path: join(SHOTS, 'rt-07-roles.png') })
  check(
    await bRow.locator('.role-badge.role-admin').isVisible(),
    'owner promotes B to admin via the members panel',
  )

  // 7 — Member list (Discord-style right sidebar): in a server channel it lists members
  // grouped by role. Close the on-demand panel first so it doesn't overlay the view.
  step('member-list sidebar shows server members grouped by role')
  await a.locator('.search-results-head .link', { hasText: 'close' }).click().catch(() => {})
  const ml = a.locator('.member-list')
  check(await ml.isVisible().catch(() => false), 'member-list sidebar shows in a server channel')
  check(
    (await ml.locator('[data-member]').filter({ hasText: userA }).count()) > 0,
    'member list includes the owner (A)',
  )
  check(
    (await ml.locator('[data-member]').filter({ hasText: userB }).count()) > 0,
    'member list includes B',
  )
  check((await ml.locator('.member-group-head').count()) > 0, 'members are grouped by role')
  // Presence: both A and B hold live sockets, so the member list shows them online
  // (green dot, data-online="1"). The list was last refreshed by the promote action
  // while both were connected.
  const bMlRow = ml.locator('.member-list-row', { hasText: userB })
  let bOnline = false
  for (let i = 0; i < 20 && !bOnline; i++) {
    bOnline = (await bMlRow.getAttribute('data-online')) === '1'
    if (!bOnline) await a.waitForTimeout(300)
  }
  check(bOnline, 'member list marks B online (presence dot) while B is connected')
  check(
    (await ml.locator('.member-list-row', { hasText: userA }).getAttribute('data-online')) === '1',
    'member list marks the owner (A) online while connected',
  )
  check(
    (await ml.locator('.presence-dot.online').count()) > 0,
    'an online presence dot renders in the member list',
  )
  await a.screenshot({ path: join(SHOTS, 'rt-09-member-list.png') })

  // 7b — Unread: while B is away in #general, A posts to the server channel → B's
  // sidebar shows an unread dot on that channel (poll-driven); opening it clears the
  // dot. (B is still a member here — the kick comes after.)
  step('A posts while B is in #general → B gets an unread dot, clears on open')
  await b.locator('.channel-list .channel-item', { hasText: 'general' }).first().click()
  await b.getByPlaceholder('Message #general').waitFor({ timeout: 8000 })
  const unreadPing = 'unread ping ' + sfx
  await a.getByPlaceholder(new RegExp('Message #' + srvChan)).fill(unreadPing)
  await a.getByRole('button', { name: 'Send' }).click()
  await a.getByText(unreadPing).waitFor({ timeout: 8000 })
  const bSrvChanBtn = b.locator('.channel-item.server-channel', { hasText: srvChan })
  let gotUnread = false
  for (let i = 0; i < 28 && !gotUnread; i++) {
    // poll is ~10s server-side; wait it out
    if ((await bSrvChanBtn.locator('.unread-dot').count()) > 0) gotUnread = true
    else await b.waitForTimeout(500)
  }
  check(gotUnread, 'B sees an unread dot on the server channel after A posts while B is away')
  await b.screenshot({ path: join(SHOTS, 'rt-12-unread.png') })
  // Tab badge: a plain unread (no mention) shows the "● Opencord" dot in B's tab title.
  check(
    /^●\s/.test(await b.title()),
    `a plain unread badges B's tab title with a dot (got "${await b.title()}")`,
  )

  // 7c — Mention: A @mentions B → B's indicator becomes a red mention badge (count).
  await a.getByPlaceholder(new RegExp('Message #' + srvChan)).fill('@' + userB + ' ping you')
  await a.getByRole('button', { name: 'Send' }).click()
  await a.getByText('ping you').waitFor({ timeout: 8000 })
  let gotMention = false
  for (let i = 0; i < 28 && !gotMention; i++) {
    if ((await bSrvChanBtn.locator('.mention-badge').count()) > 0) gotMention = true
    else await b.waitForTimeout(500)
  }
  check(gotMention, 'B sees a red mention badge after A @mentions them while away')
  check(
    ((await bSrvChanBtn.locator('.mention-badge').textContent()) ?? '').trim().length > 0,
    'the mention badge shows a count',
  )
  // Tab badge: a mention shows "(N) • Opencord" in B's tab title (the count, like Discord).
  check(
    /^\(\d+\)\s•\s/.test(await b.title()),
    `a mention badges B's tab title with the count (got "${await b.title()}")`,
  )
  await b.screenshot({ path: join(SHOTS, 'rt-12b-mention.png') })

  await bSrvChanBtn.click()
  await b.getByText(unreadPing).waitFor({ timeout: 8000 })
  await b.waitForTimeout(600)
  check(
    (await bSrvChanBtn.locator('.unread-dot').count()) === 0 &&
      (await bSrvChanBtn.locator('.mention-badge').count()) === 0,
    'opening the channel clears both the unread dot and the mention badge',
  )
  // The browser-QA seeds a global `pgseed` channel (history pagination) with 60 messages that B has
  // never opened — read it so the tab-badge assertions below see only THIS test's unread state, not
  // the seed's. Conditional: standalone realtime runs (no seed) just skip it.
  const bPgseed = b.locator('.channel-item', { hasText: 'pgseed' }).first()
  if ((await bPgseed.count()) > 0) {
    await bPgseed.click()
    await b.waitForTimeout(400)
    await bSrvChanBtn.click() // back to the server channel for the assertions + the mute test
    await b.waitForTimeout(400)
  }
  // Tab badge clears too once everything is read.
  check(
    (await b.title()) === 'Opencord',
    `reading everything clears B's tab badge (got "${await b.title()}")`,
  )

  // 7d — Channel mute: B mutes the server channel (B is viewing it), then goes to #general;
  // A posts → B gets NO unread dot/badge and the tab title stays clear (mute suppresses it).
  step('B mutes the server channel → A posts → no unread dot/tab badge for B')
  await b.locator('.channel-mute-toggle').click() // B is viewing srvChan → mutes it
  await b.locator('.channel-mute-toggle[data-muted="true"]').waitFor({ timeout: 8000 })
  await b.locator('.channel-list .channel-item', { hasText: 'general' }).first().click() // away
  await b.getByPlaceholder('Message #general').waitFor({ timeout: 8000 })
  const mutedPing = 'muted ping ' + sfx
  await a.getByPlaceholder(new RegExp('Message #' + srvChan)).fill(mutedPing)
  await a.getByRole('button', { name: 'Send' }).click()
  await a.getByText(mutedPing).waitFor({ timeout: 8000 })
  await b.waitForTimeout(12000) // wait out the ~10s unread poll — prove the dot never appears
  check(
    (await bSrvChanBtn.locator('.unread-dot').count()) === 0 &&
      (await bSrvChanBtn.locator('.mention-badge').count()) === 0,
    'a muted channel shows NO unread dot/badge even after a new message',
  )
  check((await b.title()) === 'Opencord', 'a muted channel does not badge the tab title')
  // Open it (still muted), then unmute via the toggle so later steps see normal state.
  await bSrvChanBtn.click()
  await b.locator('.channel-mute-toggle[data-muted="true"]').click() // unmute
  await b.locator('.channel-mute-toggle[data-muted="false"]').waitFor({ timeout: 8000 })
  check(
    (await b.locator('.channel-mute-toggle').getAttribute('data-muted')) === 'false',
    'unmuting the channel flips the header toggle back',
  )

  // 7h — Timeout (moderation): A (owner) temporarily mutes B, then clears it. B stays a
  // member (the ban below still has a target). The "muted member can't post" guarantee is
  // proven server-side by the Go integration test; here we verify the timeout UI — the
  // muted badge and the timeout↔unmute toggle. (B is still an admin from step 6; the
  // owner can time out a non-owner regardless of role.)
  step('A (owner) times out B → muted badge + unmute toggle, then clears it')
  await a
    .locator('.server-group', { hasText: 'team ' + sfx })
    .getByRole('button', { name: 'members' })
    .click()
  const bTimeoutRow = a.locator('.member-row', { hasText: userB })
  await bTimeoutRow.waitFor({ timeout: 8000 })
  check(
    (await bTimeoutRow.getByRole('button', { name: 'timeout' }).count()) > 0,
    'owner sees a timeout button on B’s row',
  )
  ans.a = '10' // minutes (prompt auto-answered)
  await bTimeoutRow.getByRole('button', { name: 'timeout' }).click()
  await bTimeoutRow.locator('.role-muted').waitFor({ timeout: 8000 })
  check(await bTimeoutRow.locator('.role-muted').isVisible(), 'B shows the ⏳ muted badge after timeout')
  check(
    (await bTimeoutRow.getByRole('button', { name: 'unmute' }).count()) > 0,
    'the timeout button toggles to "unmute" while B is muted',
  )
  await a.screenshot({ path: join(SHOTS, 'rt-09b-timeout.png') })
  // Clear the timeout (confirm auto-accepts) → the muted badge disappears.
  await bTimeoutRow.getByRole('button', { name: 'unmute' }).click()
  await bTimeoutRow.locator('.role-muted').waitFor({ state: 'detached', timeout: 8000 }).catch(() => {})
  check(
    (await bTimeoutRow.locator('.role-muted').count()) === 0,
    'the muted badge clears after unmute',
  )

  // 8 — Ban (moderation): A (owner) bans B from the server. Ban is the stronger form
  // of kick — it removes B (so every kick assertion still holds) AND records a ban so
  // B can't rejoin until unbanned. (The rejoin-blocked security guarantee is proven
  // adversarially by the Go integration test; here we verify the user-visible ban UI:
  // the ban button, live removal, the admin's Banned section, and unban.)
  step('A (owner) bans B → B is removed and appears in the Banned section')
  await a
    .locator('.server-group', { hasText: 'team ' + sfx })
    .getByRole('button', { name: 'members' })
    .click()
  const aOwnRow = a.locator('.member-row', { hasText: userA })
  await aOwnRow.waitFor({ timeout: 8000 })
  // The owner can't moderate themselves: no kick/ban button on A's own row.
  check(
    (await aOwnRow.getByRole('button', { name: 'ban', exact: true }).count()) === 0,
    "the owner's own row has no ban button (can't ban yourself)",
  )
  const bModRow = a.locator('.member-row', { hasText: userB })
  // Both moderation buttons render on a non-owner's row (shared gating).
  check(
    (await bModRow.getByRole('button', { name: 'kick' }).count()) > 0,
    'owner sees a kick button on B’s row',
  )
  check(
    (await bModRow.getByRole('button', { name: 'ban', exact: true }).count()) > 0,
    'owner sees a ban button on B’s row',
  )
  ans.a = 'spamming the channel' // the ban reason (prompt auto-answered)
  await bModRow.getByRole('button', { name: 'ban', exact: true }).click() // confirm auto-accepts
  // The Banned section appearing is the signal the ban took effect and the panel refreshed.
  await a.locator('.bans-head').waitFor({ timeout: 8000 })
  const bBanRow = a.locator('.banned-row', { hasText: userB })
  check(
    (await a.locator('.member-row:not(.banned-row)', { hasText: userB }).count()) === 0,
    'B is gone from the members list after the ban',
  )
  check(
    ((await a.locator('.bans-head').textContent()) ?? '').includes('Banned (1)'),
    'the Banned section shows one banned user',
  )
  check(await bBanRow.isVisible(), 'B appears in the admin Banned section after the ban')
  check(
    ((await bBanRow.locator('.member-status').textContent()) ?? '').includes('spamming'),
    'the ban reason renders next to the banned user',
  )
  await a.screenshot({ path: join(SHOTS, 'rt-10-banned.png') })

  // 8b — B's client reacts live to the ban (server-removed push): the server drops out
  // of B's sidebar and, since B was viewing it, B lands back on #general — no manual
  // refresh, no broken reconnect loop (identical to the kick path).
  step("B's client drops the server live and falls back to #general")
  const bServerGroup = b.locator('.server-group', { hasText: 'team ' + sfx })
  await bServerGroup.waitFor({ state: 'detached', timeout: 8000 }).catch(() => {})
  check(
    (await bServerGroup.count()) === 0,
    'B’s sidebar drops the banned server live (via the server-removed push)',
  )
  check(
    ((await b.locator('.brand .channel').textContent()) ?? '').includes('general'),
    'B is moved to #general after being banned (no broken reconnect loop)',
  )
  await b.screenshot({ path: join(SHOTS, 'rt-11-b-banned.png') })

  // 8c — Unban: A lifts the ban; B leaves the Banned section (they could now rejoin).
  step('A unbans B → B leaves the Banned section')
  await bBanRow.getByRole('button', { name: 'unban' }).click() // confirm auto-accepts
  await a.locator('.banned-row', { hasText: userB }).waitFor({ state: 'detached', timeout: 8000 }).catch(() => {})
  check(
    (await a.locator('.banned-row', { hasText: userB }).count()) === 0,
    'B is gone from the Banned section after unban',
  )
  await a.screenshot({ path: join(SHOTS, 'rt-12-unbanned.png') })

  // 9 — B (now unbanned) rejoins via the still-valid invite, ready for the transfer +
  // leave checks below.
  step('B rejoins the server via the still-valid invite')
  ans.b = inviteCode
  await b.getByRole('button', { name: 'Join server' }).click()
  await b.locator('.server-group', { hasText: 'team ' + sfx }).waitFor({ timeout: 10000 })

  // 9a — Transfer ownership (owner only): A hands the server to B → B becomes owner, A
  // becomes admin; then B hands it back so A owns it again (round-trip restores state).
  // Helper: open a window's members panel for the team server, wait for a fresh fetch.
  const openMembers = async (pg) => {
    await pg
      .locator('.server-group', { hasText: 'team ' + sfx })
      .getByRole('button', { name: 'members' })
      .click()
    await pg.locator('.member-row').first().waitFor({ timeout: 8000 })
  }
  // Wait (poll) until userName's row carries the expected role badge.
  const waitRole = async (pg, userName, role) => {
    const row = pg.locator('.member-row', { hasText: userName })
    for (let i = 0; i < 20; i++) {
      if ((await row.locator(`.role-badge.role-${role}`).count()) > 0) return true
      await pg.waitForTimeout(300)
    }
    return false
  }
  step('A transfers ownership to B → B owner, A admin')
  await openMembers(a)
  await a
    .locator('.member-row', { hasText: userB })
    .getByRole('button', { name: 'make owner' })
    .click() // confirm auto-accepts
  check(await waitRole(a, userB, 'owner'), 'after transfer, B shows the owner badge in A’s panel')
  check(await waitRole(a, userA, 'admin'), 'after transfer, A (old owner) shows the admin badge')
  await a.screenshot({ path: join(SHOTS, 'rt-13-transfer.png') })

  step('B (new owner) transfers ownership back to A')
  await openMembers(b) // fresh fetch — B is now the owner and sees "make owner"
  await b
    .locator('.member-row', { hasText: userA })
    .getByRole('button', { name: 'make owner' })
    .click()
  check(await waitRole(b, userA, 'owner'), 'after transfer-back, A shows the owner badge in B’s panel')
  check(await waitRole(b, userB, 'admin'), 'after transfer-back, B is an admin again')

  // 9b — Leave server: B (now an admin, a non-owner) voluntarily LEAVES from the panel.
  // A non-owner sees "leave server" (not the owner's "delete server"); leaving drops the
  // server from B's own sidebar. (B's panel is already open from the transfer-back.)
  step('B (non-owner) leaves the server → it drops from B’s sidebar')
  await b.locator('.server-settings').waitFor({ timeout: 8000 })
  check(
    (await b.locator('.leave-server-btn').count()) > 0,
    'a non-owner member sees the "leave server" button',
  )
  check(
    (await b.locator('.delete-server-btn').count()) === 0,
    'a non-owner member does NOT see the owner-only "delete server" button',
  )
  await b.locator('.leave-server-btn').click() // confirm auto-accepts
  await b
    .locator('.server-group', { hasText: 'team ' + sfx })
    .waitFor({ state: 'detached', timeout: 8000 })
  check(
    (await b.locator('.server-group', { hasText: 'team ' + sfx }).count()) === 0,
    'after leaving, the server is gone from B’s sidebar',
  )
  await b.screenshot({ path: join(SHOTS, 'rt-13-leave.png') })

  // 14 — Group DM leave (realtime): A makes a GROUP with B + a third member C, B opens it,
  // then A leaves. B — connected to the group's WS — must see A drop out of the member list
  // LIVE (the dm-membership broadcast → DM-list refetch), without reloading. This proves the
  // remaining-members live-refresh edge of leave-group that the single-client browser.mjs can't.
  // Runs LAST so its DM/unread state never perturbs the earlier tab-badge/mute assertions.
  step('group DM leave: A leaves a group → B sees A removed from the member list live')
  const userC = 'carol' + sfx
  const cReg = await a.evaluate(async (name) => {
    const r = await fetch('/api/auth/register', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ username: name, password: 'hunter2' }),
    })
    return r.ok ? 'ok' : 'register C failed ' + r.status
  }, userC)
  check(cReg === 'ok', `registered a third member C=${userC} (${cReg})`)

  // A creates the group (B + C).
  await a.locator('.channel-list .channel-item', { hasText: 'general' }).first().click() // reset A to a known view
  await a.getByRole('button', { name: '+ New DM' }).click()
  await a.locator('.group-modal').waitFor({ timeout: 4000 })
  const gChip = a.locator('.group-chip-input')
  await gChip.fill(userB)
  await gChip.press('Enter')
  await gChip.fill(userC)
  await gChip.press('Enter')
  await a.locator('.group-modal').getByRole('button', { name: /Create Group/ }).click()
  await a.locator('.group-modal').waitFor({ state: 'detached', timeout: 8000 })

  // B reloads to pick up the new group (no "added to a group" push yet), opens it (→ now
  // connected to its WS), and sees A among the members.
  await b.reload({ waitUntil: 'domcontentloaded' })
  await b.locator('.channel-item', { hasText: userC }).first().click()
  await b.locator('.brand .channel').waitFor({ timeout: 8000 })
  const bTitleBefore = (await b.locator('.brand .channel').textContent()) || ''
  check(bTitleBefore.includes(userA), `B sees A in the group title before A leaves (got "${bTitleBefore}")`)

  // A leaves the group → B, still viewing it, sees A drop from the title LIVE (no reload).
  await a.locator('.chat-header .leave-group').click()
  let bTitleAfter = bTitleBefore
  for (let i = 0; i < 60; i++) {
    bTitleAfter = (await b.locator('.brand .channel').textContent()) || ''
    if (!bTitleAfter.includes(userA)) break
    await b.waitForTimeout(200)
  }
  check(
    !bTitleAfter.includes(userA) && bTitleAfter.includes(userC),
    `B sees A removed from the group live, C remains (got "${bTitleAfter}")`,
  )
  await b.screenshot({ path: join(SHOTS, 'rt-14-group-leave.png') })

  // 15 — Group DM ADD member (realtime): proves the SendToUser push. A makes a fresh group
  // (B + E, E registered via API for a uniquely-matchable title), B opens it. A brand-new user
  // D logs in on #general — NOT in the group. A adds D → BOTH B (viewing it, via the channel
  // broadcast) sees D appear in the title AND D (on #general, via the SendToUser push) sees the
  // group appear in their sidebar — both LIVE, no reload. This is the add-member realtime edge
  // the single-client + live-API E2E can't reach: a user NOT on the channel pushed into a new DM.
  step('group DM add: A adds D → B sees D in the title + D sees the group appear, both live')
  const userE = 'erin' + sfx
  const userD = 'dave' + sfx
  await a.evaluate(async (name) => {
    await fetch('/api/auth/register', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ username: name, password: 'hunter2' }),
    })
  }, userE)
  // D gets its own browser context, lands on #general (not in any group yet).
  const davCtx = await browser.newContext({ viewport: { width: 1100, height: 820 } })
  const dav = await davCtx.newPage()
  dav.on('pageerror', (e) => {
    console.log('  [pageerror DAV] ' + e.message)
    failed++
  })
  dav.on('dialog', (dlg) => dlg.accept(undefined))
  await register(dav, userD)

  // A makes a fresh group with B + E (uniquely matchable by E), and B opens it.
  await a.locator('.channel-list .channel-item', { hasText: 'general' }).first().click()
  await a.getByRole('button', { name: '+ New DM' }).click()
  await a.locator('.group-modal').waitFor({ timeout: 4000 })
  const g2 = a.locator('.group-chip-input')
  await g2.fill(userB)
  await g2.press('Enter')
  await g2.fill(userE)
  await g2.press('Enter')
  await a.locator('.group-modal').getByRole('button', { name: /Create Group/ }).click()
  await a.locator('.group-modal').waitFor({ state: 'detached', timeout: 8000 })
  await b.reload({ waitUntil: 'domcontentloaded' })
  await b.locator('.channel-item', { hasText: userE }).first().click()
  await b.locator('.brand .channel').waitFor({ timeout: 8000 })

  // A adds D via the ➕ header action (the prompt is answered with D's username via ans.a).
  ans.a = userD
  await a.locator('.chat-header .add-to-group').click()

  // B (viewing the group) sees D appear in the title LIVE (channel broadcast → refetch).
  let bAddTitle = ''
  for (let i = 0; i < 60; i++) {
    bAddTitle = (await b.locator('.brand .channel').textContent()) || ''
    if (bAddTitle.includes(userD)) break
    await b.waitForTimeout(200)
  }
  check(bAddTitle.includes(userD), `B sees D added to the group title live (got "${bAddTitle}")`)

  // D (on #general, never reloaded) sees the new group appear in their sidebar LIVE via the
  // SendToUser push — the row is titled by its members (incl. E, unique to D's only DM).
  let dHasGroup = false
  for (let i = 0; i < 60; i++) {
    dHasGroup = (await dav.locator('.dm-list .channel-item', { hasText: userE }).count()) > 0
    if (dHasGroup) break
    await dav.waitForTimeout(200)
  }
  check(dHasGroup, 'D sees the group appear in their sidebar live (SendToUser push, no reload)')
  await dav.screenshot({ path: join(SHOTS, 'rt-15-add-live.png') })
  await davCtx.close()

  // 16 — Group DM RENAME (realtime): A renames the shared group → B, still viewing it, sees the
  // header relabel to the custom name LIVE via the dm-membership refetch (the same path leave/add
  // use), no reload. Reuses the §15 group (A + B both on its WS). Proves the "realtime relabel"
  // claim of naming slice 2 (iter 185) — previously an inherited assumption, now a guarded fact.
  step('group DM rename: A names the group → B sees the header relabel to the custom name live')
  const grpRtName = 'RT Trip ' + sfx
  const bRenameBefore = (await b.locator('.brand .channel').textContent()) || ''
  check(
    bRenameBefore.includes(userE) && !bRenameBefore.includes(grpRtName),
    `B's group header is member-titled before the rename (got "${bRenameBefore}")`,
  )
  ans.a = grpRtName // answer A's rename prompt with the custom name
  await a.locator('.chat-header .rename-group').click()
  let bRenameAfter = bRenameBefore
  for (let i = 0; i < 60; i++) {
    bRenameAfter = (await b.locator('.brand .channel').textContent()) || ''
    if (bRenameAfter.includes(grpRtName)) break
    await b.waitForTimeout(200)
  }
  check(
    bRenameAfter.includes(grpRtName) && !bRenameAfter.includes(userE),
    `B sees the group relabel to the custom name live (got "${bRenameAfter}")`,
  )
  await b.screenshot({ path: join(SHOTS, 'rt-16-rename-live.png') })

  await browser.close()
  console.log(
    `\nrealtime QA: ${failed === 0 ? 'PASS' : 'FAIL (' + failed + ' issue[s])'}` +
      `  ·  screenshots in ${SHOTS}/`,
  )
  process.exit(failed === 0 ? 0 : 1)
}

main().catch((e) => {
  console.error('realtime QA crashed:', e)
  process.exit(1)
})
