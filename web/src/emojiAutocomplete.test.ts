import { describe, it, expect } from 'vitest'
import { activeEmojiToken, matchEmojiNames, spliceEmoji } from './emojiAutocomplete'

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
