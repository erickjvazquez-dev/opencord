// Mesh WebRTC voice for small calls (2–4). Signaling rides the existing channel
// WebSocket as voice-join / voice-leave / voice-signal frames (see SPEC.md
// "Voice channels — MVP"). Each pair of participants holds exactly one
// RTCPeerConnection. Glare on simultaneous joins is resolved with the WebRTC
// "perfect negotiation" pattern; politeness is decided by user id so the two
// sides always pick opposite roles without server coordination.
//
// The session is a plain class (no React) so the connection graph survives
// re-renders. Chat.tsx owns one instance per call, feeds it relayed frames, and
// renders the roster it emits.

import { getAudioProcessing } from './voiceSettings'

// Frames we send up the channel WS. The server stamps `from` and rebroadcasts.
export type VoiceFrame =
  | { type: 'voice-join' }
  | { type: 'voice-leave' }
  | { type: 'voice-signal'; target: number; signal: unknown }
  // Announce we started/stopped screen sharing. `streamId` (start only) is the
  // screen MediaStream's id so peers can tell its tracks apart from the mic.
  | { type: 'voice-screen'; on: boolean; streamId?: string }

// An inbound voice frame as relayed to the channel (server-stamped with `from`).
export interface VoiceInbound {
  type: string
  from?: number
  username?: string
  target?: number
  signal?: unknown
  on?: boolean
  streamId?: string
}

export type PeerState = 'connecting' | 'connected' | 'failed'

// The surface Chat.tsx drives, implemented by BOTH the mesh VoiceSession and the
// SFU SfuSession — so the UI is transport-agnostic (mesh by default; SFU when the
// server hands out a token).
export interface VoiceTransport {
  start(deviceId?: string): Promise<void>
  stop(): void
  toggleMute(): boolean
  // Silence ALL incoming audio and force the local mic off (deafen). Un-deafening
  // restores incoming audio and returns the mic to its prior mute/PTT state.
  setDeafened(on: boolean): void
  setInputDevice(deviceId?: string): Promise<void>
  setOutputDevice(deviceId: string): void
  setPeerVolume(id: number, volume: number): void
  setPushToTalk(enabled: boolean): void
  setTransmitting(on: boolean): void
  // ── Screen share ──────────────────────────────────────────────────────────
  // Start/stop sharing the screen (getDisplayMedia: high-res video + optional
  // system/tab audio). Capable of 4K@60 when the source and link allow it.
  startScreenShare(): Promise<void>
  stopScreenShare(): void
  isScreenSharing(): boolean
  // Audio mixing while sharing screen audio:
  //  • setScreenSendGain — the SHARER raises/lowers the shared audio level sent to
  //    ALL viewers (gain on the outgoing track; 1 = unchanged, >1 louder).
  //  • setScreenMonitorVolume — the SHARER's own local monitor of the shared audio
  //    (0 = off, the default, so it doesn't echo through their speakers).
  //  • setPeerScreenVolume — a VIEWER's personal playback volume for a peer's shared
  //    audio (0..1), independent of that peer's mic/voice volume.
  setScreenSendGain(gain: number): void
  setScreenMonitorVolume(volume: number): void
  setPeerScreenVolume(id: number, volume: number): void
  handle(ev: VoiceInbound): void | Promise<void>
}

// A remote participant as shown in the roster.
export interface VoicePeer {
  id: number
  username: string
  state: PeerState
  // True while this peer is actively talking (client-side voice-activity detection).
  speaking: boolean
  // Per-listener playback volume for this peer, 0..1 (local only — never signaled).
  volume: number
  // True while this peer is screen sharing; screenStream carries the live screen
  // video for the UI to render. screenVolume is the local playback volume (0..1)
  // for this peer's shared audio (independent of `volume`).
  sharingScreen: boolean
  screenStream: MediaStream | null
  screenVolume: number
}

// Public STUN only (Rule A: no required paid service). On loopback/LAN, host
// candidates connect without it; TURN for hostile NATs is a later slice.
const RTC_CONFIG: RTCConfiguration = {
  iceServers: [{ urls: 'stun:stun.l.google.com:19302' }],
}

// Target outbound Opus bitrate. ~96 kbps mono is comfortably above Discord's
// default (~64 kbps) for crisper voice while staying cheap on a mesh.
const VOICE_BITRATE = 96_000

