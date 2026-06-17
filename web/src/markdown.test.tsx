import { describe, it, expect } from 'vitest'
import { isValidElement, type ReactNode } from 'react'
import { renderMarkdown } from './markdown'
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
