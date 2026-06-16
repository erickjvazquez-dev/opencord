import { useCallback, useEffect, useRef, useState, type FormEvent } from 'react'
import {
  addReaction,
  createChannel,
  createInvite,
  createServer,
  createServerChannel,
  createChannelCategory,
  deleteChannelCategory,
  fetchChannelCategories,
  deleteMessage,
  editMessage,
  fetchChannels,
  fetchDMs,
  fetchServerChannels,
  fetchPins,
  fetchServerMembers,
  fetchServers,
  openDM,
  redeemInvite,
  removeReaction,
  searchMessages,
  sendAttachments,
  uploadAvatar,
  setChannelPolicy,
  setChannelSlowmode,
  setChannelTopic,
  voiceToken,
  setMessagePinned,
  setServerMemberRole,
  kickServerMember,
  banServerMember,
  unbanServerMember,
  fetchServerBans,
  fetchServerInvites,
  revokeServerInvite,
  timeoutServerMember,
  clearMemberTimeout,
  setMyStatus,
  fetchUnreads,
  markChannelRead,
} from '../api'
import type {
  Channel,
  ChannelCategory,
  DMChannel,
  Invite,
  Message,
  Reaction,
  Server,
  ServerBan,
  ServerEvent,
  ServerMember,
  User,
} from '../types'
import { renderMarkdown } from '../markdown'
import { AttachmentList } from './Attachment'
import { Avatar } from './Avatar'
import { VoiceSession, type VoicePeer, type VoiceTransport } from '../voice'
import { SfuSession } from '../sfu'

// Quick-react palette (Discord-style). Small by design; a full picker is later.
const QUICK_EMOJIS = ['👍', '❤️', '😂', '🎉', '😮', '😢']
// Local key for "the viewer reacted with this emoji on this message".
const rkey = (msgId: number, emoji: string) => `${msgId}:${emoji}`

// Human-readable remaining lifetime for an invite (admin invites list). Absent = a
// legacy never-expire code.
function inviteExpiryLabel(expiresAt?: string): string {
  if (!expiresAt) return 'never expires'
  const ms = new Date(expiresAt).getTime() - Date.now()
  if (ms <= 0) return 'expired'
  const days = Math.floor(ms / 86_400_000)
  if (days > 0) return `expires in ${days}d`
  const hours = Math.floor(ms / 3_600_000)
  if (hours > 0) return `expires in ${hours}h`
  return 'expires soon'
}

// Uses count for an invite row: "N/M uses" when capped, else a plain running count.
function inviteUsesLabel(iv: Invite): string {
  if (iv.maxUses != null) return `${iv.uses}/${iv.maxUses} uses`
  return iv.uses === 1 ? '1 use' : `${iv.uses} uses`
}

// ── Push-to-talk global hotkey ──────────────────────────────────────────────
// The bound key is stored by its physical `KeyboardEvent.code` (layout-independent)
// in localStorage so it survives reloads; Backquote (`) is an unobtrusive default.
const PTT_KEY_STORAGE = 'opencord.pttKey'
const PTT_KEY_DEFAULT = 'Backquote'

function loadPttKey(): string {
  try {
    return localStorage.getItem(PTT_KEY_STORAGE) || PTT_KEY_DEFAULT
  } catch {
    return PTT_KEY_DEFAULT
  }
}

// A short, human-readable label for a KeyboardEvent.code (e.g. 'Backquote' → '`',
// 'KeyV' → 'V', 'Space' → 'Space').
function keyLabel(code: string): string {
  if (code === 'Backquote') return '`'
  if (code === 'Space') return 'Space'
  if (code.startsWith('Key')) return code.slice(3)
  if (code.startsWith('Digit')) return code.slice(5)
  return code
}

// True when the event originated in a text-entry element, so the PTT hotkey stands
// down — otherwise holding it to talk would type into the composer, and a keystroke
// meant for chat would open the mic.
function isEditableTarget(t: EventTarget | null): boolean {
  const el = t as HTMLElement | null
  if (!el) return false
  const tag = el.tagName
  return tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT' || el.isContentEditable
}

// ── @mention autocomplete ───────────────────────────────────────────────────
// If the caret sits inside an @mention being typed, return the partial `query`
// and the index where its `@` starts (so the token can be replaced on accept).
// The `@` must begin the text or follow whitespace, and only username chars
// (letters/digits/_/-) may run from it to the caret.
function activeMention(text: string, caret: number): { query: string; start: number } | null {
  const upto = text.slice(0, Math.max(0, caret))
  const m = /(?:^|\s)@([\w-]*)$/.exec(upto)
  if (!m) return null
  return { query: m[1], start: caret - m[1].length - 1 }
}

// Apply a count delta for one emoji to a message's reaction list (immutably):
// inserts a chip when adding the first, drops it when the last is removed.
function applyDelta(reactions: Reaction[] | undefined, emoji: string, delta: number): Reaction[] {
  const out = [...(reactions ?? [])]
  const i = out.findIndex((r) => r.emoji === emoji)
  if (i >= 0) {
    const count = out[i].count + delta
    if (count <= 0) out.splice(i, 1)
    else out[i] = { ...out[i], count }
  } else if (delta > 0) {
    out.push({ emoji, count: delta })
  }
  return out
}

