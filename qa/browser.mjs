// Opencord browser QA — drives the REAL rendered UI like a user, logging every
// click ("→ ...") and screenshotting each step into qa-screenshots/ for AI-vision
// review. Exits non-zero on any failed assertion or page error.
//
// Assumes a running stack at QA_BASE_URL (default http://localhost:5173).
// Boot one with qa/run.sh (which also runs this).
import { chromium } from 'playwright'
import { AxeBuilder } from '@axe-core/playwright'
import { mkdir } from 'node:fs/promises'
import { join } from 'node:path'
import { makePng } from './fixtures.mjs'

const BASE = process.env.QA_BASE_URL || 'http://localhost:5173'
const SHOTS = process.env.QA_SHOTS || join(import.meta.dirname, 'qa-screenshots')

// A 240×140 blurple rectangle — sniffs as image/png and renders visibly inline.
const PNG_FIXTURE = makePng(240, 140, [88, 101, 242])

let failed = 0
const step = (s) => console.log('  → ' + s)
const check = (cond, msg) => {
  console.log((cond ? '  ✓ ' : '  ✗ FAIL: ') + msg)
  if (!cond) failed++
}

async function main() {
  await mkdir(SHOTS, { recursive: true })
  // Fake media so the single-client voice entry-point check can call getUserMedia
  // headlessly (the rest of the flow is unaffected).
  const browser = await chromium.launch({
    args: [
      '--use-fake-device-for-media-stream',
      '--use-fake-ui-for-media-stream',
      '--autoplay-policy=no-user-gesture-required',
    ],
  })
  const context = await browser.newContext({
    viewport: { width: 1100, height: 820 },
    // Pre-grant OS notification permission so the in-app toggle's
    // Notification.requestPermission() resolves 'granted' headlessly (no real prompt).
    permissions: ['notifications'],
  })
  // Headless Chromium can't show a real OS notification, so STUB window.Notification
  // before the app loads: record every constructed notification to window.__notifs so
  // the desktop-notify step can assert it fired with the right author/body. Also lets
  // us flip document.hidden true (Discord only notifies when the tab is in the
  // background) via Object.defineProperty, since a headless tab is never truly hidden.
  await context.addInitScript(() => {
    window.__notifs = []
    class StubNotification {
      static permission = 'granted'
      static requestPermission() {
        return Promise.resolve('granted')
      }
      constructor(title, opts) {
        this.title = title
        this.body = (opts && opts.body) || ''
        window.__notifs.push({ title: this.title, body: this.body })
      }
      close() {}
      addEventListener() {}
    }
    // Replace the real API with the stub (assignable + queryable like the original).
    Object.defineProperty(window, 'Notification', {
      configurable: true,
      writable: true,
      value: StubNotification,
    })
  })
  const page = await context.newPage()
  page.on('pageerror', (e) => {
    console.log('  [pageerror] ' + e.message)
    failed++
  })

  const chanName = 'qa-' + String(Date.now()).slice(-6)
  // Auto-answer window.prompt with whatever the current step expects, and accept
  // window.confirm (delete). Set `promptAnswer` before an action that prompts.
  let promptAnswer = chanName
  let statusEmojiAnswer = '' // answer specifically for the "Status emoji" prompt
  let lastPromptDefault = '' // capture a prompt's pre-filled value (e.g. an invite code)
  page.on('dialog', (d) => {
    if (d.type() !== 'prompt') {
      d.accept(undefined)
      return
    }
    lastPromptDefault = d.defaultValue()
    // The status flow asks for an emoji first, then the text — answer the emoji
    // prompt from its own variable so it doesn't collide with promptAnswer.
    d.accept(/emoji/i.test(d.message()) ? statusEmojiAnswer : promptAnswer)
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

  // 1b — password show/hide toggle (login redesign): reveal flips the field to text.
  const pwField = page.getByPlaceholder('password')
  await page.getByRole('button', { name: 'Show password' }).click()
  check((await pwField.getAttribute('type')) === 'text', 'Show reveals the password (type=text)')
  await shot('01b-auth-register.png')
  await page.getByRole('button', { name: 'Hide password' }).click()
  check((await pwField.getAttribute('type')) === 'password', 'Hide re-masks the password')

  await page.getByRole('button', { name: 'Create account' }).click()

  // 2 — Chat loads.
  await page.getByPlaceholder(/Message #/).waitFor({ timeout: 15000 })
  await shot('02-chat.png')
  check(await page.getByRole('button', { name: /general/ }).isVisible(), '#general in sidebar after register')

  // 2b — Voice entry point: the control renders, is gated until the WS connects,
  // and joining solo shows the in-voice bar (guards the entry point even without
  // the multi-peer voice.mjs run).
  step('voice: Join control renders + is enabled once connected, then joins solo')
  await page.locator('.dot.online').waitFor({ timeout: 8000 })
  const joinVoiceBtn = page.getByRole('button', { name: /Join voice/ })
  check((await joinVoiceBtn.count()) > 0, 'voice: "Join voice" control is present')
  check(await joinVoiceBtn.isEnabled(), 'voice: "Join voice" is enabled once connected')
  await joinVoiceBtn.click()
  await page.locator('.voice-bar').waitFor({ timeout: 8000 })
  check((await page.locator('[data-voice-self]').count()) > 0, 'voice: joining shows the in-voice bar with your chip')
  await page.getByRole('button', { name: 'leave' }).click()
  await page.locator('.voice-bar').waitFor({ state: 'detached', timeout: 5000 }).catch(() => {})
  check((await page.locator('.voice-bar').count()) === 0, 'voice: leaving removes the voice bar')

  // 3 — Send a message; it renders with an avatar.
  const body = 'hello from the qa bot'
  step('type a message and Send')
  await page.getByPlaceholder(/Message #/).fill(body)
  await page.getByRole('button', { name: 'Send' }).click()
  await page.getByText(body).waitFor({ timeout: 8000 })
  await shot('03-message.png')
  check(await page.getByText(body).isVisible(), 'sent message appears')
  check(await page.locator('.avatar').first().isVisible(), 'avatar renders on the message')
  // A Discord-style day divider sits above the first message of each calendar day.
  // All QA messages are sent now, so the divider reads "Today".
  const divider = page.locator('.day-divider').first()
  check(await divider.isVisible(), 'a day divider renders above the messages')
  check(
    ((await divider.textContent()) ?? '').trim() === 'Today',
    `today's divider reads "Today" (got "${((await divider.textContent()) ?? '').trim()}")`,
  )
  // The message header time is Discord-style "Today at H:MM" (relative day + compact
  // time, no seconds) — not the old "8:53:23 AM".
  const headTime = ((await page.locator('.message .time').first().textContent()) ?? '').trim()
  check(
    /^Today at \d{1,2}:\d{2}/.test(headTime) && !/:\d{2}:\d{2}/.test(headTime),
    `header time is "Today at H:MM" with no seconds (got "${headTime}")`,
  )
  // Discord-style "start of channel" intro sits at the top of the scrollback.
  const intro = page.locator('.channel-intro')
  check(await intro.isVisible(), 'channel intro renders at the top of the message list')
  const introTxt = ((await intro.textContent()) ?? '').trim()
  check(
    introTxt.includes('Welcome to #general') && introTxt.includes('start of the #general channel'),
    `intro welcomes the channel (got "${introTxt.slice(0, 80)}")`,
  )

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
  // Discord-style: a grouped row hides the name/time but reveals a compact gutter
  // timestamp on hover (opacity 0 → 1). Assert it exists, reads HH:MM, and reveals.
  const hoverTime = grouped.locator('.hover-time')
  check((await hoverTime.count()) === 1, 'grouped message has a gutter hover-time')
  const gtText = ((await hoverTime.textContent()) ?? '').trim()
  check(/\d{1,2}:\d{2}/.test(gtText), `gutter time shows HH:MM, no seconds (got "${gtText}")`)
  const opacityIdle = Number(await hoverTime.evaluate((el) => getComputedStyle(el).opacity))
  await grouped.hover()
  await new Promise((r) => setTimeout(r, 150))
  await shot('03a2-hover-time.png')
  const opacityHover = Number(await hoverTime.evaluate((el) => getComputedStyle(el).opacity))
  check(
    opacityIdle < 0.5 && opacityHover > 0.9,
    `hover reveals the gutter time (idle ${opacityIdle} → hover ${opacityHover})`,
  )

  // 3c — Markdown renders to safe elements; raw HTML is escaped (XSS guard, Rule B/15).
  step('send a markdown + <script> + link message → bold/code/link render, script stays literal')
  const md = '**bold** and `code` and <script>alert(1)</script> and http://example.com'
  await page.getByPlaceholder(/Message #/).fill(md)
  await page.getByRole('button', { name: 'Send' }).click()
  const mdMsg = page.locator('.message', { hasText: 'bold' }).last()
  await mdMsg.locator('.body strong').first().waitFor({ timeout: 8000 })
  await shot('03c-markdown.png')
  check(
    (await mdMsg.locator('.body strong').first().textContent())?.trim() === 'bold',
    'markdown **bold** renders as <strong>bold</strong>',
  )
  check(await mdMsg.locator('.body code').first().isVisible(), 'inline `code` renders as <code>')
  const link = mdMsg.locator('.body a[href="http://example.com"]')
  check(await link.isVisible(), 'a http(s) URL renders as a clickable link')
  check(
    (await link.getAttribute('rel'))?.includes('noopener') ?? false,
    'autolink has rel="noopener noreferrer" (tab-nabbing safe)',
  )
  check(
    await mdMsg.getByText('<script>alert(1)</script>').isVisible(),
    'raw <script> shows as literal text (escaped)',
  )
  check(
    (await page.locator('script', { hasText: 'alert(1)' }).count()) === 0,
    'no <script> element was injected into the DOM (XSS-safe)',
  )

  // 3d — Multi-line composer: Shift+Enter inserts a newline, Enter sends.
  step('type two lines with Shift+Enter, send with Enter')
  const composer = page.getByPlaceholder(/Message #/)
  await composer.click()
  await composer.fill('line one')
  await composer.press('Shift+Enter')
  await composer.pressSequentially('line two')
  await composer.press('Enter')
  const multi = page.locator('.message .body', { hasText: 'line two' }).last()
  await multi.waitFor({ timeout: 8000 })
  await shot('03d-multiline.png')
  const multiText = await multi.innerText()
  check(
    multiText.includes('line one') && multiText.includes('line two'),
    'Shift+Enter keeps both lines in one message; Enter sends it',
  )
  check(
    (await page.locator('.message .body', { hasText: 'line one' }).count()) === 1,
    'Shift+Enter did not send "line one" as its own message',
  )

  // 3e — Blockquote (> ) renders; spoiler (||x||) is hidden until clicked.
  step('send a > blockquote + ||spoiler|| → blockquote renders, spoiler reveals on click')
  // Pace first: this is the ~5th WS frame from this client and the per-connection
  // rate limiter (burst 5, +2/s; every message OR typing frame costs a token) would
  // otherwise drop it. Wait for the token bucket to refill so the send lands.
  await page.waitForTimeout(3000)
  await composer.click()
  await composer.fill('> a quoted line')
  await composer.press('Shift+Enter')
  await composer.pressSequentially('||a secret||')
  await composer.press('Shift+Enter')
  await composer.pressSequentially('- item one')
  await composer.press('Shift+Enter')
  await composer.pressSequentially('- item two')
  await composer.press('Enter')
  const bq = page.locator('.message .body blockquote', { hasText: 'a quoted line' }).last()
  await bq.waitFor({ timeout: 8000 })
  check(await bq.isVisible(), 'blockquote (> ) renders as <blockquote>')
  const spoiler = page.locator('.message .body .spoiler', { hasText: 'a secret' }).last()
  await spoiler.waitFor({ timeout: 8000 })
  check(
    !(await spoiler.evaluate((el) => el.classList.contains('shown'))),
    'spoiler starts hidden (not revealed)',
  )
  await shot('03e-spoiler-hidden.png')
  await spoiler.click()
  check(
    await spoiler.evaluate((el) => el.classList.contains('shown')),
    'spoiler reveals on click',
  )
  await shot('03e-spoiler-shown.png')
  // Same message carries a bullet list (the lines after the spoiler).
  const listMsg = page.locator('.message', { hasText: 'item one' }).last()
  check(
    (await listMsg.locator('.body ul li').count()) === 2,
    'bullet list renders as <ul> with two <li>',
  )

  // 3f — @mention: a mention of yourself is highlighted distinctly from others.
  step('send a message mentioning self + another → self-mention is highlighted')
  await page.waitForTimeout(3000) // let the rate-limit bucket refill before this send
  await composer.click()
  await composer.fill(`hey @${user} and @someone_else and @everyone`)
  await composer.press('Enter')
  const mineMention = page.locator('.message .body .mention.mention-me', { hasText: `@${user}` }).last()
  await mineMention.waitFor({ timeout: 8000 })
  await shot('03f-mention.png')
  check(await mineMention.isVisible(), 'a mention of yourself renders with the mention-me style')
  const otherMention = page
    .locator('.message .body .mention:not(.mention-me)', { hasText: '@someone_else' })
    .last()
  check(await otherMention.isVisible(), 'a mention of someone else renders as a plain mention')
  const everyoneMention = page.locator('.message .body .mention.mention-all', { hasText: '@everyone' }).last()
  check(await everyoneMention.isVisible(), '@everyone renders as a highlighted (mention-all) mention')

  // 3f2 — Accessibility: a WCAG 2 A/AA scan of the populated chat (messages,
  // avatars, links, mentions) must have no serious/critical violations (UI north
  // star: accessible). This is content-rich, so it covers contrast etc.
  step('accessibility: axe-core WCAG scan of the chat view (no serious/critical)')
  const axe = await new AxeBuilder({ page })
    .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
    .analyze()
  const bad = axe.violations.filter((v) => v.impact === 'serious' || v.impact === 'critical')
  for (const v of bad) console.log(`     [a11y ${v.impact}] ${v.id}: ${v.help} (${v.nodes.length})`)
  check(bad.length === 0, `no serious/critical a11y violations (found ${bad.length})`)

  // 3f3 — Keyboard focus ring: the global a11y baseline must give every interactive
  // element a VISIBLE ring under keyboard focus. :focus-visible never matches a mouse
  // click, so we Tab through (which DOES match) until a <button> is focused — inputs use
  // a border instead of an outline, so target a button to assert the outline baseline.
  step('accessibility: keyboard Tab shows a visible focus ring on a focused button')
  await page.evaluate(() => document.activeElement && document.activeElement.blur())
  let focusRing = null
  for (let i = 0; i < 14; i++) {
    await page.keyboard.press('Tab')
    focusRing = await page.evaluate(() => {
      const el = document.activeElement
      if (!el || el.tagName !== 'BUTTON') return null
      const s = getComputedStyle(el)
      return {
        outlineStyle: s.outlineStyle,
        outlineWidth: parseFloat(s.outlineWidth) || 0,
        label: (el.textContent || '').trim().slice(0, 24),
      }
    })
    if (focusRing) break
  }
  check(
    !!focusRing && focusRing.outlineStyle !== 'none' && focusRing.outlineWidth > 0,
    `keyboard focus shows a visible ring on a button (got ${JSON.stringify(focusRing)})`,
  )
  await shot('03f3-focus-ring.png')

  // 3f4 — Press (:active) feedback: holding the pointer down on a button must dim it
  // (opacity < 1). The app had hover states but ZERO press feedback before this;
  // assert a held button is visibly pressed, then release. (Uses opacity, not a
  // transform — a positional nudge would move the element and break the click.)
  step('press feedback: a held button dims (:active)')
  const pressBtn = page.getByRole('button', { name: /general/ }).first()
  await pressBtn.hover()
  await page.mouse.down()
  const pressed = await pressBtn.evaluate((el) => {
    const s = getComputedStyle(el)
    return { opacity: parseFloat(s.opacity), filter: s.filter }
  })
  await page.mouse.up()
  check(
    pressed.opacity < 1 || pressed.filter !== 'none',
    `a held button shows :active feedback (got ${JSON.stringify(pressed)})`,
  )

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

  // 3g — Reply: hover a message → reply → the composer shows a "Replying to" bar →
  // send → the new message renders a quoted preview of the original (chat parity).
  step('hover message → reply → send → quoted reply preview renders')
  await page.waitForTimeout(3000) // let the rate-limit bucket refill before this send
  await msg.hover()
  await msg.getByRole('button', { name: 'reply' }).click()
  const replyBar = page.locator('.reply-bar')
  await replyBar.waitFor({ timeout: 8000 })
  await shot('03g-reply-bar.png')
  check(await replyBar.getByText('Replying to').isVisible(), 'composer shows the "Replying to" bar')
  check(
    (await replyBar.locator('strong').textContent())?.trim() === user,
    'reply bar names the original author',
  )
  const replyBody = 'a reply to the first message'
  await composer.click()
  await composer.fill(replyBody)
  await composer.press('Enter')
  const replyMsg = page.locator('.message', { hasText: replyBody }).last()
  await replyMsg.locator('.reply-context').waitFor({ timeout: 8000 })
  await shot('03g-reply-sent.png')
  check(
    (await replyMsg.locator('.reply-context .reply-author').textContent())?.trim() === user,
    'reply preview shows the original author',
  )
  check(
    (await replyMsg.locator('.reply-context .reply-snippet').textContent())?.includes(body),
    'reply preview shows the original message snippet',
  )
  check(
    (await page.locator('.reply-bar').count()) === 0,
    'the "Replying to" bar clears after sending the reply',
  )

  // 3h — Jump-to-message: clicking the quoted reply preview scrolls to + briefly flashes
  // the original message it points to (Discord parity). The flash class is transient
  // (~1.5s), so catch it right after the click.
  step('click the reply preview → the original message flashes (jump-to-message)')
  await replyMsg.locator('.reply-context').click()
  const flashed = page.locator('.message.flash')
  await flashed.waitFor({ timeout: 4000 })
  check(
    ((await flashed.locator('.body').first().textContent()) ?? '').includes(body),
    'clicking the reply preview flashes the original message it points to',
  )
  await shot('03h-jump.png')

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

  // 6c — Attachments: stage an image via the 📎 picker → it shows as a pending chip →
  // Send → it renders inline (and actually decodes) in the message. (file/image
  // attachments — the HTTP multipart path, not the WS.)
  step('attach an image via 📎 → pending chip → send → inline image renders')
  await page.waitForTimeout(3000) // rate-limit refill before the send
  await page.locator('.composer input[type=file]').setInputFiles({
    name: 'qa-pic.png',
    mimeType: 'image/png',
    buffer: PNG_FIXTURE,
  })
  await page.locator('.pending-file-name').waitFor({ timeout: 8000 })
  await shot('06c-pending-file.png')
  check(await page.locator('.pending-file-name').isVisible(), 'staged file shows as a pending chip')
  await page.getByRole('button', { name: /Send|Sending/ }).click()
  const imgAttach = page.locator('.message .attachment-image').last()
  await imgAttach.waitFor({ timeout: 12000 })
  await shot('06c-attachment-image.png')
  check(await imgAttach.isVisible(), 'uploaded image renders inline in the message')
  check(
    await imgAttach.evaluate((el) => el.complete && el.naturalWidth > 0),
    'inline image actually decoded (naturalWidth > 0), not a broken-image icon',
  )
  check(
    (await page.locator('.pending-files').count()) === 0,
    'the pending-files strip clears after sending',
  )

  // 6d — A non-image file renders as a download chip (not inline), with a size.
  step('attach a .txt file → it renders as a download chip, not inline')
  await page.waitForTimeout(3000) // rate-limit refill
  await page.locator('.composer input[type=file]').setInputFiles({
    name: 'notes.txt',
    mimeType: 'text/plain',
    buffer: Buffer.from('opencord qa attachment — plain text file\n'),
  })
  await page.locator('.pending-file-name').waitFor({ timeout: 8000 })
  await page.getByRole('button', { name: /Send|Sending/ }).click()
  const fileChip = page.locator('.message .attachment-file', { hasText: 'notes.txt' }).last()
  await fileChip.waitFor({ timeout: 10000 })
  await shot('06d-attachment-file.png')
  check(await fileChip.isVisible(), 'non-image file renders as a download chip')
  check(
    ((await fileChip.locator('.attachment-size').textContent()) ?? '').trim().length > 0,
    'download chip shows a human file size',
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

  // 7-cat — Channel categories: the owner creates a category, adds a channel inside it,
  // and the category renders as a collapsible group that nests its channel.
  step('create a category → add a channel in it → collapse/expand the group')
  promptAnswer = 'Text Channels'
  await page
    .locator('.server-group', { hasText: 'qa server' })
    .getByRole('button', { name: '+ category' })
    .click()
  const catGroup = page.locator('.channel-category', { hasText: 'Text Channels' })
  await catGroup.waitFor({ timeout: 8000 })
  check(await catGroup.isVisible(), 'the new category appears as a collapsible group')
  const catChan = 'cat' + String(Date.now()).slice(-6)
  promptAnswer = catChan
  await catGroup.locator('.category-add').click()
  const catChanBtn = catGroup.locator('.channel-item', { hasText: catChan })
  await catChanBtn.waitFor({ timeout: 8000 })
  check(await catChanBtn.isVisible(), 'a channel created in the category nests under it')
  await shot('07-cat.png')
  // Collapse → the nested channel hides; expand → it returns.
  await catGroup.locator('.category-toggle').click()
  await catChanBtn.waitFor({ state: 'detached', timeout: 8000 }).catch(() => {})
  check(
    (await catGroup.locator('.channel-item', { hasText: catChan }).count()) === 0,
    'collapsing the category hides its channels',
  )
  await catGroup.locator('.category-toggle').click()
  await catGroup.locator('.channel-item', { hasText: catChan }).waitFor({ timeout: 8000 })
  check(
    await catGroup.locator('.channel-item', { hasText: catChan }).isVisible(),
    'expanding the category shows its channels again',
  )
  // Delete the category → the collapsible group disappears, but its channel survives
  // (now uncategorized, rendered at the top) — the server sets category_id NULL.
  await catGroup.locator('.category-del').click() // confirm auto-accepts
  await catGroup.waitFor({ state: 'detached', timeout: 8000 }).catch(() => {})
  check(
    (await page.locator('.channel-category', { hasText: 'Text Channels' }).count()) === 0,
    'deleting a category removes the collapsible group',
  )
  check(
    await page.getByRole('button', { name: new RegExp(catChan) }).isVisible(),
    'the category’s channel survives (now uncategorized) after the category is deleted',
  )
  // Restore the active channel to the one holding our posted message — later steps
  // (search) run against the active channel, which we switched away from above.
  await page.getByRole('button', { name: new RegExp(srvChan) }).click()
  await page.getByPlaceholder(new RegExp('Message #' + srvChan)).waitFor({ timeout: 8000 })

  // 7a-fix — Header must stay clean in a server channel. The member-list sidebar
  // (~220px, shown >900px) narrows this column, so the header controls have far
  // less room than in #general. Regression for the iter-90 AI-vision finding:
  // without flex-wrap the flex items shrank to min-content and wrapped their TEXT
  // across lines — "Join voice" → 2 lines, "1 online ·" → 3 lines, "log out" →
  // 2 lines — a cramped, broken-looking header. The objective signal is rendered
  // text-line count per control: each label control must occupy a single line.
  step('server-channel header stays clean under the member-list sidebar (no text wrapping)')
  check(await page.locator('.member-list').isVisible(), 'member-list sidebar is present (the narrow-header case)')
  const headerLines = await page.locator('.chat-header').evaluate((header) => {
    // Rendered text lines of an element, padding/border-corrected (robust to
    // inline-block buttons whose getClientRects() collapses to one box).
    const lines = (sel) => {
      const el = header.querySelector(sel)
      if (!el) return null
      const cs = getComputedStyle(el)
      const lh = parseFloat(cs.lineHeight) || parseFloat(cs.fontSize) * 1.4
      const padV = parseFloat(cs.paddingTop) + parseFloat(cs.paddingBottom)
      const bV = parseFloat(cs.borderTopWidth) + parseFloat(cs.borderBottomWidth)
      const contentH = el.getBoundingClientRect().height - padV - bV
      return Math.max(1, Math.round(contentH / lh))
    }
    return {
      brand: lines('.brand'),
      voice: lines('.voice-join'),
      logout: lines('.meta .link'),
      hOverflow: header.scrollWidth - header.clientWidth,
    }
  })
  check(headerLines.brand === 1, `brand ("Opencord #channel") stays on one line (got ${headerLines.brand})`)
  check(headerLines.voice === 1, `"Join voice" stays on one line (got ${headerLines.voice})`)
  check(headerLines.logout === 1, `"log out" stays on one line (got ${headerLines.logout})`)
  check(headerLines.hOverflow <= 1, 'header has no horizontal overflow (scrollWidth ≤ clientWidth)')
  check(
    await page.getByRole('button', { name: /Join voice/ }).isVisible(),
    'Join voice button is reachable in the server-channel header',
  )
  check(await page.getByRole('button', { name: 'log out' }).isVisible(), 'log out is reachable in the server-channel header')

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
  const searchBox = page.locator('.search-input')
  await searchBox.fill('server')
  await searchBox.press('Enter')
  await page.locator('.search-results').waitFor({ timeout: 8000 })
  await shot('07c-search.png')
  check(
    await page.locator('.search-results').getByText(srvBody).isVisible(),
    'search finds the matching message',
  )
  await page.getByRole('button', { name: 'clear' }).click()
  check((await page.locator('.search-results').count()) === 0, 'clearing search returns to the channel')

  // 7c-jump — a search result is clickable: clicking it closes the panel and jumps to +
  // flashes the message in the channel (jump-to-message via the close-a-panel path).
  step('click a search result → panel closes + the message flashes')
  await searchBox.fill('server')
  await searchBox.press('Enter')
  const sResult = page.locator('.search-results .message.jumpable', { hasText: srvBody }).first()
  await sResult.waitFor({ timeout: 8000 })
  await sResult.click()
  check(
    (await page.locator('.search-results').count()) === 0,
    'clicking a search result closes the search panel (back to the channel)',
  )
  const sFlash = page.locator('.message.flash', { hasText: srvBody })
  await sFlash.waitFor({ timeout: 4000 })
  check((await sFlash.count()) > 0, 'clicking a search result flashes the jumped-to message')
  await shot('07c-jump.png')

  // 7c2 — Search operators: from:<author> finds the message; from:<nobody> finds none.
  step('search operator from: filters by author')
  await searchBox.fill('from:' + user)
  await searchBox.press('Enter')
  await page.locator('.search-results').waitFor({ timeout: 8000 })
  check(
    await page.locator('.search-results').getByText(srvBody).isVisible(),
    'from:<self> finds my message in the channel',
  )
  await page.getByRole('button', { name: 'clear' }).click()
  await searchBox.fill('from:nobody_' + user)
  await searchBox.press('Enter')
  await page.locator('.search-results').waitFor({ timeout: 8000 })
  check(
    (await page.locator('.search-results').getByText(srvBody).count()) === 0,
    'from:<unknown-user> matches nothing',
  )
  await page.getByRole('button', { name: 'clear' }).click()

  // 7c3 — Date operators: a far-future before: matches the just-posted message;
  // a far-future after: matches nothing (day-exclusive bounds). Fixed dates keep
  // the assertion deterministic regardless of when QA runs.
  step('search operators before:/after: filter by date')
  await searchBox.fill('before:2099-01-01')
  await searchBox.press('Enter')
  await page.locator('.search-results').waitFor({ timeout: 8000 })
  check(
    await page.locator('.search-results').getByText(srvBody).isVisible(),
    'before:<far-future> finds my recent message',
  )
  await page.getByRole('button', { name: 'clear' }).click()
  await searchBox.fill('after:2099-01-01')
  await searchBox.press('Enter')
  await page.locator('.search-results').waitFor({ timeout: 8000 })
  check(
    (await page.locator('.search-results').getByText(srvBody).count()) === 0,
    'after:<far-future> matches nothing',
  )
  await page.getByRole('button', { name: 'clear' }).click()

  // 7i — Custom emoji: as the server owner, upload a custom emoji via the API (multipart
  // POST /api/servers/{id}/emoji), then post `:qa_emoji:` in the server channel and assert
  // it renders as an inline <img class="emoji-inline"> from /api/emoji/{id}. Server-scoped:
  // the name only resolves to an image inside the server that owns it.
  step('upload a custom emoji → :qa_emoji: renders as an inline image in the server channel')
  // Do the upload in the page so it shares the origin + the token in localStorage. We
  // pass the PNG as base64 and rebuild a Blob; the server derives the uploader from JWT.
  const emojiUpload = await page.evaluate(async (pngB64) => {
    const token = localStorage.getItem('opencord.token')
    if (!token) return { ok: false, why: 'no token' }
    const auth = { Authorization: 'Bearer ' + token }
    // Find the "qa server" we created earlier and grab its id.
    const servers = await fetch('/api/servers', { headers: auth }).then((r) => r.json())
    const srv = (servers || []).find((s) => s.name === 'qa server')
    if (!srv) return { ok: false, why: 'qa server not found' }
    // base64 → bytes → PNG Blob.
    const bin = atob(pngB64)
    const bytes = new Uint8Array(bin.length)
    for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i)
    const form = new FormData()
    form.append('name', 'qa_emoji')
    form.append('file', new Blob([bytes], { type: 'image/png' }), 'qa_emoji.png')
    const res = await fetch(`/api/servers/${srv.id}/emoji`, {
      method: 'POST',
      headers: auth, // no Content-Type — the browser sets the multipart boundary
      body: form,
    })
    const body = await res.json().catch(() => ({}))
    return { ok: res.ok || res.status === 201, status: res.status, serverId: srv.id, body }
  }, PNG_FIXTURE.toString('base64'))
  check(emojiUpload.ok, `custom emoji uploads via the API (status ${emojiUpload.status})`)
  check(
    !!emojiUpload.body && emojiUpload.body.name === 'qa_emoji',
    'upload returns the created emoji record (name=qa_emoji)',
  )
  // The client caches each server's emoji map once (so it doesn't refetch on every
  // channel switch). This server's map was already cached (empty) before the upload, so
  // reload the page — a real user reopening the app picks up newly-added emoji — which
  // clears the in-memory cache; the session persists via localStorage (token + user).
  await page.reload({ waitUntil: 'domcontentloaded' })
  await page.getByPlaceholder(/Message #/).waitFor({ timeout: 15000 })
  // Re-enter the server channel and let the fresh per-server emoji fetch settle (and the
  // message rate-limiter refill).
  await page.getByRole('button', { name: new RegExp(srvChan) }).click()
  await page.getByPlaceholder(new RegExp('Message #' + srvChan)).waitFor({ timeout: 8000 })
  await page.waitForTimeout(1500)
  // Post a message containing the shortcode in the server channel.
  await page.getByPlaceholder(new RegExp('Message #' + srvChan)).fill('react with :qa_emoji: now')
  await page.getByRole('button', { name: 'Send' }).click()
  // The literal text shouldn't appear as `:qa_emoji:` — it becomes an image. Assert the
  // inline emoji img is present in a message, with a src pointing at /api/emoji/.
  const emojiImg = page.locator('.message .body img.emoji-inline').last()
  await emojiImg.waitFor({ timeout: 8000 })
  // The src is now a blob: URL (the bytes are fetched WITH the bearer token and wrapped
  // in an object URL — an auth-gated <img src> can't send the header), so assert the
  // numeric emoji id via the data-emoji-id attribute instead of the src path.
  const emojiId = (await emojiImg.getAttribute('data-emoji-id')) || ''
  const emojiAlt = (await emojiImg.getAttribute('alt')) || ''
  await shot('03i-custom-emoji.png')
  check(
    await emojiImg.isVisible(),
    ':qa_emoji: renders as an inline img.emoji-inline in the server channel',
  )
  check(/^\d+$/.test(emojiId), `inline emoji carries a numeric data-emoji-id (got ${emojiId})`)
  check(emojiAlt === ':qa_emoji:', `inline emoji keeps its shortcode as alt text (got ${emojiAlt})`)
  // The image must actually LOAD (decode), not just exist as an element — a broken
  // emoji would render the alt text + a broken-image icon and pass the element checks
  // above. This is the whole point of the auth-gated-blob fix: it MUST now load. Poll
  // naturalWidth>0 (waiting for the decode) so this isn't a race against the download.
  let emojiLoaded = false
  for (let i = 0; i < 40; i++) {
    emojiLoaded = await emojiImg.evaluate((img) => img.complete && img.naturalWidth > 0).catch(() => false)
    if (emojiLoaded) break
    await new Promise((r) => setTimeout(r, 200))
  }
  check(emojiLoaded, 'the inline emoji image actually LOADS (naturalWidth>0, not a broken image)')

  // 7d — Members panel: the server owner sees themselves with the owner role.
  step('open the server members panel')
  await page
    .locator('.server-group', { hasText: 'qa server' })
    .getByRole('button', { name: 'members' })
    .click()
  await page.locator('.member-row').first().waitFor({ timeout: 8000 })
  await shot('07d-members.png')
  check(
    await page.locator('.member-row .role-badge.role-owner').first().isVisible(),
    'members panel shows the owner role',
  )

  // 7d3 — Invites management (admin): the section lists the active code, a freshly
  // created invite appears (unlimited + a max-uses one), and revoking one removes it. The
  // QA bot is the owner (admin), so the admin-only Invites section renders. The "+ New
  // invite" button prompts for a max-uses cap; the dialog handler auto-answers it with
  // `promptAnswer` (and also the revoke confirm + any copy-fallback prompt).
  step('invites section: list, create (unlimited + capped), revoke')
  const invitesHead = page.locator('.invites-head')
  await invitesHead.waitFor({ timeout: 8000 })
  check(await invitesHead.isVisible(), 'admin sees the Invites section in the members panel')
  const invitesBefore = await page.locator('.invite-row').count()
  check(invitesBefore >= 1, 'the previously-minted invite is listed')
  // Create an unlimited invite (blank max-uses).
  promptAnswer = ''
  await page.locator('.new-invite-btn').click()
  await page.waitForFunction(
    (n) => document.querySelectorAll('.invite-row').length > n,
    invitesBefore,
    { timeout: 8000 },
  )
  const invitesAfterCreate = await page.locator('.invite-row').count()
  check(invitesAfterCreate > invitesBefore, 'creating an invite adds it to the list')
  check(
    (await page.locator('.invite-meta', { hasText: /\d+ uses?/ }).count()) > 0,
    'an invite row shows a uses count',
  )
  // Create a capped invite (max 5 uses) → a row shows "0/5 uses".
  promptAnswer = '5'
  await page.locator('.new-invite-btn').click()
  await page.locator('.invite-meta', { hasText: '0/5 uses' }).first().waitFor({ timeout: 8000 })
  check(
    (await page.locator('.invite-meta', { hasText: '0/5 uses' }).count()) > 0,
    'a max-uses invite renders its 0/5 uses cap',
  )
  await shot('07d3-invites.png')
  const invitesAfterCapped = await page.locator('.invite-row').count()
  await page.locator('.invite-row .revoke-invite-btn').first().click()
  await page.waitForFunction(
    (n) => document.querySelectorAll('.invite-row').length < n,
    invitesAfterCapped,
    { timeout: 8000 },
  )
  check(
    (await page.locator('.invite-row').count()) < invitesAfterCapped,
    'revoking an invite removes it from the list',
  )

  // 7d3e — Emoji manager (slice 3a): the admin uploads a custom emoji THROUGH THE UI
  // (name + image file) in the members panel, the new emoji appears in the manager list,
  // and a `:ui_emoji:` sent right after renders as an inline image WITHOUT a page reload
  // (proving the upload invalidated the per-server `:name:` cache live — the slice-3a UX
  // win over slices 1-2). Then deleting it via the manager removes it from the list.
  // The members panel is still open from the invites steps above.
  step('emoji manager: upload :ui_emoji: via the UI → list + live :name: render (no reload) → delete')
  const emojiHead = page.locator('.emoji-manager-head')
  await emojiHead.waitFor({ timeout: 8000 })
  check(await emojiHead.isVisible(), 'admin sees the Emoji manager section in the members panel')
  const emojiRowsBefore = await page.locator('.emoji-manager-row').count()
  // Fill the name + set the file input to a tiny PNG, then click Upload.
  await page.getByLabel('emoji name').fill('ui_emoji')
  await page.getByLabel('emoji image').setInputFiles({
    name: 'ui_emoji.png',
    mimeType: 'image/png',
    buffer: PNG_FIXTURE,
  })
  await page.locator('.emoji-upload-btn').click()
  // The new emoji row appears (its image src points at /api/emoji/{id}).
  const uiEmojiRow = page.locator('.emoji-manager-row', { hasText: ':ui_emoji:' })
  await uiEmojiRow.waitFor({ timeout: 8000 })
  await shot('03j-emoji-manager.png')
  check(await uiEmojiRow.isVisible(), 'the uploaded emoji appears as a row in the manager list')
  check(
    (await page.locator('.emoji-manager-row').count()) > emojiRowsBefore,
    'uploading via the UI adds a row to the emoji list',
  )
  check(
    /^\d+$/.test((await uiEmojiRow.locator('.emoji-manager-img').getAttribute('data-emoji-id')) || ''),
    'the manager row image carries a numeric data-emoji-id (src is now a blob: URL)',
  )
  // Close the panel and send `:ui_emoji:` in the server channel — NO page reload. The
  // upload invalidated the per-server cache, so the renderer must resolve it to an image.
  await page.getByRole('button', { name: 'close' }).click()
  await page.locator('.search-results').waitFor({ state: 'detached', timeout: 4000 }).catch(() => {})
  await page.getByPlaceholder(new RegExp('Message #' + srvChan)).waitFor({ timeout: 8000 })
  await page.waitForTimeout(1200) // let the message rate-limiter refill
  await page.getByPlaceholder(new RegExp('Message #' + srvChan)).fill('live emoji :ui_emoji: yes')
  await page.getByRole('button', { name: 'Send' }).click()
  const uiEmojiImg = page.locator('.message .body img.emoji-inline[alt=":ui_emoji:"]').last()
  await uiEmojiImg.waitFor({ timeout: 8000 })
  check(
    await uiEmojiImg.isVisible(),
    ':ui_emoji: renders as an inline img.emoji-inline live after a UI upload (NO page reload)',
  )
  check(
    /^\d+$/.test((await uiEmojiImg.getAttribute('data-emoji-id')) || ''),
    'the live-rendered emoji carries a numeric data-emoji-id (cache was invalidated, not reloaded)',
  )

  // 7d3f — Composer emoji picker (slice 3b): the 🙂 toggle appears in the composer because
  // the active server now has custom emoji. Open it → the popover lists :ui_emoji: as an
  // image button → click inserts `:ui_emoji:` into the draft at the caret → Send → it
  // renders as an inline image. Pure convenience over typing `:name:`. (The panel is
  // closed and we're focused on the server channel, with `ui_emoji` in the cache.)
  step('composer emoji picker: open → lists :ui_emoji: → click inserts the shortcode → renders')
  const emojiPickerBtn = page.getByRole('button', { name: 'insert custom emoji' })
  await emojiPickerBtn.waitFor({ timeout: 8000 })
  check(
    await emojiPickerBtn.isVisible(),
    'the composer emoji-picker button appears (server has custom emoji)',
  )
  // Type a draft first so we can prove the shortcode is inserted at the caret, not nuked.
  const composerInput = page.getByPlaceholder(new RegExp('Message #' + srvChan))
  await composerInput.fill('pick: ')
  await emojiPickerBtn.click()
  const emojiPopover = page.locator('.emoji-picker-popover')
  await emojiPopover.waitFor({ timeout: 4000 })
  check(await emojiPopover.isVisible(), 'clicking the button opens the emoji-picker popover')
  const popoverItem = emojiPopover.locator('.emoji-picker-item[data-emoji-name="ui_emoji"]')
  await popoverItem.waitFor({ timeout: 4000 })
  check(
    await popoverItem.locator('img.emoji-inline').isVisible(),
    'the popover lists :ui_emoji: as an img.emoji-inline button',
  )
  await shot('03k-emoji-picker.png')
  // Click the emoji → it inserts `:ui_emoji:` into the draft and closes the popover.
  await popoverItem.click()
  await emojiPopover.waitFor({ state: 'detached', timeout: 4000 })
  check((await page.locator('.emoji-picker-popover').count()) === 0, 'selecting an emoji closes the popover')
  const draftAfter = await composerInput.inputValue()
  check(
    draftAfter.includes(':ui_emoji:'),
    `clicking inserts :ui_emoji: into the composer draft (got "${draftAfter}")`,
  )
  // Send it → it renders as an inline image (same path as a manually-typed shortcode).
  await page.waitForTimeout(1200) // let the message rate-limiter refill
  await page.getByRole('button', { name: 'Send' }).click()
  const pickedEmojiImg = page.locator('.message .body img.emoji-inline[alt=":ui_emoji:"]').last()
  await pickedEmojiImg.waitFor({ timeout: 8000 })
  check(
    await pickedEmojiImg.isVisible(),
    'a message composed via the picker renders :ui_emoji: as an inline img.emoji-inline',
  )

  // 03l — Custom-emoji REACTION (slice over slice-3b): the active server has :ui_emoji:
  // in its cache, so the per-message reaction palette now lists it AFTER the unicode quick
  // emoji. Hover a message → react → pick the custom emoji → a reaction chip appears that
  // contains an img.emoji-inline pointing at /api/emoji/{id} and is highlighted as .mine.
  // Toggling it off removes the chip. The marker is stored verbatim as `custom:{id}` in the
  // existing reactions column (no schema/WS change). (We're focused on the server channel
  // with `ui_emoji` cached; the members panel is closed.)
  step('hover message → react → pick the CUSTOM emoji → custom reaction chip appears (.mine) → toggle off')
  // Post a fresh plain-text target message in the server channel (mirrors the 03b unicode
  // reaction step — reacting to a known-text message avoids the bottom-edge hover-overlay
  // interception). The custom emoji shows up in the palette because the active server has
  // :ui_emoji: cached, independent of the message's own content.
  await page.waitForTimeout(1200) // let the message rate-limiter refill
  const customReactBody = 'react to me with a custom emoji'
  await page.getByPlaceholder(new RegExp('Message #' + srvChan)).fill(customReactBody)
  await page.getByRole('button', { name: 'Send' }).click()
  const customReactMsg = page.locator('.message', { hasText: customReactBody }).first()
  await customReactMsg.waitFor({ timeout: 8000 })
  await customReactMsg.hover()
  await customReactMsg.getByRole('button', { name: 'react' }).click()
  const reactPalette = page.locator('.emoji-picker')
  await reactPalette.waitFor({ timeout: 4000 })
  const customOption = reactPalette.locator('.emoji-option[data-emoji-name="ui_emoji"]')
  await customOption.waitFor({ timeout: 4000 })
  check(
    await customOption.locator('img.emoji-inline').isVisible(),
    'the reaction palette lists the custom emoji as an img.emoji-inline option (after the unicode ones)',
  )
  await customOption.click()
  // A reaction chip appears, highlighted as mine, holding the custom-emoji image.
  const customChip = customReactMsg.locator('.reaction.mine', {
    has: page.locator('img.emoji-inline'),
  })
  await customChip.waitFor({ timeout: 8000 })
  await shot('03l-custom-reaction.png')
  check(await customChip.isVisible(), 'a custom-emoji reaction chip appears, highlighted as mine')
  const customChipImg = customChip.locator('img.emoji-inline')
  const customChipId = (await customChipImg.getAttribute('data-emoji-id')) || ''
  check(
    /^\d+$/.test(customChipId),
    `the custom reaction chip image carries a numeric data-emoji-id (got ${customChipId})`,
  )
  // Like the inline emoji, the chip image must actually LOAD (it's a token-fetched blob:
  // URL now) — poll naturalWidth>0 so a broken image can't pass the element checks above.
  let chipLoaded = false
  for (let i = 0; i < 40; i++) {
    chipLoaded = await customChipImg.evaluate((img) => img.complete && img.naturalWidth > 0).catch(() => false)
    if (chipLoaded) break
    await new Promise((r) => setTimeout(r, 200))
  }
  check(chipLoaded, 'the custom reaction chip image actually LOADS (naturalWidth>0, not a broken image)')
  check(
    (await customChip.locator('.rcount').textContent())?.trim() === '1',
    'the custom reaction count shows 1',
  )
  // Toggle it off → the chip is removed (same opaque-marker toggle as a unicode reaction).
  await customChip.click()
  await customChip.waitFor({ state: 'detached', timeout: 8000 })
  check(
    (await customReactMsg.locator('.reaction.mine', { has: page.locator('img.emoji-inline') }).count()) === 0,
    'toggling the custom reaction off removes the chip',
  )

  // Reopen the members panel → delete the emoji via the manager → its row disappears.
  await page
    .locator('.server-group', { hasText: 'qa server' })
    .getByRole('button', { name: 'members' })
    .click()
  await page.locator('.emoji-manager-row', { hasText: ':ui_emoji:' }).waitFor({ timeout: 8000 })
  const emojiRowsBeforeDel = await page.locator('.emoji-manager-row').count()
  await page
    .locator('.emoji-manager-row', { hasText: ':ui_emoji:' })
    .locator('.emoji-delete-btn')
    .click() // the confirm() is auto-accepted by the dialog handler
  await page
    .locator('.emoji-manager-row', { hasText: ':ui_emoji:' })
    .waitFor({ state: 'detached', timeout: 8000 })
  check(
    (await page.locator('.emoji-manager-row', { hasText: ':ui_emoji:' }).count()) === 0 &&
      (await page.locator('.emoji-manager-row').count()) < emojiRowsBeforeDel,
    'deleting an emoji via the manager removes it from the list',
  )
  await page.getByRole('button', { name: 'close' }).click()

  // 7d0 — User Settings modal opens from the header ⚙ and closes on Esc. The account
  // controls (avatar, custom status + emoji, presence) now live inside it (Discord-style).
  step('open User Settings via the ⚙ chip → modal renders; Esc closes it')
  await page.getByRole('button', { name: 'user settings' }).click()
  await page.locator('.settings-modal').waitFor({ timeout: 8000 })
  check(await page.locator('.settings-modal').isVisible(), 'User Settings modal opens from the ⚙ chip')
  check(
    await page.locator('.settings-tab.active', { hasText: 'My Account' }).isVisible(),
    'My Account tab is active by default',
  )
  await shot('07d0-settings-open.png')
  await page.keyboard.press('Escape')
  await page.locator('.settings-modal').waitFor({ state: 'detached', timeout: 4000 })
  check((await page.locator('.settings-modal').count()) === 0, 'Esc closes the settings modal')

  // 7d2 — Custom status + emoji: set both inside the settings modal (My Account),
  // Save → they render under your name in the member list.
  step('settings → set a custom status + emoji → both show in the member list')
  const myStatus = 'shipping presence'
  const myStatusEmoji = '🚀'
  await page.getByRole('button', { name: 'user settings' }).click()
  await page.locator('.settings-modal').waitFor({ timeout: 8000 })
  await page.getByLabel('status emoji').fill(myStatusEmoji)
  await page.getByLabel('custom status').fill(myStatus)
  // exact: the My Account tab also has a "Save profile" button which contains "Save".
  await page.locator('.settings-modal').getByRole('button', { name: 'Save', exact: true }).click()
  await page
    .locator('.member-list .member-status', { hasText: myStatus })
    .waitFor({ timeout: 8000 })
  check(
    await page.locator('.member-list .member-status', { hasText: myStatus }).isVisible(),
    'custom status renders under the member name in the sidebar',
  )
  check(
    ((await page.locator('.member-list .member-status .status-emoji').first().textContent()) ?? '').includes(
      myStatusEmoji,
    ),
    'status emoji renders before the status line in the member list',
  )
  await shot('07d2-status.png')

  // 7d2b — Presence picker (in settings): choosing "Do Not Disturb" recolors the self
  // dot (red) in the member list; reset back to online afterwards. Modal still open.
  step('settings → set presence to Do Not Disturb → self dot recolors; reset online')
  await page.locator('.settings-modal .presence-select').selectOption('dnd')
  await page.locator('.member-list .presence-dot.dnd').first().waitFor({ timeout: 8000 })
  check(
    await page.locator('.member-list .presence-dot.dnd').first().isVisible(),
    'self presence dot turns dnd (red) in the member list',
  )
  await shot('07d2b-presence.png')
  await page.locator('.settings-modal .presence-select').selectOption('online') // reset for later steps
  await page.locator('.member-list .presence-dot.online').first().waitFor({ timeout: 8000 })
  await page.getByRole('button', { name: 'close settings' }).click()
  await page.locator('.settings-modal').waitFor({ state: 'detached', timeout: 4000 })

  // 7d3 — Voice & Video settings tab (slice 3a): the DSP toggles render, a toggle
  // persists across a reload (localStorage), and the mic-test meter responds to the
  // fake-device tone. Chromium runs with --use-fake-device/ui-for-media-stream, so
  // getUserMedia resolves and the synthetic mic emits a continuous tone.
  step('settings → Voice & Video tab → DSP toggles render; mic-test meter moves')
  await page.getByRole('button', { name: 'user settings' }).click()
  await page.locator('.settings-modal').waitFor({ timeout: 8000 })
  await page.locator('.settings-tab', { hasText: 'Voice & Video' }).click()
  await page.getByLabel('noise suppression').waitFor({ timeout: 8000 })
  check(
    await page.getByLabel('noise suppression').isVisible() &&
      (await page.getByLabel('echo cancellation').isVisible()) &&
      (await page.getByLabel('automatic gain control').isVisible()),
    'the 3 DSP toggles render in the Voice & Video tab',
  )
  check(
    await page.getByLabel('input device').isVisible() && (await page.getByLabel('output device').isVisible()),
    'input + output device pickers render',
  )
  // Output volume slider: render + drag to 50% + assert it persisted to localStorage.
  check(await page.getByLabel('output volume').isVisible(), 'output-volume slider renders')
  await page.getByLabel('output volume').fill('50')
  const volStored = await page.evaluate(() => localStorage.getItem('opencord.voice.outputVolume'))
  check(volStored === '0.5', `output volume persists to localStorage (got ${volStored})`)
  // Input (mic) volume slider: render + drag to 40% + assert it persisted, and that it
  // re-reads from localStorage on a modal remount (close + reopen the Voice & Video tab).
  check(await page.getByLabel('input volume').isVisible(), 'input-volume slider renders')
  await page.getByLabel('input volume').fill('40')
  const ivolStored = await page.evaluate(() => localStorage.getItem('opencord.voice.inputVolume'))
  check(ivolStored === '0.4', `input volume persists to localStorage (got ${ivolStored})`)
  await page.getByRole('button', { name: 'close settings' }).click()
  await page.locator('.settings-modal').waitFor({ state: 'detached', timeout: 4000 })
  await page.getByRole('button', { name: 'user settings' }).click()
  await page.locator('.settings-modal').waitFor({ timeout: 8000 })
  await page.locator('.settings-tab', { hasText: 'Voice & Video' }).click()
  const ivolReread = await page.getByLabel('input volume').inputValue()
  check(ivolReread === '40', `input volume re-reads from localStorage on remount (got ${ivolReread})`)
  // Flip noise suppression OFF (defaults on) — it must land in localStorage and the
  // UI must re-read it on remount. (No page reload: that would drop the server-channel
  // context the next steps need; localStorage + modal remount proves the round-trip.)
  check(await page.getByLabel('noise suppression').isChecked(), 'noise suppression defaults ON')
  await page.getByLabel('noise suppression').uncheck()
  await shot('07d3-voice-settings.png')
  const nsStored = await page.evaluate(() => localStorage.getItem('opencord.voice.noiseSuppression'))
  check(nsStored === '0', `unchecking noise suppression persists to localStorage (got ${nsStored})`)
  // Mic test: start → the meter fill should climb above 0 from the fake tone. Chromium's
  // fake mic PULSES (beeps), so a single instantaneous read can land in a silent gap —
  // poll for the PEAK over a window and break as soon as we see movement.
  await page.getByRole('button', { name: "Let's Check" }).click()
  let micPeak = 0
  for (let i = 0; i < 45; i++) {
    const lv = await page
      .locator('.mic-meter-fill')
      .evaluate((el) => parseFloat(el.getAttribute('data-level') || '0'))
    if (lv > micPeak) micPeak = lv
    if (micPeak > 0) break
    await page.waitForTimeout(100)
  }
  check(micPeak > 0, `mic-test meter responds to the fake mic tone (peak=${micPeak})`)
  await shot('07d3b-mic-test.png')
  await page.getByRole('button', { name: 'Stop Testing' }).click()
  // Close + reopen the modal (Settings remounts → re-reads getAudioProcessing()).
  await page.getByRole('button', { name: 'close settings' }).click()
  await page.locator('.settings-modal').waitFor({ state: 'detached', timeout: 4000 })
  await page.getByRole('button', { name: 'user settings' }).click()
  await page.locator('.settings-modal').waitFor({ timeout: 8000 })
  await page.locator('.settings-tab', { hasText: 'Voice & Video' }).click()
  await page.getByLabel('noise suppression').waitFor({ timeout: 8000 })
  check(
    !(await page.getByLabel('noise suppression').isChecked()),
    'the noise-suppression toggle stayed OFF after a modal remount (localStorage re-read)',
  )
  // Restore the default (on) so later voice QA captures with full DSP.
  await page.getByLabel('noise suppression').check()

  // 7d3c — Camera (slice 3b): the camera picker renders and "Test Camera" opens a live
  // preview. Chromium's fake video device supplies frames, so the <video> decodes
  // (videoWidth > 0) and the preview container goes .live.
  step('settings → Voice & Video → Test Camera → live preview decodes frames')
  check(
    await page.getByLabel('camera', { exact: true }).isVisible(),
    'camera device picker renders',
  )
  await page.getByRole('button', { name: 'Test Camera' }).click()
  const camVideo = page.locator('.cam-preview-video')
  await page.waitForFunction(
    () => {
      const v = document.querySelector('.cam-preview-video')
      return v && v.videoWidth > 0
    },
    { timeout: 8000 },
  )
  check(
    await camVideo.evaluate((v) => v.videoWidth > 0 && v.videoHeight > 0),
    'camera preview <video> decodes the fake-device frames (videoWidth > 0)',
  )
  check(
    (await page.locator('.cam-preview.live').count()) === 1,
    'the preview container is marked live while testing',
  )
  await shot('07d3c-camera-preview.png')
  await page.getByRole('button', { name: 'Stop Camera' }).click()
  check(
    (await page.locator('.cam-preview.live').count()) === 0,
    'stopping the camera tears the preview down',
  )
  await page.getByRole('button', { name: 'close settings' }).click()
  await page.locator('.settings-modal').waitFor({ state: 'detached', timeout: 4000 })

  // 7d4 — Auto-idle (presence): with the threshold shortened, a quiet stretch flips the
  // self presence pip online → idle (amber); the next activity restores online. The pip
  // lives on the header user chip and reflects myPresence everywhere.
  step('auto-idle: inactivity → presence idle; activity → online')
  // Shorten the idle threshold, then a single activity re-arms the timer at the new value.
  await page.evaluate(() => {
    window.__ocIdleMs = 1500
  })
  await page.mouse.move(4, 4)
  // Stay quiet (no input events) past the threshold → auto-idle should fire.
  await page.locator('.self-chip-pip.presence-idle').waitFor({ timeout: 8000 })
  check(
    await page.locator('.self-chip-pip.presence-idle').isVisible(),
    'after inactivity the self presence pip turns idle (amber)',
  )
  await shot('07d4-auto-idle.png')
  // Activity → auto-restore to online. Restore a long threshold first so later steps
  // (which have their own waits) are never auto-idled.
  await page.evaluate(() => {
    window.__ocIdleMs = 9_999_999
  })
  await page.mouse.move(20, 20)
  await page.mouse.move(40, 40)
  await page.locator('.self-chip-pip.presence-online').waitFor({ timeout: 8000 })
  check(
    await page.locator('.self-chip-pip.presence-online').isVisible(),
    'activity restores the self presence pip to online',
  )

  // 7d5 — Channel mute: the header toggle mutes/unmutes the current channel and dims it
  // in the sidebar. (The actual unread-badge suppression is proven two-client in
  // realtime.mjs; here we verify the toggle UI + the sidebar dim round-trip.)
  step('mute the current channel via the header toggle → muted state + sidebar dim')
  const muteToggle = page.locator('.channel-mute-toggle')
  check((await muteToggle.getAttribute('data-muted')) === 'false', 'the channel starts unmuted')
  await muteToggle.click()
  await page.locator('.channel-mute-toggle[data-muted="true"]').waitFor({ timeout: 8000 })
  check((await muteToggle.getAttribute('data-muted')) === 'true', 'the header toggle flips to muted')
  check(
    (await page.locator('.channel-item.server-channel.muted').count()) > 0,
    'the muted channel is dimmed in the sidebar',
  )
  await shot('07d5-channel-muted.png')
  await muteToggle.click() // unmute again (clean up so later steps see normal state)
  await page.locator('.channel-mute-toggle[data-muted="false"]').waitFor({ timeout: 8000 })
  check(
    (await muteToggle.getAttribute('data-muted')) === 'false',
    'unmuting flips the toggle back',
  )

  // 7d6 — Profile: set About Me + pronouns in settings, then click my member row to open
  // the profile card. About Me carries an XSS payload to prove the card renders it INERT
  // (React-escaped — no element/script injection) — Rule 15.
  step('settings → set About Me (with XSS payload) + pronouns → member row → card shows them, inert')
  const xssMarker = 'pwn' + chanName
  const myAbout = `<img src=x onerror="window.__ocXss='${xssMarker}'">building opencord ${chanName}`
  const myPron = 'they/them'
  await page.getByRole('button', { name: 'user settings' }).click()
  await page.locator('.settings-modal').waitFor({ timeout: 8000 })
  await page.getByLabel('pronouns').fill(myPron)
  await page.getByLabel('about me').fill(myAbout)
  await page.getByRole('button', { name: 'Save profile' }).click()
  await page.waitForTimeout(400) // let the member-list refetch land
  await page.getByRole('button', { name: 'close settings' }).click()
  await page.locator('.settings-modal').waitFor({ state: 'detached', timeout: 4000 })
  // Click my own member row (it carries my username) → the profile card.
  await page.locator('.member-list-row', { hasText: user }).first().click()
  await page.locator('.profile-card').waitFor({ timeout: 8000 })
  check(
    ((await page.locator('.profile-about').textContent()) ?? '').includes(myAbout),
    'the profile card shows the About Me text verbatim (escaped, not parsed as HTML)',
  )
  // Rule 15: the <img onerror> must NOT have executed (React escaped it to text).
  check(
    (await page.evaluate(() => window.__ocXss)) !== xssMarker &&
      (await page.locator('.profile-about img').count()) === 0,
    'the About Me XSS payload is INERT (no element injected, no script ran)',
  )
  check(
    ((await page.locator('.profile-pronouns').textContent()) ?? '').includes(myPron),
    'the profile card shows the pronouns',
  )
  check(
    ((await page.locator('.profile-name').textContent()) ?? '').includes(user),
    'the profile card shows the username',
  )
  await shot('07d6-profile-card.png')
  await page.keyboard.press('Escape')
  await page.locator('.profile-card').waitFor({ state: 'detached', timeout: 4000 })
  check((await page.locator('.profile-card').count()) === 0, 'Esc closes the profile card')

  // 7d7 — Profile from a message author: in #general (no member list), clicking a message
  // author's name fetches their public profile and opens the card (the GET /users/{id}/profile
  // path). Proves the card works outside server channels.
  step('#general → click a message author → profile card opens (fetched profile)')
  await page.locator('.channel-list .channel-item', { hasText: 'general' }).first().click()
  await page.getByPlaceholder('Message #general').waitFor({ timeout: 8000 })
  // The qa bot posted in #general earlier; click their author name in the message list.
  await page.locator('.message .author-link', { hasText: user }).first().click()
  await page.locator('.profile-card').waitFor({ timeout: 8000 })
  check(
    ((await page.locator('.profile-name').textContent()) ?? '').includes(user),
    'clicking a message author opens that user’s profile card',
  )
  check(
    ((await page.locator('.profile-pronouns').textContent()) ?? '').includes(myPron),
    'the fetched profile card carries the pronouns',
  )
  await shot('07d7-profile-from-message.png')
  await page.keyboard.press('Escape')
  await page.locator('.profile-card').waitFor({ state: 'detached', timeout: 4000 })

  // 03n/03o — User blocking (client slice 2): a SECOND author posts in #general; the
  // main user opens that author's profile card → Block → their message disappears from
  // the channel; Settings → Privacy lists the blocked user → Unblock → the message
  // reappears. We're in #general (no member list), so the second author is reached via
  // their message author-link and the GET /users/{id}/profile path. The second user is
  // registered + posts over a raw WS from the page (same origin), like the desktop-notify
  // step — the message arrives via the live broadcast and renders in the open #general.
  step('blocking: a second author posts in #general → block → their message hides → unblock → it returns')
  await page.getByRole('button', { name: /general/ }).click()
  await page.getByPlaceholder('Message #general').waitFor({ timeout: 8000 })
  const blockUser = 'qablock' + String(Date.now()).slice(-7)
  const blockBody = `message from the blocked author ${chanName}`
  const blockPost = await page.evaluate(
    async ({ other, body }) => {
      const reg = await fetch('/api/auth/register', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username: other, password: 'hunter2' }),
      })
      if (!reg.ok) return { ok: false, why: 'register failed ' + reg.status }
      const { token } = await reg.json()
      const chans = await fetch('/api/channels', {
        headers: { Authorization: 'Bearer ' + token },
      }).then((r) => r.json())
      const general = (chans || []).find((c) => c.name === 'general')
      if (!general) return { ok: false, why: 'no #general' }
      const proto = location.protocol === 'https:' ? 'wss' : 'ws'
      const url = `${proto}://${location.host}/ws?token=${encodeURIComponent(token)}&channel=${general.id}`
      return await new Promise((resolve) => {
        const ws = new WebSocket(url)
        const done = (r) => {
          try {
            ws.close()
          } catch {
            /* already closing */
          }
          resolve(r)
        }
        const timer = setTimeout(() => done({ ok: false, why: 'ws open timeout' }), 8000)
        ws.onopen = () => {
          ws.send(JSON.stringify({ body }))
          clearTimeout(timer)
          setTimeout(() => done({ ok: true }), 1500)
        }
        ws.onerror = () => {
          clearTimeout(timer)
          done({ ok: false, why: 'ws error' })
        }
      })
    },
    { other: blockUser, body: blockBody },
  )
  check(blockPost.ok, `second author posted in #general over the WS (${blockPost.why || 'ok'})`)
  // The second author's message arrives + renders in the open #general (live broadcast).
  const blockMsg = page.locator('.message', { hasText: blockBody })
  await blockMsg.first().waitFor({ timeout: 8000 })
  check(await blockMsg.first().isVisible(), 'the second author’s message renders before blocking')
  // Open their profile card via their message author-link, then Block (danger button).
  await page.locator('.message .author-link', { hasText: blockUser }).first().click()
  await page.locator('.profile-card').waitFor({ timeout: 8000 })
  const blockBtn = page.locator('.profile-block-btn')
  await blockBtn.waitFor({ timeout: 8000 })
  check(await blockBtn.isVisible(), 'the profile card shows a Block button for another user')
  check(
    ((await blockBtn.textContent()) ?? '').trim() === 'Block' &&
      (await blockBtn.getAttribute('data-blocked')) === 'false',
    'the button reads "Block" (not yet blocked)',
  )
  await shot('03n-block.png')
  await blockBtn.click()
  // Blocking closes the card AND hides every message from that author.
  await page.locator('.profile-card').waitFor({ state: 'detached', timeout: 4000 })
  await blockMsg.first().waitFor({ state: 'detached', timeout: 8000 }).catch(() => {})
  check(
    (await page.locator('.message', { hasText: blockBody }).count()) === 0,
    'the blocked author’s message disappears from the channel',
  )
  // Settings → Privacy: the blocked user is listed; Unblock removes the row + un-hides.
  await page.getByRole('button', { name: 'user settings' }).click()
  await page.locator('.settings-modal').waitFor({ timeout: 8000 })
  await page.locator('.settings-tab', { hasText: 'Privacy' }).click()
  const blockedRow = page.locator('.blocked-row', { hasText: blockUser })
  await blockedRow.waitFor({ timeout: 8000 })
  check(await blockedRow.isVisible(), 'Settings → Privacy lists the blocked user')
  await shot('03o-blocked-list.png')
  await blockedRow.locator('.blocked-unblock-btn').click()
  await blockedRow.waitFor({ state: 'detached', timeout: 8000 })
  check(
    (await page.locator('.blocked-row', { hasText: blockUser }).count()) === 0,
    'unblocking removes the user from the Privacy list',
  )
  await page.getByRole('button', { name: 'close settings' }).click()
  await page.locator('.settings-modal').waitFor({ state: 'detached', timeout: 4000 })
  // The unblocked author’s message reappears in #general (Chat's hide set refreshed).
  await page.locator('.message', { hasText: blockBody }).first().waitFor({ timeout: 8000 })
  check(
    await page.locator('.message', { hasText: blockBody }).first().isVisible(),
    'the unblocked author’s message reappears in the channel',
  )

  // Navigate BACK to the server channel so the downstream server-channel steps (read-only,
  // pins, etc.) have their expected context (iter-130 lesson: a detour must restore context).
  await page.getByRole('button', { name: new RegExp(srvChan) }).click()
  await page.getByPlaceholder(new RegExp('Message #' + srvChan)).waitFor({ timeout: 8000 })

  // 7e — Read-only: the owner toggles the server channel read-only.
  step('toggle the server channel read-only')
  await page.getByRole('button', { name: 'make read-only' }).click()
  await page.locator('.readonly-badge').waitFor({ timeout: 8000 })
  await shot('07e-readonly.png')
  check(await page.locator('.readonly-badge').isVisible(), 'channel shows the read-only badge after toggle')
  check(
    await page.getByRole('button', { name: 'allow everyone' }).isVisible(),
    'toggle flips to "allow everyone"',
  )

  // 7f — Channel topic: the admin sets a topic via "edit topic"; it shows in the header.
  step('admin sets a channel topic → it appears in the header')
  promptAnswer = 'Welcome to the QA server channel'
  await page.getByRole('button', { name: 'edit topic' }).click()
  await page.locator('.channel-topic').waitFor({ timeout: 8000 })
  await shot('07f-topic.png')
  check(
    (await page.locator('.channel-topic').textContent())?.includes('Welcome to the QA server channel') ?? false,
    'channel topic appears in the header after editing',
  )

  // 7f2 — Slowmode: the admin sets a per-channel cooldown; a 🐌 badge appears.
  step('admin sets slowmode → 🐌 badge appears in the header')
  promptAnswer = '10'
  await page.getByRole('button', { name: 'slowmode', exact: true }).click()
  await page.locator('.slowmode-badge').waitFor({ timeout: 8000 })
  await shot('07f2-slowmode.png')
  check(
    (await page.locator('.slowmode-badge').textContent())?.includes('10s') ?? false,
    'channel shows the 🐌 slowmode badge after the admin sets it',
  )

  // 7g — Pin: the admin pins the server-channel message; a pin badge appears (live WS).
  step('admin pins a message → pin badge appears')
  const srvMsg = page.locator('.message', { hasText: srvBody }).first()
  await srvMsg.hover()
  await srvMsg.getByRole('button', { name: 'pin', exact: true }).click()
  await srvMsg.locator('.pin-badge').waitFor({ timeout: 8000 })
  await shot('07g-pin.png')
  check(await srvMsg.locator('.pin-badge').isVisible(), 'pinned message shows the pin badge')

  // 7h — Pins panel: the "pins" header button lists the channel's pinned messages.
  step('open the pins panel → it lists the pinned message')
  await page.getByRole('button', { name: 'pins', exact: true }).click()
  const pinsPanel = page.locator('.search-results', { hasText: 'pinned message' })
  await pinsPanel.waitFor({ timeout: 8000 })
  await shot('07h-pins-panel.png')
  check(await pinsPanel.getByText(srvBody).isVisible(), 'pins panel lists the pinned message')
  await pinsPanel.getByRole('button', { name: /close/ }).click()

  // 7i — Uploaded avatar: the viewer sets a profile picture via the settings modal
  // (My Account → Change Avatar) → the settings preview + header chip show the image
  // immediately; after a reload, their message avatars render the image too.
  step('settings → upload an avatar → preview + header + message avatars become the image')
  await page.getByRole('button', { name: 'user settings' }).click()
  await page.locator('.settings-modal').waitFor({ timeout: 8000 })
  await page.locator('.settings-modal input[type=file]').setInputFiles({
    name: 'avatar.png',
    mimeType: 'image/png',
    buffer: makePng(96, 96, [120, 90, 220]),
  })
  const settingsImg = page.locator('.settings-avatar-preview .avatar-settings.avatar-img')
  await settingsImg.waitFor({ timeout: 10000 })
  check(await settingsImg.isVisible(), 'settings avatar preview becomes the uploaded image immediately')
  await shot('07i-avatar-settings.png')
  await page.getByRole('button', { name: 'close settings' }).click()
  await page.locator('.settings-modal').waitFor({ state: 'detached', timeout: 4000 })
  const headerImg = page.locator('.self-chip .avatar-self.avatar-img')
  await headerImg.waitFor({ timeout: 10000 })
  await shot('07i-avatar-header.png')
  check(await headerImg.isVisible(), 'header chip avatar becomes the uploaded image')
  check(
    await headerImg.evaluate((el) => el.complete && el.naturalWidth > 0),
    'header avatar image actually decoded',
  )
  // Reload → #general (where the viewer posted) → their message avatar is now an image.
  await page.reload({ waitUntil: 'domcontentloaded' })
  await page.getByRole('button', { name: /general/ }).click()
  const msgImg = page.locator('.message .avatar-img').first()
  await msgImg.waitFor({ timeout: 12000 })
  await shot('07i-avatar-message.png')
  check(await msgImg.isVisible(), 'after reload, message-list avatars render the uploaded image')

  // 03m — Desktop notifications (Web Notifications API): with the opt-in toggle enabled
  // AND the tab "hidden", a NEW message that @-mentions the qabot in the active channel
  // (#general) must construct a desktop notification carrying the author + a body
  // snippet. Headless can't show a real OS notification, so window.Notification is
  // stubbed (context init script) to record every construction into window.__notifs;
  // OS permission is pre-granted on the context. We post the @mention as a SECOND user
  // over a raw WebSocket from the page (same origin) so it arrives via the live
  // `message` broadcast — the exact path the notification fires on. (qabot is on
  // #general from the avatar step above; the reload reinstalled the stub + cleared
  // __notifs.) Discord's low-noise rule: off by default, only while the tab is hidden.
  step('settings → Notifications → enable the desktop-notify toggle (requests permission)')
  await page.getByRole('button', { name: 'user settings' }).click()
  await page.locator('.settings-modal').waitFor({ timeout: 8000 })
  await page.locator('.settings-tab', { hasText: 'Notifications' }).click()
  const notifyToggle = page.getByLabel('desktop notifications')
  await notifyToggle.waitFor({ timeout: 8000 })
  check(!(await notifyToggle.isChecked()), 'desktop notifications default OFF')
  await notifyToggle.check() // a user gesture → requestPermission() resolves 'granted' (stub)
  await page.locator('input[aria-label="desktop notifications"]:checked').waitFor({ timeout: 8000 })
  check(await notifyToggle.isChecked(), 'enabling the toggle persists ON once permission is granted')
  const notifyStored = await page.evaluate(() => localStorage.getItem('opencord.notify.desktop'))
  check(notifyStored === '1', `the desktop-notify pref persists to localStorage (got ${notifyStored})`)
  await shot('03m-desktop-notify.png')
  await page.getByRole('button', { name: 'close settings' }).click()
  await page.locator('.settings-modal').waitFor({ state: 'detached', timeout: 4000 })

  // Make sure qabot is viewing #general (the active channel the message must arrive in).
  await page.getByRole('button', { name: /general/ }).click()
  await page.getByPlaceholder('Message #general').waitFor({ timeout: 8000 })
  // Clear any notifications recorded so far, then force the tab "hidden" (a headless tab
  // never truly backgrounds), since the rule only notifies while the tab is in the
  // background. dispatch visibilitychange so React's document.hidden read is current.
  await page.evaluate(() => {
    window.__notifs = []
    Object.defineProperty(document, 'hidden', { configurable: true, get: () => true })
    Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => 'hidden' })
    document.dispatchEvent(new Event('visibilitychange'))
  })

  step('a second user @mentions qabot in #general while the tab is hidden → desktop notification fires')
  const notifyBody = `hey @${user} desktop ping ${chanName}`
  const notifResult = await page.evaluate(
    async ({ other, body }) => {
      // Register a fresh second user (same origin) and grab their token.
      const reg = await fetch('/api/auth/register', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username: other, password: 'hunter2' }),
      })
      if (!reg.ok) return { ok: false, why: 'register failed ' + reg.status }
      const { token } = await reg.json()
      // Find #general's id with the new user's token.
      const chans = await fetch('/api/channels', {
        headers: { Authorization: 'Bearer ' + token },
      }).then((r) => r.json())
      const general = (chans || []).find((c) => c.name === 'general')
      if (!general) return { ok: false, why: 'no #general' }
      // Open a raw WS as the second user on #general and post the @mention. The qabot's
      // open socket on #general receives it via the live broadcast → notification fires.
      const proto = location.protocol === 'https:' ? 'wss' : 'ws'
      const url = `${proto}://${location.host}/ws?token=${encodeURIComponent(token)}&channel=${general.id}`
      return await new Promise((resolve) => {
        const ws = new WebSocket(url)
        const done = (r) => {
          try {
            ws.close()
          } catch {
            /* already closing */
          }
          resolve(r)
        }
        const timer = setTimeout(() => done({ ok: false, why: 'ws open timeout' }), 8000)
        ws.onopen = () => {
          ws.send(JSON.stringify({ body }))
          clearTimeout(timer)
          // Give the server a moment to fan the broadcast back to qabot's socket.
          setTimeout(() => done({ ok: true }), 1500)
        }
        ws.onerror = () => {
          clearTimeout(timer)
          done({ ok: false, why: 'ws error' })
        }
      })
    },
    { other: 'qabuddy' + String(Date.now()).slice(-7), body: notifyBody },
  )
  check(notifResult.ok, `second user posted the @mention over the WS (${notifResult.why || 'ok'})`)
  // The mention must arrive + render in qabot's #general (proves the live broadcast landed).
  await page.locator('.message .body', { hasText: 'desktop ping ' + chanName }).last().waitFor({ timeout: 8000 })
  // Poll window.__notifs for the recorded construction (the handler runs synchronously on
  // the WS frame, but poll a few ticks to avoid a microtask race).
  let notifs = []
  for (let i = 0; i < 30; i++) {
    notifs = await page.evaluate(() => window.__notifs || [])
    if (notifs.length > 0) break
    await page.waitForTimeout(100)
  }
  check(notifs.length > 0, `a desktop notification was constructed (got ${notifs.length})`)
  const fired = notifs[notifs.length - 1] || { title: '', body: '' }
  // The author is the SECOND user; a server (non-DM) mention tags the title "(mention)".
  check(
    fired.title.includes('qabuddy') && fired.title.includes('(mention)'),
    `the notification title carries the author + "(mention)" tag (got "${fired.title}")`,
  )
  check(
    fired.body.includes('desktop ping ' + chanName),
    `the notification body carries the message snippet (got "${fired.body}")`,
  )
  // Restore document.hidden = false so the later steps (mobile, etc.) see a focused tab.
  await page.evaluate(() => {
    Object.defineProperty(document, 'hidden', { configurable: true, get: () => false })
    Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => 'visible' })
    document.dispatchEvent(new Event('visibilitychange'))
  })

  // 7j — Server settings (owner): create a throwaway server, rename it (the sidebar
  // relabels live), then delete it (typed-name confirm) and watch it leave the sidebar.
  // Isolated on its own server so it never disturbs the "qa server" flow above.
  step('server settings: create → rename → delete a throwaway server')
  // Boundary-width fixture: a long name proves the sidebar truncates (ellipsis) instead
  // of overflowing the 220px sidebar — short names never exercised this (GOAL polish item).
  promptAnswer = 'qa settings srv long enough to overflow the sidebar'
  await page.getByRole('button', { name: '+ New server' }).click()
  await page.locator('.server-name', { hasText: 'qa settings srv' }).waitFor({ timeout: 8000 })
  // The long name must ellipsis-truncate: the name element clips its content
  // (scrollWidth > clientWidth) AND its rendered right edge stays within the sidebar,
  // so it never overflows the fixed 220px column (GOAL polish item, tick-114).
  const longNameRow = page.locator('.server-name-text', { hasText: 'qa settings srv' }).first()
  const fit = await longNameRow.evaluate((el) => {
    const sidebar = el.closest('.sidebar')
    const elRight = el.getBoundingClientRect().right
    const barRight = sidebar ? sidebar.getBoundingClientRect().right : Infinity
    return { truncating: el.scrollWidth > el.clientWidth, withinSidebar: elRight <= barRight + 1 }
  })
  check(fit.truncating, 'a long server name truncates with an ellipsis (content is clipped)')
  check(fit.withinSidebar, 'the truncated name stays within the sidebar (no horizontal overflow)')
  await shot('07j-long-name.png')
  // Open its members panel — the Server settings section lives at the top (owner-only).
  await page
    .locator('.server-group', { hasText: 'qa settings srv' })
    .getByRole('button', { name: 'members' })
    .click()
  await page.locator('.server-settings').waitFor({ timeout: 8000 })
  check(
    await page.locator('.server-settings-head').isVisible(),
    'owner sees the Server settings section in the members panel',
  )
  // Rename → the sidebar relabels (optimistic + the live server-renamed WS push).
  promptAnswer = 'qa settings renamed'
  await page.locator('.rename-server-btn').click()
  await page.locator('.server-name', { hasText: 'qa settings renamed' }).waitFor({ timeout: 8000 })
  await shot('07j-server-renamed.png')
  check(
    await page.locator('.server-name', { hasText: 'qa settings renamed' }).isVisible(),
    'renaming a server relabels it in the sidebar',
  )
  // Delete → the typed-name confirm prompt is auto-answered with the (renamed) name.
  promptAnswer = 'qa settings renamed'
  await page.locator('.delete-server-btn').click()
  await page
    .locator('.server-name', { hasText: 'qa settings renamed' })
    .waitFor({ state: 'detached', timeout: 8000 })
  await shot('07j-server-deleted.png')
  check(
    (await page.locator('.server-name', { hasText: 'qa settings renamed' }).count()) === 0,
    'deleting a server removes it from the sidebar',
  )

  // 7k — Group DMs (v0.6 slice 2): create a group from the "+ New DM" modal and see it
  // rendered in the DM list titled by its members (groups are unnamed). Register two extra
  // users first so there are real people to add.
  step('create a group DM from the New DM modal → it appears in the DM list titled by members')
  const grpA = 'qagrpa' + String(Date.now()).slice(-6)
  const grpB = 'qagrpb' + String(Date.now()).slice(-6)
  const regBoth = await page.evaluate(async (names) => {
    for (const n of names) {
      const r = await fetch('/api/auth/register', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username: n, password: 'hunter2' }),
      })
      if (!r.ok) return 'register ' + n + ' failed ' + r.status
    }
    return 'ok'
  }, [grpA, grpB])
  check(regBoth === 'ok', `registered two users to group with (${regBoth})`)

  await page.locator('.add-channel', { hasText: 'New DM' }).click()
  await page.locator('.group-modal').waitFor({ timeout: 4000 })
  const chipInput = page.locator('.group-chip-input')
  await chipInput.click()
  await chipInput.fill(grpA)
  await chipInput.press('Enter')
  await chipInput.fill(grpB)
  await chipInput.press('Enter')
  check((await page.locator('.group-chip').count()) === 2, 'both members became chips in the modal')
  await shot('07k-group-modal.png')
  await page.locator('.group-modal').getByRole('button', { name: /Create Group/ }).click()
  await page.locator('.group-modal').waitFor({ state: 'detached', timeout: 8000 })
  const groupRow = page.locator('.dm-list .channel-item', { hasText: grpA }).first()
  await groupRow.waitFor({ timeout: 6000 })
  check(
    (await groupRow.locator('.dm-group-avatar').count()) === 1,
    'the group DM row shows the group-glyph avatar',
  )
  const groupTitle = (await groupRow.locator('.item-name').textContent()) || ''
  check(
    groupTitle.includes(grpA) && groupTitle.includes(grpB),
    `group row is titled by both members (got "${groupTitle}")`,
  )
  await shot('07k-group-row.png')

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

  // 8b — Mobile header controls: the always-shown user controls (⚙ user-settings chip
  // + log out) must stay reachable and NOT cause horizontal overflow at phone width.
  // The presence/status controls moved into the settings modal this cycle.
  step('phone-width header: ⚙ settings chip reachable, no horizontal overflow')
  await page.waitForTimeout(350) // let the drawer's slide-out transition settle for a clean shot
  check(
    await page.getByRole('button', { name: 'user settings' }).isVisible(),
    'user-settings chip is reachable on mobile',
  )
  check(await page.getByRole('button', { name: 'log out' }).isVisible(), 'log out is reachable on mobile')
  const mHeaderOverflow = await page
    .locator('.chat-header')
    .evaluate((h) => h.scrollWidth - h.clientWidth)
  check(mHeaderOverflow <= 1, `mobile header has no horizontal overflow (got ${mHeaderOverflow}px)`)
  await shot('08b-mobile-header.png')

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