// Screen-share capture: request up to 4K@60 with a high max bitrate so a sharp,
// smooth share is possible when the source + link can sustain it; the browser
// negotiates down gracefully on weaker hardware/networks. `frameRate.max` is the
// cap, not a demand. System/tab audio is requested too (the user's picker decides).
const SCREEN_CONSTRAINTS: DisplayMediaStreamOptions = {
  video: {
    width: { ideal: 3840, max: 3840 },
    height: { ideal: 2160, max: 2160 },
    frameRate: { ideal: 60, max: 60 },
  },
  audio: {
    echoCancellation: false, // system audio must not be processed as a "mic"
    noiseSuppression: false,
    autoGainControl: false,
  },
}

// Max outbound bitrate for the screen video. 8 Mbps comfortably carries detailed
// 4K UI / slides; high-motion 4K60 will use more if the encoder/link allow it.
const SCREEN_BITRATE = 8_000_000

// Voice-activity detection (the "who's talking" ring). A source whose time-domain
// RMS exceeds VAD_THRESHOLD is loud; it stays flagged "speaking" for VAD_HANG_MS
// after its last loud sample (hysteresis, so the ring doesn't flicker between
// syllables). Sampled every VAD_INTERVAL_MS.
const VAD_THRESHOLD = 0.02
const VAD_HANG_MS = 250
const VAD_INTERVAL_MS = 120
const VAD_FFT_SIZE = 512

// High-quality voice capture. Browser DSP (echo cancellation, noise suppression,
// auto-gain) is the same toolchain Discord uses; 48 kHz mono keeps Opus crisp.
// No `deviceId` → the OS's *current* default input (the headset the user is
// actually talking into); an explicit id pins a chosen device. The 3 DSP flags
// come from the user's Voice & Video settings (all default ON, so capture is
// unchanged until they opt out in the settings tab).
function audioConstraints(deviceId?: string): MediaStreamConstraints {
  const { ns, ec, agc } = getAudioProcessing()
  return {
    audio: {
      ...(deviceId ? { deviceId: { exact: deviceId } } : {}),
      echoCancellation: ec,
      noiseSuppression: ns,
      autoGainControl: agc,
      channelCount: 1,
      sampleRate: 48_000,
    },
    video: false,
  }
}

// setSinkId is widely shipped but still absent from some lib.dom versions.
type SinkCapableAudio = HTMLAudioElement & { setSinkId?: (id: string) => Promise<void> }

// What the SDP/ICE `signal` payload carries (one or the other per frame).
interface SignalPayload {
  description?: RTCSessionDescriptionInit
  candidate?: RTCIceCandidateInit
}

interface Peer {
  pc: RTCPeerConnection
  username: string
  state: PeerState
  // Perfect-negotiation bookkeeping (per MDN): are we mid-offer, are we ignoring
  // a colliding remote offer, and are we the "polite" side for this pair.
  makingOffer: boolean
  ignoreOffer: boolean
  polite: boolean
  audioEl: HTMLAudioElement
  // Voice-activity state: analyser on the remote stream, last-loud timestamp, and
  // the debounced speaking flag reported to the UI.
  analyser: AnalyserNode | null
  loudAt: number
  speaking: boolean
  // Per-listener playback volume (0..1), applied to this peer's <audio> element.
  volume: number
  // Screen share received from this peer: the screen MediaStream id (so its audio
  // is told apart from the mic), the live video stream for the UI, a dedicated
  // <audio> element for the screen's audio, and its local playback volume (0..1).
  screenStreamId: string | null
  screenStream: MediaStream | null
  screenAudioEl: HTMLAudioElement | null
  screenVolume: number
}

export class VoiceSession {
  private peers = new Map<number, Peer>()
  private localStream: MediaStream | null = null
  private muted = false
  // Deafened: all incoming audio is silenced AND the local mic is forced off.
  private deafened = false
  // Push-to-talk: when enabled, the mic is live ONLY while `transmitting` (you're
  // holding the Talk control); otherwise it's silent. Supersedes `muted`.
  private pttEnabled = false
  private transmitting = false
  private stopped = false
  // Chosen devices (undefined/'' = follow the OS default). Output is applied to
  // every peer's <audio> sink; input is the constraint for capture.
  private inputDeviceId: string | undefined
  private outputDeviceId = ''
  // Voice-activity detection: a shared AudioContext, an analyser on the local mic,
  // a sampling timer, and the local speaking state (debounced like peers').
  private audioCtx: AudioContext | null = null
  private localAnalyser: AnalyserNode | null = null
  private vadTimer: ReturnType<typeof setInterval> | null = null
  private localLoudAt = 0
  private localSpeaking = false

