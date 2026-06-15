// SFU voice transport over LiveKit (the scale path — thousands per call, vs mesh's
// ~4). Used only when the server hands out a token (POST /api/voice/token returns
// sfu:true); otherwise the mesh VoiceSession is used. Same surface as the mesh
// session (VoiceTransport) so Chat.tsx is transport-agnostic.
//
// livekit-client is LAZY-IMPORTED (dynamic import) so it's code-split out of the
// main bundle — a self-hoster running mesh-only never downloads it.
import type {
  Room,
  RemoteParticipant,
  Participant,
  RemoteTrack,
  RemoteTrackPublication,
} from 'livekit-client'
import type { VoiceInbound, VoicePeer, VoiceTransport } from './voice'

// What POST /api/voice/token returns when the SFU is configured.
export interface SfuToken {
  url: string
  room: string
  token: string
}

// Cap on how many remote audio streams a client subscribes to at once. A 1000-person
// room can't mix 1000 streams; like Discord/Clubhouse we forward only the loudest few.
// (Most calls are far under this, so it's a no-op until rooms get big.)
export const MAX_AUDIO_SUBSCRIPTIONS = 12

// Choose which remote participants' audio to subscribe to. Small rooms (≤ max) keep
// everyone. At scale, prioritise currently-active speakers, then recently-active ones
// (sticky, so brief pauses don't drop a speaker), then fill any remaining slots
// deterministically so a quiet large room still has *some* audio. Pure + unit-tested.
export function selectAudioSubscriptions(
  all: string[],
  activeNow: string[],
  recent: string[],
  max: number,
): Set<string> {
  if (all.length <= max) return new Set(all)
  const allSet = new Set(all)
  const pick = new Set<string>()
  const add = (id: string) => {
    if (pick.size < max && allSet.has(id)) pick.add(id)
  }
  for (const id of activeNow) add(id)
  for (const id of recent) add(id)
  for (const id of all) add(id)
  return pick
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
  // Attached remote audio elements (id → <audio>), for playback + per-peer volume.
  private audioEls = new Map<number, HTMLAudioElement>()
  // Recently-active speaker identities, most-recent first (sticky top-N selection).
  private recent: string[] = []
  // Currently-speaking identities (from the latest ActiveSpeakersChanged).
  private activeNow = new Set<string>()

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
      .on(RoomEvent.ParticipantConnected, () => {
        this.applySubscriptions()
        this.emitRoster()
      })
      .on(RoomEvent.ParticipantDisconnected, () => {
        this.applySubscriptions()
        this.emitRoster()
      })
      .on(RoomEvent.ConnectionStateChanged, () => this.emitRoster())
      .on(RoomEvent.TrackSubscribed, (track: RemoteTrack, _pub, p: RemoteParticipant) =>
        this.onTrackSubscribed(track, p),
      )
      .on(RoomEvent.TrackUnsubscribed, (track: RemoteTrack, _pub, p: RemoteParticipant) =>
        this.onTrackUnsubscribed(track, p),
      )
      .on(RoomEvent.ActiveSpeakersChanged, (speakers: Participant[]) => this.onSpeakers(speakers))

    // autoSubscribe:false → WE decide which audio to pull, so a huge room never tries
    // to mix every stream (top-N active speakers; see selectAudioSubscriptions).
    await room.connect(this.tok.url, this.tok.token, { autoSubscribe: false })
    if (this.stopped) {
      await room.disconnect()
      return
    }
    this.applySubscriptions()
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
    this.audioEls.forEach((el) => {
      el.srcObject = null
      el.remove()
    })
    this.audioEls.clear()
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
    const el = this.audioEls.get(id)
    if (el) el.volume = v
    this.emitRoster()
  }

  // No-op: the SFU has its own signaling; channel-WS voice frames aren't used here.
  handle(_ev: VoiceInbound): void {}

  // A subscribed remote audio track arrived → attach it for playback (and so the
  // QA can see it), honoring this peer's chosen volume.
  private onTrackSubscribed(track: RemoteTrack, p: RemoteParticipant): void {
    if (track.kind !== 'audio') return
    const id = idFromIdentity(p.identity)
    const el = track.attach() as HTMLAudioElement
    el.dataset.voiceAudio = String(id)
    el.style.display = 'none'
    el.volume = this.volumes.get(id) ?? 1
    document.body.appendChild(el)
    this.audioEls.get(id)?.remove()
    this.audioEls.set(id, el)
  }

  private onTrackUnsubscribed(track: RemoteTrack, p: RemoteParticipant): void {
    if (track.kind !== 'audio') return
    track.detach().forEach((el) => el.remove())
    this.audioEls.delete(idFromIdentity(p.identity))
  }

  // Subscribe to the audio of the top-N participants (selectAudioSubscriptions),
  // unsubscribe from the rest — so a 1000-person room only pulls the loudest few.
  private applySubscriptions(): void {
    if (!this.room) return
    const all = [...this.room.remoteParticipants.values()]
    const want = selectAudioSubscriptions(
      all.map((p) => p.identity),
      [...this.activeNow],
      this.recent,
      MAX_AUDIO_SUBSCRIPTIONS,
    )
    for (const p of all) {
      const sub = want.has(p.identity)
      p.audioTrackPublications.forEach((pub: RemoteTrackPublication) => {
        if (pub.isSubscribed !== sub) pub.setSubscribed(sub)
      })
    }
  }

  private onSpeakers(speakers: Participant[]): void {
    this.activeNow = new Set(speakers.map((s) => s.identity))
    // Sticky recency list: move current speakers to the front (most-recent first).
    for (const s of speakers) {
      this.recent = [s.identity, ...this.recent.filter((id) => id !== s.identity)]
    }
    if (this.room) {
      this.onLocalSpeaking?.(this.activeNow.has(this.room.localParticipant.identity))
    }
    this.applySubscriptions()
    this.emitRoster(this.activeNow)
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
