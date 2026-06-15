// SFU voice transport over LiveKit (the scale path — thousands per call, vs mesh's
// ~4). Used only when the server hands out a token (POST /api/voice/token returns
// sfu:true); otherwise the mesh VoiceSession is used. Same surface as the mesh
// session (VoiceTransport) so Chat.tsx is transport-agnostic.
//
// livekit-client is LAZY-IMPORTED (dynamic import) so it's code-split out of the
// main bundle — a self-hoster running mesh-only never downloads it.
import type { Room, RemoteParticipant, Participant } from 'livekit-client'
import type { VoiceInbound, VoicePeer, VoiceTransport } from './voice'

// What POST /api/voice/token returns when the SFU is configured.
export interface SfuToken {
  url: string
  room: string
  token: string
}

// Our LiveKit participant identity is "u<userID>" (see the token endpoint).
function idFromIdentity(identity: string): number {
  const n = parseInt(identity.replace(/^u/, ''), 10)
  return Number.isFinite(n) ? n : 0
}

export class SfuSession implements VoiceTransport {
  private room: Room | null = null
  private muted = false
  private pttEnabled = false
  private transmitting = false
  private stopped = false
  // Chosen per-peer playback volumes (id → 0..1), re-applied as participants join.
  private volumes = new Map<number, number>()

  constructor(
    private tok: SfuToken,
    private onRoster: (peers: VoicePeer[]) => void,
    private onLocalSpeaking?: (speaking: boolean) => void,
  ) {}

  async start(deviceId?: string): Promise<void> {
    const { Room, RoomEvent } = await import('livekit-client')
    if (this.stopped) return
    const room = new Room({ adaptiveStream: false, dynacast: false })
    this.room = room
    room
      .on(RoomEvent.ParticipantConnected, () => this.emitRoster())
      .on(RoomEvent.ParticipantDisconnected, () => this.emitRoster())
      .on(RoomEvent.ConnectionStateChanged, () => this.emitRoster())
      .on(RoomEvent.TrackSubscribed, (_t, _pub, p: RemoteParticipant) => this.applyVolume(p))
      .on(RoomEvent.ActiveSpeakersChanged, (speakers: Participant[]) => this.onSpeakers(speakers))

    await room.connect(this.tok.url, this.tok.token)
    if (this.stopped) {
      await room.disconnect()
      return
    }
    // Publish the mic (honoring an explicit input device). PTT/mute then gate it.
    await room.localParticipant.setMicrophoneEnabled(true, deviceId ? { deviceId } : undefined)
    await this.applyMicState()
    this.emitRoster()
  }

  stop(): void {
    if (this.stopped) return
    this.stopped = true
    void this.room?.disconnect()
    this.room = null
    this.onRoster([])
  }

  toggleMute(): boolean {
    this.muted = !this.muted
    void this.applyMicState()
    return this.muted
  }

  setPushToTalk(enabled: boolean): void {
    this.pttEnabled = enabled
    this.transmitting = false
    void this.applyMicState()
  }

  setTransmitting(on: boolean): void {
    if (!this.pttEnabled) return
    this.transmitting = on
    void this.applyMicState()
  }

  // The mic is live when: PTT on → only while transmitting; PTT off → unless muted.
  private async applyMicState(): Promise<void> {
    const live = this.pttEnabled ? this.transmitting : !this.muted
    await this.room?.localParticipant.setMicrophoneEnabled(live)
    if (!live) this.onLocalSpeaking?.(false)
  }

  async setInputDevice(deviceId?: string): Promise<void> {
    if (deviceId) await this.room?.switchActiveDevice('audioinput', deviceId)
  }

  setOutputDevice(deviceId: string): void {
    if (deviceId) void this.room?.switchActiveDevice('audiooutput', deviceId)
  }

  setPeerVolume(id: number, volume: number): void {
    const v = Math.min(1, Math.max(0, volume))
    this.volumes.set(id, v)
    const p = this.remoteById(id)
    if (p) p.setVolume(v)
    this.emitRoster()
  }

  // No-op: the SFU has its own signaling; channel-WS voice frames aren't used here.
  handle(_ev: VoiceInbound): void {}

  private remoteById(id: number): RemoteParticipant | undefined {
    if (!this.room) return undefined
    for (const p of this.room.remoteParticipants.values()) {
      if (idFromIdentity(p.identity) === id) return p
    }
    return undefined
  }

  private applyVolume(p: RemoteParticipant): void {
    const v = this.volumes.get(idFromIdentity(p.identity))
    if (v != null) p.setVolume(v)
  }

  private onSpeakers(speakers: Participant[]): void {
    const ids = new Set(speakers.map((s) => s.identity))
    if (this.room) {
      this.onLocalSpeaking?.(ids.has(this.room.localParticipant.identity))
    }
    this.emitRoster(ids)
  }

  private emitRoster(activeIdentities?: Set<string>): void {
    if (!this.room) {
      this.onRoster([])
      return
    }
    const peers: VoicePeer[] = []
    for (const p of this.room.remoteParticipants.values()) {
      const id = idFromIdentity(p.identity)
      peers.push({
        id,
        username: p.name || p.identity,
        state: 'connected',
        speaking: activeIdentities ? activeIdentities.has(p.identity) : p.isSpeaking,
        volume: this.volumes.get(id) ?? 1,
      })
    }
    this.onRoster(peers)
  }
}
