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
  await a.context().setOffline(false)
  const reconnected = await a
    .locator('.dot.online')
    .waitFor({ timeout: 20000 })
    .then(() => true)
    .catch(() => false)
  check(reconnected, 'A’s connection re-establishes after going back online')
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

  // 4 — Direct messages: A opens a private DM with B and sends a message; B reloads,
  // finds the DM in their sidebar, and reads it. Proves the DM UI end-to-end.
  step('A opens a DM with B (+ New DM) and sends a private message')
  ans.a = userB // answer the "which user?" prompt
  await a.getByRole('button', { name: '+ New DM' }).click()
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
  await b.getByRole('button', { name: new RegExp(userA) }).click()
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
