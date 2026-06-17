// Persistent, per-device voice preferences (Rule A — local only, no backend). Device
// ids and DSP toggles are specific to the machine the user is on, so they live in
// localStorage rather than on the account. This module is the single source of truth;
// both the in-call voice bar and the User Settings → Voice & Video tab read/write it,
// and voice.ts capture reads the DSP flags here.

const IN_KEY = 'opencord.voice.inputDeviceId'
const OUT_KEY = 'opencord.voice.outputDeviceId'
const NS_KEY = 'opencord.voice.noiseSuppression'
const EC_KEY = 'opencord.voice.echoCancellation'
const AGC_KEY = 'opencord.voice.autoGainControl'

export interface AudioProcessing {
  ns: boolean // noise suppression
  ec: boolean // echo cancellation
  agc: boolean // auto-gain control
}

// localStorage can throw (private mode / disabled); never let a pref read break voice.
function read(key: string): string | null {
  try {
    return localStorage.getItem(key)
  } catch {
    return null
  }
}
function write(key: string, val: string): void {
  try {
    localStorage.setItem(key, val)
  } catch {
    /* storage unavailable — prefs just don't persist this session */
  }
}

// A toggle defaults to ON unless explicitly stored as '0' (so a fresh user gets the
// same high-quality DSP the app always applied — no behavior change until they opt out).
function readBool(key: string): boolean {
  return read(key) !== '0'
}

export function getInputDeviceId(): string {
  return read(IN_KEY) ?? ''
}
export function setInputDeviceId(id: string): void {
  write(IN_KEY, id)
}
export function getOutputDeviceId(): string {
  return read(OUT_KEY) ?? ''
}
export function setOutputDeviceId(id: string): void {
  write(OUT_KEY, id)
}

export function getAudioProcessing(): AudioProcessing {
  return { ns: readBool(NS_KEY), ec: readBool(EC_KEY), agc: readBool(AGC_KEY) }
}
export function setAudioProcessing(partial: Partial<AudioProcessing>): void {
  if (partial.ns !== undefined) write(NS_KEY, partial.ns ? '1' : '0')
  if (partial.ec !== undefined) write(EC_KEY, partial.ec ? '1' : '0')
  if (partial.agc !== undefined) write(AGC_KEY, partial.agc ? '1' : '0')
}
