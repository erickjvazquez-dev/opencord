// Opencord browser QA — drives the REAL rendered UI like a user, logging every
// click ("→ ...") and screenshotting each step into qa-screenshots/ for AI-vision
// review. Exits non-zero on any failed assertion or page error.
//
// Assumes a running stack at QA_BASE_URL (default http://localhost:5173).
// Boot one with qa/run.sh (which also runs this).
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

async function main() {
  await mkdir(SHOTS, { recursive: true })
  const browser = await chromium.launch()
  const page = await (await browser.newContext({ viewport: { width: 1100, height: 820 } })).newPage()
  page.on('pageerror', (e) => {
    console.log('  [pageerror] ' + e.message)
    failed++
  })

  const chanName = 'qa-' + String(Date.now()).slice(-6)
  // Auto-answer window.prompt with whatever the current step expects, and accept
  // window.confirm (delete). Set `promptAnswer` before an action that prompts.
  let promptAnswer = chanName
  let lastPromptDefault = '' // capture a prompt's pre-filled value (e.g. an invite code)
  page.on('dialog', (d) => {
    if (d.type() === 'prompt') lastPromptDefault = d.defaultValue()
    d.accept(d.type() === 'prompt' ? promptAnswer : undefined)
  })
  const shot = (name) => page.screenshot({ path: join(SHOTS, name) })

  // 1 — Auth screen + register.
  step(`open ${BASE}`)
  await page.goto(BASE, { waitUntil: 'domcontentloaded' })
  await page.getByPlaceholder('username').waitFor({ timeout: 15000 })
  await shot('01-auth.png')
  check(await page.getByPlaceholder('username').isVisible(), 'auth screen renders')

  const user = 'qabot' + String(Date.now()).slice(-7)
  step('switch to Register, fill credentials, submit')
  await page.getByRole('button', { name: 'No account? Register' }).click()
  await page.getByPlaceholder('username').fill(user)
  await page.getByPlaceholder('password').fill('hunter2')
  await page.getByRole('button', { name: 'Create account' }).click()

  // 2 — Chat loads.
  await page.getByPlaceholder(/Message #/).waitFor({ timeout: 15000 })
  await shot('02-chat.png')
  check(await page.getByRole('button', { name: /general/ }).isVisible(), '#general in sidebar after register')

  // 3 — Send a message; it renders with an avatar.
  const body = 'hello from the qa bot'
  step('type a message and Send')
  await page.getByPlaceholder(/Message #/).fill(body)
  await page.getByRole('button', { name: 'Send' }).click()
  await page.getByText(body).waitFor({ timeout: 8000 })
  await shot('03-message.png')
  check(await page.getByText(body).isVisible(), 'sent message appears')
  check(await page.locator('.avatar').first().isVisible(), 'avatar renders on the message')

  // 3a — A second message from the same author groups (no repeated avatar/name).
  step('send a second message → it groups under the first')
  const body2 = 'second line, same author'
  await page.getByPlaceholder(/Message #/).fill(body2)
  await page.getByRole('button', { name: 'Send' }).click()
  await page.getByText(body2).waitFor({ timeout: 8000 })
  const grouped = page.locator('.message', { hasText: body2 }).first()
  await shot('03a-grouped.png')
  check(
    await grouped.evaluate((el) => el.classList.contains('grouped')),
    'consecutive same-author message is grouped',
  )
  check((await grouped.locator('.avatar').count()) === 0, 'grouped message hides the repeated avatar')

  const msg = page.locator('.message', { hasText: body }).first()

  // 3b — React with 👍 (add): a highlighted chip with count 1 appears (live WS).
  step('hover message → react → 👍')
  await msg.hover()
  await msg.getByRole('button', { name: 'react' }).click()
  await page.locator('.emoji-picker').getByRole('button', { name: '👍' }).click()
  await msg.locator('.reaction.mine').waitFor({ timeout: 8000 })
  await shot('03b-reaction.png')
  check(await msg.locator('.reaction.mine').isVisible(), 'reaction chip appears, highlighted as mine')
  check(
    (await msg.locator('.reaction .rcount').first().textContent())?.trim() === '1',
    'reaction count shows 1',
  )

  // 4 — Edit the message (its reaction must survive the edit).
  step('hover message → edit → change → save')
  await msg.hover()
  await msg.getByRole('button', { name: 'edit' }).click()
  await page.locator('.edit-row input').fill('edited by the qa bot')
  await page.getByRole('button', { name: 'save' }).click()
  await page.getByText('edited by the qa bot').waitFor({ timeout: 8000 })
  await shot('04-edited.png')
  check(await page.getByText('(edited)').first().isVisible(), 'edited indicator shows')
  const edited = page.locator('.message', { hasText: 'edited by the qa bot' }).first()
  check(await edited.locator('.reaction.mine').isVisible(), 'reaction survives the edit')

  // 5 — Create a channel and see it in the sidebar.
  step('create a channel via "+ New channel"')
  await page.getByRole('button', { name: '+ New channel' }).click()
  await page.getByRole('button', { name: new RegExp(chanName) }).waitFor({ timeout: 8000 })
  await shot('05-channel.png')
  check(await page.getByRole('button', { name: new RegExp(chanName) }).isVisible(), 'new channel appears in sidebar')

  // 6 — Back to #general, delete the message.
  step('switch to #general → hover message → delete')
  await page.getByRole('button', { name: /general/ }).click()
  const msg2 = page.locator('.message', { hasText: 'edited by the qa bot' }).first()
  await msg2.hover()
  await msg2.getByRole('button', { name: 'delete' }).click()
  await page.getByText('[deleted]').first().waitFor({ timeout: 8000 })
  await shot('06-deleted.png')
  check(await page.getByText('[deleted]').first().isVisible(), 'deleted message renders [deleted]')

  // 6b — Inverse of grouping: deleting the message above a grouped follow-up must
  // break the run, so the follow-up un-groups and its avatar returns.
  const follow = page.locator('.message', { hasText: 'second line, same author' }).first()
  await follow.locator('.avatar').waitFor({ timeout: 8000 })
  check(
    (await follow.locator('.avatar').count()) === 1,
    'follow-up un-groups (avatar returns) when the message above it is deleted',
  )

  // 7 — Servers: create a server, add a channel under it, and post in that channel.
  step('create a server → add a channel → post in it')
  promptAnswer = 'qa server'
  await page.getByRole('button', { name: '+ New server' }).click()
  await page.locator('.server-name', { hasText: 'qa server' }).waitFor({ timeout: 8000 })
  check(
    await page.locator('.server-name', { hasText: 'qa server' }).isVisible(),
    'new server appears in the Servers section',
  )
  const srvChan = 'srv' + String(Date.now()).slice(-6)
  promptAnswer = srvChan
  await page
    .locator('.server-group', { hasText: 'qa server' })
    .getByRole('button', { name: '+ channel' })
    .click()
  await page.getByRole('button', { name: new RegExp(srvChan) }).waitFor({ timeout: 8000 })
  check(
    await page.getByRole('button', { name: new RegExp(srvChan) }).isVisible(),
    'server channel appears under its server',
  )
  const srvBody = 'hello from a server channel'
  await page.getByPlaceholder(new RegExp('Message #' + srvChan)).fill(srvBody)
  await page.getByRole('button', { name: 'Send' }).click()
  await page.getByText(srvBody).waitFor({ timeout: 8000 })
  await shot('07-server.png')
  check(await page.getByText(srvBody).isVisible(), 'message posts in the server channel')

  // 7b — Invite: the invite button mints a code (shown in a prompt to copy).
  step('click invite → a server invite code is generated')
  lastPromptDefault = ''
  await page
    .locator('.server-group', { hasText: 'qa server' })
    .getByRole('button', { name: 'invite' })
    .click()
  // createInvite is async, so the prompt fires after a round-trip — poll for it.
  for (let i = 0; i < 50 && lastPromptDefault.length < 6; i++) await page.waitForTimeout(100)
  check(lastPromptDefault.length >= 6, 'invite button produces a shareable code')

  // 7c — Search the current channel and confirm the matching message shows.
  step('search the channel → matching message appears, clear returns to live')
  await page.getByPlaceholder('Search this channel').fill('server')
  await page.getByPlaceholder('Search this channel').press('Enter')
  await page.locator('.search-results').waitFor({ timeout: 8000 })
  await shot('07c-search.png')
  check(
    await page.locator('.search-results').getByText(srvBody).isVisible(),
    'search finds the matching message',
  )
  await page.getByRole('button', { name: 'clear' }).click()
  check((await page.locator('.search-results').count()) === 0, 'clearing search returns to the channel')

  // 8 — Mobile: at a phone viewport the sidebar collapses into a drawer behind a
  // menu toggle, and selecting a channel closes it.
  step('shrink to a phone viewport → sidebar becomes a drawer')
  await page.setViewportSize({ width: 390, height: 780 })
  await page.waitForTimeout(350) // let the drawer's slide transition settle
  check(await page.getByRole('button', { name: 'menu' }).isVisible(), 'menu toggle appears on mobile')
  await shot('08-mobile-closed.png')
  await page.getByRole('button', { name: 'menu' }).click()
  await page.locator('.app.sidebar-open').waitFor({ timeout: 4000 })
  await page.waitForTimeout(350) // let the drawer finish sliding in
  await shot('08-mobile-open.png')
  check((await page.locator('.app.sidebar-open').count()) === 1, 'menu toggle opens the drawer')
  await page.getByRole('button', { name: /general/ }).click()
  await page.locator('.app:not(.sidebar-open)').waitFor({ timeout: 4000 })
  check(
    (await page.locator('.app.sidebar-open').count()) === 0,
    'selecting a channel closes the drawer',
  )

  await browser.close()
  console.log(
    `\nbrowser QA: ${failed === 0 ? 'PASS' : 'FAIL (' + failed + ' issue[s])'}` +
      `  ·  screenshots in ${SHOTS}/`,
  )
  process.exit(failed === 0 ? 0 : 1)
}

main().catch((e) => {
  console.error('QA crashed:', e)
  process.exit(1)
})