  // Screen share (local, when WE are sharing). screenStream is the getDisplayMedia
  // capture; screenVideoTrack/screenSendAudioTrack are what we publish to peers.
  // The send-gain graph lets the sharer scale the audio level all viewers hear;
  // the monitor element lets the sharer hear their own shared audio (default off).
  private screenStream: MediaStream | null = null
  private screenVideoTrack: MediaStreamTrack | null = null
  private screenSendAudioTrack: MediaStreamTrack | null = null
  private screenSendGain: GainNode | null = null
  private screenSendGainValue = 1
  private screenMonitorEl: HTMLAudioElement | null = null
  private screenMonitorVolume = 0

  constructor(
    private myId: number,
    private send: (frame: VoiceFrame) => void,
    private onRoster: (peers: VoicePeer[]) => void,
    // Reports the local participant's speaking state (optional; the roster carries
    // remote peers' speaking state).
    private onLocalSpeaking?: (speaking: boolean) => void,
    // Reports OUR own screen-share stream (for a local preview), or null on stop.
    private onLocalScreen?: (stream: MediaStream | null) => void,
    // ICE servers from the server (configured STUN + optional TURN for hostile NATs).
    // Falls back to the built-in public STUN if the voice-token call didn't provide any.
    private iceServers?: RTCIceServer[],
  ) {}

  // RTCConfiguration for every peer connection: prefer the server-provided ICE servers
  // (configurable STUN + optional TURN relay); fall back to the built-in public STUN.
  private rtcConfig(): RTCConfiguration {
    return this.iceServers && this.iceServers.length > 0
      ? { iceServers: this.iceServers }
      : RTC_CONFIG
  }

  // Acquire the mic and announce we're in the call. Rejects if the mic is denied
  // — the caller surfaces that to the user and discards the session. With no
  // deviceId we capture the OS default input, i.e. whatever the user is on.
  async start(deviceId?: string): Promise<void> {
    this.inputDeviceId = deviceId
    this.localStream = await navigator.mediaDevices.getUserMedia(audioConstraints(deviceId))
    if (this.stopped) {
      // Left before the mic resolved — don't leak the device.
      this.localStream.getTracks().forEach((t) => t.stop())
      this.localStream = null
      return
    }
    this.startVad()
    this.send({ type: 'voice-join' })
  }

  // Spin up Web Audio metering for the speaking indicator. Best-effort: if Web
  // Audio is unavailable the call still works, just without the "who's talking" ring.
  private startVad(): void {
    try {
      this.audioCtx = new AudioContext()
      void this.audioCtx.resume().catch(() => {})
      if (this.localStream) this.localAnalyser = this.makeAnalyser(this.localStream)
      const buf = new Uint8Array(new ArrayBuffer(VAD_FFT_SIZE))
      this.vadTimer = setInterval(() => this.sampleVad(buf), VAD_INTERVAL_MS)
    } catch {
      /* Web Audio unavailable — no speaking indicator; voice itself is unaffected. */
    }
  }

  // Build an analyser tapping a stream's audio for level metering. Not connected to
  // the destination, so it never routes audio (no echo); purely a meter.
  private makeAnalyser(stream: MediaStream): AnalyserNode | null {
    if (!this.audioCtx) return null
    try {
      const src = this.audioCtx.createMediaStreamSource(stream)
      const an = this.audioCtx.createAnalyser()
      an.fftSize = VAD_FFT_SIZE
      an.smoothingTimeConstant = 0.3
      src.connect(an)
      return an
    } catch {
      return null
    }
  }

  // Time-domain RMS of an analyser's current frame (0 = silence, ~1 = full scale).
  private rms(an: AnalyserNode, buf: Uint8Array<ArrayBuffer>): number {
    an.getByteTimeDomainData(buf)
    let sum = 0
    for (let i = 0; i < buf.length; i++) {
      const x = (buf[i] - 128) / 128
      sum += x * x
    }
    return Math.sqrt(sum / buf.length)
  }

