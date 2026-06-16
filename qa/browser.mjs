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
  const page = await (await browser.newContext({ viewport: { width: 1100, height: 820 } })).newPage()
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

  await page.getByRole('button', { name: 'close' }).click()

  // 7d2 — Custom status + emoji: set both via the header, see them render under your
  // name in the member list, and reflected in the header button.
  step('set a custom status + emoji → both show in the member list + header')
  const myStatus = 'shipping presence'
  const myStatusEmoji = '🚀'
  statusEmojiAnswer = myStatusEmoji
  promptAnswer = myStatus
  await page.getByRole('button', { name: /set status/ }).click()
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
  check(
    ((await page.locator('.status-edit').textContent()) ?? '').includes(myStatus),
    'header status button reflects the current status',
  )
  check(
    ((await page.locator('.status-edit .status-emoji').textContent()) ?? '').includes(myStatusEmoji),
    'header status button shows the status emoji',
  )
  await shot('07d2-status.png')
  statusEmojiAnswer = '' // reset so later prompt-driven steps are unaffected

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

  // 7i — Uploaded avatar: the viewer sets a profile picture via the header avatar
  // button → the header shows the image immediately; after a reload, their message
  // avatars render the image too (replacing initials everywhere).
  step('upload an avatar via the header → header + message avatars become the image')
  await page.locator('.meta input[type=file]').setInputFiles({
    name: 'avatar.png',
    mimeType: 'image/png',
    buffer: makePng(96, 96, [120, 90, 220]),
  })
  const headerImg = page.locator('.self-avatar-btn .avatar-self.avatar-img')
  await headerImg.waitFor({ timeout: 10000 })
  await shot('07i-avatar-header.png')
  check(await headerImg.isVisible(), 'header avatar becomes the uploaded image immediately')
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
