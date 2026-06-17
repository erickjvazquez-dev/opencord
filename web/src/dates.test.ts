import { describe, it, expect } from 'vitest'
import { dayLabel, shortTime, messageTimestamp } from './dates'

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

describe('shortTime (gutter hover timestamp)', () => {
  it('shows hour:minute and omits seconds (locale-robust)', () => {
    const s = shortTime(new Date('2026-06-17T09:41:30'))
    expect(s).toMatch(/\d{1,2}:\d{2}/) // has hour:minute
    expect(s).not.toContain(':30') // no seconds component (would be ":30")
  })
  it('renders a non-empty string for midnight and noon', () => {
    expect(shortTime(new Date('2026-06-17T00:00:00')).length).toBeGreaterThan(0)
    expect(shortTime(new Date('2026-06-17T12:00:00')).length).toBeGreaterThan(0)
  })
})

describe('messageTimestamp (Discord-style message header time)', () => {
  const now = new Date('2026-06-17T15:00:00')
  it('prefixes the relative day and joins with "at", no seconds', () => {
    expect(messageTimestamp(new Date('2026-06-17T09:41:30'), now)).toMatch(/^Today at \d{1,2}:\d{2}/)
    expect(messageTimestamp(new Date('2026-06-16T09:41:00'), now)).toMatch(/^Yesterday at /)
    expect(messageTimestamp(new Date('2026-06-15T09:41:00'), now)).toMatch(/^June 15, 2026 at /)
  })
  it('never includes seconds', () => {
    expect(messageTimestamp(new Date('2026-06-17T09:41:30'), now)).not.toContain(':30')
  })
})
