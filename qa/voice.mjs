// Opencord voice QA — TWO browser contexts proving the mesh WebRTC voice call
// actually connects end-to-end (GOAL.md "Voice channels", Rule 14): user A and
// user B both Join voice in #general and a real RTCPeerConnection between them
// reaches connectionState "connected", with remote audio tracks flowing. The
// curl/WS E2E can't see this — only a real browser with media can.
//
// Chromium is launched with fake media so getUserMedia resolves headlessly:
//   --use-fake-device-for-media-stream  → a synthetic mic (continuous tone)
//   --use-fake-ui-for-media-stream      → auto-grant the mic permission
//   --autoplay-policy=no-user-gesture-required → remote <audio> may autoplay
//
// Assumes a running stack at QA_BASE_URL (default http://localhost:5173); boot
// one with qa/run.sh, which runs this after realtime.mjs.
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
// Poll a locator until it has exactly `n` matches (roster grow/shrink is async).
const waitForCount = async (loc, n, ms = 12000) => {
  const end = Date.now() + ms
  while (Date.now() < end) {
    if ((await loc.count()) === n) return true
    await new Promise((r) => setTimeout(r, 200))
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
  await mkdir(SHOTS, { recursive: true })
  const browser = await chromium.launch({
    args: [
      '--use-fake-device-for-media-stream',
      '--use-fake-ui-for-media-stream',
      '--autoplay-policy=no-user-gesture-required',
    ],
  })
  const a = await (await browser.newContext({ viewport: { width: 1100, height: 820 } })).newPage()
  const b = await (await browser.newContext({ viewport: { width: 1100, height: 820 } })).newPage()
  for (const [who, pg] of [
    ['A', a],
    ['B', b],
  ]) {
    pg.on('pageerror', (e) => {
      console.log(`  [pageerror ${who}] ` + e.message)
      failed++
    })
    pg.on('console', (m) => {
      if (m.type() === 'error' || m.text().includes('[voice]'))
        console.log(`  [console ${who}] ` + m.text())
    })
    // The mic-permission alert (getUserMedia denied) would otherwise hang headless.
    pg.on('dialog', (d) => {
      console.log(`  [dialog ${who}] ` + d.message())
      void d.dismiss()
    })
  }

  const sfx = String(Date.now()).slice(-7)
  const userA = 'voxa' + sfx
  const userB = 'voxb' + sfx

  step(`register two users (A=${userA}, B=${userB}) in #general`)
  await register(a, userA)
  await register(b, userB)

  // The channel WS must be OPEN before Join voice (it relays the signaling); the
  // header's online dot turns green on connect.
  await a.locator('.dot.online').waitFor({ timeout: 10000 })
  await b.locator('.dot.online').waitFor({ timeout: 10000 })

  // A joins first, then B. The existing member (A) offers to the joiner (B);
  // the joiner discovers A purely by answering that offer (no server roster).
  step('A joins voice')
  await a.getByRole('button', { name: /Join voice/ }).click()
  await a.locator('.voice-bar').waitFor({ timeout: 10000 })
  check(await a.locator('.voice-bar').isVisible(), 'A is in the voice bar')

  step('B joins voice')
  await b.getByRole('button', { name: /Join voice/ }).click()
  await b.locator('.voice-bar').waitFor({ timeout: 10000 })
  check(await b.locator('.voice-bar').isVisible(), 'B is in the voice bar')

  // The real proof: a mesh peer connection reaches "connected" on BOTH sides.
  step('both peers reach a CONNECTED mesh audio link')
  const aConnected = a.locator('[data-voice-peer][data-state="connected"]')
  const bConnected = b.locator('[data-voice-peer][data-state="connected"]')
  await aConnected.first().waitFor({ timeout: 25000 }).catch(() => {})
  await bConnected.first().waitFor({ timeout: 25000 }).catch(() => {})
  check((await aConnected.count()) > 0, 'A ↔ B connection state = connected (A side)')
  check((await bConnected.count()) > 0, 'A ↔ B connection state = connected (B side)')

  step('each roster lists the other participant')
  check(
    (await a.locator('[data-voice-peer]').filter({ hasText: userB }).count()) > 0,
    "A's roster lists B",
  )
  check(
    (await b.locator('[data-voice-peer]').filter({ hasText: userA }).count()) > 0,
    "B's roster lists A",
  )

  step('remote audio actually arrives (a live inbound audio track on each side)')
  const remoteTrack = (page) =>
    page.evaluate(
      () =>
        [...document.querySelectorAll('audio[data-voice-audio]')].some(
          (el) => el.srcObject && el.srcObject.getAudioTracks?.().length > 0,
        ),
    )
  check(await remoteTrack(a), 'A has a live remote audio track')
  check(await remoteTrack(b), 'B has a live remote audio track')

  step('device auto-detect + manual override UI is present')
  check(
    (await a.locator('.voice-device select[aria-label="microphone"]').count()) > 0,
    'microphone selector present (Auto + devices)',
  )

  await a.screenshot({ path: join(SHOTS, 'voice-01-a-connected.png') })
  await b.screenshot({ path: join(SHOTS, 'voice-02-b-connected.png') })

  step('mute toggles the local mic label')
  await a.getByRole('button', { name: 'mute' }).click()
  check(
    await a.locator('[data-voice-self]').filter({ hasText: 'muted' }).count() > 0,
    "A's own chip shows muted after mute",
  )

  step('B leaves → A sees the roster empty out live')
  await b.getByRole('button', { name: 'leave' }).click()
  check(
    await waitForCount(a.locator('[data-voice-peer]'), 0),
    "A's voice roster empties after B leaves",
  )

  await browser.close()
  console.log(failed === 0 ? '\nVOICE QA PASSED' : `\nVOICE QA FAILED (${failed} check(s))`)
  process.exit(failed === 0 ? 0 : 1)
}

main().catch((e) => {
  console.error(e)
  process.exit(1)
})
