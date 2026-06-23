import { describe, it, expect } from 'vitest'
import {
  activeEmojiToken,
  matchEmojiNames,
  mergeEmojiCandidates,
  spliceEmoji,
} from './emojiAutocomplete'
import { UNICODE_EMOJI, unicodeEmoji, searchUnicodeEmoji } from './emojiData'

describe('activeEmojiToken', () => {
  it('matches a :partial of >=2 chars at the start of the text', () => {
    expect(activeEmojiToken(':sm', 3)).toEqual({ query: 'sm', start: 0 })
  })
  it('matches a :partial that follows whitespace', () => {
    expect(activeEmojiToken('hi :sm', 6)).toEqual({ query: 'sm', start: 3 })
  })
  it('does not match a lone colon', () => {
    expect(activeEmojiToken(':', 1)).toBeNull()
  })
  it('does not match a single-char partial (needs >=2)', () => {
    expect(activeEmojiToken(':s', 2)).toBeNull()
  })
  it('does not match the emoticon :)', () => {
    expect(activeEmojiToken(':)', 2)).toBeNull()
  })
  it('does not match a completed :name: with its closing colon', () => {
    expect(activeEmojiToken(':smile:', 7)).toBeNull()
  })
  it('does not match when the colon follows a non-space char (e.g. a url)', () => {
    expect(activeEmojiToken('http://ab', 9)).toBeNull()
  })
  it('respects the caret — ignores text after it', () => {
    expect(activeEmojiToken(':smile rest', 4)).toEqual({ query: 'smi', start: 0 })
  })
  it('matches names with digits, underscores and hyphens', () => {
    expect(activeEmojiToken(':qa_em', 6)).toEqual({ query: 'qa_em', start: 0 })
  })
})

describe('matchEmojiNames', () => {
  it('filters by case-insensitive startsWith, preserving order', () => {
    expect(matchEmojiNames(['smile', 'smirk', 'wave'], 'sm')).toEqual(['smile', 'smirk'])
  })
  it('is case-insensitive on both sides', () => {
    expect(matchEmojiNames(['Smile', 'SMIRK'], 'sm')).toEqual(['Smile', 'SMIRK'])
  })
  it('caps the number of results', () => {
    expect(matchEmojiNames(['aa', 'ab', 'ac', 'ad'], 'a', 2)).toEqual(['aa', 'ab'])
  })
  it('an empty query returns everything (capped)', () => {
    expect(matchEmojiNames(['a', 'b'], '')).toEqual(['a', 'b'])
  })
  it('returns [] when nothing matches', () => {
    expect(matchEmojiNames(['smile'], 'zz')).toEqual([])
  })
})

describe('spliceEmoji', () => {
  it('replaces a :partial at the start with :name: and a trailing space', () => {
    expect(spliceEmoji(':sm', { start: 0, len: 3 }, 'smile')).toEqual({ next: ':smile: ', caret: 8 })
  })
  it('splices mid-text and keeps the trailing content', () => {
    expect(spliceEmoji('hi :sm there', { start: 3, len: 3 }, 'smile')).toEqual({
      next: 'hi :smile:  there',
      caret: 11,
    })
  })
})

describe('unicodeEmoji (vendored shortcode→char map)', () => {
  it('resolves common standard shortcodes to their unicode char', () => {
    expect(unicodeEmoji('joy')).toBe('😂')
    expect(unicodeEmoji('fire')).toBe('🔥')
    expect(unicodeEmoji('thumbsup')).toBe('👍')
    expect(unicodeEmoji('tada')).toBe('🎉')
  })
  it('is case-insensitive (the :slug: regex lowercases, but be robust)', () => {
    expect(unicodeEmoji('JOY')).toBe('😂')
  })
  it('returns undefined for an unknown name', () => {
    expect(unicodeEmoji('definitelynotanemoji')).toBeUndefined()
  })
  it('every key is lowercase [a-z0-9_] so it is reachable by render + autocomplete', () => {
    for (const key of UNICODE_EMOJI.keys()) {
      expect(key).toMatch(/^[a-z0-9_]+$/)
    }
  })
})

describe('mergeEmojiCandidates (custom precedence + unicode fill)', () => {
  it('lists custom matches first, then unicode matches for the query', () => {
    // custom `fireball` + unicode `fire`/`fireworks…` for query "fir"
    const merged = mergeEmojiCandidates(['fireball'], UNICODE_EMOJI.keys(), 'fir', 8)
    expect(merged[0]).toBe('fireball')
    expect(merged).toContain('fire')
  })
  it('dedupes a name present in BOTH (custom shadows unicode, keeps custom position)', () => {
    const merged = mergeEmojiCandidates(['fire'], UNICODE_EMOJI.keys(), 'fire', 8)
    expect(merged.filter((n) => n === 'fire')).toHaveLength(1)
    expect(merged[0]).toBe('fire')
  })
  it('returns unicode-only matches when there are no custom emoji', () => {
    const merged = mergeEmojiCandidates([], UNICODE_EMOJI.keys(), 'joy', 8)
    expect(merged).toContain('joy')
  })
  it('respects the cap, custom taking the first slots', () => {
    const merged = mergeEmojiCandidates(['a_custom_one', 'b_custom_two'], UNICODE_EMOJI.keys(), 's', 3)
    expect(merged).toHaveLength(3)
    expect(merged.slice(0, 2)).toEqual(['a_custom_one', 'b_custom_two'])
  })
  it('returns empty when nothing matches the query', () => {
    expect(mergeEmojiCandidates([], UNICODE_EMOJI.keys(), 'zzzznope', 8)).toEqual([])
  })
})

describe('searchUnicodeEmoji (reaction picker search)', () => {
  it('returns [name, char] pairs whose name CONTAINS the query (substring)', () => {
    const res = searchUnicodeEmoji('heart')
    const names = res.map(([n]) => n)
    // substring match surfaces broken_heart / heartpulse, not just a prefix
    expect(names).toContain('heart')
    expect(names).toContain('broken_heart')
    expect(res.every(([n]) => n.includes('heart'))).toBe(true)
  })
  it('returns the unicode char alongside each name', () => {
    const res = searchUnicodeEmoji('joy')
    expect(res).toContainEqual(['joy', '😂'])
  })
  it('is case-insensitive', () => {
    expect(searchUnicodeEmoji('FIRE').some(([n]) => n === 'fire')).toBe(true)
  })
  it('respects the cap', () => {
    expect(searchUnicodeEmoji('e', 5)).toHaveLength(5)
  })
  it('an empty query returns a default grid (first cap emoji)', () => {
    expect(searchUnicodeEmoji('', 10)).toHaveLength(10)
  })
  it('returns [] when nothing matches', () => {
    expect(searchUnicodeEmoji('zzzznope')).toEqual([])
  })
  it('every returned char is within the 16-byte reaction cap (validEmoji parity)', () => {
    const enc = new TextEncoder()
    for (const [, char] of searchUnicodeEmoji('', 9999)) {
      expect(enc.encode(char).length).toBeLessThanOrEqual(16)
    }
  })
})
