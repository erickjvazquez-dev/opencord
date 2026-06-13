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
