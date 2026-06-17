import { describe, it, expect } from 'vitest'
import { effectiveVolume } from './voice'

// effectiveVolume is the master-output-volume math: a peer's personal volume scaled by
// the master, clamped to [0,1]. It drives every peer <audio> element's playback gain.
describe('effectiveVolume', () => {
  it('master 1.0 leaves the peer volume unchanged', () => {
    expect(effectiveVolume(0.8, 1)).toBeCloseTo(0.8)
    expect(effectiveVolume(1, 1)).toBe(1)
  })

  it('scales the peer volume by the master', () => {
    expect(effectiveVolume(1, 0.5)).toBeCloseTo(0.5)
    expect(effectiveVolume(0.6, 0.5)).toBeCloseTo(0.3)
  })

  it('master 0 mutes regardless of the peer volume', () => {
    expect(effectiveVolume(1, 0)).toBe(0)
    expect(effectiveVolume(0.7, 0)).toBe(0)
  })

  it('clamps the product into [0,1]', () => {
    expect(effectiveVolume(2, 2)).toBe(1) // never exceeds full scale
    expect(effectiveVolume(-1, 0.5)).toBe(0) // never goes negative
  })
})
