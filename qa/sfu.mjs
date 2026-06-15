// Opencord SFU E2E — drives the REAL app voice path over a local LiveKit. Two
// fake-media browsers register, open the same channel, and Join voice. With the
// server wired to LiveKit (OPENCORD_SFU_*), the app's joinVoice fetches a token and
// connects via the SfuSession (livekit-client) instead of mesh; each must then see
// the other in the voice roster as connected — proving the whole client SFU path
// (token endpoint → SfuSession → real LiveKit → roster) and, implicitly, that a real
// LiveKit accepts our minted tokens.
//
// Boot the stack with qa/sfu-run.sh (LiveKit + Opencord-SFU + vite). Env QA_BASE_URL
// (default http://localhost:5173).
import { chromium } from 'playwright'

const BASE = process.env.QA_BASE_URL || 'http://localhost:5173'

let failed = 0
const step = (s) => console.log('  → ' + s)
const check = (cond, msg) => {
  console.log((cond ? '  ✓ ' : '  ✗ FAIL: ') + msg)
  if (!cond) failed++
}
const waitForCount = async (loc, n, ms = 25000) => {
  const end = Date.now() + ms
  while (Date.now() < end) {
    if ((await loc.count()) >= n) return true
    await new Promise((r) => setTimeout(r, 250))
  }
  return false
}

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
  const browser = await chromium.launch({
    args: [
      '--use-fake-device-for-media-stream',
      '--use-fake-ui-for-media-stream',
      '--autoplay-policy=no-user-gesture-required',
    ],
  })
  const a = await (await browser.newContext({ viewport: { width: 1100, height: 820 } })).newPage()
  const b = await (await browser.newContext({ viewport: { width: 1100, height: 820 } })).newPage()
  for (const [who, pg] of [['A', a], ['B', b]]) {
    pg.on('pageerror', (e) => { console.log(`  [pageerror ${who}] ` + e.message); failed++ })
    pg.on('console', (m) => { if (m.type() === 'error') console.log(`  [console ${who}] ` + m.text()) })
  }

  const sfx = String(Date.now()).slice(-7)
  step(`register two users (in #general) against ${BASE}`)
  await register(a, 'sfua' + sfx)
  await register(b, 'sfub' + sfx)
  for (const pg of [a, b]) await pg.locator('.dot.online').waitFor({ timeout: 10000 })

  step('A joins voice (SFU transport)')
  await a.getByRole('button', { name: /Join voice/ }).click()
  await a.locator('.voice-bar').waitFor({ timeout: 10000 })
  check(await a.locator('.voice-bar').isVisible(), 'A is in the voice bar')

  step('B joins voice')
  await b.getByRole('button', { name: /Join voice/ }).click()
  await b.locator('.voice-bar').waitFor({ timeout: 10000 })
  check(await b.locator('.voice-bar').isVisible(), 'B is in the voice bar')

  step('each sees the other connected via the SFU (roster over LiveKit)')
  check(
    await waitForCount(a.locator('[data-voice-peer][data-state="connected"]'), 1),
    'A sees B connected through the SFU',
  )
  check(
    await waitForCount(b.locator('[data-voice-peer][data-state="connected"]'), 1),
    'B sees A connected through the SFU',
  )

  // With autoSubscribe:false + top-N selection, presence alone isn't enough — prove
  // the client actually SUBSCRIBED to the peer's audio (a small room → everyone).
  step('each actually subscribes to the other audio (top-N selection, small room → all)')
  const hasRemoteAudio = (page) =>
    page.evaluate(
      () =>
        [...document.querySelectorAll('audio[data-voice-audio]')].some(
          (el) => el.srcObject && el.srcObject.getAudioTracks?.().length > 0,
        ),
    )
  const waitAudio = async (page, ms = 12000) => {
    const end = Date.now() + ms
    while (Date.now() < end) {
      if (await hasRemoteAudio(page)) return true
      await new Promise((r) => setTimeout(r, 300))
    }
    return false
  }
  check(await waitAudio(a), 'A subscribed to a remote audio track via the SFU')
  check(await waitAudio(b), 'B subscribed to a remote audio track via the SFU')

  step('leaving removes the peer')
  await b.getByRole('button', { name: 'leave' }).click()
  check(await waitForCount(a.locator('[data-voice-peer]'), 0, 12000), "A's roster drops B after B leaves")

  await browser.close()
  console.log(failed === 0 ? '\nSFU E2E PASSED' : `\nSFU E2E FAILED (${failed})`)
  process.exit(failed === 0 ? 0 : 1)
}

main().catch((e) => { console.error(e); process.exit(1) })
