import { describe, it, expect } from 'vitest'
import type { DMChannel } from './types'
import { dmOthers, dmIsGroup, dmTitle, dmMembersLabel, parseIdentifiers } from './dm'

const base = { id: 1, createdAt: '2026-01-01T00:00:00Z' }
const u = (id: number, username: string) => ({ id, username })

describe('dmOthers', () => {
  it('returns the users[] array when present', () => {
    const dm: DMChannel = { ...base, user: u(2, 'bob'), users: [u(2, 'bob'), u(3, 'carol')] }
    expect(dmOthers(dm).map((m) => m.username)).toEqual(['bob', 'carol'])
  })
  it('falls back to the legacy singular user when users is absent', () => {
    const dm: DMChannel = { ...base, user: u(2, 'bob') }
    expect(dmOthers(dm).map((m) => m.username)).toEqual(['bob'])
  })
  it('falls back when users is an empty array', () => {
    const dm: DMChannel = { ...base, user: u(2, 'bob'), users: [] }
    expect(dmOthers(dm).map((m) => m.username)).toEqual(['bob'])
  })
})

describe('dmIsGroup', () => {
  it('is false for a 1:1 (one other)', () => {
    expect(dmIsGroup({ ...base, user: u(2, 'bob'), users: [u(2, 'bob')] })).toBe(false)
  })
  it('is false for a legacy 1:1 payload (no users)', () => {
    expect(dmIsGroup({ ...base, user: u(2, 'bob') })).toBe(false)
  })
  it('is true for two or more others', () => {
    expect(dmIsGroup({ ...base, user: u(2, 'bob'), users: [u(2, 'bob'), u(3, 'carol')] })).toBe(true)
  })
})

describe('dmTitle', () => {
  it('is the single username for a 1:1', () => {
    expect(dmTitle({ ...base, user: u(2, 'bob'), users: [u(2, 'bob')] })).toBe('bob')
  })
  it('comma-joins the members for an unnamed group', () => {
    const dm: DMChannel = { ...base, user: u(2, 'bob'), users: [u(2, 'bob'), u(3, 'carol'), u(4, 'dave')] }
    expect(dmTitle(dm)).toBe('bob, carol, dave')
  })
  it('uses the custom name for a named group', () => {
    const dm: DMChannel = {
      ...base,
      user: u(2, 'bob'),
      users: [u(2, 'bob'), u(3, 'carol')],
      name: 'Weekend Trip',
    }
    expect(dmTitle(dm)).toBe('Weekend Trip')
  })
  it('falls back to members when the name is blank/whitespace', () => {
    const dm: DMChannel = {
      ...base,
      user: u(2, 'bob'),
      users: [u(2, 'bob'), u(3, 'carol')],
      name: '   ',
    }
    expect(dmTitle(dm)).toBe('bob, carol')
  })
  it('ignores a name on a 1:1 (only groups are named) — still the username', () => {
    // The server never names a 1:1; be defensive — a stray name is ignored, title = username.
    const dm: DMChannel = { ...base, user: u(2, 'bob'), users: [u(2, 'bob')], name: 'oops' }
    expect(dmTitle(dm)).toBe('bob')
  })
})

describe('dmMembersLabel', () => {
  it('always comma-joins the members, ignoring any custom name', () => {
    const dm: DMChannel = {
      ...base,
      user: u(2, 'bob'),
      users: [u(2, 'bob'), u(3, 'carol'), u(4, 'dave')],
      name: 'Weekend Trip',
    }
    expect(dmMembersLabel(dm)).toBe('bob, carol, dave')
  })
})

describe('parseIdentifiers', () => {
  it('splits on commas and whitespace, trims, drops blanks', () => {
    expect(parseIdentifiers('  bob, carol   dave ')).toEqual(['bob', 'carol', 'dave'])
  })
  it('dedupes while preserving first-seen order', () => {
    expect(parseIdentifiers('bob, bob, carol, bob')).toEqual(['bob', 'carol'])
  })
  it('returns an empty list for blank input', () => {
    expect(parseIdentifiers('   ,  ,\n')).toEqual([])
  })
  it('accepts numeric ids as strings', () => {
    expect(parseIdentifiers('42, bob, 7')).toEqual(['42', 'bob', '7'])
  })
})
