import { describe, it, expect } from 'vitest'
import { selectAudioSubscriptions } from './sfu'

// The top-N audio subscription picker is the heart of scaling voice past mesh: a
// huge room must never try to mix every stream. These cover the cases that matter.
describe('selectAudioSubscriptions', () => {
  it('subscribes to everyone when the room is within the cap', () => {
    const got = selectAudioSubscriptions(['a', 'b', 'c'], [], [], 12)
    expect(got).toEqual(new Set(['a', 'b', 'c']))
  })

  it('caps at max, prioritising current active speakers', () => {
    const all = Array.from({ length: 50 }, (_, i) => 'p' + i)
    const active = ['p40', 'p41', 'p42']
    const got = selectAudioSubscriptions(all, active, [], 3)
    expect(got.size).toBe(3)
    for (const id of active) expect(got.has(id)).toBe(true)
  })

  it('fills remaining slots with recently-active speakers (sticky, no flapping)', () => {
    const all = Array.from({ length: 50 }, (_, i) => 'p' + i)
    const active = ['p10'] // only one talking right now
    const recent = ['p20', 'p21'] // talked moments ago — keep them
    const got = selectAudioSubscriptions(all, active, recent, 3)
    expect(got).toEqual(new Set(['p10', 'p20', 'p21']))
  })

  it('still fills slots deterministically when nobody is active (quiet big room)', () => {
    const all = Array.from({ length: 20 }, (_, i) => 'p' + i)
    const got = selectAudioSubscriptions(all, [], [], 4)
    expect(got).toEqual(new Set(['p0', 'p1', 'p2', 'p3']))
  })

  it('ignores active/recent ids that are no longer in the room', () => {
    const all = ['a', 'b', 'c', 'd', 'e']
    const got = selectAudioSubscriptions(all, ['gone1'], ['gone2', 'c'], 2)
    // gone1/gone2 dropped; 'c' (recent + present) kept, then fill with first available.
    expect(got.has('c')).toBe(true)
    expect(got.size).toBe(2)
    expect(got.has('gone1')).toBe(false)
    expect(got.has('gone2')).toBe(false)
  })

  it('never exceeds max even with overlap between active and recent', () => {
    const all = Array.from({ length: 30 }, (_, i) => 'p' + i)
    const got = selectAudioSubscriptions(all, ['p1', 'p2'], ['p2', 'p3', 'p1'], 3)
    expect(got.size).toBe(3)
  })
})