  // One VAD tick: refresh each source's last-loud time, recompute the debounced
  // speaking flags, and notify the UI only when something changed.
  private sampleVad(buf: Uint8Array<ArrayBuffer>): void {
    const now = Date.now()
    // Only your live mic can register as speaking (silent under mute, or under PTT
    // when you're not holding Talk).
    const micLive = this.pttEnabled ? this.transmitting : !this.muted
    if (this.localAnalyser && micLive && this.rms(this.localAnalyser, buf) > VAD_THRESHOLD) {
      this.localLoudAt = now
    }
    const localSpeaking = micLive && now - this.localLoudAt < VAD_HANG_MS
    if (localSpeaking !== this.localSpeaking) {
      this.localSpeaking = localSpeaking
      this.onLocalSpeaking?.(localSpeaking)
    }
    let changed = false
    for (const peer of this.peers.values()) {
      if (peer.analyser && this.rms(peer.analyser, buf) > VAD_THRESHOLD) peer.loudAt = now
      const speaking = now - peer.loudAt < VAD_HANG_MS
      if (speaking !== peer.speaking) {
        peer.speaking = speaking
        changed = true
      }
    }
    if (changed) this.emitRoster()
  }

  // Switch the capture device live: re-acquire and hot-swap the track on every
  // peer (no renegotiation). Pass undefined to follow the OS default again. Used
  // both for manual selection and to auto-follow a freshly plugged-in headset.
  async setInputDevice(deviceId?: string): Promise<void> {
    if (this.stopped) return
    const next = await navigator.mediaDevices.getUserMedia(audioConstraints(deviceId))
    const track = next.getAudioTracks()[0]
    if (!track) return
    // Match the current mute / push-to-talk state on the freshly captured track.
    track.enabled = this.pttEnabled ? this.transmitting : !this.muted
    for (const peer of this.peers.values()) {
      // The mic sender — never the screen-audio sender (also kind 'audio').
      const sender = peer.pc
        .getSenders()
        .find((s) => s.track?.kind === 'audio' && s.track !== this.screenSendAudioTrack)
      if (sender) {
        await sender.replaceTrack(track)
        void this.tuneSender(sender)
      }
    }
    this.localStream?.getTracks().forEach((t) => t.stop())
    this.localStream = next
    this.inputDeviceId = deviceId
    // Re-tap the new mic for the speaking meter.
    if (this.audioCtx) this.localAnalyser = this.makeAnalyser(next)
  }

  // Route remote audio to a chosen output device (headphones/speakers). '' (or a
  // browser without setSinkId) leaves it on the OS default, which auto-follows.
  setOutputDevice(deviceId: string): void {
    this.outputDeviceId = deviceId
    for (const peer of this.peers.values()) this.applySink(peer.audioEl)
  }

  // Set how loudly *you* hear one peer (0..1). Local only — never signaled, so it
  // can't be abused to make someone else louder for everyone.
  setPeerVolume(id: number, volume: number): void {
    const peer = this.peers.get(id)
    if (!peer) return
    peer.volume = Math.min(1, Math.max(0, volume))
    peer.audioEl.volume = peer.volume
    this.emitRoster()
  }

  currentInputDevice(): string | undefined {
    return this.inputDeviceId
  }

  // Leave the call: tell the channel, tear down every peer and release the mic.
  stop(): void {
    if (this.stopped) return
    // End any active screen share first (releases the OS capture + notifies peers)
    // while we can still send, before we mark ourselves stopped.
    this.stopScreenShare()
    this.stopped = true
    this.send({ type: 'voice-leave' })
    if (this.vadTimer) {
      clearInterval(this.vadTimer)
      this.vadTimer = null
    }
    this.localAnalyser = null
    void this.audioCtx?.close().catch(() => {})
    this.audioCtx = null
    for (const id of [...this.peers.keys()]) this.dropPeer(id)
    this.localStream?.getTracks().forEach((t) => t.stop())
    this.localStream = null
    this.emitRoster()
  }

  // Toggle the local mic (keeps the peer connections up). Returns the new muted
  // state. No-op effect under push-to-talk (transmitting governs the mic there).
  toggleMute(): boolean {
    this.muted = !this.muted
    this.applyMicState()
    return this.muted
  }

