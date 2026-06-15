// Opencord SFU proof — connects TWO browsers to a REAL LiveKit server using tokens
// minted by Opencord's POST /api/voice/token, and asserts each sees the other as a
// remote participant. This closes the deferred gap from the token-endpoint slice:
// it proves a real LiveKit *accepts* our minted token (format correct) and that the
// SFU forwards participants — the foundation for the client SFU transport.
//
// Assumes a running LiveKit + an Opencord server wired to it (OPENCORD_SFU_*). Boot
// both with qa/sfu-run.sh. Env: OC_API (Opencord base, default http://localhost:8080).
import { chromium } from 'playwright'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

const OC = process.env.OC_API || 'http://localhost:8080'
const UMD = readFileSync(join(import.meta.dirname, 'node_modules/livekit-client/dist/livekit-client.umd.js'), 'utf8')

let failed = 0
const step = (s) => console.log('  → ' + s)
const check = (cond, msg) => {
  console.log((cond ? '  ✓ ' : '  ✗ FAIL: ') + msg)
  if (!cond) failed++
}

async function register(user) {
  const r = await fetch(`${OC}/api/auth/register`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username: user, password: 'hunter2pass' }),
  })
  if (!r.ok) throw new Error(`register ${user}: ${r.status}`)
  return (await r.json()).token
}

async function voiceToken(authToken) {
  const r = await fetch(`${OC}/api/voice/token?channel=1`, {
    method: 'POST',
    headers: { Authorization: `Bearer ${authToken}` },
  })
  if (!r.ok) throw new Error(`voice token: ${r.status}`)
  return r.json() // { sfu, url, room, token }
}

// Connect a page to LiveKit with the given token; keep the room on window.__room
// so we can poll its participant set afterward. Returns {state} or {error}.
async function connectLiveKit(page, url, token) {
  await page.addScriptTag({ content: UMD })
  return page.evaluate(
    async ([url, token]) => {
      const LK = window.LivekitClient || window.LiveKitClient || window.livekit
      if (!LK || !LK.Room) return { error: 'livekit-client UMD global not found' }
      const room = new LK.Room()
      window.__room = room
      try {
        await room.connect(url, token)
      } catch (e) {
        return { error: 'connect failed: ' + (e && e.message) }
      }
      return { state: room.state }
    },
    [url, token],
  )
}

// Poll how many remote participants this page's room currently sees.
async function pollRemotes(page, want = 1, ms = 10000) {
  const end = Date.now() + ms
  while (Date.now() < end) {
    const n = await page.evaluate(() =>
      window.__room && window.__room.remoteParticipants ? window.__room.remoteParticipants.size : -1,
    )
    if (n >= want) return true
    await new Promise((r) => setTimeout(r, 250))
  }
  return false
}

async function main() {
  const sfx = String(Date.now()).slice(-7)
  step(`register two users + mint LiveKit tokens via Opencord (${OC})`)
  const tokA = await voiceToken(await register('sfua' + sfx))
  const tokB = await voiceToken(await register('sfub' + sfx))
  check(tokA.sfu === true && !!tokA.token && tokA.room === 'opencord-ch-1', 'endpoint returned an SFU token for A')
  check(tokB.sfu === true && !!tokB.token, 'endpoint returned an SFU token for B')
  const url = tokA.url
  console.log('  livekit url: ' + url + '  room: ' + tokA.room)

  const browser = await chromium.launch({
    args: ['--use-fake-device-for-media-stream', '--use-fake-ui-for-media-stream', '--autoplay-policy=no-user-gesture-required'],
  })
  // localhost is a secure context (needed for WebRTC); load the Opencord SPA origin.
  const a = await (await browser.newContext()).newPage()
  const b = await (await browser.newContext()).newPage()
  for (const [who, pg] of [['A', a], ['B', b]]) {
    pg.on('console', (m) => { if (m.type() === 'error') console.log(`  [console ${who}] ` + m.text()) })
    await pg.goto(OC + '/', { waitUntil: 'domcontentloaded' })
  }

  step('both browsers connect to the REAL LiveKit with the minted tokens')
  const [ra, rb] = await Promise.all([connectLiveKit(a, url, tokA.token), connectLiveKit(b, url, tokB.token)])
  console.log('  A:', JSON.stringify(ra), ' B:', JSON.stringify(rb))
  check(ra.state === 'connected', 'A connected to LiveKit (token accepted by a real server)')
  check(rb.state === 'connected', 'B connected to LiveKit (token accepted)')

  step('the SFU forwards presence — each sees the other')
  check(await pollRemotes(a), 'A sees B as a remote participant')
  check(await pollRemotes(b), 'B sees A as a remote participant')

  await browser.close()
  console.log(failed === 0 ? '\nSFU PROOF PASSED' : `\nSFU PROOF FAILED (${failed})`)
  process.exit(failed === 0 ? 0 : 1)
}

main().catch((e) => { console.error(e); process.exit(1) })
