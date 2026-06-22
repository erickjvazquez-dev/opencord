// Persistent, per-device voice preferences (Rule A — local only, no backend). Device
// ids and DSP toggles are specific to the machine the user is on, so they live in
// localStorage rather than on the account. This module is the single source of truth;
// both the in-call voice bar and the User Settings → Voice & Video tab read/write it,
// and voice.ts capture reads the DSP flags here.

const IN_KEY = 'opencord.voice.inputDeviceId'
const OUT_KEY = 'opencord.voice.outputDeviceId'
const CAM_KEY = 'opencord.voice.cameraDeviceId'
const VOL_KEY = 'opencord.voice.outputVolume'
const IVOL_KEY = 'opencord.voice.inputVolume'
const NS_KEY = 'opencord.voice.noiseSuppression'
const EC_KEY = 'opencord.voice.echoCancellation'
const AGC_KEY = 'opencord.voice.autoGainControl'
const SELF_MUTE_KEY = 'opencord.voice.selfMute'
const SELF_DEAFEN_KEY = 'opencord.voice.selfDeafen'

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

// A toggle that defaults to OFF unless explicitly stored as '1' (the inverse of readBool).
// Used for opt-in self-mute/self-deafen — a fresh user joins a call un-muted, as before.
function readBoolOff(key: string): boolean {
  return read(key) === '1'
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
export function getCameraDeviceId(): string {
  return read(CAM_KEY) ?? ''
}
export function setCameraDeviceId(id: string): void {
  write(CAM_KEY, id)
}

// Master output volume (0..1, default 1 = unchanged). Scales how loud you hear
// every peer, on top of each peer's individual volume.
export function getOutputVolume(): number {
  const raw = read(VOL_KEY)
  if (raw === null) return 1
  const n = parseFloat(raw)
  return Number.isFinite(n) ? Math.min(1, Math.max(0, n)) : 1
}
export function setOutputVolume(v: number): void {
  write(VOL_KEY, String(Math.min(1, Math.max(0, v))))
}

// Input (mic) volume (0..1, default 1 = unchanged). Scales how loud every peer
// hears YOU, applied as a gain node in the capture chain (voice.ts). Mirrors the
// output-volume pair.
export function getInputVolume(): number {
  const raw = read(IVOL_KEY)
  if (raw === null) return 1
  const n = parseFloat(raw)
  return Number.isFinite(n) ? Math.min(1, Math.max(0, n)) : 1
}
export function setInputVolume(v: number): void {
  write(IVOL_KEY, String(Math.min(1, Math.max(0, v))))
}

export function getAudioProcessing(): AudioProcessing {
  return { ns: readBool(NS_KEY), ec: readBool(EC_KEY), agc: readBool(AGC_KEY) }
}
export function setAudioProcessing(partial: Partial<AudioProcessing>): void {
  if (partial.ns !== undefined) write(NS_KEY, partial.ns ? '1' : '0')
  if (partial.ec !== undefined) write(EC_KEY, partial.ec ? '1' : '0')
  if (partial.agc !== undefined) write(AGC_KEY, partial.agc ? '1' : '0')
}

// Persisted "join already muted/deafened" defaults (Discord parity — the bottom-left
// user panel mute/deafen state survives reloads and seeds the NEXT call you join).
// Default OFF: a fresh user joins un-muted, exactly as before.
export function getSelfMute(): boolean {
  return readBoolOff(SELF_MUTE_KEY)
}
export function setSelfMute(on: boolean): void {
  write(SELF_MUTE_KEY, on ? '1' : '0')
}
export function getSelfDeafen(): boolean {
  return readBoolOff(SELF_DEAFEN_KEY)
}
export function setSelfDeafen(on: boolean): void {
  write(SELF_DEAFEN_KEY, on ? '1' : '0')
}
