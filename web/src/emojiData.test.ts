import { describe, it, expect } from 'vitest'
import { UNICODE_EMOJI, unicodeEmoji, searchUnicodeEmoji } from './emojiData'

describe('unicodeEmoji lookup', () => {
  it('resolves known shortcodes to their unicode char', () => {
    expect(unicodeEmoji('joy')).toBe('😂')
    expect(unicodeEmoji('fire')).toBe('🔥')
    expect(unicodeEmoji('thumbsup')).toBe('👍')
    expect(unicodeEmoji('100')).toBe('💯')
  })
  it('is case-insensitive', () => {
    expect(unicodeEmoji('JOY')).toBe('😂')
    expect(unicodeEmoji('Fire')).toBe('🔥')
  })
  it('returns undefined for unknown / empty names (caller keeps the literal)', () => {
    expect(unicodeEmoji('definitely_not_an_emoji')).toBeUndefined()
    expect(unicodeEmoji('')).toBeUndefined()
  })
})

describe('searchUnicodeEmoji', () => {
  it('matches by substring, not just prefix', () => {
    const names = searchUnicodeEmoji('heart').map(([n]) => n)
    expect(names).toContain('heart')
    expect(names).toContain('broken_heart') // substring, not a prefix of "heart"
    expect(names.every(n => n.includes('heart'))).toBe(true)
  })
  it('is case-insensitive and trims the query', () => {
    expect(searchUnicodeEmoji('  FIRE  ').map(([n]) => n)).toContain('fire')
  })
  it('returns [name, char] pairs whose char is the map value', () => {
    for (const [name, char] of searchUnicodeEmoji('fire')) {
      expect(UNICODE_EMOJI.get(name)).toBe(char)
    }
  })
  it('caps the result count (default 40 and custom)', () => {
    expect(searchUnicodeEmoji('').length).toBe(40) // empty query = default grid
    expect(searchUnicodeEmoji('', 5).length).toBe(5)
    expect(searchUnicodeEmoji('a', 3).length).toBeLessThanOrEqual(3)
  })
  it('returns an empty array when nothing matches', () => {
    expect(searchUnicodeEmoji('zzzznomatch')).toEqual([])
  })
  it('preserves insertion order (stable grid)', () => {
    const all = [...UNICODE_EMOJI.keys()]
    const got = searchUnicodeEmoji('', 10).map(([n]) => n)
    expect(got).toEqual(all.slice(0, 10))
  })
})

describe('vendored map integrity (Rule A: offline, reachable by render + composer)', () => {
  it('every key is lowercase [a-z0-9_] so the :slug: render + autocomplete can reach it', () => {
    const bad = [...UNICODE_EMOJI.keys()].filter(k => !/^[a-z0-9_]+$/.test(k))
    expect(bad).toEqual([])
  })
  it('every value is a non-empty string (no blank entries)', () => {
    const blank = [...UNICODE_EMOJI.values()].filter(v => v === '')
    expect(blank).toEqual([])
  })
  it('is a generously sized common set (guards accidental truncation)', () => {
    expect(UNICODE_EMOJI.size).toBeGreaterThan(200)
  })
})