export function Chat({
  token,
  user,
  onLogout,
}: {
  token: string
  user: User
  onLogout: () => void
}) {
  const [channels, setChannels] = useState<Channel[]>([])
  const [dms, setDms] = useState<DMChannel[]>([])
  const [servers, setServers] = useState<Server[]>([])
  // serverId → its channels (members-only; fetched per server).
  const [serverChannels, setServerChannels] = useState<Record<number, Channel[]>>({})
  // Per-server channel categories (Discord-style collapsible groups), keyed by server id.
  const [serverCategories, setServerCategories] = useState<Record<number, ChannelCategory[]>>({})
  // Category ids the viewer has collapsed in the sidebar (client-only UI state).
  const [collapsedCats, setCollapsedCats] = useState<Set<number>>(new Set())
  const [channelId, setChannelId] = useState<number | null>(null)
  const [messages, setMessages] = useState<Message[]>([])
  // Message id to briefly highlight after a jump (click a reply preview / pin / search hit).
  const [flashId, setFlashId] = useState<number | null>(null)
  // A jump target awaiting the message list to be on-screen (e.g. after closing a panel).
  const pendingJump = useRef<number | null>(null)
  const [online, setOnline] = useState(0)
  const [connected, setConnected] = useState(false)
  const [draft, setDraft] = useState('')
  // Attachments staged in the composer (sent over HTTP multipart, not the WS).
  const [pendingFiles, setPendingFiles] = useState<File[]>([])
  const [uploading, setUploading] = useState(false)
  // Bumped after the viewer uploads their own avatar → re-fetch the header avatar.
  const [avatarVersion, setAvatarVersion] = useState(0)
  const [editingId, setEditingId] = useState<number | null>(null)
  const [editDraft, setEditDraft] = useState('')
  // Reply target: non-null shows the "Replying to …" bar and tags the next send.
  const [replyingTo, setReplyingTo] = useState<Message | null>(null)
  const [typing, setTyping] = useState<string[]>([])
  // Emojis the viewer has reacted with, as "msgId:emoji" keys. The live `reaction`
  // broadcast is count-only (mine=false), so we own this locally; seeded from history.
  const [myReactions, setMyReactions] = useState<Set<string>>(new Set())
  const [pickerFor, setPickerFor] = useState<number | null>(null)
  // Mobile: the sidebar is an off-canvas drawer toggled by the header menu button.
  const [sidebarOpen, setSidebarOpen] = useState(false)
  // In-channel search: `results` non-null means the message list shows matches instead.
  const [searchQuery, setSearchQuery] = useState('')
  const [searchResults, setSearchResults] = useState<Message[] | null>(null)
  // Pins panel: non-null shows the channel's pinned messages.
  const [pins, setPins] = useState<Message[] | null>(null)
  // Server members panel: non-null shows the member list (with the owner's role controls).
  const [membersOf, setMembersOf] = useState<{ serverId: number; members: ServerMember[] } | null>(
    null,
  )
  // Banned users for the server whose members panel is open (admin-only; null until loaded).
  const [bans, setBans] = useState<ServerBan[] | null>(null)
  // Active invites for the server whose members panel is open (admin-only; null until loaded).
  const [serverInvites, setServerInvites] = useState<Invite[] | null>(null)
  // Persistent right-hand member list (Discord-style) for the current server channel.
  const [memberList, setMemberList] = useState<ServerMember[]>([])
  // The caller's own custom status (synced from whichever member list includes them).
  const [myStatus, setMyStatus_] = useState('')
  // Unread channels → unread @-mention count (0 = unread, no mention). Sidebar dots +
  // red mention badges. Synced on load + a ~10s poll.
  const [unread, setUnread] = useState<Map<number, number>>(new Map())
  // Voice call (mesh WebRTC over the channel WS). `inCall` gates the UI; the
  // VoiceSession in voiceRef owns the peer connections and emits the roster.
  const [inCall, setInCall] = useState(false)
  const [muted, setMuted] = useState(false)
  // Deafened: silences all incoming audio and forces your own mic off (Discord-style).
  const [deafened, setDeafened] = useState(false)
  const [voicePeers, setVoicePeers] = useState<VoicePeer[]>([])
  // Whether the local user is currently talking (drives their own speaking ring).
  const [speakingSelf, setSpeakingSelf] = useState(false)
  // Push-to-talk: `pttOn` enables the mode; `transmitting` is true while holding Talk.
  const [pttOn, setPttOn] = useState(false)
  const [transmitting, setTransmitting] = useState(false)
  // Global PTT hotkey: `pttKey` is the bound physical key (held anywhere to talk);
  // `bindingKey` is the rebind-capture mode.
  const [pttKey, setPttKey] = useState(loadPttKey)
  const [bindingKey, setBindingKey] = useState(false)
  // Screen share: `localScreen` is our own capture (a preview tile when we share);
  // `screenSendGain` is how loud our shared audio is sent to viewers (0..4, 1=as-is);
  // `screenMonitor` is our own local monitor of that audio (0..1, 0=off, avoids echo).
  const [localScreen, setLocalScreen] = useState<MediaStream | null>(null)
  const [screenSendGain, setScreenSendGain] = useState(1)
  const [screenMonitor, setScreenMonitor] = useState(0)
  // Screen-share view sizing: tile size (small/medium/large) + how the video fits
  // the tile (contain = letterbox the whole screen, cover = fill+crop). Fullscreen
  // is per-tile via the Fullscreen API. Viewer-local; doesn't affect the sender.
  const [screenSize, setScreenSize] = useState<'sm' | 'md' | 'lg'>('md')
  const [screenFit, setScreenFit] = useState<'contain' | 'cover'>('contain')

  // @mention autocomplete: candidate usernames matching the partial being typed,
  // the highlighted index, the textarea ref (for caret restore), and the range of
  // the `@token` being replaced on accept.
  const [mentionMatches, setMentionMatches] = useState<string[]>([])
  const [mentionIndex, setMentionIndex] = useState(0)
  const composerRef = useRef<HTMLTextAreaElement>(null)
  const mentionRange = useRef<{ start: number; len: number } | null>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)
  const avatarInputRef = useRef<HTMLInputElement>(null)

  const wsRef = useRef<WebSocket | null>(null)
  const voiceRef = useRef<VoiceTransport | null>(null)
  const bottomRef = useRef<HTMLDivElement>(null)
  const typingTimers = useRef<Record<string, ReturnType<typeof setTimeout>>>({})
  const lastTypingSent = useRef(0)

  // Load the user's servers and each server's channels + categories into their maps.
  const refreshServers = async () => {
    try {
      const srvs = await fetchServers(token)
      setServers(srvs)
      const entries = await Promise.all(
        srvs.map(
          async (s) =>
            [
              s.id,
              await fetchServerChannels(token, s.id).catch(() => []),
              await fetchChannelCategories(token, s.id).catch(() => []),
            ] as const,
        ),
      )
      setServerChannels(Object.fromEntries(entries.map(([id, chans]) => [id, chans])))
      setServerCategories(Object.fromEntries(entries.map(([id, , cats]) => [id, cats])))
    } catch {
      /* leave servers as-is on failure */
    }
  }

  useEffect(() => {
    fetchChannels(token)
      .then((cs) => {
        setChannels(cs)
        setChannelId((cur) => cur ?? cs.find((c) => c.name === 'general')?.id ?? cs[0]?.id ?? null)
      })
      .catch(() => {})
    fetchDMs(token).then(setDms).catch(() => {})
    void refreshServers()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [token])

  useEffect(() => {
    if (channelId == null) return
    setMessages([])
    setEditingId(null)
    setReplyingTo(null)
    setTyping([])
    setMyReactions(new Set())
    setPickerFor(null)
    setSearchResults(null)
    setSearchQuery('')
    setMembersOf(null)
    setMentionMatches([])
    setPendingFiles([])
    if (fileInputRef.current) fileInputRef.current.value = ''

    // The channel WebSocket carries chat, presence, AND voice/screen-share signaling.
    // Networks drop (Wi-Fi blips, sleep, a proxy closing an idle socket), so we
    // RECONNECT with capped backoff instead of silently dying — otherwise a dropped
    // socket kills chat and makes a screen share invisible to the other side (its
    // renegotiation can't signal), even though already-connected P2P voice keeps going.
    let stopped = false
    let attempt = 0
    let reconnectTimer: ReturnType<typeof setTimeout> | null = null

    const connect = () => {
      const proto = location.protocol === 'https:' ? 'wss' : 'ws'
      const ws = new WebSocket(
        `${proto}://${location.host}/ws?token=${encodeURIComponent(token)}&channel=${channelId}`,
      )
      wsRef.current = ws
      ws.onopen = () => {
        attempt = 0
        setConnected(true)
      }
      ws.onclose = () => {
        setConnected(false)
        if (stopped) return // intentional close on channel switch / unmount
        const delay = Math.min(1000 * 2 ** attempt, 15000)
        attempt++
        reconnectTimer = setTimeout(connect, delay)
      }
      ws.onmessage = (ev) => {
      const data = JSON.parse(ev.data) as ServerEvent
      if (data.type === 'history' && data.history) {
        const hist = data.history
        setMessages(hist)
        // Seed "mine" from the server's per-viewer flags (history is viewer-scoped).
        const mine = new Set<string>()
        for (const m of hist) for (const r of m.reactions ?? []) if (r.mine) mine.add(rkey(m.id, r.emoji))
        setMyReactions(mine)
      } else if (data.type === 'message' && data.message) {
        const m = data.message
        setMessages((prev) => [...prev, m])
      } else if (data.type === 'message-edited' && data.message) {
        const m = data.message
        // Server's edited message carries no reactions — keep the ones we have.
        setMessages((prev) =>
          prev.map((x) => (x.id === m.id ? { ...x, ...m, reactions: m.reactions ?? x.reactions } : x)),
        )
      } else if (data.type === 'reaction' && data.message) {
        // Count-only broadcast: take the server's authoritative counts; "mine" is
        // overlaid from myReactions at render time.
        const m = data.message
        setMessages((prev) =>
          prev.map((x) => (x.id === m.id ? { ...x, reactions: m.reactions ?? [] } : x)),
        )
      } else if (data.type === 'message-deleted' && data.message) {
        const id = data.message.id
        setMessages((prev) =>
          prev.map((x) => (x.id === id ? { ...x, deleted: true, body: '[deleted]' } : x)),
        )
      } else if (data.type === 'message-pinned' && data.message) {
        const { id, pinned } = data.message
        setMessages((prev) => prev.map((x) => (x.id === id ? { ...x, pinned } : x)))
      } else if (data.type === 'typing' && data.username && data.username !== user.username) {
        const who = data.username
        setTyping((prev) => (prev.includes(who) ? prev : [...prev, who]))
        clearTimeout(typingTimers.current[who])
        typingTimers.current[who] = setTimeout(() => {
          setTyping((prev) => prev.filter((u) => u !== who))
          delete typingTimers.current[who]
        }, 3000)
      } else if (
        data.type === 'voice-join' ||
        data.type === 'voice-leave' ||
        data.type === 'voice-signal' ||
        data.type === 'voice-screen'
      ) {
        // Mesh-voice signaling (incl. screen-share start/stop) → the active call.
        void voiceRef.current?.handle(data)
        } else if (data.type === 'presence') setOnline(data.online ?? 0)
        else if (data.type === 'server-removed' && data.serverId) {
          // We were kicked: drop the server from the sidebar; if we're viewing one of
          // its channels, fall back to #general. (The socket is also being evicted.)
          const removedId = data.serverId
          const wasViewing = (serverChannels[removedId] ?? []).some((c) => c.id === channelId)
          setServers((cur) => cur.filter((s) => s.id !== removedId))
          setServerChannels((cur) => {
            const next = { ...cur }
            delete next[removedId]
            return next
          })
          if (wasViewing) {
            const general = channels.find((c) => c.name === 'general') ?? channels[0]
            if (general) setChannelId(general.id)
          }
          window.alert('You were removed from this server.')
        } else if (data.type === 'error' && data.error) window.alert(data.error)
      }
    }
    connect()

    return () => {
      // Channel switch / unmount: stop reconnecting, then leave any voice call
      // (tell peers, free the mic) before the socket closes so voice-leave goes out.
      stopped = true
      if (reconnectTimer) clearTimeout(reconnectTimer)
      if (voiceRef.current) {
        voiceRef.current.stop()
        voiceRef.current = null
        setInCall(false)
        setVoicePeers([])
        setMuted(false)
        setDeafened(false)
        setSpeakingSelf(false)
        setPttOn(false)
        setTransmitting(false)
        setBindingKey(false)
        setLocalScreen(null)
      }
      wsRef.current?.close()
      Object.values(typingTimers.current).forEach(clearTimeout)
      typingTimers.current = {}
      // Leaving a channel marks it read (server-side) and clears its local unread dot,
      // so the next poll won't re-flag the messages we just saw.
      if (channelId != null) {
        void markChannelRead(token, channelId)
        setUnread((prev) => {
          if (!prev.has(channelId)) return prev
          const next = new Map(prev)
          next.delete(channelId)
          return next
        })
      }
    }
  }, [token, channelId, user.username])

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [messages])

  // doJump scrolls the pending target message into view + briefly flashes it. No-op if the
  // message isn't in the rendered list yet (panel still open, or it's older than the loaded
  // window) — the effect below retries once the list is on screen / messages change.
  const doJump = useCallback(() => {
    const id = pendingJump.current
    if (id == null) return
    const el = document.getElementById(`msg-${id}`)
    if (!el) return
    pendingJump.current = null
    el.scrollIntoView({ behavior: 'smooth', block: 'center' })
    setFlashId(id)
    window.setTimeout(() => setFlashId((cur) => (cur === id ? null : cur)), 1500)
  }, [])

  // Jump to a message: close any open panel so the message list shows, then scroll+flash.
  // requestAnimationFrame covers the inline case (reply preview — list already visible);
  // the effect below covers the close-a-panel-first case (pins / search result).
  const jumpToMessage = (id: number) => {
    pendingJump.current = id
    setSearchResults(null)
    setPins(null)
    setMembersOf(null)
    requestAnimationFrame(doJump)
  }

  // Once a closed panel / new messages put the list on screen, finish a pending jump.
  useEffect(() => {
    if (pendingJump.current != null) doJump()
  }, [searchResults, pins, membersOf, messages, doJump])

  const submitDraft = () => {
    const body = draft.trim()
    // Attachments go over HTTP multipart (files don't fit a 4 KiB WS frame); a
    // plain message goes over the WS. With files, the body is optional.
    if (pendingFiles.length > 0) {
      void sendPending(body)
      return
    }
    if (!body || wsRef.current?.readyState !== WebSocket.OPEN) return
    wsRef.current.send(JSON.stringify({ body, replyTo: replyingTo?.id }))
    setDraft('')
    setReplyingTo(null)
    setMentionMatches([])
  }

  // Upload the staged files (plus the optional body) over HTTP multipart. The server
  // broadcasts the finished message, so it renders via the WS echo like any other —
  // we don't append it locally (avoids a double-render).
  const sendPending = async (body: string) => {
    if (channelId == null || uploading) return
    setUploading(true)
    try {
      await sendAttachments(token, channelId, body, pendingFiles, replyingTo?.id)
      setDraft('')
      setReplyingTo(null)
      setMentionMatches([])
      setPendingFiles([])
      if (fileInputRef.current) fileInputRef.current.value = ''
    } catch (e) {
      window.alert(e instanceof Error ? e.message : 'could not send attachment')
    } finally {
      setUploading(false)
    }
  }

  // Stage files chosen via the 📎 picker (capped at 10, matching the server).
  const onFilesPicked = (list: FileList | null) => {
    if (!list || list.length === 0) return
    setPendingFiles((prev) => [...prev, ...Array.from(list)].slice(0, 10))
  }

  const removePendingFile = (idx: number) => {
    setPendingFiles((prev) => prev.filter((_, i) => i !== idx))
    if (fileInputRef.current) fileInputRef.current.value = ''
  }

  // Upload the viewer's own avatar (header click → picker). On success, bump the
  // version so the header avatar re-fetches the new image (cache-busted).
  const onAvatarPicked = async (file: File | undefined) => {
    if (!file) return
    try {
      await uploadAvatar(token, file)
      setAvatarVersion((v) => v + 1)
    } catch (e) {
      window.alert(e instanceof Error ? e.message : 'could not upload avatar')
    } finally {
      if (avatarInputRef.current) avatarInputRef.current.value = ''
    }
  }

  const send = (e: FormEvent) => {
    e.preventDefault()
    submitDraft()
  }

  // Recompute the @mention suggestions for the current draft + caret. Candidates are
  // the distinct usernames active in this channel (message authors), minus yourself,
  // that start with the partial — no extra fetch, works in every channel type.
  const refreshMentions = (value: string, caret: number) => {
    const active = activeMention(value, caret)
    if (!active) {
      if (mentionMatches.length) setMentionMatches([])
      mentionRange.current = null
      return
    }
    const q = active.query.toLowerCase()
    const seen = new Set<string>()
    const names: string[] = []
    for (const m of messages) {
      const u = m.username
      if (!u || u === user.username || seen.has(u)) continue
      seen.add(u)
      if (u.toLowerCase().startsWith(q)) names.push(u)
    }
    mentionRange.current = { start: active.start, len: active.query.length + 1 }
    setMentionMatches(names.slice(0, 6))
    setMentionIndex(0)
  }

  // Replace the `@partial` under the caret with `@name ` and restore the caret.
  const acceptMention = (name: string) => {
    const range = mentionRange.current
    if (!range) return
    const inserted = `@${name} `
    const before = draft.slice(0, range.start)
    const next = before + inserted + draft.slice(range.start + range.len)
    setDraft(next)
    setMentionMatches([])
    mentionRange.current = null
    const caret = before.length + inserted.length
    requestAnimationFrame(() => {
      const ta = composerRef.current
      if (ta) {
        ta.focus()
        ta.setSelectionRange(caret, caret)
      }
    })
  }

  // ── Voice call (mesh WebRTC) ────────────────────────────────────────────────
  // Available audio devices + the user's pick ('' = follow the OS default, i.e.
  // whatever headset/mic they're currently using). Labels only populate after the
  // mic permission is granted, so we (re)enumerate once in a call.
  const [audioInputs, setAudioInputs] = useState<MediaDeviceInfo[]>([])
  const [audioOutputs, setAudioOutputs] = useState<MediaDeviceInfo[]>([])
  const [inputDevice, setInputDevice] = useState('')
  const [outputDevice, setOutputDevice] = useState('')
  const inputDeviceRef = useRef('')
  inputDeviceRef.current = inputDevice

  const refreshDevices = async () => {
    try {
      const devs = await navigator.mediaDevices.enumerateDevices()
      setAudioInputs(devs.filter((d) => d.kind === 'audioinput'))
      setAudioOutputs(devs.filter((d) => d.kind === 'audiooutput'))
    } catch {
      /* enumeration unsupported — selectors just stay empty */
    }
  }

  const joinVoice = async () => {
    if (voiceRef.current || channelId == null || wsRef.current?.readyState !== WebSocket.OPEN) return
    // Use the SFU when the server offers one (scales to thousands); else mesh.
    const t = await voiceToken(token, channelId).catch(
      (): {
        sfu: boolean
        url?: string
        room?: string
        token?: string
        iceServers?: RTCIceServer[]
      } => ({ sfu: false }),
    )
    const session: VoiceTransport =
      t.sfu && t.url && t.token && t.room
        ? new SfuSession(
            { url: t.url, room: t.room, token: t.token },
            setVoicePeers,
            setSpeakingSelf,
            setLocalScreen,
          )
        : new VoiceSession(
            user.id,
            (frame) => wsRef.current?.send(JSON.stringify(frame)),
            setVoicePeers,
            setSpeakingSelf,
            setLocalScreen,
            t.iceServers,
          )
    voiceRef.current = session
    try {
      await session.start(inputDevice || undefined)
      if (outputDevice) session.setOutputDevice(outputDevice)
      setInCall(true)
      setMuted(false)
      void refreshDevices()
    } catch {
      voiceRef.current = null
      window.alert('Could not access your microphone. Check the browser permission and try again.')
    }
  }

  const leaveVoice = () => {
    voiceRef.current?.stop()
    voiceRef.current = null
    setInCall(false)
    setVoicePeers([])
    setMuted(false)
    setDeafened(false)
    setSpeakingSelf(false)
    setPttOn(false)
    setTransmitting(false)
    setBindingKey(false)
    setLocalScreen(null)
  }

  const toggleMute = () => {
    if (voiceRef.current) setMuted(voiceRef.current.toggleMute())
  }

  // Screen share: start capture (the session prompts the OS picker), or stop. The
  // session reports our own stream via setLocalScreen, which drives the preview +
  // the sharer audio controls. getDisplayMedia rejects if the user cancels.
  const toggleScreenShare = async () => {
    const s = voiceRef.current
    if (!s) return
    if (s.isScreenSharing()) {
      s.stopScreenShare()
      return
    }
    try {
      await s.startScreenShare()
    } catch (err) {
      // Cancelling the picker throws NotAllowed/AbortError — that's not an error
      // worth alerting; surface anything else (e.g. SFU-unsupported).
      const name = (err as DOMException)?.name
      if (name !== 'NotAllowedError' && name !== 'AbortError') {
        window.alert((err as Error)?.message || 'Could not start screen sharing.')
      }
    }
  }
  // SHARER: scale the screen audio sent to all viewers (0..4, 1 = as captured).
  const changeScreenSendGain = (g: number) => {
    setScreenSendGain(g)
    voiceRef.current?.setScreenSendGain(g)
  }
  // SHARER: your own local monitor of the shared audio (0..1, 0 = off).
  const changeScreenMonitor = (v: number) => {
    setScreenMonitor(v)
    voiceRef.current?.setScreenMonitorVolume(v)
  }
  // VIEWER: how loudly you hear a peer's shared audio (0..1).
  const changePeerScreenVolume = (id: number, v: number) => {
    voiceRef.current?.setPeerScreenVolume(id, v)
  }

  // Deafen: silence all incoming audio + force your own mic off. The session also
  // gates the mic, so reflect that locally (deafen implies muted; un-deafen does not
  // auto-unmute — Discord leaves you muted if you were before).
  const toggleDeafen = () => {
    const next = !deafened
    setDeafened(next)
    voiceRef.current?.setDeafened(next)
  }

  // Push-to-talk: toggling the mode resets transmission; holding the Talk control
  // opens the mic, releasing closes it.
  const togglePtt = () => {
    const next = !pttOn
    setPttOn(next)
    setTransmitting(false)
    setBindingKey(false)
    voiceRef.current?.setPushToTalk(next)
  }
  const setTalk = (on: boolean) => {
    setTransmitting(on)
    voiceRef.current?.setTransmitting(on)
  }

  // Global push-to-talk hotkey: while in a call with PTT enabled, holding the bound
  // key opens the mic from anywhere in the app; releasing (or losing window focus)
  // closes it. Suppressed while typing in a field and while rebinding the key.
  useEffect(() => {
    if (!inCall || !pttOn || bindingKey) return
    const open = (on: boolean) => {
      setTransmitting(on)
      voiceRef.current?.setTransmitting(on)
    }
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.code !== pttKey || e.repeat || e.metaKey || e.ctrlKey || e.altKey) return
      if (isEditableTarget(e.target)) return
      e.preventDefault()
      open(true)
    }
    const onKeyUp = (e: KeyboardEvent) => {
      if (e.code !== pttKey) return
      e.preventDefault()
      open(false)
    }
    // Release on blur so the mic can't stay open if you tab away mid-hold.
    const onBlur = () => open(false)
    window.addEventListener('keydown', onKeyDown)
    window.addEventListener('keyup', onKeyUp)
    window.addEventListener('blur', onBlur)
    return () => {
      window.removeEventListener('keydown', onKeyDown)
      window.removeEventListener('keyup', onKeyUp)
      window.removeEventListener('blur', onBlur)
      open(false)
    }
  }, [inCall, pttOn, pttKey, bindingKey])

  // Rebind capture: the next key press becomes the PTT hotkey (Escape cancels).
  useEffect(() => {
    if (!bindingKey) return
    const onKeyDown = (e: KeyboardEvent) => {
      e.preventDefault()
      if (e.code !== 'Escape') {
        setPttKey(e.code)
        try {
          localStorage.setItem(PTT_KEY_STORAGE, e.code)
        } catch {
          /* storage unavailable — the key still applies for this session */
        }
      }
      setBindingKey(false)
    }
    window.addEventListener('keydown', onKeyDown, { capture: true })
    return () => window.removeEventListener('keydown', onKeyDown, { capture: true })
  }, [bindingKey])

  const changeInputDevice = (id: string) => {
    setInputDevice(id)
    void voiceRef.current?.setInputDevice(id || undefined)
  }

  const changeOutputDevice = (id: string) => {
    setOutputDevice(id)
    voiceRef.current?.setOutputDevice(id)
  }

  // Auto-detect device changes (e.g. a gaming headset plugged in): refresh the
  // lists and, when on "Auto", re-acquire so the OS's new default takes over.
  useEffect(() => {
    if (!inCall) return
    const onChange = () => {
      void refreshDevices()
      if (inputDeviceRef.current === '') void voiceRef.current?.setInputDevice(undefined)
    }
    navigator.mediaDevices?.addEventListener?.('devicechange', onChange)
    return () => navigator.mediaDevices?.removeEventListener?.('devicechange', onChange)
  }, [inCall])

  const addChannel = async () => {
    const name = window.prompt('New channel name (2-32 chars: a-z, 0-9, _ or -):')?.trim()
    if (!name) return
    try {
      const c = await createChannel(token, name)
      setChannels((cs) => [...cs, c])
      setChannelId(c.id)
    } catch (err) {
      window.alert(err instanceof Error ? err.message : 'could not create channel')
    }
  }

  const startDM = async () => {
    const ident = window.prompt('Direct message who? Enter a username or user ID')?.trim()
    if (!ident) return
    try {
      const dm = await openDM(token, ident)
      setDms((cur) => (cur.some((d) => d.id === dm.id) ? cur : [...cur, dm]))
      setChannelId(dm.id)
    } catch (err) {
      window.alert(err instanceof Error ? err.message : 'could not open DM')
    }
  }

  const addServer = async () => {
    const name = window.prompt('New server name:')?.trim()
    if (!name) return
    try {
      const srv = await createServer(token, name)
      setServers((cur) => [...cur, srv])
      const chans = await fetchServerChannels(token, srv.id).catch(() => [])
      setServerChannels((cur) => ({ ...cur, [srv.id]: chans }))
    } catch (err) {
      window.alert(err instanceof Error ? err.message : 'could not create server')
    }
  }

  const joinServerPrompt = async () => {
    const code = window.prompt('Join a server — paste an invite code:')?.trim()
    if (!code) return
    try {
      const srv = await redeemInvite(token, code)
      await refreshServers()
      const chans = await fetchServerChannels(token, srv.id).catch(() => [])
      if (chans[0]) setChannelId(chans[0].id)
    } catch (err) {
      window.alert(err instanceof Error ? err.message : 'could not redeem invite')
    }
  }

  const inviteToServer = async (serverId: number) => {
    try {
      const code = await createInvite(token, serverId)
      window.prompt('Invite code (share it so others can join):', code)
    } catch (err) {
      window.alert(err instanceof Error ? err.message : 'could not create invite')
    }
  }

  // Load a server's bans into the panel — admin-only on the server, so swallow a 403
  // (non-admins simply see no bans section).
  const loadBans = async (serverId: number, role?: string) => {
    if (role !== 'owner' && role !== 'admin') {
      setBans(null)
      return
    }
    try {
      setBans(await fetchServerBans(token, serverId))
    } catch {
      setBans(null)
    }
  }

  // Load a server's active invites into the panel — admin-only, so swallow a 403
  // (non-admins simply see no invites section). Mirrors loadBans.
  const loadInvites = async (serverId: number, role?: string) => {
    if (role !== 'owner' && role !== 'admin') {
      setServerInvites(null)
      return
    }
    try {
      setServerInvites(await fetchServerInvites(token, serverId))
    } catch {
      setServerInvites(null)
    }
  }

  const openMembers = async (serverId: number) => {
    try {
      setPins(null)
      const members = await fetchServerMembers(token, serverId)
      setMembersOf({ serverId, members })
      if (String(serverId) === activeServerId) setMemberList(members) // keep the sidebar in sync
      const myRole = members.find((m) => m.userId === user.id)?.role
      void loadBans(serverId, myRole)
      void loadInvites(serverId, myRole)
    } catch (err) {
      window.alert(err instanceof Error ? err.message : 'could not load members')
    }
  }

  // Mint a new invite from the management panel (admin) and refresh the list so it
  // appears immediately. Also surfaces the fresh code so the admin can copy it. Prompts
  // for an optional max-uses cap (blank = unlimited).
  const createPanelInvite = async (serverId: number) => {
    const ans = window.prompt('Max uses for this invite (blank = unlimited):', '')
    if (ans === null) return // cancelled
    let maxUses: number | undefined
    if (ans.trim() !== '') {
      const n = Number(ans.trim())
      if (!Number.isInteger(n) || n < 1 || n > 1000) {
        window.alert('Max uses must be a whole number from 1 to 1000 (or blank for unlimited).')
        return
      }
      maxUses = n
    }
    try {
      const code = await createInvite(token, serverId, maxUses)
      await loadInvites(serverId, 'admin')
      void copyInvite(code)
    } catch (err) {
      window.alert(err instanceof Error ? err.message : 'could not create invite')
    }
  }

  // Revoke an invite code (admin) so it can no longer be redeemed, then refresh the list.
  const revokeInvite = async (serverId: number, code: string) => {
    if (!window.confirm(`Revoke this invite? Anyone who hasn't joined yet can no longer use it.`))
      return
    try {
      await revokeServerInvite(token, serverId, code)
      await loadInvites(serverId, 'admin')
    } catch (err) {
      window.alert(err instanceof Error ? err.message : 'could not revoke invite')
    }
  }

  // Copy an invite code to the clipboard (best-effort; falls back to a prompt the user
  // can copy from if the Clipboard API is unavailable, e.g. a non-secure context).
  const copyInvite = async (code: string) => {
    try {
      await navigator.clipboard.writeText(code)
    } catch {
      window.prompt('Copy this invite code:', code)
    }
  }

  // Pins panel: load and show the channel's pinned messages (mutually exclusive with
  // the search/members panels).
  const openPins = async () => {
    if (channelId == null) return
    try {
      setMembersOf(null)
      setSearchResults(null)
      setSearchQuery('')
      setPins(await fetchPins(token, channelId))
    } catch (err) {
      window.alert(err instanceof Error ? err.message : 'could not load pins')
    }
  }
  const closePins = () => setPins(null)
  const changeRole = async (serverId: number, userId: number, role: string) => {
    try {
      await setServerMemberRole(token, serverId, userId, role)
      const members = await fetchServerMembers(token, serverId)
      setMembersOf({ serverId, members })
      if (String(serverId) === activeServerId) setMemberList(members) // sidebar stays in sync
    } catch (err) {
      window.alert(err instanceof Error ? err.message : 'could not change role')
    }
  }
  const kickMember = async (serverId: number, userId: number, username: string) => {
    if (!window.confirm(`Kick ${username} from this server?`)) return
    try {
      await kickServerMember(token, serverId, userId)
      const members = await fetchServerMembers(token, serverId)
      setMembersOf({ serverId, members })
      if (String(serverId) === activeServerId) setMemberList(members) // sidebar stays in sync
    } catch (err) {
      window.alert(err instanceof Error ? err.message : 'could not kick member')
    }
  }
  // Ban: removes the member AND blocks rejoining until unbanned. Stronger than kick.
  const banMember = async (serverId: number, userId: number, username: string) => {
    if (!window.confirm(`Ban ${username}? They'll be removed and can't rejoin until unbanned.`))
      return
    const reason = window.prompt('Reason (optional):', '') ?? ''
    try {
      await banServerMember(token, serverId, userId, reason)
      const members = await fetchServerMembers(token, serverId)
      setMembersOf({ serverId, members })
      if (String(serverId) === activeServerId) setMemberList(members) // sidebar stays in sync
      void loadBans(serverId, members.find((m) => m.userId === user.id)?.role)
    } catch (err) {
      window.alert(err instanceof Error ? err.message : 'could not ban member')
    }
  }
  const unbanMember = async (serverId: number, userId: number, username: string) => {
    if (!window.confirm(`Unban ${username}? They'll be able to rejoin with an invite.`)) return
    try {
      await unbanServerMember(token, serverId, userId)
      const members = membersOf?.members ?? (await fetchServerMembers(token, serverId))
      void loadBans(serverId, members.find((m) => m.userId === user.id)?.role)
    } catch (err) {
      window.alert(err instanceof Error ? err.message : 'could not unban member')
    }
  }
  // Timeout: temporarily mute a member (prompt for minutes). The server clamps it.
  const timeoutMember = async (serverId: number, userId: number, username: string) => {
    const mins = window.prompt(`Time out ${username} for how many minutes?`, '10')
    if (mins === null) return // cancelled
    const minutes = Number(mins)
    if (!Number.isFinite(minutes) || minutes <= 0) {
      window.alert('Enter a positive number of minutes.')
      return
    }
    try {
      await timeoutServerMember(token, serverId, userId, Math.round(minutes * 60))
      const members = await fetchServerMembers(token, serverId)
      setMembersOf({ serverId, members })
      if (String(serverId) === activeServerId) setMemberList(members)
    } catch (err) {
      window.alert(err instanceof Error ? err.message : 'could not time out member')
    }
  }
  const clearTimeout_ = async (serverId: number, userId: number, username: string) => {
    if (!window.confirm(`Clear ${username}'s timeout?`)) return
    try {
      await clearMemberTimeout(token, serverId, userId)
      const members = await fetchServerMembers(token, serverId)
      setMembersOf({ serverId, members })
      if (String(serverId) === activeServerId) setMemberList(members)
    } catch (err) {
      window.alert(err instanceof Error ? err.message : 'could not clear timeout')
    }
  }
  const editMyStatus = async () => {
    const next = window.prompt('Set your status (leave blank to clear):', myStatus)
    if (next === null) return // cancelled
    const trimmed = next.trim()
    try {
      await setMyStatus(token, trimmed)
      setMyStatus_(trimmed)
      // Refresh the active server's member list so the new status shows immediately.
      if (activeServerId) {
        const members = await fetchServerMembers(token, Number(activeServerId))
        setMemberList(members)
      }
    } catch (err) {
      window.alert(err instanceof Error ? err.message : 'could not set status')
    }
  }

  // Create a channel, optionally inside a category (categoryId).
  const addServerChannel = async (serverId: number, categoryId?: number) => {
    const name = window.prompt('New channel name (2-32 chars: a-z, 0-9, _ or -):')?.trim()
    if (!name) return
    try {
      const c = await createServerChannel(token, serverId, name, categoryId)
      setServerChannels((cur) => ({ ...cur, [serverId]: [...(cur[serverId] ?? []), c] }))
      setChannelId(c.id)
    } catch (err) {
      window.alert(err instanceof Error ? err.message : 'could not create channel')
    }
  }
  // Create a channel category (admin); it shows as a collapsible group in the sidebar.
  const addCategory = async (serverId: number) => {
    const name = window.prompt('New category name (e.g. "Text Channels"):')?.trim()
    if (!name) return
    try {
      const cat = await createChannelCategory(token, serverId, name)
      setServerCategories((cur) => ({ ...cur, [serverId]: [...(cur[serverId] ?? []), cat] }))
    } catch (err) {
      window.alert(err instanceof Error ? err.message : 'could not create category')
    }
  }
  // Delete a category. Its channels survive (server sets their category_id NULL), so we
  // drop the category locally AND clear the matching channels' categoryId so they render
  // as uncategorized without a refetch.
  const removeCategory = async (serverId: number, categoryId: number, name: string) => {
    if (!window.confirm(`Delete the "${name}" category? Its channels become uncategorized.`))
      return
    try {
      await deleteChannelCategory(token, serverId, categoryId)
      setServerCategories((cur) => ({
        ...cur,
        [serverId]: (cur[serverId] ?? []).filter((c) => c.id !== categoryId),
      }))
      setServerChannels((cur) => ({
        ...cur,
        [serverId]: (cur[serverId] ?? []).map((c) =>
          c.categoryId === categoryId ? { ...c, categoryId: undefined } : c,
        ),
      }))
    } catch (err) {
      window.alert(err instanceof Error ? err.message : 'could not delete category')
    }
  }
  const toggleCategory = (categoryId: number) =>
    setCollapsedCats((cur) => {
      const next = new Set(cur)
      if (next.has(categoryId)) next.delete(categoryId)
      else next.add(categoryId)
      return next
    })
  // One server-channel button (reused for uncategorized channels + each category group).
  const channelButton = (c: Channel) => (
    <button
      key={c.id}
      className={`channel-item server-channel${c.id === channelId ? ' active' : ''}${isUnread(c.id) ? ' unread' : ''}`}
      onClick={() => selectChannel(c.id)}
    >
      <span className="hash">#</span>
      {c.name}
      {unreadIndicator(c.id)}
    </button>
  )

  const startEdit = (m: Message) => {
    setEditingId(m.id)
    setEditDraft(m.body)
  }
  const cancelEdit = () => {
    setEditingId(null)
    setEditDraft('')
  }
  const submitEdit = async (id: number) => {
    const body = editDraft.trim()
    if (!body) return
    try {
      await editMessage(token, id, body) // the WS broadcast updates the list
    } catch (err) {
      window.alert(err instanceof Error ? err.message : 'could not edit message')
    } finally {
      cancelEdit()
    }
  }
  const remove = async (m: Message) => {
    if (!window.confirm('Delete this message?')) return
    try {
      await deleteMessage(token, m.id) // the WS broadcast marks it deleted
    } catch (err) {
      window.alert(err instanceof Error ? err.message : 'could not delete message')
    }
  }

  const togglePin = async (m: Message) => {
    try {
      await setMessagePinned(token, m.id, !m.pinned) // the WS broadcast updates the flag
    } catch (err) {
      window.alert(err instanceof Error ? err.message : 'could not change pin')
    }
  }

  // Toggle the viewer's reaction on a message. Optimistically flips local "mine"
  // and nudges the count; the WS `reaction` broadcast reconciles counts. Reverts
  // on HTTP failure.
  const toggleReaction = async (m: Message, emoji: string) => {
    const key = rkey(m.id, emoji)
    const mineNow = myReactions.has(key)
    const delta = mineNow ? -1 : 1
    setMyReactions((prev) => {
      const next = new Set(prev)
      if (mineNow) next.delete(key)
      else next.add(key)
      return next
    })
    setMessages((prev) =>
      prev.map((x) => (x.id === m.id ? { ...x, reactions: applyDelta(x.reactions, emoji, delta) } : x)),
    )
    try {
      if (mineNow) await removeReaction(token, m.id, emoji)
      else await addReaction(token, m.id, emoji)
    } catch (err) {
      setMyReactions((prev) => {
        const next = new Set(prev)
        if (mineNow) next.add(key)
        else next.delete(key)
        return next
      })
      setMessages((prev) =>
        prev.map((x) =>
          x.id === m.id ? { ...x, reactions: applyDelta(x.reactions, emoji, -delta) } : x,
        ),
      )
      window.alert(err instanceof Error ? err.message : 'could not update reaction')
    }
  }

  const current = channels.find((c) => c.id === channelId)
  const activeDM = dms.find((d) => d.id === channelId)
  const activeServerChannel = Object.values(serverChannels)
    .flat()
    .find((c) => c.id === channelId)
  const activeChannelName = current?.name ?? activeServerChannel?.name
  // My role in the server whose member panel is open (owner/admin/member/undefined).
  const myRoleInPanel = membersOf?.members.find((x) => x.userId === user.id)?.role
  const iAmServerOwner = myRoleInPanel === 'owner'
  // Moderation: in a server channel, an owner/admin may delete anyone's message.
  const activeServerId = Object.keys(serverChannels).find((sid) =>
    serverChannels[Number(sid)]?.some((c) => c.id === channelId),
  )
  const myActiveRole = activeServerId
    ? servers.find((s) => s.id === Number(activeServerId))?.role
    : undefined
  const canModerate = myActiveRole === 'owner' || myActiveRole === 'admin'
  const activeIsReadOnly = activeServerChannel?.postPolicy === 'admins'
  // Am I currently timed out (muted) in the active server channel? Derived from the
  // polled member list; the server enforces it regardless, this just reflects it in UI.
  const myTimeoutUntil = memberList.find((m) => m.userId === user.id)?.timeoutUntil
  const iAmTimedOut = !!myTimeoutUntil && new Date(myTimeoutUntil).getTime() > Date.now()
  const canPost = (!activeIsReadOnly || canModerate) && !iAmTimedOut
  // Pinning matches the server's rule: admins in a server channel, any member elsewhere.
  const canPin = !activeServerChannel || canModerate

  // Load the persistent member list when viewing a server channel (Discord shows it
  // for servers, not DMs / the global channel). Polls so a member who joins/leaves or
  // is promoted shows up without a manual refresh (we have no per-member presence
  // event yet — a live member-joined broadcast is a follow-up).
  useEffect(() => {
    if (!activeServerId) {
      setMemberList([])
      return
    }
    let live = true
    const load = () =>
      fetchServerMembers(token, Number(activeServerId))
        .then((m) => live && setMemberList(m))
        .catch(() => {})
    void load()
    const timer = setInterval(load, 15000)
    return () => {
      live = false
      clearInterval(timer)
    }
  }, [activeServerId, token])

  // Keep my own status label in sync from whichever member list includes me.
  useEffect(() => {
    const mine =
      memberList.find((m) => m.userId === user.id) ??
      membersOf?.members.find((m) => m.userId === user.id)
    if (mine && (mine.status ?? '') !== myStatus) setMyStatus_(mine.status ?? '')
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [memberList, membersOf, user.id])

  // Sidebar unread dots: fetch on load + poll. The active channel is filtered out at
  // render time (you're viewing it), and marked read on leave (WS-effect cleanup), so
  // the poll won't re-flag messages you've already seen.
  useEffect(() => {
    let live = true
    const load = () =>
      fetchUnreads(token)
        .then((cs) => live && setUnread(new Map(cs.map((c) => [c.id, c.mentions]))))
        .catch(() => {})
    void load()
    const timer = setInterval(load, 10000)
    return () => {
      live = false
      clearInterval(timer)
    }
  }, [token])

  // Sidebar indicators (never for the channel you're viewing): a channel is unread if in
  // the map; mentionCount > 0 shows the red badge instead of the plain dot.
  const isUnread = (id: number) => id !== channelId && unread.has(id)
  const mentionCount = (id: number) => (id !== channelId ? (unread.get(id) ?? 0) : 0)
  const unreadIndicator = (id: number) => {
    const m = mentionCount(id)
    if (m > 0) return <span className="mention-badge">{m > 9 ? '9+' : m}</span>
    if (isUnread(id)) return <span className="unread-dot" aria-label="unread" />
    return null
  }

  // Pick a channel and (on mobile) close the drawer so the chat is visible.
  const selectChannel = (id: number) => {
    setChannelId(id)
    setSidebarOpen(false)
    setPins(null) // a panel from the previous channel shouldn't linger
  }

  const runSearch = async (e: FormEvent) => {
    e.preventDefault()
    const q = searchQuery.trim()
    if (!q || channelId == null) return
    try {
      setPins(null)
      setSearchResults(await searchMessages(token, channelId, q))
    } catch (err) {
      window.alert(err instanceof Error ? err.message : 'could not search')
    }
  }
  const clearSearch = () => {
    setSearchResults(null)
    setSearchQuery('')
  }

  // Admin toggle: flip the active server channel between open and read-only ('admins').
  const toggleReadOnly = async () => {
    if (!activeServerChannel || activeServerId == null) return
    const sid = Number(activeServerId)
    const next = activeServerChannel.postPolicy === 'admins' ? 'everyone' : 'admins'
    try {
      await setChannelPolicy(token, activeServerChannel.id, next)
      setServerChannels((cur) => ({
        ...cur,
        [sid]: (cur[sid] ?? []).map((c) =>
          c.id === activeServerChannel.id ? { ...c, postPolicy: next } : c,
        ),
      }))
    } catch (err) {
      window.alert(err instanceof Error ? err.message : 'could not change channel policy')
    }
  }

  // Admin edit: set the active server channel's topic (header description).
  const editTopic = async () => {
    if (!activeServerChannel || activeServerId == null) return
    const sid = Number(activeServerId)
    const next = window.prompt('Channel topic:', activeServerChannel.topic ?? '')
    if (next === null) return // cancelled
    try {
      await setChannelTopic(token, activeServerChannel.id, next)
      setServerChannels((cur) => ({
        ...cur,
        [sid]: (cur[sid] ?? []).map((c) =>
          c.id === activeServerChannel.id ? { ...c, topic: next } : c,
        ),
      }))
    } catch (err) {
      window.alert(err instanceof Error ? err.message : 'could not set topic')
    }
  }

  // Admin sets the channel's per-message cooldown for non-admins (0 = off).
  const editSlowmode = async () => {
    if (!activeServerChannel || activeServerId == null) return
    const sid = Number(activeServerId)
    const cur = activeServerChannel.slowmodeSeconds ?? 0
    const answer = window.prompt('Slowmode — seconds between messages (0 = off, max 21600):', String(cur))
    if (answer === null) return // cancelled
    const seconds = Math.floor(Number(answer))
    if (!Number.isFinite(seconds) || seconds < 0 || seconds > 21600) {
      window.alert('Enter a number from 0 to 21600.')
      return
    }
    try {
      await setChannelSlowmode(token, activeServerChannel.id, seconds)
      setServerChannels((c) => ({
        ...c,
        [sid]: (c[sid] ?? []).map((ch) =>
          ch.id === activeServerChannel.id ? { ...ch, slowmodeSeconds: seconds } : ch,
        ),
      }))
    } catch (err) {
      window.alert(err instanceof Error ? err.message : 'could not set slowmode')
    }
  }

  return (
    <div className={sidebarOpen ? 'app sidebar-open' : 'app'}>
      {sidebarOpen && (
        <div className="sidebar-backdrop" onClick={() => setSidebarOpen(false)} aria-hidden />
      )}
      <aside className="sidebar">
        <div className="sidebar-head">Channels</div>
        <nav className="channel-list">
          {channels.map((c) => (
            <button
              key={c.id}
              className={`channel-item${c.id === channelId ? ' active' : ''}${isUnread(c.id) ? ' unread' : ''}`}
              onClick={() => selectChannel(c.id)}
            >
              <span className="hash">#</span>
              {c.name}
              {unreadIndicator(c.id)}
            </button>
          ))}
        </nav>
        <button className="add-channel" onClick={addChannel}>
          + New channel
        </button>

        <div className="sidebar-head">Direct Messages</div>
        <nav className="channel-list dm-list">
          {dms.map((d) => (
            <button
              key={d.id}
              className={`channel-item${d.id === channelId ? ' active' : ''}${isUnread(d.id) ? ' unread' : ''}`}
              onClick={() => selectChannel(d.id)}
            >
              <Avatar token={token} userId={d.user.id} username={d.user.username} className="dm-avatar" />
              {d.user.username}
              {unreadIndicator(d.id)}
            </button>
          ))}
        </nav>
        <button className="add-channel" onClick={startDM}>
          + New DM
        </button>

        <div className="sidebar-head">Servers</div>
        <nav className="channel-list server-list">
          {servers.map((s) => (
            <div key={s.id} className="server-group">
              <div className="server-name">
                {s.name} <span className="server-id">#{s.id}</span>
              </div>
              {/* Uncategorized channels render first (today's behaviour). */}
              {(serverChannels[s.id] ?? [])
                .filter((c) => c.categoryId == null)
                .map(channelButton)}
              {/* Then each category as a collapsible group with its channels nested. */}
              {(serverCategories[s.id] ?? []).map((cat) => {
                const collapsed = collapsedCats.has(cat.id)
                const chans = (serverChannels[s.id] ?? []).filter((c) => c.categoryId === cat.id)
                return (
                  <div key={cat.id} className="channel-category">
                    <div className="category-head">
                      <button
                        className="category-toggle"
                        onClick={() => toggleCategory(cat.id)}
                        title={collapsed ? 'expand' : 'collapse'}
                      >
                        <span className="category-caret">{collapsed ? '▸' : '▾'}</span>
                        {cat.name}
                      </button>
                      <button
                        className="category-add"
                        title="add a channel in this category"
                        onClick={() => void addServerChannel(s.id, cat.id)}
                      >
                        +
                      </button>
                      <button
                        className="category-del"
                        title="delete this category"
                        aria-label={`delete the ${cat.name} category`}
                        onClick={() => void removeCategory(s.id, cat.id, cat.name)}
                      >
                        ✕
                      </button>
                    </div>
                    {!collapsed && chans.map(channelButton)}
                  </div>
                )
              })}
              <div className="server-group-actions">
                <button className="server-add-channel" onClick={() => void addServerChannel(s.id)}>
                  + channel
                </button>
                <button className="server-add-channel" onClick={() => void addCategory(s.id)}>
                  + category
                </button>
                <button className="server-add-channel" onClick={() => void inviteToServer(s.id)}>
                  invite
                </button>
                <button className="server-add-channel" onClick={() => void openMembers(s.id)}>
                  members
                </button>
              </div>
            </div>
          ))}
        </nav>
        <div className="server-actions">
          <button className="add-channel" onClick={addServer}>
            + New server
          </button>
          <button className="add-channel" onClick={joinServerPrompt}>
            Join server
          </button>
        </div>
      </aside>

      <div className="chat">
        <header className="chat-header">
          <button
            className="menu-toggle"
            aria-label="menu"
            onClick={() => setSidebarOpen((o) => !o)}
          >
            ☰
          </button>
          <div className="brand">
            Opencord{' '}
            <span className="channel">
              {activeDM ? `@${activeDM.user.username}` : `#${activeChannelName ?? '…'}`}
            </span>
            {activeIsReadOnly && (
              <span className="readonly-badge" title="read-only — only admins can post">
                🔒 read-only
              </span>
            )}
            {activeServerChannel?.topic && (
              <span className="channel-topic" title={activeServerChannel.topic}>
                {activeServerChannel.topic}
              </span>
            )}
            {(activeServerChannel?.slowmodeSeconds ?? 0) > 0 && (
              <span
                className="slowmode-badge"
                title={`slowmode — ${activeServerChannel?.slowmodeSeconds}s between messages`}
              >
                🐌 {activeServerChannel?.slowmodeSeconds}s
              </span>
            )}
          </div>
          {activeServerChannel && canModerate && (
            <button className="link readonly-toggle" onClick={() => void toggleReadOnly()}>
              {activeIsReadOnly ? 'allow everyone' : 'make read-only'}
            </button>
          )}
          {activeServerChannel && canModerate && (
            <button className="link slowmode-edit" onClick={() => void editSlowmode()}>
              slowmode
            </button>
          )}
          {activeServerChannel && canModerate && (
            <button className="link topic-edit" onClick={() => void editTopic()}>
              edit topic
            </button>
          )}
          {channelId != null && (
            <button className="link pins-open" onClick={() => void openPins()}>
              pins
            </button>
          )}
          {channelId != null && !inCall && (
            <button
              className="link voice-join"
              onClick={() => void joinVoice()}
              disabled={!connected}
              title={connected ? 'Start a voice call in this channel' : 'Connecting…'}
            >
              🎙 Join voice
            </button>
          )}
          <form className="search-form" onSubmit={runSearch}>
            <input
              className="search-input"
              placeholder="Search… (try from:user has:link)"
              title="Filters: from:<user>, has:link, has:image, has:file"
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
            />
          </form>
          <div className="meta">
            <span className={connected ? 'dot online' : 'dot offline'} />
            {online} online ·
            <input
              ref={avatarInputRef}
              type="file"
              accept="image/png,image/jpeg,image/gif,image/webp"
              className="file-input-hidden"
              onChange={(e) => void onAvatarPicked(e.target.files?.[0])}
            />
            <button
              type="button"
              className="self-avatar-btn"
              title="Change your avatar"
              aria-label="change your avatar"
              onClick={() => avatarInputRef.current?.click()}
            >
              <Avatar
                token={token}
                userId={user.id}
                username={user.username}
                className="avatar avatar-self"
                bust={avatarVersion}
              />
              <span>{user.username}</span>
            </button>
            <button
              className="link status-edit"
              onClick={() => void editMyStatus()}
              title="Set your status"
            >
              {myStatus ? `“${myStatus}”` : 'set status'}
            </button>
            <button className="link" onClick={onLogout}>
              log out
            </button>
          </div>
        </header>

        {inCall && (
          <div className="voice-bar" role="region" aria-label="voice call">
            <span className="voice-bar-title">🔊 In voice</span>
            <span
              className={`voice-chip you${speakingSelf ? ' speaking' : ''}`}
              data-voice-self
              data-speaking={speakingSelf}
            >
              {user.username} (you){deafened ? ' · deafened' : muted ? ' · muted' : ''}
            </span>
            {voicePeers.map((p) => (
              <span
                key={p.id}
                className={`voice-chip${p.speaking ? ' speaking' : ''}`}
                data-voice-peer
                data-state={p.state}
                data-speaking={p.speaking}
              >
                <span className={`dot voice-${p.state}`} aria-hidden />
                {p.username || `user ${p.id}`}
                <input
                  className="voice-volume"
                  type="range"
                  min={0}
                  max={100}
                  value={Math.round(p.volume * 100)}
                  onChange={(e) => voiceRef.current?.setPeerVolume(p.id, Number(e.target.value) / 100)}
                  data-volume-for={p.id}
                  aria-label={`volume for ${p.username || `user ${p.id}`}`}
                  title={`Volume: ${Math.round(p.volume * 100)}%`}
                />
              </span>
            ))}
            {voicePeers.length === 0 && <span className="voice-empty">waiting for others…</span>}
            <span className="voice-spacer" />
            <label className="voice-device" title="Microphone — Auto follows your system default">
              🎙
              <select
                value={inputDevice}
                onChange={(e) => changeInputDevice(e.target.value)}
                aria-label="microphone"
              >
                <option value="">Auto (system default)</option>
                {audioInputs.map((d, i) => (
                  <option key={d.deviceId} value={d.deviceId}>
                    {d.label || `Microphone ${i + 1}`}
                  </option>
                ))}
              </select>
            </label>
            {audioOutputs.length > 0 && (
              <label className="voice-device" title="Output — where you hear others">
                🎧
                <select
                  value={outputDevice}
                  onChange={(e) => changeOutputDevice(e.target.value)}
                  aria-label="audio output"
                >
                  <option value="">Auto (system default)</option>
                  {audioOutputs.map((d, i) => (
                    <option key={d.deviceId} value={d.deviceId}>
                      {d.label || `Output ${i + 1}`}
                    </option>
                  ))}
                </select>
              </label>
            )}
            <button
              className={`link voice-ptt-toggle${pttOn ? ' on' : ''}`}
              onClick={togglePtt}
              title="Push-to-talk: mic is live only while you hold Talk"
              data-ptt={pttOn}
            >
              {pttOn ? 'PTT on' : 'PTT'}
            </button>
            {pttOn ? (
              <>
                <button
                  className={`voice-talk${transmitting ? ' talking' : ''}`}
                  onPointerDown={() => setTalk(true)}
                  onPointerUp={() => setTalk(false)}
                  onPointerLeave={() => setTalk(false)}
                  data-transmitting={transmitting}
                  title={`Hold to talk, or hold ${keyLabel(pttKey)} anywhere`}
                  aria-label="hold to talk"
                >
                  {transmitting ? '🎙 Talking…' : '🎙 Hold to talk'}
                </button>
                <button
                  type="button"
                  className={`link voice-ptt-key${bindingKey ? ' binding' : ''}`}
                  onClick={() => setBindingKey((b) => !b)}
                  title="Rebind the push-to-talk hotkey (hold it anywhere to talk)"
                  data-ptt-key={pttKey}
                  data-binding={bindingKey}
                >
                  {bindingKey ? 'press a key…' : `key: ${keyLabel(pttKey)}`}
                </button>
              </>
            ) : (
              <button className="link voice-mute" onClick={toggleMute}>
                {muted ? 'unmute' : 'mute'}
              </button>
            )}
            <button
              className={`link voice-deafen${deafened ? ' on' : ''}`}
              onClick={toggleDeafen}
              title="Deafen: silence everyone (also mutes your mic)"
              data-deafened={deafened}
            >
              {deafened ? 'undeafen' : 'deafen'}
            </button>
            <button
              className={`link voice-screen-toggle${localScreen ? ' on' : ''}`}
              onClick={() => void toggleScreenShare()}
              title="Share your screen (up to 4K/60 — with system audio if you allow it)"
              data-sharing={localScreen != null}
            >
              {localScreen ? 'stop sharing' : '🖥 share screen'}
            </button>
            <button className="link voice-leave" onClick={leaveVoice}>
              leave
            </button>
          </div>
        )}

        {inCall && (localScreen || voicePeers.some((p) => p.sharingScreen)) && (
          <div className="screen-stage" role="region" aria-label="screen shares" data-size={screenSize}>
            <div className="screen-stage-toolbar">
              <span className="screen-stage-label">Screens</span>
              <span className="screen-sizes" role="group" aria-label="screen size">
                {(['sm', 'md', 'lg'] as const).map((s) => (
                  <button
                    key={s}
                    type="button"
                    className={`link screen-size-btn${screenSize === s ? ' on' : ''}`}
                    onClick={() => setScreenSize(s)}
                    data-screen-size={s}
                    aria-pressed={screenSize === s}
                  >
                    {s === 'sm' ? 'small' : s === 'md' ? 'medium' : 'large'}
                  </button>
                ))}
              </span>
              <button
                type="button"
                className="link screen-fit-btn"
                onClick={() => setScreenFit((f) => (f === 'contain' ? 'cover' : 'contain'))}
                data-screen-fit={screenFit}
                title="Fit shows the whole screen (letterboxed); Fill crops to fill the tile"
              >
                {screenFit === 'contain' ? 'fit' : 'fill'}
              </button>
            </div>
            {localScreen && (
              <div className="screen-tile" data-screen-self>
                <video
                  className="screen-video"
                  autoPlay
                  muted
                  playsInline
                  style={{ objectFit: screenFit }}
                  ref={(el) => {
                    if (el && el.srcObject !== localScreen) el.srcObject = localScreen
                  }}
                />
                <div className="screen-tile-bar">
                  <span className="screen-tile-name">You are sharing</span>
                  <button
                    type="button"
                    className="link screen-fullscreen"
                    onClick={(e) =>
                      void e.currentTarget.closest('.screen-tile')?.querySelector('video')?.requestFullscreen?.()
                    }
                    title="Fullscreen"
                    aria-label="fullscreen this screen"
                  >
                    ⛶
                  </button>
                  <label className="screen-level" title="Audio level sent to viewers">
                    out
                    <input
                      type="range"
                      min={0}
                      max={200}
                      value={Math.round(screenSendGain * 100)}
                      onChange={(e) => changeScreenSendGain(Number(e.target.value) / 100)}
                      data-screen-send-gain
                      aria-label="shared audio level sent to viewers"
                    />
                  </label>
                  <label className="screen-level" title="Your own monitor of the shared audio (off by default)">
                    monitor
                    <input
                      type="range"
                      min={0}
                      max={100}
                      value={Math.round(screenMonitor * 100)}
                      onChange={(e) => changeScreenMonitor(Number(e.target.value) / 100)}
                      data-screen-monitor
                      aria-label="your local monitor of the shared audio"
                    />
                  </label>
                </div>
              </div>
            )}
            {voicePeers
              .filter((p) => p.sharingScreen && p.screenStream)
              .map((p) => (
                <div key={p.id} className="screen-tile" data-screen-peer={p.id}>
                  <video
                    className="screen-video"
                    autoPlay
                    muted
                    playsInline
                    style={{ objectFit: screenFit }}
                    ref={(el) => {
                      if (el && el.srcObject !== p.screenStream) el.srcObject = p.screenStream
                    }}
                  />
                  <div className="screen-tile-bar">
                    <span className="screen-tile-name">{p.username || `user ${p.id}`}’s screen</span>
                    <label className="screen-level" title="How loudly you hear this share's audio">
                      🔉
                      <input
                        type="range"
                        min={0}
                        max={100}
                        value={Math.round(p.screenVolume * 100)}
                        onChange={(e) => changePeerScreenVolume(p.id, Number(e.target.value) / 100)}
                        data-screen-volume-for={p.id}
                        aria-label={`shared audio volume for ${p.username || `user ${p.id}`}`}
                      />
                    </label>
                    <button
                      type="button"
                      className="link screen-fullscreen"
                      onClick={(e) =>
                        void e.currentTarget.closest('.screen-tile')?.querySelector('video')?.requestFullscreen?.()
                      }
                      title="Fullscreen"
                      aria-label={`fullscreen ${p.username || `user ${p.id}`}'s screen`}
                    >
                      ⛶
                    </button>
                  </div>
                </div>
              ))}
          </div>
        )}

        <main className="messages">
          {membersOf !== null && (
            <div className="search-results">
              <div className="search-results-head">
                <span>Members ({membersOf.members.length})</span>
                <button
                  className="link"
                  onClick={() => {
                    setMembersOf(null)
                    setBans(null)
                    setServerInvites(null)
                  }}
                >
                  ✕ close
                </button>
              </div>
              {membersOf.members.map((mb) => (
                <div key={mb.userId} className={`member-row${mb.online ? '' : ' offline'}`}>
                  <span className="avatar-presence">
                    <Avatar token={token} userId={mb.userId} username={mb.username} />
                    <span
                      className={`presence-dot ${mb.online ? 'online' : 'offline'}`}
                      title={mb.online ? 'online' : 'offline'}
                    />
                  </span>
                  <span className="member-id">
                    <span className="author">{mb.username}</span>
                    {mb.status && (
                      <span className="member-status" title={mb.status}>
                        {mb.status}
                      </span>
                    )}
                  </span>
                  <span className={`role-badge role-${mb.role}`}>{mb.role}</span>
                  {mb.timeoutUntil && new Date(mb.timeoutUntil).getTime() > Date.now() && (
                    <span
                      className="role-badge role-muted"
                      title={`muted until ${new Date(mb.timeoutUntil).toLocaleString()}`}
                    >
                      ⏳ muted
                    </span>
                  )}
                  {iAmServerOwner && mb.role !== 'owner' && (
                    <button
                      className="link role-toggle"
                      onClick={() =>
                        void changeRole(
                          membersOf.serverId,
                          mb.userId,
                          mb.role === 'admin' ? 'member' : 'admin',
                        )
                      }
                    >
                      {mb.role === 'admin' ? 'demote' : 'make admin'}
                    </button>
                  )}
                  {/* Kick: owner may remove any non-owner; an admin may remove plain
                      members only. The server enforces this regardless of the UI. */}
                  {mb.userId !== user.id &&
                    mb.role !== 'owner' &&
                    (myRoleInPanel === 'owner' ||
                      (myRoleInPanel === 'admin' && mb.role === 'member')) && (
                      <>
                        <button
                          className="link kick-btn"
                          onClick={() =>
                            void kickMember(membersOf.serverId, mb.userId, mb.username)
                          }
                        >
                          kick
                        </button>
                        {/* Ban: same authz as kick, but also blocks rejoining. */}
                        <button
                          className="link ban-btn"
                          onClick={() => void banMember(membersOf.serverId, mb.userId, mb.username)}
                        >
                          ban
                        </button>
                        {/* Timeout: temporary mute. Toggles to "unmute" while active. */}
                        {mb.timeoutUntil && new Date(mb.timeoutUntil).getTime() > Date.now() ? (
                          <button
                            className="link timeout-btn"
                            onClick={() =>
                              void clearTimeout_(membersOf.serverId, mb.userId, mb.username)
                            }
                          >
                            unmute
                          </button>
                        ) : (
                          <button
                            className="link timeout-btn"
                            onClick={() =>
                              void timeoutMember(membersOf.serverId, mb.userId, mb.username)
                            }
                          >
                            timeout
                          </button>
                        )}
                      </>
                    )}
                </div>
              ))}
              {/* Active invites (admin view): mint a new code or revoke a leaked one.
                  Dedicated classes throughout (NOT .bans-head / .member-row / .member-id):
                  those are QA + behavioural selectors elsewhere and an invite row must
                  never masquerade as a member/ban row. */}
              {serverInvites !== null && (
                <>
                  <div className="invites-head">
                    <span>Invites ({serverInvites.length})</span>
                    <button
                      className="link new-invite-btn"
                      onClick={() => void createPanelInvite(membersOf.serverId)}
                    >
                      + New invite
                    </button>
                  </div>
                  {serverInvites.length === 0 && (
                    <div className="invites-empty">
                      No active invites — create one to let people join.
                    </div>
                  )}
                  {serverInvites.map((iv) => (
                    <div key={iv.code} className="invite-row">
                      <code className="invite-code" title="invite code">
                        {iv.code}
                      </code>
                      <span className="invite-info">
                        <span className="invite-meta">
                          {inviteExpiryLabel(iv.expiresAt)} · {inviteUsesLabel(iv)}
                        </span>
                        <span className="invite-by" title={`created by ${iv.creatorName}`}>
                          by {iv.creatorName}
                        </span>
                      </span>
                      <button className="link copy-invite-btn" onClick={() => void copyInvite(iv.code)}>
                        copy
                      </button>
                      <button
                        className="link revoke-invite-btn"
                        onClick={() => void revokeInvite(membersOf.serverId, iv.code)}
                      >
                        revoke
                      </button>
                    </div>
                  ))}
                </>
              )}
              {/* Banned users (admin view): unban restores their ability to rejoin. */}
              {bans !== null && bans.length > 0 && (
                <>
                  <div className="bans-head">Banned ({bans.length})</div>
                  {bans.map((b) => (
                    <div key={b.userId} className="member-row banned-row">
                      <span className="avatar-presence">
                        <Avatar token={token} userId={b.userId} username={b.username} />
                      </span>
                      <span className="member-id">
                        <span className="author">{b.username}</span>
                        {b.reason && (
                          <span className="member-status" title={b.reason}>
                            {b.reason}
                          </span>
                        )}
                      </span>
                      <span className="role-badge role-banned">banned</span>
                      <button
                        className="link unban-btn"
                        onClick={() => void unbanMember(membersOf.serverId, b.userId, b.username)}
                      >
                        unban
                      </button>
                    </div>
                  ))}
                </>
              )}
            </div>
          )}
          {membersOf === null && searchResults !== null && (
            <div className="search-results">
              <div className="search-results-head">
                <span>
                  {searchResults.length} result{searchResults.length === 1 ? '' : 's'} for “
                  {searchQuery}”
                </span>
                <button className="link" onClick={clearSearch}>
                  ✕ clear
                </button>
              </div>
              {searchResults.length === 0 && <div className="search-empty">No matches.</div>}
              {searchResults.map((m) => (
                <div
                  key={m.id}
                  className="message jumpable"
                  title="jump to this message"
                  onClick={() => jumpToMessage(m.id)}
                >
                  <Avatar token={token} userId={m.userId} username={m.username} />
                  <div className="message-content">
                    <div className="message-head">
                      <span className="author">{m.username}</span>
                      <span className="time">{new Date(m.createdAt).toLocaleTimeString()}</span>
                    </div>
                    <div className="body">{m.deleted ? m.body : renderMarkdown(m.body, { me: user.username })}</div>
                  </div>
                </div>
              ))}
            </div>
          )}
          {membersOf === null && searchResults === null && pins !== null && (
            <div className="search-results">
              <div className="search-results-head">
                <span>
                  📌 {pins.length} pinned message{pins.length === 1 ? '' : 's'}
                </span>
                <button className="link" onClick={closePins}>
                  ✕ close
                </button>
              </div>
              {pins.length === 0 && <div className="search-empty">No pinned messages yet.</div>}
              {pins.map((m) => (
                <div
                  key={m.id}
                  className="message jumpable"
                  title="jump to this message"
                  onClick={() => jumpToMessage(m.id)}
                >
                  <Avatar token={token} userId={m.userId} username={m.username} />
                  <div className="message-content">
                    <div className="message-head">
                      <span className="author">{m.username}</span>
                      <span className="time">{new Date(m.createdAt).toLocaleTimeString()}</span>
                    </div>
                    <div className="body">
                      {m.deleted ? m.body : renderMarkdown(m.body, { me: user.username })}
                    </div>
                  </div>
                </div>
              ))}
            </div>
          )}
          {membersOf === null &&
            searchResults === null &&
            pins === null &&
            messages.map((m, i) => {
            const prev = i > 0 ? messages[i - 1] : null
            // Group consecutive messages from the same author within 5 min (Discord-style):
            // hide the repeated avatar + name. A deleted message breaks the run.
            const grouped =
              !!prev &&
              !m.deleted &&
              !prev.deleted &&
              prev.userId === m.userId &&
              new Date(m.createdAt).getTime() - new Date(prev.createdAt).getTime() < 5 * 60 * 1000
            return (
              <div
                key={m.id}
                id={`msg-${m.id}`}
                className={`message${m.deleted ? ' deleted' : ''}${grouped ? ' grouped' : ''}${m.id === flashId ? ' flash' : ''}`}
              >
                {grouped ? (
                  <div className="avatar-spacer" aria-hidden />
                ) : (
                  <Avatar token={token} userId={m.userId} username={m.username} />
                )}
                <div className="message-content">
                  {!grouped && (
                    <div className="message-head">
                      <span className="author">{m.username}</span>
                      <span className="time">{new Date(m.createdAt).toLocaleTimeString()}</span>
                      {m.editedAt && !m.deleted && <span className="edited">(edited)</span>}
                    </div>
                  )}
                  {m.pinned && !m.deleted && (
                    <span className="pin-badge" title="pinned message">📌 pinned</span>
                  )}
                  {!m.deleted && editingId !== m.id && (
                    <span className="msg-actions">
                      <button onClick={() => setReplyingTo(m)}>reply</button>
                      {m.userId === user.id && (
                        <button onClick={() => startEdit(m)}>edit</button>
                      )}
                      {(m.userId === user.id || canModerate) && (
                        <button onClick={() => remove(m)}>delete</button>
                      )}
                      {canPin && (
                        <button onClick={() => void togglePin(m)}>{m.pinned ? 'unpin' : 'pin'}</button>
                      )}
                      <button onClick={() => setPickerFor((p) => (p === m.id ? null : m.id))}>
                        react
                      </button>
                    </span>
                  )}
                  {m.replyTo && !m.deleted && (
                    <button
                      type="button"
                      className="reply-context"
                      aria-label={`jump to ${m.replyToAuthor}'s message`}
                      title="jump to the replied-to message"
                      onClick={() => jumpToMessage(m.replyTo as number)}
                    >
                      <span className="reply-arrow" aria-hidden>
                        ↰
                      </span>
                      <span className="reply-author">{m.replyToAuthor}</span>
                      <span className="reply-snippet">{m.replyToBody}</span>
                    </button>
                  )}
                  {editingId === m.id ? (
                    <div className="edit-row">
                      <input
                        autoFocus
                        value={editDraft}
                        onChange={(e) => setEditDraft(e.target.value)}
                        onKeyDown={(e) => {
                          if (e.key === 'Enter') void submitEdit(m.id)
                          else if (e.key === 'Escape') cancelEdit()
                        }}
                      />
                      <button onClick={() => void submitEdit(m.id)}>save</button>
                      <button className="link" onClick={cancelEdit}>
                        cancel
                      </button>
                    </div>
                  ) : (
                    <div className="body">{m.deleted ? m.body : renderMarkdown(m.body, { me: user.username })}</div>
                  )}
                  {!m.deleted && (m.attachments?.length ?? 0) > 0 && (
                    <AttachmentList token={token} attachments={m.attachments!} />
                  )}
                  {pickerFor === m.id && !m.deleted && (
                    <div className="emoji-picker">
                      {QUICK_EMOJIS.map((e) => (
                        <button
                          key={e}
                          className="emoji-option"
                          onClick={() => {
                            void toggleReaction(m, e)
                            setPickerFor(null)
                          }}
                        >
                          {e}
                        </button>
                      ))}
                    </div>
                  )}
                  {!m.deleted && (m.reactions?.length ?? 0) > 0 && (
                    <div className="reactions">
                      {m.reactions!.map((r) => {
                        const mine = myReactions.has(rkey(m.id, r.emoji))
                        return (
                          <button
                            key={r.emoji}
                            className={mine ? 'reaction mine' : 'reaction'}
                            aria-pressed={mine}
                            onClick={() => void toggleReaction(m, r.emoji)}
                          >
                            <span className="emoji">{r.emoji}</span>
                            <span className="rcount">{r.count}</span>
                          </button>
                        )
                      })}
                    </div>
                  )}
                </div>
              </div>
            )
          })}
          <div ref={bottomRef} />
        </main>

        {typing.length > 0 && (
          <div className="typing">
            {typing.join(', ')} {typing.length === 1 ? 'is' : 'are'} typing…
          </div>
        )}
        {replyingTo && (
          <div className="reply-bar">
            <span className="reply-bar-text">
              Replying to <strong>{replyingTo.username}</strong>
            </span>
            <button
              type="button"
              className="reply-cancel"
              aria-label="cancel reply"
              onClick={() => setReplyingTo(null)}
            >
              ✕
            </button>
          </div>
        )}
        {mentionMatches.length > 0 && (
          <div className="mention-autocomplete" role="listbox" aria-label="mention suggestions">
            {mentionMatches.map((name, i) => (
              <button
                type="button"
                key={name}
                role="option"
                aria-selected={i === mentionIndex}
                className={`mention-option${i === mentionIndex ? ' active' : ''}`}
                data-mention-option={name}
                // mousedown (not click) so the textarea doesn't blur before we insert.
                onMouseDown={(e) => {
                  e.preventDefault()
                  acceptMention(name)
                }}
              >
                @{name}
              </button>
            ))}
          </div>
        )}
        {pendingFiles.length > 0 && (
          <div className="pending-files" aria-label="files to send">
            {pendingFiles.map((f, i) => (
              <span className="pending-file" key={`${f.name}-${f.size}-${i}`}>
                <span className="pending-file-name">{f.name}</span>
                <button
                  type="button"
                  className="pending-file-remove"
                  aria-label={`remove ${f.name}`}
                  onClick={() => removePendingFile(i)}
                >
                  ✕
                </button>
              </span>
            ))}
          </div>
        )}
        <form className="composer" onSubmit={send}>
          <input
            ref={fileInputRef}
            type="file"
            multiple
            className="file-input-hidden"
            onChange={(e) => onFilesPicked(e.target.files)}
          />
          <button
            type="button"
            className="attach-btn"
            aria-label="attach files"
            title="Attach files"
            disabled={!connected || !canPost || uploading}
            onClick={() => fileInputRef.current?.click()}
          >
            📎
          </button>
          <textarea
            ref={composerRef}
            className="composer-input"
            rows={1}
            placeholder={
              !connected
                ? 'connecting…'
                : iAmTimedOut
                  ? `you're timed out until ${new Date(myTimeoutUntil as string).toLocaleTimeString()}`
                  : !canPost
                    ? 'read-only — only admins can post'
                    : activeDM
                      ? `Message @${activeDM.user.username}`
                      : `Message #${activeChannelName ?? ''}`
            }
            value={draft}
            onChange={(e) => {
              setDraft(e.target.value)
              refreshMentions(e.target.value, e.target.selectionStart ?? e.target.value.length)
              // Auto-grow with the content, bounded by CSS max-height.
              e.target.style.height = 'auto'
              e.target.style.height = `${e.target.scrollHeight}px`
              const now = Date.now()
              if (wsRef.current?.readyState === WebSocket.OPEN && now - lastTypingSent.current > 2000) {
                lastTypingSent.current = now
                wsRef.current.send(JSON.stringify({ type: 'typing' }))
              }
            }}
            onKeyDown={(e) => {
              // When the @mention dropdown is open, it owns the arrows/Enter/Tab/Esc.
              if (mentionMatches.length > 0) {
                if (e.key === 'ArrowDown') {
                  e.preventDefault()
                  setMentionIndex((i) => (i + 1) % mentionMatches.length)
                  return
                }
                if (e.key === 'ArrowUp') {
                  e.preventDefault()
                  setMentionIndex((i) => (i - 1 + mentionMatches.length) % mentionMatches.length)
                  return
                }
                if (e.key === 'Enter' || e.key === 'Tab') {
                  e.preventDefault()
                  acceptMention(mentionMatches[mentionIndex])
                  return
                }
                if (e.key === 'Escape') {
                  e.preventDefault()
                  setMentionMatches([])
                  return
                }
              }
              // Enter sends; Shift+Enter inserts a newline (Discord convention).
              if (e.key === 'Enter' && !e.shiftKey) {
                e.preventDefault()
                submitDraft()
                e.currentTarget.style.height = 'auto'
              }
            }}
            disabled={!connected || !canPost}
          />
          <button
            disabled={!connected || !canPost || uploading || (!draft.trim() && pendingFiles.length === 0)}
          >
            {uploading ? 'Sending…' : 'Send'}
          </button>
        </form>
      </div>

      {activeServerId && memberList.length > 0 && (
        <aside className="member-list" aria-label="server members">
          {(
            [
              ['Admins', memberList.filter((m) => m.role === 'owner' || m.role === 'admin')],
              ['Members', memberList.filter((m) => m.role === 'member')],
            ] as const
          )
            .filter(([, group]) => group.length > 0)
            .map(([label, group]) => (
              <div key={label} className="member-group">
                <div className="member-group-head">
                  {label} — {group.length}
                </div>
                {group.map((mb) => (
                  <div
                    key={mb.userId}
                    className={`member-list-row${mb.online ? '' : ' offline'}`}
                    data-member={mb.userId}
                    data-online={mb.online ? '1' : '0'}
                  >
                    <span className="avatar-presence">
                      <Avatar token={token} userId={mb.userId} username={mb.username} />
                      <span
                        className={`presence-dot ${mb.online ? 'online' : 'offline'}`}
                        title={mb.online ? 'online' : 'offline'}
                      />
                    </span>
                    <span className="member-id">
                      <span className="author">{mb.username}</span>
                      {mb.status && (
                        <span className="member-status" title={mb.status}>
                          {mb.status}
                        </span>
                      )}
                    </span>
                    {mb.role !== 'member' && (
                      <span className={`role-badge role-${mb.role}`}>{mb.role}</span>
                    )}
                  </div>
                ))}
              </div>
            ))}
        </aside>
      )}
    </div>
  )
}
