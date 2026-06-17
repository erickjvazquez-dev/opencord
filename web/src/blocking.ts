import type { Message } from './types'

// Drop messages authored by a blocked user so they never render. Filtering up front
// (before the date-divider + grouping pass) keeps that logic computing over the VISIBLE
// list — a hidden author can't break a grouping run or leave an orphaned day divider.
// Live WS messages from a blocked user are excluded here too (the render filters on the
// blocked set), so no special-casing is needed for the realtime path.
export function visibleMessages(messages: Message[], blocked: Set<number>): Message[] {
  if (blocked.size === 0) return messages
  return messages.filter((m) => !blocked.has(m.userId))
}
