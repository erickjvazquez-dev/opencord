import { describe, it, expect } from 'vitest'
import { isValidElement, type ReactNode } from 'react'
import { renderMarkdown, highlightMatches } from './markdown'
import { EmojiImg } from './components/EmojiImg'

// renderMarkdown returns React elements (never an HTML string), so these tests walk
// the element tree directly — no DOM library needed. flatten() collects every node;
// imgs()/text() pull out the bits a case cares about.

function flatten(node: ReactNode): ReactNode[] {
  const out: ReactNode[] = []
  const visit = (n: ReactNode) => {
    if (n == null || typeof n === 'boolean') return
    if (Array.isArray(n)) {
      n.forEach(visit)
      return
    }
    out.push(n)
    if (isValidElement(n)) {
      const kids = (n.props as { children?: ReactNode }).children
      if (kids != null) visit(kids)
    }
  }
  visit(node)
  return out
}

// Every <EmojiImg> element in the tree. Emoji are now rendered via the EmojiImg
// component (it fetches the auth-gated bytes WITH the bearer token and renders a blob
// URL — an <img src> can't send the header), so the markdown emits the component with
// a numeric `id`, not a raw <img> with a `src`.
function emojiImgs(node: ReactNode) {
  return flatten(node).filter(
    (n): n is React.ReactElement<Record<string, unknown>> =>
      isValidElement(n) && n.type === EmojiImg,
  )
}

// All raw string text in the tree, concatenated (lets us assert literals survive).
function allText(node: ReactNode): string {
  return flatten(node)
    .filter((n): n is string => typeof n === 'string')
    .join('')
}

describe('renderMarkdown custom emoji (:name:)', () => {
  it('renders a known :smile: as an EmojiImg with id 7 + the bearer token', () => {
    const out = renderMarkdown('hi :smile: there', { emoji: new Map([['smile', 7]]), token: 'tok' })
    const imgs = emojiImgs(out)
    expect(imgs).toHaveLength(1)
    expect(imgs[0].props.id).toBe(7)
    expect(imgs[0].props.alt).toBe(':smile:')
    expect(imgs[0].props.token).toBe('tok')
    // The surrounding words survive as literal text.
    expect(allText(out)).toContain('hi ')
    expect(allText(out)).toContain(' there')
  })

  it('leaves an UNKNOWN :nope: (not in the map) as literal text', () => {
    const out = renderMarkdown('say :nope: ok', { emoji: new Map([['smile', 7]]) })
    expect(emojiImgs(out)).toHaveLength(0)
    expect(allText(out)).toContain(':nope:')
  })

  it('leaves :name: literal when no map / empty map is given', () => {
    expect(emojiImgs(renderMarkdown('a :smile: b'))).toHaveLength(0)
    expect(allText(renderMarkdown('a :smile: b'))).toContain(':smile:')
    const empty = renderMarkdown('a :smile: b', { emoji: new Map() })
    expect(emojiImgs(empty)).toHaveLength(0)
    expect(allText(empty)).toContain(':smile:')
  })

  it('does not match invalid slugs (space, uppercase) even if a same-named entry existed', () => {
    // Even though the map has the lowercased keys, the token charset is strict.
    const map = new Map<string, number>([['bad', 1], ['upper', 2]])
    const spaced = renderMarkdown('x :bad name: y', { emoji: map })
    expect(emojiImgs(spaced)).toHaveLength(0)
    expect(allText(spaced)).toContain(':bad name:')

    const upper = renderMarkdown('x :UPPER: y', { emoji: new Map([['upper', 2]]) })
    expect(emojiImgs(upper)).toHaveLength(0)
    expect(allText(upper)).toContain(':UPPER:')
  })

  it('a later known emoji still resolves after an unknown one', () => {
    const out = renderMarkdown(':nope: then :smile:', { emoji: new Map([['smile', 7]]) })
    const imgs = emojiImgs(out)
    expect(imgs).toHaveLength(1)
    expect(imgs[0].props.id).toBe(7)
    expect(allText(out)).toContain(':nope:')
  })

  it('renders two known emoji in one message', () => {
    const out = renderMarkdown(':a1: and :b2:', { emoji: new Map([['a1', 3], ['b2', 4]]) })
    const imgs = emojiImgs(out)
    expect(imgs.map((i) => i.props.id)).toEqual([3, 4])
  })

  it('does not corrupt a https:// URL or adjacent :: colons', () => {
    const map = new Map([['com', 99], ['smile', 7]])
    const out = renderMarkdown('see https://x.com here', { emoji: map })
    // No emoji image — the autolink wins and `:com:` never forms inside the URL.
    expect(emojiImgs(out)).toHaveLength(0)
    // The link is preserved as an <a> whose href is the URL.
    const link = flatten(out).find(
      (n): n is React.ReactElement<{ href?: string }> => isValidElement(n) && n.type === 'a',
    )
    expect(link?.props.href).toBe('https://x.com')

    // Bare `::` (too few chars) never matches even with a valid entry present.
    const colons = renderMarkdown('a :: b :smile:', { emoji: map })
    expect(allText(colons)).toContain('::')
    expect(emojiImgs(colons)).toHaveLength(1)
  })

  it('keeps other markdown working alongside emoji (bold + emoji)', () => {
    const out = renderMarkdown('**bold** :smile:', { emoji: new Map([['smile', 7]]) })
    expect(emojiImgs(out)).toHaveLength(1)
    const strong = flatten(out).find((n) => isValidElement(n) && n.type === 'strong')
    expect(strong).toBeTruthy()
  })
})

