import { describe, it, expect } from 'vitest'
import { dayLabel } from './dates'

describe('dayLabel (Discord-style day divider)', () => {
  const now = new Date('2026-06-17T15:00:00')

  it('labels a message from the same calendar day "Today"', () => {
    expect(dayLabel(new Date('2026-06-17T09:30:00'), now)).toBe('Today')
    // Boundary: just after midnight today is still "Today".
    expect(dayLabel(new Date('2026-06-17T00:00:01'), now)).toBe('Today')
  })

  it('labels the previous calendar day "Yesterday"', () => {
    expect(dayLabel(new Date('2026-06-16T23:59:59'), now)).toBe('Yesterday')
    expect(dayLabel(new Date('2026-06-16T08:00:00'), now)).toBe('Yesterday')
  })

  it('labels older days with a full local date', () => {
    expect(dayLabel(new Date('2026-06-15T12:00:00'), now)).toBe('June 15, 2026')
    expect(dayLabel(new Date('2025-12-31T12:00:00'), now)).toBe('December 31, 2025')
  })

  it('handles the month boundary for "Yesterday" (not just day-1 arithmetic)', () => {
    const firstOfMonth = new Date('2026-07-01T10:00:00')
    expect(dayLabel(new Date('2026-06-30T22:00:00'), firstOfMonth)).toBe('Yesterday')
    expect(dayLabel(new Date('2026-07-01T01:00:00'), firstOfMonth)).toBe('Today')
  })
})