  // Deafen / un-deafen: mute (or restore) every remote audio element and force the
  // mic off while deafened. Playback only — the remote MediaStreams are untouched, so
  // speaking rings keep showing who's talking.
  setDeafened(on: boolean): void {
    this.deafened = on
    this.peers.forEach((p) => {
      p.audioEl.muted = on
      if (p.screenAudioEl) p.screenAudioEl.muted = on // deafen silences shared audio too
    })
    this.applyMicState()
  }

  // Turn push-to-talk on/off. Enabling it silences the mic until you hold Talk.
  setPushToTalk(enabled: boolean): void {
    this.pttEnabled = enabled
    this.transmitting = false
    this.applyMicState()
  }

  // While push-to-talk is on, open (hold) or close (release) the mic.
  setTransmitting(on: boolean): void {
    if (!this.pttEnabled) return
    this.transmitting = on
    this.applyMicState()
  }

  // Drive the mic track + speaking ring from the current mute / PTT state. The mic
  // is live when: PTT on → only while transmitting; PTT off → unless muted.
  private applyMicState(): void {
    const live = !this.deafened && (this.pttEnabled ? this.transmitting : !this.muted)
    this.localStream?.getAudioTracks().forEach((t) => (t.enabled = live))
    // A silent mic can't be "speaking" — clear the ring immediately, don't wait
    // for the VAD hang window.
    if (!live && this.localSpeaking) {
      this.localSpeaking = false
      this.onLocalSpeaking?.(false)
    }
  }

  // ── Screen share ────────────────────────────────────────────────────────────

  isScreenSharing(): boolean {
    return this.screenStream != null
  }

  // Capture the screen (and optional system/tab audio) and publish it to every
  // peer. Rejects if the user cancels the picker or capture is unsupported — the
  // caller surfaces that. Adding the tracks triggers perfect-negotiation renegotiation.
  async startScreenShare(): Promise<void> {
    if (this.stopped || this.screenStream) return
    const stream = await navigator.mediaDevices.getDisplayMedia(SCREEN_CONSTRAINTS)
    if (this.stopped) {
      stream.getTracks().forEach((t) => t.stop())
      return
    }
    this.screenStream = stream
    this.screenVideoTrack = stream.getVideoTracks()[0] ?? null
    if (this.screenVideoTrack) {
      // 'detail' keeps text/UI crisp; the browser trades frame rate for clarity.
      this.screenVideoTrack.contentHint = 'detail'
      // The browser's own "Stop sharing" affordance ends the track — mirror it.
      this.screenVideoTrack.onended = () => this.stopScreenShare()
    }
    // Route any captured system audio through a gain node so the sharer can scale
    // what viewers hear; the processed track is what we publish.
    const rawAudio = stream.getAudioTracks()[0]
    if (rawAudio) this.screenSendAudioTrack = this.buildScreenSendAudio(rawAudio)

    for (const peer of this.peers.values()) this.addScreenTracksToPeer(peer)
    // Tell peers which stream id is the screen so they classify its tracks, then
    // expose our own stream for a local preview.
    this.send({ type: 'voice-screen', on: true, streamId: stream.id })
    this.onLocalScreen?.(stream)
  }

  // Stop sharing: drop the screen senders from every peer, tear down capture +
  // the audio graph, and tell peers. Safe to call when not sharing.
  stopScreenShare(): void {
    if (!this.screenStream) return
    for (const peer of this.peers.values()) this.removeScreenTracksFromPeer(peer)
    this.screenStream.getTracks().forEach((t) => t.stop())
    this.screenSendAudioTrack?.stop()
    try {
      this.screenSendGain?.disconnect()
    } catch {
      /* already disconnected */
    }
    if (this.screenMonitorEl) {
      this.screenMonitorEl.srcObject = null
      this.screenMonitorEl.remove()
      this.screenMonitorEl = null
    }
    this.screenStream = null
    this.screenVideoTrack = null
    this.screenSendAudioTrack = null
    this.screenSendGain = null
    if (!this.stopped) this.send({ type: 'voice-screen', on: false })
    this.onLocalScreen?.(null)
  }

  // SHARER control: scale the screen-audio level sent to ALL viewers (1 = as
  // captured, >1 louder, 0 = silent). Applied on the outgoing gain node live.
  setScreenSendGain(gain: number): void {
    this.screenSendGainValue = Math.max(0, Math.min(4, gain))
    if (this.screenSendGain) this.screenSendGain.gain.value = this.screenSendGainValue
  }

