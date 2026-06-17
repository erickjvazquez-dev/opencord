import { describe, it, expect, beforeEach } from 'vitest'
import { shouldNotify, mentionsMe, getDesktopNotify, setDesktopNotify } from './notify'

// notify reads the global `localStorage`; vitest's node env has none, so install a
// fresh in-memory shim per test (mirrors voiceSettings.test.ts). The decision fn is
// pure and doesn't touch storage, so its cases don't depend on the shim.
beforeEach(() => {
  const store = new Map<string, string>()
  globalThis.localStorage = {
    getItem: (k: string) => (store.has(k) ? (store.get(k) as string) : null),
    setItem: (k: string, v: string) => void store.set(k, String(v)),
    removeItem: (k: string) => void store.delete(k),
    clear: () => store.clear(),
    key: () => null,
    length: 0,
  } as Storage
})

// The "all conditions met" baseline for shouldNotify: a server message that mentions
// me, while the tab is hidden, with the feature on and OS permission granted. Each
// test flips ONE field to prove that field is necessary.
const granted = {
  enabled: true,
  permission: 'granted',
  hidden: true,
  isMine: false,
  isDM: false,
  mentionsMe: true,
}

describe('shouldNotify — the positive cases', () => {
  it('notifies for a server @mention while hidden (all conditions met)', () => {
    expect(shouldNotify(granted)).toBe(true)
  })
  it('notifies for a DM while hidden even without a mention', () => {
    expect(shouldNotify({ ...granted, isDM: true, mentionsMe: false })).toBe(true)
  })
  it('notifies for a DM that also mentions me', () => {
    expect(shouldNotify({ ...granted, isDM: true, mentionsMe: true })).toBe(true)
  })
})

describe('shouldNotify — each guard suppresses it', () => {
  it('does not notify when the feature is disabled', () => {
    expect(shouldNotify({ ...granted, enabled: false })).toBe(false)
  })
  it('does not notify when permission is denied', () => {
    expect(shouldNotify({ ...granted, permission: 'denied' })).toBe(false)
  })
  it('does not notify when permission is the default (not yet granted)', () => {
    expect(shouldNotify({ ...granted, permission: 'default' })).toBe(false)
  })
  it('does not notify when the tab is focused (hidden=false)', () => {
    expect(shouldNotify({ ...granted, hidden: false })).toBe(false)
  })
  it('does not notify for your own message (isMine)', () => {
    expect(shouldNotify({ ...granted, isMine: true })).toBe(false)
  })
  it('does not notify for a server message that is neither a DM nor a mention', () => {
    expect(shouldNotify({ ...granted, isDM: false, mentionsMe: false })).toBe(false)
  })
  it('your own message in a DM still does not notify (isMine wins)', () => {
    expect(shouldNotify({ ...granted, isDM: true, isMine: true })).toBe(false)
  })
})

describe('mentionsMe', () => {
  it('matches @me (exact, case-insensitive)', () => {
    expect(mentionsMe('hey @alice are you there', 'alice')).toBe(true)
    expect(mentionsMe('hey @ALICE', 'alice')).toBe(true)
    expect(mentionsMe('hey @alice', 'ALICE')).toBe(true)
  })
  it('matches @everyone and @here regardless of username', () => {
    expect(mentionsMe('listen up @everyone', 'alice')).toBe(true)
    expect(mentionsMe('@here quick q', 'alice')).toBe(true)
    expect(mentionsMe('@EVERYONE shout', 'alice')).toBe(true)
  })
  it('does NOT match @meelsewhere (substring of a longer name)', () => {
    expect(mentionsMe('ping @aliceblue not you', 'alice')).toBe(false)
    expect(mentionsMe('@everyones party', 'alice')).toBe(false)
    expect(mentionsMe('@heretic', 'alice')).toBe(false)
  })
  it('does NOT match a different user', () => {
    expect(mentionsMe('hey @bob', 'alice')).toBe(false)
  })
  it('does NOT match the bare name without an @', () => {
    expect(mentionsMe('alice was here', 'alice')).toBe(false)
  })
  it('matches a mention at the very start or end of the body', () => {
    expect(mentionsMe('@alice', 'alice')).toBe(true)
    expect(mentionsMe('cc @alice', 'alice')).toBe(true)
  })
  it('is robust to a username containing regex metacharacters', () => {
    // The username is regex-escaped, so a dotted name only matches itself, not "axc".
    expect(mentionsMe('hi @a.c', 'a.c')).toBe(true)
    expect(mentionsMe('hi @axc', 'a.c')).toBe(false)
  })
  it('handles an empty body / empty username safely', () => {
    expect(mentionsMe('', 'alice')).toBe(false)
    expect(mentionsMe('@alice', '')).toBe(false)
    // @everyone still fires even with no username.
    expect(mentionsMe('@everyone', '')).toBe(true)
  })
})

describe('getDesktopNotify / setDesktopNotify', () => {
  it('defaults to false when nothing is stored', () => {
    expect(getDesktopNotify()).toBe(false)
  })
  it('round-trips true and false', () => {
    setDesktopNotify(true)
    expect(getDesktopNotify()).toBe(true)
    setDesktopNotify(false)
    expect(getDesktopNotify()).toBe(false)
  })
  it('persists the literal "1" for on (only "1" means on)', () => {
    setDesktopNotify(true)
    expect(globalThis.localStorage.getItem('opencord.notify.desktop')).toBe('1')
  })
})