// Every <div> whose className contains `cls` (headers/subtext render as styled divs).
function divsWithClass(node: ReactNode, cls: string) {
  return flatten(node).filter(
    (n): n is React.ReactElement<{ className?: string }> =>
      isValidElement(n) && n.type === 'div' && String((n.props as { className?: string }).className || '').includes(cls),
  )
}

describe('renderMarkdown headers + subtext (Discord parity)', () => {
  it('renders #/##/### as md-h1/md-h2/md-h3 with the text', () => {
    for (const [src, cls] of [['# Big', 'md-h1'], ['## Mid', 'md-h2'], ['### Small', 'md-h3']]) {
      const out = renderMarkdown(src)
      const hs = divsWithClass(out, cls)
      expect(hs).toHaveLength(1)
      expect(allText(out)).toContain(src.replace(/^#+\s+/, ''))
    }
  })
  it('renders -# as a md-subtext div', () => {
    const out = renderMarkdown('-# tiny note')
    expect(divsWithClass(out, 'md-subtext')).toHaveLength(1)
    expect(allText(out)).toContain('tiny note')
  })
  it('keeps inline markdown working inside a header (# **bold**)', () => {
    const out = renderMarkdown('# hi **there**')
    expect(divsWithClass(out, 'md-h1')).toHaveLength(1)
    expect(flatten(out).some((n) => isValidElement(n) && n.type === 'strong')).toBe(true)
  })
  it('does NOT treat #channel (no space) or #### (4+) or a bare "# " as a header', () => {
    expect(divsWithClass(renderMarkdown('#channel chatter'), 'md-h')).toHaveLength(0)
    expect(divsWithClass(renderMarkdown('#### too deep'), 'md-h')).toHaveLength(0) // 4 hashes → not 1..3
    expect(divsWithClass(renderMarkdown('# '), 'md-h')).toHaveLength(0) // no content
  })
  it('a header with raw HTML keeps it inert (no script/img element, Rule B)', () => {
    const out = renderMarkdown('# <script>alert(1)</script>')
    expect(tags(out, 'script')).toHaveLength(0)
    expect(allText(out)).toContain('<script>')
  })
})

// Every <a> element the renderer emitted.
function links(node: ReactNode) {
  return flatten(node).filter(
    (n): n is React.ReactElement<Record<string, unknown>> => isValidElement(n) && n.type === 'a',
  )
}
// Every element whose type is a raw HTML tag NAME (string), e.g. an injected 'script'/'img'.
function tags(node: ReactNode, tag: string) {
  return flatten(node).filter((n) => isValidElement(n) && n.type === tag)
}

// Rule 15 — the autolinker is the single most security-sensitive render path: it is the only
// place a URL becomes a clickable <a href>. These tests ENCODE its safety invariants so a future
// change (e.g. broadening the scheme regex) that reintroduced javascript:/data: XSS fails loudly.
// markdown.tsx returns React elements only (never dangerouslySetInnerHTML), so raw HTML in a
// message body must always render as literal, inert text (Rule B).
describe('renderMarkdown link & raw-HTML safety (Rule 15)', () => {
  it('does NOT autolink a javascript: URL — it stays literal text, no <a>', () => {
    const out = renderMarkdown('click javascript:alert(1) now')
    expect(links(out)).toHaveLength(0)
    expect(allText(out)).toContain('javascript:alert(1)')
  })

  it('does NOT autolink a data: URL (would be an HTML-payload vector) — literal, no <a>', () => {
    const out = renderMarkdown('x data:text/html,<b>hi</b> y')
    expect(links(out)).toHaveLength(0)
    expect(allText(out)).toContain('data:text/html')
  })

  it('only http(s):// autolinks, and the <a> carries target=_blank + rel=noopener noreferrer', () => {
    for (const url of ['http://a.example', 'https://b.example/path?q=1']) {
      const ls = links(renderMarkdown(`go ${url} end`))
      expect(ls).toHaveLength(1)
      expect(ls[0].props.href).toBe(url)
      expect(ls[0].props.target).toBe('_blank')
      // noopener/noreferrer defeats reverse-tabnabbing (the opened page can't touch window.opener).
      expect(ls[0].props.rel).toBe('noopener noreferrer')
    }
  })

  it('never extracts attributes from a URL — an embedded quote stays inside href, no on* handler', () => {
    // The whole token (incl. the quote + onmouseover text) is one URL string set as `href`;
    // React escapes it as an attribute value. The parser must NOT split it into separate props.
    const out = renderMarkdown('http://evil.example/"onmouseover="alert(1) tail')
    const ls = links(out)
    expect(ls).toHaveLength(1)
    // href is the literal URL string (trailing sentence punctuation may be trimmed) — still http.
    expect(String(ls[0].props.href).startsWith('http://evil.example/')).toBe(true)
    // No event-handler prop was ever created from the URL content.
    expect(Object.keys(ls[0].props).some((k) => /^on/i.test(k))).toBe(false)
    expect('onmouseover' in ls[0].props).toBe(false)
  })

  it('renders raw <script>/<img onerror> in a message as INERT literal text (no element injected)', () => {
    const out = renderMarkdown('<script>alert(1)</script> and <img src=x onerror=alert(2)>')
    // No real <script> or <img> tag element was emitted — only literal strings.
    expect(tags(out, 'script')).toHaveLength(0)
    expect(tags(out, 'img')).toHaveLength(0)
    const text = allText(out)
    expect(text).toContain('<script>')
    expect(text).toContain('<img src=x onerror=alert(2)>')
  })
})

// Every <mark> element (the highlighted search term).
function marks(node: ReactNode) {
  return flatten(node).filter((n) => isValidElement(n) && n.type === 'mark')
}

describe('highlightMatches (search-term highlighting)', () => {
  it('wraps a case-insensitive match in a single <mark>, preserving surrounding text', () => {
    const out = highlightMatches(renderMarkdown('hello SerVer world'), 'server')
    const ms = marks(out)
    expect(ms).toHaveLength(1)
    expect(allText(out)).toContain('hello ')
    expect(allText(out)).toContain('world')
    // the matched substring (original casing) is inside the mark
    expect(allText(ms[0])).toBe('SerVer')
  })
  it('highlights a match INSIDE markdown (**bold**) and keeps the <strong>', () => {
    const out = highlightMatches(renderMarkdown('a **boldword** b'), 'bold')
    expect(marks(out)).toHaveLength(1)
    expect(flatten(out).some((n) => isValidElement(n) && n.type === 'strong')).toBe(true)
  })
  it('highlights every occurrence', () => {
    const out = highlightMatches(renderMarkdown('ab ab ab'), 'ab')
    expect(marks(out)).toHaveLength(3)
  })
  it('an empty / whitespace query leaves the tree unchanged (no marks)', () => {
    expect(marks(highlightMatches(renderMarkdown('anything here'), ''))).toHaveLength(0)
    expect(marks(highlightMatches(renderMarkdown('anything here'), '   '))).toHaveLength(0)
  })
  it('a raw-HTML body still renders inert even with a matching query (no element injected)', () => {
    const out = highlightMatches(renderMarkdown('<script>alert(1)</script>'), 'script')
    expect(tags(out, 'script')).toHaveLength(0)
    expect(allText(out)).toContain('<script>')
    expect(marks(out).length).toBeGreaterThan(0) // the literal text "script" is highlighted, still inert
  })
})

// Masked links [text](url) are a second URL→<a> path, so they carry the SAME Rule-15
// invariants as the autolinker PLUS an anti-phishing tell. The URL group is http(s) in the
// regex, so a non-http scheme never forms an <a> at all.
describe('renderMarkdown masked links [text](url) (Rule 15)', () => {
  it('renders [text](http(s)://…) as an <a> whose href=url, text=text, title=url, _blank+noopener', () => {
    for (const url of ['http://a.example', 'https://b.example/p?q=1']) {
      const out = renderMarkdown(`see [click here](${url}) end`)
      const ls = links(out)
      expect(ls).toHaveLength(1)
      expect(ls[0].props.href).toBe(url)
      expect(ls[0].props.target).toBe('_blank')
      expect(ls[0].props.rel).toBe('noopener noreferrer')
      // title=the real URL is the anti-spoof: hover reveals the destination.
      expect(ls[0].props.title).toBe(url)
      expect(allText(out)).toContain('click here')
      expect(allText(out)).not.toContain('](') // the raw syntax was consumed, not left literal
    }
  })
  it('the phishing shape (text looks like another domain) still links to the REAL url + titles it', () => {
    const out = renderMarkdown('[google.com](http://evil.example)')
    const ls = links(out)
    expect(ls).toHaveLength(1)
    expect(ls[0].props.href).toBe('http://evil.example') // the real destination, not the text
    expect(ls[0].props.title).toBe('http://evil.example') // surfaced on hover (anti-spoof)
    expect(allText(out)).toContain('google.com') // the lying display text
  })
  it('does NOT form an <a> for a javascript:/data: URL — the whole token stays literal', () => {
    for (const bad of ['[click](javascript:alert(1))', '[x](data:text/html,<b>hi</b>)']) {
      const out = renderMarkdown(bad)
      expect(links(out)).toHaveLength(0)
      expect(allText(out)).toContain('[') // rendered as inert literal text, no link
    }
  })
  it('never extracts an on* handler from a quote inside the masked URL', () => {
    const out = renderMarkdown('[t](http://evil.example/"onmouseover="alert(1))')
    const ls = links(out)
    expect(ls).toHaveLength(1)
    expect(String(ls[0].props.href).startsWith('http://evil.example/')).toBe(true)
    expect(Object.keys(ls[0].props).some((k) => /^on/i.test(k))).toBe(false)
  })
  it('inline markdown works inside the link text ([**bold**](url) keeps the <strong>)', () => {
    const out = renderMarkdown('[**bold**](https://x.example)')
    expect(links(out)).toHaveLength(1)
    expect(flatten(out).some((n) => isValidElement(n) && n.type === 'strong')).toBe(true)
  })
})
