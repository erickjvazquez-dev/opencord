import { describe, it, expect, beforeEach } from 'vitest'
import * as vs from './voiceSettings'

// voiceSettings reads the global `localStorage`; vitest's node env has none, so install a
// fresh in-memory shim per test for a clean round-trip. This module is load-bearing for
// devices, DSP toggles, output volume, and camera — its defaults/clamping must be exact.
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

describe('voiceSettings defaults (nothing stored)', () => {
  it('device ids default to empty (= OS default / Auto)', () => {
    expect(vs.getInputDeviceId()).toBe('')
    expect(vs.getOutputDeviceId()).toBe('')
    expect(vs.getCameraDeviceId()).toBe('')
  })
  it('output volume defaults to 1 (unchanged)', () => {
    expect(vs.getOutputVolume()).toBe(1)
  })
  it('input volume defaults to 1 (unchanged)', () => {
    expect(vs.getInputVolume()).toBe(1)
  })
  it('DSP toggles default ON (same high-quality capture as before opt-out)', () => {
    expect(vs.getAudioProcessing()).toEqual({ ns: true, ec: true, agc: true })
  })
  it('self-mute / self-deafen default OFF (a fresh user joins un-muted)', () => {
    expect(vs.getSelfMute()).toBe(false)
    expect(vs.getSelfDeafen()).toBe(false)
  })
})

describe('voiceSettings round-trips', () => {
  it('persists + reads back each device id', () => {
    vs.setInputDeviceId('mic-1')
    vs.setOutputDeviceId('spk-2')
    vs.setCameraDeviceId('cam-3')
    expect(vs.getInputDeviceId()).toBe('mic-1')
    expect(vs.getOutputDeviceId()).toBe('spk-2')
    expect(vs.getCameraDeviceId()).toBe('cam-3')
  })
})

describe('voiceSettings output volume clamping', () => {
  it('clamps into [0,1] on set AND on get', () => {
    vs.setOutputVolume(0.5)
    expect(vs.getOutputVolume()).toBe(0.5)
    vs.setOutputVolume(2) // above range
    expect(vs.getOutputVolume()).toBe(1)
    vs.setOutputVolume(-1) // below range
    expect(vs.getOutputVolume()).toBe(0)
  })
  it('falls back to 1 on a corrupt stored value', () => {
    globalThis.localStorage.setItem('opencord.voice.outputVolume', 'not-a-number')
    expect(vs.getOutputVolume()).toBe(1)
  })
})

describe('voiceSettings input volume clamping', () => {
  it('clamps into [0,1] on set AND on get', () => {
    vs.setInputVolume(0.5)
    expect(vs.getInputVolume()).toBe(0.5)
    vs.setInputVolume(2) // above range
    expect(vs.getInputVolume()).toBe(1)
    vs.setInputVolume(-1) // below range
    expect(vs.getInputVolume()).toBe(0)
  })
  it('falls back to 1 on a corrupt stored value', () => {
    globalThis.localStorage.setItem('opencord.voice.inputVolume', 'not-a-number')
    expect(vs.getInputVolume()).toBe(1)
  })
  it('is independent of output volume (separate keys)', () => {
    vs.setInputVolume(0.3)
    vs.setOutputVolume(0.9)
    expect(vs.getInputVolume()).toBe(0.3)
    expect(vs.getOutputVolume()).toBe(0.9)
  })
})

describe('voiceSettings DSP toggles (only "0" means off)', () => {
  it('a toggle set to false persists as off; others stay on', () => {
    vs.setAudioProcessing({ ns: false })
    expect(vs.getAudioProcessing()).toEqual({ ns: false, ec: true, agc: true })
    // The off value is the literal '0' (the readBool sentinel).
    expect(globalThis.localStorage.getItem('opencord.voice.noiseSuppression')).toBe('0')
  })
  it('turning a toggle back on restores it', () => {
    vs.setAudioProcessing({ ns: false })
    vs.setAudioProcessing({ ns: true })
    expect(vs.getAudioProcessing().ns).toBe(true)
  })
  it('a partial update leaves the untouched flags alone', () => {
    vs.setAudioProcessing({ ec: false })
    const p = vs.getAudioProcessing()
    expect(p).toEqual({ ns: true, ec: false, agc: true })
  })
})

describe('voiceSettings self-mute / self-deafen (opt-in, only "1" means on)', () => {
  it('persists + reads back self-mute independently of self-deafen', () => {
    vs.setSelfMute(true)
    expect(vs.getSelfMute()).toBe(true)
    expect(vs.getSelfDeafen()).toBe(false)
    // The on value is the literal '1' (the readBoolOff sentinel).
    expect(globalThis.localStorage.getItem('opencord.voice.selfMute')).toBe('1')
  })
  it('persists + reads back self-deafen independently of self-mute', () => {
    vs.setSelfDeafen(true)
    expect(vs.getSelfDeafen()).toBe(true)
    expect(vs.getSelfMute()).toBe(false)
  })
  it('turning a flag back off restores the default', () => {
    vs.setSelfMute(true)
    vs.setSelfMute(false)
    expect(vs.getSelfMute()).toBe(false)
  })
  it('a corrupt stored value reads as OFF (anything but "1")', () => {
    globalThis.localStorage.setItem('opencord.voice.selfMute', 'yes')
    expect(vs.getSelfMute()).toBe(false)
  })
})
