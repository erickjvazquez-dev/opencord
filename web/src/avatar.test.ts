import { describe, it, expect } from 'vitest'
import { avatarColor, avatarTextColor, initials } from './avatar'

// --- independent oracle: hsl -> sRGB -> WCAG relative luminance / contrast ---
// Deliberately a separate implementation from avatar.ts so the test verifies the
// readability CLAIM, not just that the code agrees with itself.
function hslToRgb(h: number, s: number, l: number): [number, number, number] {
  const a = s * Math.min(l, 1 - l)
  const f = (n: number) => {
    const k = (n + h / 30) % 12
    return l - a * Math.max(-1, Math.min(k - 3, 9 - k, 1))
  }
  return [f(0), f(8), f(4)]
}
function relLum([r, g, b]: [number, number, number]): number {
  const lin = (c: number) => (c <= 0.03928 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4))
  return 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b)
}
function contrast(l1: number, l2: number): number {
  const [hi, lo] = l1 > l2 ? [l1, l2] : [l2, l1]
  return (hi + 0.05) / (lo + 0.05)
}
function hueOf(name: string): number {
  const m = avatarColor(name).match(/^hsl\((\d+), 55%, 45%\)$/)
  if (!m) throw new Error(`avatarColor(${name}) = ${avatarColor(name)} — unexpected format`)
  return Number(m[1])
}

const SAMPLE = ['alice', 'bob', 'Carol', 'dave99', 'zoë', '日本語', 'a', 'ZZ', 'mallory', 'opencord-admin']

describe('avatarColor', () => {
  it('is hsl with fixed S/L and a hue in [0,360)', () => {
    for (const n of SAMPLE) {
      const h = hueOf(n)
      expect(h).toBeGreaterThanOrEqual(0)
      expect(h).toBeLessThan(360)
    }
  })
  it('is deterministic for the same name', () => {
    for (const n of SAMPLE) expect(avatarColor(n)).toBe(avatarColor(n))
  })
})

describe('avatarTextColor', () => {
  it('only ever returns black or white, deterministically', () => {
    for (const n of SAMPLE) {
      const c = avatarTextColor(n)
      expect(['#000000', '#ffffff']).toContain(c)
      expect(avatarTextColor(n)).toBe(c)
    }
  })
  it('picks the higher-contrast option against its own background (the readability claim)', () => {
    for (const n of SAMPLE) {
      const bgLum = relLum(hslToRgb(hueOf(n), 0.55, 0.45))
      const whiteContrast = contrast(relLum([1, 1, 1]), bgLum)
      const blackContrast = contrast(bgLum, relLum([0, 0, 0]))
      const chosen = avatarTextColor(n)
      const better = whiteContrast >= blackContrast ? '#ffffff' : '#000000'
      expect(chosen).toBe(better)
    }
  })
})

describe('initials', () => {
  it('takes the first two chars uppercased', () => {
    expect(initials('alice')).toBe('AL')
    expect(initials('Bob')).toBe('BO')
  })
  it('handles one-char and empty names without throwing', () => {
    expect(initials('x')).toBe('X')
    expect(initials('')).toBe('')
  })
  it('uppercases unicode where it has a case mapping', () => {
    expect(initials('zoë')).toBe('ZO')
    expect(initials('ñandú')).toBe('ÑA')
  })
})
