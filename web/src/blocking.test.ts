import { describe, it, expect } from 'vitest'
import { visibleMessages } from './blocking'
import type { Message } from './types'

// Minimal Message factory — only the fields visibleMessages reads (userId) matter; the
// rest are filled to satisfy the type.
const mk = (id: number, userId: number): Message => ({
  id,
  channelId: 1,
  userId,
  username: `u${userId}`,
  body: `m${id}`,
  createdAt: new Date(id * 1000).toISOString(),
})

describe('visibleMessages — hides blocked authors', () => {
  const msgs = [mk(1, 10), mk(2, 20), mk(3, 10), mk(4, 30)]

  it('returns the same list (identity) when nothing is blocked', () => {
    const out = visibleMessages(msgs, new Set())
    expect(out).toBe(msgs) // no copy when there's nothing to filter
  })

  it('drops every message from a blocked author, keeps order', () => {
    const out = visibleMessages(msgs, new Set([10]))
    expect(out.map((m) => m.id)).toEqual([2, 4])
  })

  it('can hide multiple blocked authors at once', () => {
    const out = visibleMessages(msgs, new Set([10, 30]))
    expect(out.map((m) => m.id)).toEqual([2])
  })

  it('unblocking (empty set again) restores all messages', () => {
    expect(visibleMessages(msgs, new Set()).map((m) => m.id)).toEqual([1, 2, 3, 4])
  })

  it('does not mutate the input list', () => {
    const copy = [...msgs]
    visibleMessages(msgs, new Set([20]))
    expect(msgs).toEqual(copy)
  })
})