  // SHARER control: local monitor volume for your OWN shared audio (0 = off, the
  // default, so it doesn't echo through your speakers; raise it on headphones).
  setScreenMonitorVolume(volume: number): void {
    this.screenMonitorVolume = Math.max(0, Math.min(1, volume))
    if (this.screenMonitorEl) this.screenMonitorEl.volume = this.screenMonitorVolume
  }

  // VIEWER control: how loudly YOU hear a peer's shared audio (0..1), separate
  // from that peer's mic/voice volume. Local only — never signaled.
  setPeerScreenVolume(id: number, volume: number): void {
    const peer = this.peers.get(id)
    if (!peer) return
    peer.screenVolume = Math.max(0, Math.min(1, volume))
    if (peer.screenAudioEl) peer.screenAudioEl.volume = peer.screenVolume
    this.emitRoster()
  }

  // Build the outgoing screen-audio track: raw capture → GainNode → destination,
  // so setScreenSendGain scales it live. Also wires the sharer's local monitor
  // element (muted by default). Falls back to the raw track if Web Audio is absent.
  private buildScreenSendAudio(raw: MediaStreamTrack): MediaStreamTrack {
    const ctx = this.audioCtx
    if (!ctx) return raw
    try {
      const src = ctx.createMediaStreamSource(new MediaStream([raw]))
      const gain = ctx.createGain()
      gain.gain.value = this.screenSendGainValue
      const dest = ctx.createMediaStreamDestination()
      src.connect(gain).connect(dest)
      this.screenSendGain = gain
      // Local monitor: play the captured audio back to the sharer, off by default.
      const mon = new Audio()
      mon.autoplay = true
      mon.srcObject = new MediaStream([raw])
      mon.volume = this.screenMonitorVolume
      mon.style.display = 'none'
      document.body.appendChild(mon)
      void mon.play().catch(() => {})
      this.screenMonitorEl = mon
      return dest.stream.getAudioTracks()[0] ?? raw
    } catch {
      return raw
    }
  }

  // Add our screen video (+ processed audio) to one peer, associated with the
  // screen MediaStream's id so the receiver groups them as the screen, not the mic.
  private addScreenTracksToPeer(peer: Peer): void {
    if (!this.screenStream) return
    if (this.screenVideoTrack) {
      const sender = peer.pc.addTrack(this.screenVideoTrack, this.screenStream)
      void this.tuneSender(sender, SCREEN_BITRATE)
    }
    if (this.screenSendAudioTrack) peer.pc.addTrack(this.screenSendAudioTrack, this.screenStream)
  }

  // Remove our screen senders from one peer (renegotiates them away).
  private removeScreenTracksFromPeer(peer: Peer): void {
    for (const sender of peer.pc.getSenders()) {
      const t = sender.track
      if (t && (t === this.screenVideoTrack || t === this.screenSendAudioTrack)) {
        try {
          peer.pc.removeTrack(sender)
        } catch {
          /* peer already closing */
        }
      }
    }
  }

  // Feed a server-stamped voice-* frame relayed on the channel WS.
  async handle(ev: VoiceInbound): Promise<void> {
    if (this.stopped || ev.from == null || ev.from === this.myId) return
    if (ev.type === 'voice-join') {
      // A participant appeared. Open a peer; perfect negotiation drives the
      // offer/answer from whichever side fires negotiationneeded first.
      this.ensurePeer(ev.from, ev.username ?? '')
      // If WE are already sharing, re-announce so the new joiner learns our screen
      // stream id (ensurePeer also publishes our screen tracks to them).
      if (this.screenStream) {
        this.send({ type: 'voice-screen', on: true, streamId: this.screenStream.id })
      }
    } else if (ev.type === 'voice-leave') {
      this.dropPeer(ev.from)
    } else if (ev.type === 'voice-screen') {
      this.onScreenAnnounce(ev.from, ev.username ?? '', ev.on === true, ev.streamId)
    } else if (ev.type === 'voice-signal' && ev.target === this.myId) {
      await this.onSignal(ev.from, ev.username ?? '', ev.signal as SignalPayload | undefined)
    }
  }

