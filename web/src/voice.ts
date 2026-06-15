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

// A remote participant as shown in the roster.
export interface VoicePeer {
  id: number
  username: string
  state: PeerState
}

// Public STUN only (Rule A: no required paid service). On loopback/LAN, host
// candidates connect without it; TURN for hostile NATs is a later slice.
const RTC_CONFIG: RTCConfiguration = {
  iceServers: [{ urls: 'stun:stun.l.google.com:19302' }],
}

// Target outbound Opus bitrate. ~96 kbps mono is comfortably above Discord's
// default (~64 kbps) for crisper voice while staying cheap on a mesh.
const VOICE_BITRATE = 96_000

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
}

export class VoiceSession {
  private peers = new Map<number, Peer>()
  private localStream: MediaStream | null = null
  private muted = false
  private stopped = false
  // Chosen devices (undefined/'' = follow the OS default). Output is applied to
  // every peer's <audio> sink; input is the constraint for capture.
  private inputDeviceId: string | undefined
  private outputDeviceId = ''

  constructor(
    private myId: number,
    private send: (frame: VoiceFrame) => void,
    private onRoster: (peers: VoicePeer[]) => void,
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
    this.send({ type: 'voice-join' })
  }

  // Switch the capture device live: re-acquire and hot-swap the track on every
  // peer (no renegotiation). Pass undefined to follow the OS default again. Used
  // both for manual selection and to auto-follow a freshly plugged-in headset.
  async setInputDevice(deviceId?: string): Promise<void> {
    if (this.stopped) return
    const next = await navigator.mediaDevices.getUserMedia(audioConstraints(deviceId))
    const track = next.getAudioTracks()[0]
    if (!track) return
    track.enabled = !this.muted
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
  }

  // Route remote audio to a chosen output device (headphones/speakers). '' (or a
  // browser without setSinkId) leaves it on the OS default, which auto-follows.
  setOutputDevice(deviceId: string): void {
    this.outputDeviceId = deviceId
    for (const peer of this.peers.values()) this.applySink(peer.audioEl)
  }

  currentInputDevice(): string | undefined {
    return this.inputDeviceId
  }

  // Leave the call: tell the channel, tear down every peer and release the mic.
  stop(): void {
    if (this.stopped) return
    this.stopped = true
    this.send({ type: 'voice-leave' })
    for (const id of [...this.peers.keys()]) this.dropPeer(id)
    this.localStream?.getTracks().forEach((t) => t.stop())
    this.localStream = null
    this.emitRoster()
  }

  // Toggle the local mic by enabling/disabling the audio track (keeps the peer
  // connections up). Returns the new muted state.
  toggleMute(): boolean {
    this.muted = !this.muted
    this.localStream?.getAudioTracks().forEach((t) => (t.enabled = !this.muted))
    return this.muted
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
      peer.audioEl.srcObject = streams[0] ?? null
      void peer.audioEl.play().catch(() => {})
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
    }))
    this.onRoster(peers)
  }
}
