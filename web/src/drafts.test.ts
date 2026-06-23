import { describe, it, expect } from 'vitest'
import { parseStoredDrafts, serializeDrafts, DRAFT_MAX_LEN, DRAFT_MAX_CHANNELS } from './drafts'

describe('parseStoredDrafts (untrusted localStorage — Rule 15)', () => {
  it('round-trips a normal drafts map', () => {
    const m = parseStoredDrafts(JSON.stringify({ '1': 'hi', '2': 'there' }))
    expect(m.get(1)).toBe('hi')
    expect(m.get(2)).toBe('there')
    expect(m.size).toBe(2)
  })
  it('returns empty for null / corrupt JSON (never throws)', () => {
    expect(parseStoredDrafts(null).size).toBe(0)
    expect(parseStoredDrafts('not json {{{').size).toBe(0)
    expect(parseStoredDrafts('').size).toBe(0)
  })
  it('ignores non-object shapes (array, string, number)', () => {
    expect(parseStoredDrafts(JSON.stringify([1, 2, 3])).size).toBe(0)
    expect(parseStoredDrafts(JSON.stringify('a string')).size).toBe(0)
    expect(parseStoredDrafts(JSON.stringify(42)).size).toBe(0)
  })
  it('skips non-numeric keys, non-string and empty values', () => {
    const m = parseStoredDrafts(JSON.stringify({ abc: 'x', '1': 5, '2': '', '3': 'ok' }))
    expect(m.has(NaN)).toBe(false)
    expect(m.has(1)).toBe(false) // value was a number
    expect(m.has(2)).toBe(false) // empty
    expect(m.get(3)).toBe('ok')
    expect(m.size).toBe(1)
  })
  it('clamps an oversized draft to DRAFT_MAX_LEN', () => {
    const huge = 'a'.repeat(DRAFT_MAX_LEN * 3)
    const m = parseStoredDrafts(JSON.stringify({ '1': huge }))
    expect(m.get(1)!.length).toBe(DRAFT_MAX_LEN)
  })
  it('caps the number of channels (a tampered map cannot bloat memory)', () => {
    const obj: Record<string, string> = {}
    for (let i = 0; i < DRAFT_MAX_CHANNELS * 4; i++) obj[String(i + 1)] = 'x'
    expect(parseStoredDrafts(JSON.stringify(obj)).size).toBe(DRAFT_MAX_CHANNELS)
  })
})

describe('serializeDrafts', () => {
  it('drops empty drafts and round-trips through parse', () => {
    const m = new Map<number, string>([[1, 'keep'], [2, ''], [3, 'also']])
    const back = parseStoredDrafts(serializeDrafts(m))
    expect(back.get(1)).toBe('keep')
    expect(back.has(2)).toBe(false)
    expect(back.get(3)).toBe('also')
  })
  it('clamps each draft and caps the channel count on write', () => {
    const m = new Map<number, string>()
    for (let i = 0; i < DRAFT_MAX_CHANNELS * 2; i++) m.set(i + 1, 'b'.repeat(DRAFT_MAX_LEN * 2))
    const back = parseStoredDrafts(serializeDrafts(m))
    expect(back.size).toBe(DRAFT_MAX_CHANNELS)
    for (const v of back.values()) expect(v.length).toBe(DRAFT_MAX_LEN)
  })
})
