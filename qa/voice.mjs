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
  const newPage = async () =>
    (await browser.newContext({ viewport: { width: 1100, height: 820 } })).newPage()
  const a = await newPage()
  const b = await newPage()
  const c = await newPage()
  for (const [who, pg] of [
    ['A', a],
    ['B', b],
    ['C', c],
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
  const userC = 'voxc' + sfx

  step(`register three users (A=${userA}, B=${userB}, C=${userC}) in #general`)
  await register(a, userA)
  await register(b, userB)
  await register(c, userC)

  // The channel WS must be OPEN before Join voice (it relays the signaling); the
  // header's online dot turns green on connect.
  for (const pg of [a, b, c]) await pg.locator('.dot.online').waitFor({ timeout: 10000 })

  // Join one at a time, A→B→C. Each existing member offers to the new joiner; the
  // joiner discovers everyone purely by answering the incoming offers (no server
  // roster). After C joins, every pair must hold its own RTCPeerConnection — a
  // genuine N-way mesh, not just a pair.
  const joinCall = async (pg, who) => {
    await pg.getByRole('button', { name: /Join voice/ }).click()
    await pg.locator('.voice-bar').waitFor({ timeout: 10000 })
    check(await pg.locator('.voice-bar').isVisible(), `${who} is in the voice bar`)
  }
  step('A joins voice')
  await joinCall(a, 'A')
  step('B joins voice (A↔B)')
  await joinCall(b, 'B')
  step('C joins voice (mesh becomes A↔B↔C↔A)')
  await joinCall(c, 'C')

  // The real proof of a 3-way mesh: each participant holds TWO connected links.
  step('every participant reaches 2 CONNECTED peers (full 3-way mesh)')
  const connectedPeers = (pg) => pg.locator('[data-voice-peer][data-state="connected"]')
  for (const [pg, who] of [
    [a, 'A'],
    [b, 'B'],
    [c, 'C'],
  ]) {
    check(
      await waitForCount(connectedPeers(pg), 2, 30000),
      `${who} is connected to both other peers (2 connected links)`,
    )
  }

  step('each roster lists the other two participants')
  const rosterHas = async (pg, name) =>
    (await pg.locator('[data-voice-peer]').filter({ hasText: name }).count()) > 0
  check((await rosterHas(a, userB)) && (await rosterHas(a, userC)), "A's roster lists B and C")
  check((await rosterHas(b, userA)) && (await rosterHas(b, userC)), "B's roster lists A and C")
  check((await rosterHas(c, userA)) && (await rosterHas(c, userB)), "C's roster lists A and B")

  step('remote audio arrives from both peers (two live inbound tracks each)')
  // Poll, don't one-shot: ontrack can land a beat after connectionState flips to
  // "connected", and over a real (production) network that gap is wider.
  const remoteTrackCount = (page) =>
    page.evaluate(
      () =>
        [...document.querySelectorAll('audio[data-voice-audio]')].filter(
          (el) => el.srcObject && el.srcObject.getAudioTracks?.().length > 0,
        ).length,
    )
  const waitForRemoteTracks = async (page, n, ms = 15000) => {
    const end = Date.now() + ms
    while (Date.now() < end) {
      if ((await remoteTrackCount(page)) >= n) return true
      await new Promise((r) => setTimeout(r, 300))
    }
    return false
  }
  check(await waitForRemoteTracks(a, 2), 'A has live remote audio from both peers')
  check(await waitForRemoteTracks(b, 2), 'B has live remote audio from both peers')
  check(await waitForRemoteTracks(c, 2), 'C has live remote audio from both peers')

  step('device auto-detect + manual override UI is present')
  check(
    (await a.locator('.voice-device select[aria-label="microphone"]').count()) > 0,
    'microphone selector present (Auto + devices)',
  )

  await a.screenshot({ path: join(SHOTS, 'voice-01-a-mesh3.png') })
  await c.screenshot({ path: join(SHOTS, 'voice-02-c-mesh3.png') })

  // Chromium's fake mic emits a periodic tone, so voice-activity detection should
  // light a speaking ring on someone (self or a peer) within a few seconds.
  step('speaking indicator lights up from the fake mic tone')
  const speakingSeen = async (page, ms = 9000) => {
    const end = Date.now() + ms
    while (Date.now() < end) {
      if ((await page.locator('[data-speaking="true"]').count()) > 0) {
        await page.screenshot({ path: join(SHOTS, 'voice-03-speaking.png') })
        return true
      }
      await new Promise((r) => setTimeout(r, 150))
    }
    return false
  }
  check(await speakingSeen(a), 'A sees a speaking indicator (active-speaker VAD works)')

  step('per-user volume slider adjusts that peer audio element')
  const vslider = a.locator('[data-volume-for]').first()
  const vpid = await vslider.getAttribute('data-volume-for')
  // Set a controlled range input the React way: native value setter + input event.
  await vslider.evaluate((el) => {
    const setter = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value').set
    setter.call(el, '40')
    el.dispatchEvent(new Event('input', { bubbles: true }))
  })
  const vol = await a.evaluate((id) => {
    const au = document.querySelector(`audio[data-voice-audio="${id}"]`)
    return au ? au.volume : -1
  }, vpid)
  check(Math.abs(vol - 0.4) < 0.05, `slider at 40% sets that peer's audio volume to ~0.4 (got ${vol})`)

  step('mute toggles the local mic label')
  await a.getByRole('button', { name: 'mute' }).click()
  check(
    (await a.locator('[data-voice-self]').filter({ hasText: 'muted' }).count()) > 0,
    "A's own chip shows muted after mute",
  )

  // One peer leaving must collapse only its own links — the remaining pair stays up.
  step('B leaves → A and C drop B but stay connected to each other')
  await b.getByRole('button', { name: 'leave' }).click()
  check(await waitForCount(a.locator('[data-voice-peer]'), 1), "A's roster drops to 1 peer (C)")
  check(await waitForCount(c.locator('[data-voice-peer]'), 1), "C's roster drops to 1 peer (A)")
  check(!(await rosterHas(a, userB)), "A's roster no longer lists B")
  check((await connectedPeers(a).count()) === 1, 'A ↔ C link survives B leaving')

  step('C leaves → A sees the roster empty out')
  await c.getByRole('button', { name: 'leave' }).click()
  check(await waitForCount(a.locator('[data-voice-peer]'), 0), "A's voice roster empties after C leaves")

  await browser.close()
  console.log(failed === 0 ? '\nVOICE QA PASSED' : `\nVOICE QA FAILED (${failed} check(s))`)
  process.exit(failed === 0 ? 0 : 1)
}

main().catch((e) => {
  console.error(e)
  process.exit(1)
})