  // A peer announced they started (on) or stopped (off) screen sharing. On start
  // we record their screen stream id so the screen's tracks are told apart from
  // the mic — and reclassify any screen track that arrived before this frame. On
  // stop we tear down their screen video/audio.
  private onScreenAnnounce(id: number, username: string, on: boolean, streamId?: string): void {
    const peer = this.ensurePeer(id, username)
    if (on && streamId) {
      peer.screenStreamId = streamId
      // A screen audio track may have landed before this frame and been treated as
      // the mic — if the mic element now holds the screen stream, move it over.
      const micStream = peer.audioEl.srcObject as MediaStream | null
      if (micStream && micStream.id === streamId) {
        peer.audioEl.srcObject = null
        this.attachScreenAudio(peer, micStream)
      }
    } else {
      this.teardownPeerScreen(peer)
    }
    this.emitRoster()
  }

  private ensurePeer(id: number, username: string): Peer {
    const existing = this.peers.get(id)
    if (existing) {
      if (username && existing.username !== username) {
        existing.username = username
        this.emitRoster()
      }
      return existing
    }

    const pc = new RTCPeerConnection(this.rtcConfig())
    const audioEl = new Audio()
    audioEl.autoplay = true
    audioEl.dataset.voiceAudio = String(id)
    audioEl.style.display = 'none'
    audioEl.muted = this.deafened // a peer arriving while deafened stays silent
    document.body.appendChild(audioEl)
    const peer: Peer = {
      pc,
      username,
      state: 'connecting',
      makingOffer: false,
      ignoreOffer: false,
      // Total order on ids → the two sides always disagree on politeness.
      polite: this.myId > id,
      audioEl,
      analyser: null,
      loudAt: 0,
      speaking: false,
      volume: 1,
      screenStreamId: null,
      screenStream: null,
      screenAudioEl: null,
      screenVolume: 1,
    }
    this.peers.set(id, peer)
    this.applySink(audioEl)

    // Send our mic to this peer. Adding a track schedules negotiationneeded.
    this.localStream?.getTracks().forEach((t) => {
      const sender = pc.addTrack(t, this.localStream!)
      void this.tuneSender(sender)
    })
    // If we're already sharing our screen, publish it to this (new) peer too.
    if (this.screenStream) this.addScreenTracksToPeer(peer)

    pc.onnegotiationneeded = async () => {
      try {
        peer.makingOffer = true
        await pc.setLocalDescription()
        this.send({ type: 'voice-signal', target: id, signal: { description: pc.localDescription } })
      } catch (err) {
        console.error('[voice] negotiation failed', err)
      } finally {
        peer.makingOffer = false
      }
    }
    pc.onicecandidate = ({ candidate }) => {
      if (candidate) this.send({ type: 'voice-signal', target: id, signal: { candidate } })
    }
    pc.ontrack = ({ track, streams }) => {
      const stream = streams[0] ?? null
      // Video is always the screen share (the mesh mic is audio-only).
      if (track.kind === 'video') {
        peer.screenStreamId = stream?.id ?? peer.screenStreamId
        peer.screenStream = stream
        // Safety net: if the track truly ends (not just the voice-screen frame),
        // tear the tile down so a stopped share can't linger.
        track.onended = () => {
          if (peer.screenStream === stream) {
            this.teardownPeerScreen(peer)
            this.emitRoster()
          }
        }
        this.emitRoster()
        return
      }
      // Audio belonging to the announced screen stream is screen audio; route it to
      // its own element (its own volume), not the mic element.
      if (stream && peer.screenStreamId && stream.id === peer.screenStreamId) {
        this.attachScreenAudio(peer, stream)
        return
      }
      // Otherwise it's the mic.
      peer.audioEl.srcObject = stream
      peer.audioEl.volume = peer.volume
      void peer.audioEl.play().catch(() => {})
      // Tap the remote stream for the speaking meter.
      if (stream) peer.analyser = this.makeAnalyser(stream)
    }
    pc.onconnectionstatechange = () => {
      const s = pc.connectionState
      if (s === 'failed' || s === 'closed') {
        this.dropPeer(id)
        return
      }
      peer.state = s === 'connected' ? 'connected' : 'connecting'
      this.emitRoster()
    }

    this.emitRoster()
    return peer
  }

