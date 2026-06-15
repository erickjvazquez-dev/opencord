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

// Frames we send up the channel WS. The server stamps `from` and rebroadcasts.
export type VoiceFrame =
  | { type: 'voice-join' }
  | { type: 'voice-leave' }
  | { type: 'voice-signal'; target: number; signal: unknown }

// An inbound voice frame as relayed to the channel (server-stamped with `from`).
export interface VoiceInbound {
  type: string
  from?: number
  username?: string
  target?: number
  signal?: unknown
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
}

// Public STUN only (Rule A: no required paid service). On loopback/LAN, host
// candidates connect without it; TURN for hostile NATs is a later slice.
const RTC_CONFIG: RTCConfiguration = {
  iceServers: [{ urls: 'stun:stun.l.google.com:19302' }],
}

// Target outbound Opus bitrate. ~96 kbps mono is comfortably above Discord's
// default (~64 kbps) for crisper voice while staying cheap on a mesh.
const VOICE_BITRATE = 96_000

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
// actually talking into); an explicit id pins a chosen device.
function audioConstraints(deviceId?: string): MediaStreamConstraints {
  return {
    audio: {
      ...(deviceId ? { deviceId: { exact: deviceId } } : {}),
      echoCancellation: true,
      noiseSuppression: true,
      autoGainControl: true,
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

  constructor(
    private myId: number,
    private send: (frame: VoiceFrame) => void,
    private onRoster: (peers: VoicePeer[]) => void,
    // Reports the local participant's speaking state (optional; the roster carries
    // remote peers' speaking state).
    private onLocalSpeaking?: (speaking: boolean) => void,
  ) {}

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
      const sender = peer.pc.getSenders().find((s) => s.track?.kind === 'audio')
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
    this.peers.forEach((p) => (p.audioEl.muted = on))
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

  // Feed a server-stamped voice-* frame relayed on the channel WS.
  async handle(ev: VoiceInbound): Promise<void> {
    if (this.stopped || ev.from == null || ev.from === this.myId) return
    if (ev.type === 'voice-join') {
      // A participant appeared. Open a peer; perfect negotiation drives the
      // offer/answer from whichever side fires negotiationneeded first.
      this.ensurePeer(ev.from, ev.username ?? '')
    } else if (ev.type === 'voice-leave') {
      this.dropPeer(ev.from)
    } else if (ev.type === 'voice-signal' && ev.target === this.myId) {
      await this.onSignal(ev.from, ev.username ?? '', ev.signal as SignalPayload | undefined)
    }
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

    const pc = new RTCPeerConnection(RTC_CONFIG)
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
    }
    this.peers.set(id, peer)
    this.applySink(audioEl)

    // Send our mic to this peer. Adding a track schedules negotiationneeded.
    this.localStream?.getTracks().forEach((t) => {
      const sender = pc.addTrack(t, this.localStream!)
      void this.tuneSender(sender)
    })

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
    pc.ontrack = ({ streams }) => {
      const stream = streams[0] ?? null
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

  // Raise the sender's max bitrate for crisper voice. encodings may be empty
  // before negotiation, so seed one; best-effort (older browsers may reject).
  private async tuneSender(sender: RTCRtpSender): Promise<void> {
    try {
      const params = sender.getParameters()
      if (!params.encodings || params.encodings.length === 0) params.encodings = [{}]
      params.encodings[0].maxBitrate = VOICE_BITRATE
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
    }))
    this.onRoster(peers)
  }
}