  private async onSignal(id: number, username: string, signal?: SignalPayload): Promise<void> {
    if (!signal) return
    const peer = this.ensurePeer(id, username)
    const { pc } = peer
    try {
      if (signal.description) {
        const desc = signal.description
        const offerCollision =
          desc.type === 'offer' && (peer.makingOffer || pc.signalingState !== 'stable')
        // Impolite side wins a collision by ignoring the incoming offer; polite
        // side yields (setRemoteDescription rolls back our own offer implicitly).
        peer.ignoreOffer = !peer.polite && offerCollision
        if (peer.ignoreOffer) return
        await pc.setRemoteDescription(desc)
        if (desc.type === 'offer') {
          await pc.setLocalDescription()
          this.send({
            type: 'voice-signal',
            target: id,
            signal: { description: pc.localDescription },
          })
        }
      } else if (signal.candidate) {
        try {
          await pc.addIceCandidate(signal.candidate)
        } catch (err) {
          // A candidate that arrives for an offer we ignored is expected noise.
          if (!peer.ignoreOffer) throw err
        }
      }
    } catch (err) {
      console.error('[voice] signal handling failed', err)
    }
  }

  // Raise the sender's max bitrate (crisper voice, or a high ceiling for 4K screen
  // video). encodings may be empty before negotiation, so seed one; best-effort
  // (older browsers may reject). For video, prefer holding resolution over frame
  // rate so shared text/UI stays sharp.
  private async tuneSender(sender: RTCRtpSender, maxBitrate = VOICE_BITRATE): Promise<void> {
    try {
      const params = sender.getParameters()
      if (!params.encodings || params.encodings.length === 0) params.encodings = [{}]
      params.encodings[0].maxBitrate = maxBitrate
      if (sender.track?.kind === 'video') {
        params.degradationPreference = 'maintain-resolution'
      }
      await sender.setParameters(params)
    } catch {
      /* unsupported — fall back to the browser default bitrate */
    }
  }

  // Route a peer's audio element to the chosen output device, if any/supported.
  private applySink(el: HTMLAudioElement): void {
    const sinkable = el as SinkCapableAudio
    if (this.outputDeviceId && typeof sinkable.setSinkId === 'function') {
      void sinkable.setSinkId(this.outputDeviceId).catch(() => {})
    }
  }

  // Play a peer's screen-share audio through its own hidden element so its volume
  // is independent of the mic and it can be deafened. The screen video is rendered
  // by the UI (from peer.screenStream) — this element handles only the audio.
  private attachScreenAudio(peer: Peer, stream: MediaStream): void {
    let el = peer.screenAudioEl
    if (!el) {
      el = new Audio()
      el.autoplay = true
      el.dataset.voiceScreenAudio = String(this.peerId(peer))
      el.style.display = 'none'
      el.muted = this.deafened // a share arriving while deafened stays silent
      document.body.appendChild(el)
      this.applySink(el)
      peer.screenAudioEl = el
    }
    el.srcObject = stream
    el.volume = peer.screenVolume
    void el.play().catch(() => {})
  }

  // Tear down a peer's received screen share (video + audio); used on their
  // voice-screen "off" and when the peer leaves.
  private teardownPeerScreen(peer: Peer): void {
    peer.screenStreamId = null
    peer.screenStream = null
    if (peer.screenAudioEl) {
      peer.screenAudioEl.srcObject = null
      peer.screenAudioEl.remove()
      peer.screenAudioEl = null
    }
  }

  private peerId(peer: Peer): number {
    for (const [id, p] of this.peers) if (p === peer) return id
    return 0
  }

  private dropPeer(id: number): void {
    const peer = this.peers.get(id)
    if (!peer) return
    peer.pc.onconnectionstatechange = null
    peer.pc.onnegotiationneeded = null
    peer.pc.onicecandidate = null
    peer.pc.ontrack = null
    peer.pc.close()
    peer.audioEl.srcObject = null
    peer.audioEl.remove()
    this.teardownPeerScreen(peer)
    this.peers.delete(id)
    this.emitRoster()
  }

  private emitRoster(): void {
    const peers: VoicePeer[] = [...this.peers.entries()].map(([id, p]) => ({
      id,
      username: p.username,
      state: p.state,
      speaking: p.speaking,
      volume: p.volume,
      sharingScreen: p.screenStream != null,
      screenStream: p.screenStream,
      screenVolume: p.screenVolume,
    }))
    this.onRoster(peers)
  }
}
