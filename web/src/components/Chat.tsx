import { Fragment, useCallback, useEffect, useRef, useState, type FormEvent } from 'react'
import {
  addReaction,
  createChannel,
  createInvite,
  createServer,
  renameServer,
  deleteServer,
  leaveServer,
  createServerChannel,
  createChannelCategory,
  deleteChannelCategory,
  fetchChannelCategories,
  deleteMessage,
  editMessage,
  fetchChannels,
  fetchDMs,
  fetchServerChannels,
  listServerEmoji,
  uploadServerEmoji,
  deleteServerEmoji,
  fetchPins,
  fetchThreads,
  createThread,
  fetchServerMembers,
  fetchServers,
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
  listServerRoles,
  assignServerRole,
  unassignServerRole,
  transferServerOwnership,
  kickServerMember,
  banServerMember,
  unbanServerMember,
  fetchServerBans,
  fetchServerInvites,
  revokeServerInvite,
  timeoutServerMember,
  clearMemberTimeout,
  setMyStatus,
  setMyProfile,
  setMyPresence,
  fetchUnreads,
  markChannelRead,
  fetchMutedChannels,
  setChannelMuted,
  fetchUserProfile,
  blockUser,
  unblockUser,
  listBlocked,
} from '../api'
import type {
  Channel,
  ChannelCategory,
  DMChannel,
  Invite,
  Message,
  Reaction,
  Role,
  Server,
  ServerBan,
  ServerEmoji,
  ServerEvent,
  ServerMember,
  User,
} from '../types'
import { renderMarkdown } from '../markdown'
import { visibleMessages } from '../blocking'
import { getDesktopNotify, mentionsMe, shouldNotify, showNotification } from '../notify'
import { dayLabel, shortTime, messageTimestamp } from '../dates'
import { AttachmentList } from './Attachment'
import { Avatar } from './Avatar'
import { EmojiImg } from './EmojiImg'
import { Settings } from './Settings'
import { ProfileCard } from './ProfileCard'
import { NewGroupModal } from './NewGroupModal'
import { RolesManagerModal } from './RolesManagerModal'
import { dmTitle, dmIsGroup, dmOthers } from '../dm'
import * as voiceSettings from '../voiceSettings'
import { VoiceSession, type VoicePeer, type VoiceTransport } from '../voice'
import { SfuSession } from '../sfu'

// A short, single-line body for a desktop notification (the OS truncates anyway, but
// we cap + collapse newlines so the preview stays tidy). Empty body → a generic line
// (e.g. an attachment-only message).
function notifySnippet(body: string): string {
  const oneLine = (body ?? '').replace(/\s+/g, ' ').trim()
  if (!oneLine) return 'Sent a message'
  return oneLine.length > 120 ? oneLine.slice(0, 119) + '…' : oneLine
}

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
  const [newDMOpen, setNewDMOpen] = useState(false)
  // Custom colored roles (v0.7): the active server's roles + which server the manager is open for.
  const [serverRoles, setServerRoles] = useState<Role[]>([])
  const [rolesManagerFor, setRolesManagerFor] = useState<number | null>(null)
  const [servers, setServers] = useState<Server[]>([])
  // serverId → its channels (members-only; fetched per server).
  const [serverChannels, setServerChannels] = useState<Record<number, Channel[]>>({})
  // Per-server channel categories (Discord-style collapsible groups), keyed by server id.
  const [serverCategories, setServerCategories] = useState<Record<number, ChannelCategory[]>>({})
  // Per-server custom-emoji maps (emoji name → id), keyed by server id and cached so we
  // don't refetch on every channel switch within the same server. `:name:` only renders
  // as an image when the name is in the ACTIVE server's map; #general and DMs have none.
  const [serverEmoji, setServerEmoji] = useState<Record<number, Map<string, number>>>({})
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
  // Composer custom-emoji picker: open state for the popover above the textarea that
  // lists the active server's custom emoji and inserts `:name:` at the caret on click.
  // Separate from the per-message reaction palette (`pickerFor`) above.
  const [emojiPickerOpen, setEmojiPickerOpen] = useState(false)
  // Mobile: the sidebar is an off-canvas drawer toggled by the header menu button.
  const [sidebarOpen, setSidebarOpen] = useState(false)
  // In-channel search: `results` non-null means the message list shows matches instead.
  const [searchQuery, setSearchQuery] = useState('')
  const [searchResults, setSearchResults] = useState<Message[] | null>(null)
  // Pins panel: non-null shows the channel's pinned messages.
  const [pins, setPins] = useState<Message[] | null>(null)
  // Threads (v0.8): `threads` non-null shows the channel's thread panel; `activeThread` is the
  // thread currently being viewed (its name/back-link, since threads aren't in any channel list).
  const [threads, setThreads] = useState<Channel[] | null>(null)
  const [activeThread, setActiveThread] = useState<Channel | null>(null)
  // Server members panel: non-null shows the member list (with the owner's role controls).
  const [membersOf, setMembersOf] = useState<{ serverId: number; members: ServerMember[] } | null>(
    null,
  )
  // Banned users for the server whose members panel is open (admin-only; null until loaded).
  const [bans, setBans] = useState<ServerBan[] | null>(null)
  // Active invites for the server whose members panel is open (admin-only; null until loaded).
  const [serverInvites, setServerInvites] = useState<Invite[] | null>(null)
  // Custom emoji for the server whose members panel is open (admin-only; null until
  // loaded). Drives the Emoji manager section: list + upload + delete.
  const [emojiManager, setEmojiManager] = useState<ServerEmoji[] | null>(null)
  // Upload form state for the Emoji manager: the chosen name, the picked file, a busy
  // flag, and the last error to surface (e.g. 409 name taken / 413 too big).
  const [emojiName, setEmojiName] = useState('')
  const [emojiFile, setEmojiFile] = useState<File | null>(null)
  const [emojiUploading, setEmojiUploading] = useState(false)
  const [emojiError, setEmojiError] = useState('')
  // Persistent right-hand member list (Discord-style) for the current server channel.
  const [memberList, setMemberList] = useState<ServerMember[]>([])
  // The caller's own custom status (synced from whichever member list includes them).
  const [myStatus, setMyStatus_] = useState('')
  // The caller's own status emoji (synced alongside myStatus).
  const [myStatusEmoji, setMyStatusEmoji_] = useState('')
  // The caller's own profile (About Me + pronouns), synced from their member row.
  const [myAbout, setMyAbout_] = useState('')
  const [myPronouns, setMyPronouns_] = useState('')
  // The member whose profile card is open (clicked in the member list), or null.
  const [profileMember, setProfileMember] = useState<ServerMember | null>(null)
  // User ids I've blocked: their messages are hidden from every channel (filtered at
  // render time, so live WS messages from a blocked user never appear either). Managed
  // from the profile card (Block/Unblock) and the Settings → Privacy list. Fetched on load.
  const [blocked, setBlocked] = useState<Set<number>>(new Set())
  // The caller's own chosen presence (online|idle|dnd|invisible), synced from their
  // own member-list row (which reports the true self state).
  const [myPresence, setMyPresence_] = useState('online')
  // Auto-idle: refs the inactivity timer reads/writes without re-subscribing. `auto`
  // tracks whether the CURRENT idle was set by us (so activity restores online) vs a
  // manual idle (which we must not override).
  const myPresenceRef = useRef(myPresence)
  myPresenceRef.current = myPresence
  const autoIdledRef = useRef(false)
  // User Settings overlay (⚙ in the header). Houses the account controls — avatar,
  // custom status + emoji, presence — that used to live scattered in the header.
  const [settingsOpen, setSettingsOpen] = useState(false)
  // Unread channels → unread @-mention count (0 = unread, no mention). Sidebar dots +
  // red mention badges. Synced on load + a ~10s poll.
  const [unread, setUnread] = useState<Map<number, number>>(new Map())
  // Channels the caller has muted: excluded from unread/mention/tab badges (server-side),
  // dimmed in the sidebar, and reflected by the header mute toggle.
  const [mutedChannels, setMutedChannels] = useState<Set<number>>(new Set())
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
  // Whether our local shared video is the screen or the camera (drives label + mirror).
  const [localVideoKind, setLocalVideoKind] = useState<'screen' | 'camera'>('screen')
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
  // Wraps the composer emoji-picker button + popover so an outside click can close it.
  const emojiPickerRef = useRef<HTMLDivElement>(null)

  const wsRef = useRef<WebSocket | null>(null)
  const voiceRef = useRef<VoiceTransport | null>(null)
  const bottomRef = useRef<HTMLDivElement>(null)
  const typingTimers = useRef<Record<string, ReturnType<typeof setTimeout>>>({})
  const lastTypingSent = useRef(0)
  // Whether the active channel is a DM — kept fresh for the WS message handler, whose
  // effect doesn't re-subscribe on `dms` changes (so it can't read `activeDM` directly
  // without going stale). Drives the desktop-notification rule (DMs always notify).
  const activeIsDMRef = useRef(false)

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
    listBlocked(token)
      .then((us) => setBlocked(new Set(us.map((u) => u.id))))
      .catch(() => {})
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
    setEmojiPickerOpen(false)
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
        // Desktop notification (off by default; opt-in via Settings → Notifications).
        // Discord's low-noise rule: only when the tab is hidden and the message is a
        // DM or @-mentions you, and never for your own messages. Guard the API so an
        // unsupported browser is a clean no-op (Rule A).
        if (typeof Notification !== 'undefined') {
          const isMine = m.userId === user.id
          const isDM = activeIsDMRef.current
          const mentioned = mentionsMe(m.body, user.username)
          if (
            shouldNotify({
              enabled: getDesktopNotify(),
              permission: Notification.permission,
              hidden: document.hidden,
              isMine,
              isDM,
              mentionsMe: mentioned,
            })
          ) {
            showNotification(`${m.username}${isDM ? '' : ' (mention)'}`, notifySnippet(m.body))
          }
        }
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
        } else if (data.type === 'server-renamed' && data.serverId && data.name) {
          // The server was renamed by its owner/admin — relabel it live in the sidebar.
          const { serverId: renamedId, name: newName } = data
          setServers((cur) => cur.map((s) => (s.id === renamedId ? { ...s, name: newName } : s)))
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

  // Insert a `:name:` custom-emoji shortcode into the draft at the caret (or append
  // with a leading space when the textarea isn't focused), then refocus and close the
  // picker. Mirrors acceptMention's caret-restore so it composes with the auto-resize.
  const insertEmojiShortcode = (name: string) => {
    const code = `:${name}:`
    const ta = composerRef.current
    setDraft((cur) => {
      // Splice at the live caret when we have one; otherwise append (space-separated).
      if (ta && document.activeElement === ta) {
        const start = ta.selectionStart ?? cur.length
        const end = ta.selectionEnd ?? cur.length
        const before = cur.slice(0, start)
        const next = before + code + cur.slice(end)
        const caret = before.length + code.length
        requestAnimationFrame(() => {
          const el = composerRef.current
          if (el) {
            el.focus()
            el.setSelectionRange(caret, caret)
            // Keep the auto-grow height in sync with the new content.
            el.style.height = 'auto'
            el.style.height = `${el.scrollHeight}px`
          }
        })
        return next
      }
      const next = cur.length === 0 || cur.endsWith(' ') ? cur + code : `${cur} ${code}`
      requestAnimationFrame(() => {
        const el = composerRef.current
        if (el) {
          el.focus()
          el.setSelectionRange(next.length, next.length)
          el.style.height = 'auto'
          el.style.height = `${el.scrollHeight}px`
        }
      })
      return next
    })
    setEmojiPickerOpen(false)
  }

  // Close the composer emoji picker on an outside click or Esc (mirrors the modal
  // dismiss pattern used by ProfileCard / Settings). Only bound while it's open.
  useEffect(() => {
    if (!emojiPickerOpen) return
    const onDown = (e: MouseEvent) => {
      if (emojiPickerRef.current && !emojiPickerRef.current.contains(e.target as Node)) {
        setEmojiPickerOpen(false)
      }
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setEmojiPickerOpen(false)
    }
    document.addEventListener('mousedown', onDown)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onDown)
      document.removeEventListener('keydown', onKey)
    }
  }, [emojiPickerOpen])

  // ── Voice call (mesh WebRTC) ────────────────────────────────────────────────
  // Available audio devices + the user's pick ('' = follow the OS default, i.e.
  // whatever headset/mic they're currently using). Labels only populate after the
  // mic permission is granted, so we (re)enumerate once in a call.
  const [audioInputs, setAudioInputs] = useState<MediaDeviceInfo[]>([])
  const [audioOutputs, setAudioOutputs] = useState<MediaDeviceInfo[]>([])
  // Seed from the persisted Voice & Video prefs so the in-call picker AND the settings
  // tab start on the user's last chosen device (single localStorage source of truth).
  const [inputDevice, setInputDevice] = useState(() => voiceSettings.getInputDeviceId())
  const [outputDevice, setOutputDevice] = useState(() => voiceSettings.getOutputDeviceId())
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
    // Our own shared-video preview: track the stream + whether it's screen or camera.
    const onLocalVideo = (stream: MediaStream | null, kind?: 'screen' | 'camera') => {
      setLocalScreen(stream)
      if (kind) setLocalVideoKind(kind)
    }
    const session: VoiceTransport =
      t.sfu && t.url && t.token && t.room
        ? new SfuSession(
            { url: t.url, room: t.room, token: t.token },
            setVoicePeers,
            setSpeakingSelf,
            onLocalVideo,
          )
        : new VoiceSession(
            user.id,
            (frame) => wsRef.current?.send(JSON.stringify(frame)),
            setVoicePeers,
            setSpeakingSelf,
            onLocalVideo,
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
  // the sharer audio controls. getDisplayMedia rejects if the user cancels. Screen
  // and camera share one mesh video slot, so starting screen stops the camera first.
  const toggleScreenShare = async () => {
    const s = voiceRef.current
    if (!s) return
    if (s.currentVideoKind() === 'screen') {
      s.stopScreenShare()
      return
    }
    try {
      if (s.currentVideoKind() === 'camera') s.stopScreenShare() // free the shared video slot
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

  // Camera: turn the camera on/off in the call. Mutually exclusive with screen share
  // (one mesh video slot) — starting the camera stops a running screen share first.
  const toggleCamera = async () => {
    const s = voiceRef.current
    if (!s) return
    if (s.currentVideoKind() === 'camera') {
      s.stopScreenShare() // stops the shared video (the camera)
      return
    }
    try {
      if (s.currentVideoKind() === 'screen') s.stopScreenShare() // free the slot
      await s.startCamera()
    } catch (err) {
      const name = (err as DOMException)?.name
      if (name !== 'NotAllowedError' && name !== 'AbortError') {
        window.alert((err as Error)?.message || 'Could not start the camera.')
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
    voiceSettings.setInputDeviceId(id)
    void voiceRef.current?.setInputDevice(id || undefined)
  }

  const changeOutputDevice = (id: string) => {
    setOutputDevice(id)
    voiceSettings.setOutputDeviceId(id)
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

  // A DM/group created via NewGroupModal: add it to the list (de-duped) and open it.
  const addDM = (dm: DMChannel) => {
    setDms((cur) => (cur.some((d) => d.id === dm.id) ? cur : [...cur, dm]))
    setChannelId(dm.id)
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

  // Load a server's custom emoji into the manager — admin-only, so swallow a 403
  // (non-admins simply see no emoji section). Mirrors loadInvites/loadBans.
  const loadEmoji = async (serverId: number, role?: string) => {
    if (role !== 'owner' && role !== 'admin') {
      setEmojiManager(null)
      return
    }
    try {
      setEmojiManager(await listServerEmoji(token, serverId))
    } catch {
      setEmojiManager(null)
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
      void loadEmoji(serverId, myRole)
    } catch (err) {
      window.alert(err instanceof Error ? err.message : 'could not load members')
    }
  }

  // Refresh the cached per-server `:name:` → id map from the live emoji list so a
  // subsequently-sent (or re-rendered) shortcode resolves to an image WITHOUT a page
  // reload — the slice-3a UX win. Called after every successful upload/delete.
  const refreshEmojiCache = (serverId: number, list: ServerEmoji[]) => {
    const map = new Map(list.map((e) => [e.name, e.id]))
    setServerEmoji((cur) => ({ ...cur, [serverId]: map }))
  }

  // Upload a new emoji from the manager (admin). On success: refresh the manager list,
  // invalidate the per-server cache (live `:name:` rendering), and clear the form. On
  // error: surface the server's message (409 name taken / 400 invalid / 413 too big).
  const uploadEmojiFromManager = async (serverId: number) => {
    setEmojiError('')
    const name = emojiName.trim().toLowerCase()
    if (!/^[a-z0-9_]{2,32}$/.test(name)) {
      setEmojiError('Name must be 2–32 chars: lowercase letters, numbers, or underscores.')
      return
    }
    if (!emojiFile) {
      setEmojiError('Pick an image file to upload.')
      return
    }
    setEmojiUploading(true)
    try {
      await uploadServerEmoji(token, serverId, name, emojiFile)
      const list = await listServerEmoji(token, serverId)
      setEmojiManager(list)
      refreshEmojiCache(serverId, list)
      setEmojiName('')
      setEmojiFile(null)
    } catch (err) {
      setEmojiError(err instanceof Error ? err.message : 'could not upload emoji')
    } finally {
      setEmojiUploading(false)
    }
  }

  // Delete an emoji from the manager (admin). On success: drop it from the list and
  // invalidate the per-server cache so `:name:` stops resolving without a reload.
  const deleteEmojiFromManager = async (serverId: number, emojiId: number, name: string) => {
    if (!window.confirm(`Delete the :${name}: emoji? Messages using it will show the literal text.`))
      return
    try {
      await deleteServerEmoji(token, serverId, emojiId)
      const list = await listServerEmoji(token, serverId)
      setEmojiManager(list)
      refreshEmojiCache(serverId, list)
    } catch (err) {
      window.alert(err instanceof Error ? err.message : 'could not delete emoji')
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

  // Threads (v0.8): open the panel of the active channel's threads (mirrors the pins panel).
  const openThreads = async () => {
    if (channelId == null) return
    try {
      setMembersOf(null)
      setSearchResults(null)
      setPins(null)
      setSearchQuery('')
      setThreads(await fetchThreads(token, channelId))
    } catch (err) {
      window.alert(err instanceof Error ? err.message : 'could not load threads')
    }
  }
  const closeThreads = () => setThreads(null)

  // Open a thread channel: reuse selectChannel's WS reconnect, and remember it as the active
  // thread so the header can show its name + a back-link to the parent.
  const selectThread = (t: Channel) => {
    setChannelId(t.id)
    setSidebarOpen(false)
    setPins(null)
    setThreads(null)
    setActiveThread(t)
  }

  // Create a thread off `parentChannelId` (prompt for a name), optionally anchored to a
  // message, then jump into it.
  const startThread = async (parentChannelId: number, fromMessageId?: number) => {
    const name = window.prompt('New thread name:')?.trim()
    if (!name) return
    try {
      const t = await createThread(token, parentChannelId, name, fromMessageId)
      setThreads((cur) => (cur ? [t, ...cur] : cur))
      selectThread(t)
    } catch (err) {
      window.alert(err instanceof Error ? err.message : 'could not create thread')
    }
  }

  // The message-hover "thread" action: open the message's existing thread if it already has
  // one (Discord behavior — one thread per message), else start a new anchored thread.
  const threadFromMessage = (m: Message) => {
    if (m.threadId != null) {
      selectThread({ id: m.threadId, name: m.threadName ?? 'thread', kind: 'thread', createdAt: '', parentId: m.channelId })
      return
    }
    void startThread(m.channelId, m.id)
  }
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
  // Transfer ownership to another member (owner only). The old owner becomes an admin;
  // re-fetch members + the server list so both roles update in place.
  const transferOwnership = async (serverId: number, userId: number, username: string) => {
    if (
      !window.confirm(
        `Make ${username} the owner of this server? You'll become an admin and can't undo this.`,
      )
    )
      return
    try {
      await transferServerOwnership(token, serverId, userId)
      const [members, srvs] = await Promise.all([
        fetchServerMembers(token, serverId),
        fetchServers(token),
      ])
      setMembersOf({ serverId, members })
      setServers(srvs)
      if (String(serverId) === activeServerId) setMemberList(members)
    } catch (err) {
      window.alert(err instanceof Error ? err.message : 'could not transfer ownership')
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
  // Rename a server (owner/admin). Optimistically relabels the sidebar; the WS
  // "server-renamed" push keeps every other member in sync.
  const renameServerPanel = async (serverId: number, current: string) => {
    const next = window.prompt('Rename server (1-64 chars):', current)?.trim()
    if (!next || next === current) return
    try {
      const srv = await renameServer(token, serverId, next)
      setServers((cur) => cur.map((s) => (s.id === serverId ? { ...s, name: srv.name } : s)))
    } catch (err) {
      window.alert(err instanceof Error ? err.message : 'could not rename server')
    }
  }
  // Delete a server (owner only, destructive). Requires typing the name to confirm,
  // then drops it from the sidebar and falls back to #general if we were viewing it.
  const deleteServerPanel = async (serverId: number, name: string) => {
    const typed = window.prompt(
      `Delete "${name}"? This permanently removes its channels and messages and can't be undone.\n\nType the server name to confirm:`,
    )?.trim()
    if (typed !== name) {
      if (typed != null) window.alert('Name did not match — server not deleted.')
      return
    }
    try {
      await deleteServer(token, serverId)
      const wasViewing = (serverChannels[serverId] ?? []).some((c) => c.id === channelId)
      setServers((cur) => cur.filter((s) => s.id !== serverId))
      setServerChannels((cur) => {
        const nextMap = { ...cur }
        delete nextMap[serverId]
        return nextMap
      })
      setMembersOf(null)
      setBans(null)
      setServerInvites(null)
      setEmojiManager(null)
      if (wasViewing) {
        const general = channels.find((c) => c.name === 'general') ?? channels[0]
        if (general) setChannelId(general.id)
      }
    } catch (err) {
      window.alert(err instanceof Error ? err.message : 'could not delete server')
    }
  }
  // Leave a server (non-owner members). Confirms, then drops it from the sidebar and
  // falls back to #general if we were viewing it — same cleanup as a delete/kick.
  const leaveServerPanel = async (serverId: number, name: string) => {
    if (!window.confirm(`Leave "${name}"? You'll need a new invite to rejoin.`)) return
    try {
      await leaveServer(token, serverId)
      const wasViewing = (serverChannels[serverId] ?? []).some((c) => c.id === channelId)
      setServers((cur) => cur.filter((s) => s.id !== serverId))
      setServerChannels((cur) => {
        const nextMap = { ...cur }
        delete nextMap[serverId]
        return nextMap
      })
      setMembersOf(null)
      setBans(null)
      setServerInvites(null)
      setEmojiManager(null)
      if (wasViewing) {
        const general = channels.find((c) => c.name === 'general') ?? channels[0]
        if (general) setChannelId(general.id)
      }
    } catch (err) {
      window.alert(err instanceof Error ? err.message : 'could not leave server')
    }
  }
  // Save the caller's custom status + emoji (already trimmed by the caller). Driven by
  // the User Settings modal's Save button (replaces the old window.prompt flow).
  const saveStatus = async (status: string, emoji: string) => {
    try {
      await setMyStatus(token, status, emoji)
      setMyStatus_(status)
      setMyStatusEmoji_(emoji)
      // Refresh the active server's member list so the new status shows immediately.
      if (activeServerId) {
        const members = await fetchServerMembers(token, Number(activeServerId))
        setMemberList(members)
      }
    } catch (err) {
      window.alert(err instanceof Error ? err.message : 'could not set status')
    }
  }

  // Save my profile (About Me + pronouns), then refresh the member list so my own card
  // reflects it. Driven by the User Settings modal's My Account tab.
  const saveProfile = async (about: string, pronouns: string) => {
    try {
      await setMyProfile(token, about, pronouns)
      setMyAbout_(about)
      setMyPronouns_(pronouns)
      if (activeServerId) {
        const members = await fetchServerMembers(token, Number(activeServerId))
        setMemberList(members)
      }
    } catch (err) {
      window.alert(err instanceof Error ? err.message : 'could not set profile')
    }
  }

  // Open the profile card for a user by id — fetch their public profile (works anywhere,
  // incl. #general/DMs where there's no member list to read from). Used by message clicks.
  const openUserProfile = async (userId: number) => {
    const p = await fetchUserProfile(token, userId)
    if (p) setProfileMember(p)
  }

  // Block / unblock a user. Optimistically flips the local `blocked` set (so their
  // messages hide/show instantly), persists it, and reverts on error. You can never
  // block yourself. Blocking also closes the profile card (you're done with them).
  const toggleBlock = async (userId: number) => {
    if (userId === user.id) return
    const wasBlocked = blocked.has(userId)
    setBlocked((prev) => {
      const next = new Set(prev)
      if (wasBlocked) next.delete(userId)
      else next.add(userId)
      return next
    })
    if (!wasBlocked) setProfileMember(null) // blocking dismisses their card
    try {
      if (wasBlocked) await unblockUser(token, userId)
      else await blockUser(token, userId)
    } catch (err) {
      // Roll back the optimistic toggle.
      setBlocked((prev) => {
        const next = new Set(prev)
        if (wasBlocked) next.add(userId)
        else next.delete(userId)
        return next
      })
      window.alert(err instanceof Error ? err.message : 'could not update block')
    }
  }

  // Re-read the authoritative block list from the server. Settings unblocks update their
  // own list locally AND call this so Chat's message-hide set stays consistent.
  const refreshBlocked = async () => {
    try {
      const us = await listBlocked(token)
      setBlocked(new Set(us.map((u) => u.id)))
    } catch {
      /* leave the set as-is on failure */
    }
  }

  // Change my presence (online|idle|dnd|invisible). Optimistically update the picker,
  // then refresh the member list so the dot recolors immediately. `auto` marks an
  // automatic (inactivity) change; a manual change cancels auto-idle restoration.
  const changePresence = async (next: string, auto = false) => {
    if (!auto) autoIdledRef.current = false
    const prev = myPresence
    setMyPresence_(next)
    try {
      await setMyPresence(token, next)
      if (activeServerId) {
        const members = await fetchServerMembers(token, Number(activeServerId))
        setMemberList(members)
      }
    } catch (err) {
      setMyPresence_(prev)
      if (!auto) window.alert(err instanceof Error ? err.message : 'could not set presence')
    }
  }
  // Always-fresh reference for the inactivity timer (avoids a stale-closure presence).
  const changePresenceRef = useRef(changePresence)
  changePresenceRef.current = changePresence

  // Auto-idle (Discord-style): after a stretch of no activity, drop online → idle; on
  // the next activity restore online. Never overrides a manual idle/dnd/invisible — it
  // only transitions a presence WE set automatically. The threshold is overridable via
  // window.__ocIdleMs (browser QA shortens it; defaults to 10 min like Discord).
  useEffect(() => {
    if (!token) return
    let timer: ReturnType<typeof setTimeout> | null = null
    const onActivity = () => {
      if (autoIdledRef.current) {
        autoIdledRef.current = false
        void changePresenceRef.current('online', true)
      }
      if (timer) clearTimeout(timer)
      // Read the threshold on each re-arm so it can be tuned live (browser QA shortens it).
      const idleMs = (window as unknown as { __ocIdleMs?: number }).__ocIdleMs ?? 10 * 60 * 1000
      timer = setTimeout(() => {
        if (myPresenceRef.current === 'online') {
          autoIdledRef.current = true
          void changePresenceRef.current('idle', true)
        }
      }, idleMs)
    }
    const events = ['mousemove', 'mousedown', 'keydown', 'touchstart', 'wheel']
    events.forEach((e) => window.addEventListener(e, onActivity, { passive: true }))
    onActivity() // arm the timer now
    return () => {
      events.forEach((e) => window.removeEventListener(e, onActivity))
      if (timer) clearTimeout(timer)
    }
  }, [token])

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
      className={`channel-item server-channel${c.id === channelId ? ' active' : ''}${isUnread(c.id) ? ' unread' : ''}${mutedChannels.has(c.id) ? ' muted' : ''}`}
      onClick={() => selectChannel(c.id)}
    >
      <span className="hash">#</span>
      <span className="item-name" title={c.name}>
        {c.name}
      </span>
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
  // Keep the WS message handler's view of "is the active channel a DM" current.
  activeIsDMRef.current = !!activeDM
  const activeServerChannel = Object.values(serverChannels)
    .flat()
    .find((c) => c.id === channelId)
  const activeChannelName = current?.name ?? activeServerChannel?.name ?? activeThread?.name
  // A thread is being viewed iff the active channel is the tracked active thread.
  const inThread = !!activeThread && activeThread.id === channelId
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

  // Custom colored roles (v0.7): keep the active server's roles loaded (for the profile-card
  // role chips + name colors), and refresh members+roles after any assignment so colors update.
  useEffect(() => {
    const sid = activeServerId ? Number(activeServerId) : null
    if (!sid) {
      setServerRoles([])
      return
    }
    let cancelled = false
    listServerRoles(token, sid)
      .then((rs) => !cancelled && setServerRoles(rs))
      .catch(() => !cancelled && setServerRoles([]))
    return () => {
      cancelled = true
    }
  }, [activeServerId, token])

  const refreshRolesAndMembers = async () => {
    const sid = activeServerId ? Number(activeServerId) : null
    if (!sid) return
    try {
      const [members, roles] = await Promise.all([
        fetchServerMembers(token, sid),
        listServerRoles(token, sid),
      ])
      setMemberList(members)
      setServerRoles(roles)
      setMembersOf((cur) => (cur && cur.serverId === sid ? { serverId: sid, members } : cur))
      setProfileMember((cur) => (cur ? members.find((m) => m.userId === cur.userId) ?? cur : cur))
    } catch {
      /* transient — leave the prior state */
    }
  }

  const assignRole = async (userId: number, roleId: number) => {
    const sid = activeServerId ? Number(activeServerId) : null
    if (!sid) return
    try {
      await assignServerRole(token, sid, userId, roleId)
      await refreshRolesAndMembers()
    } catch (err) {
      window.alert(err instanceof Error ? err.message : 'could not assign role')
    }
  }
  const unassignRole = async (userId: number, roleId: number) => {
    const sid = activeServerId ? Number(activeServerId) : null
    if (!sid) return
    try {
      await unassignServerRole(token, sid, userId, roleId)
      await refreshRolesAndMembers()
    } catch (err) {
      window.alert(err instanceof Error ? err.message : 'could not unassign role')
    }
  }
  // Am I currently timed out (muted) in the active server channel? Derived from the
  // polled member list; the server enforces it regardless, this just reflects it in UI.
  const myTimeoutUntil = memberList.find((m) => m.userId === user.id)?.timeoutUntil
  const iAmTimedOut = !!myTimeoutUntil && new Date(myTimeoutUntil).getTime() > Date.now()
  const canPost = (!activeIsReadOnly || canModerate) && !iAmTimedOut
  // Pinning matches the server's rule: admins in a server channel, any member elsewhere.
  const canPin = !activeServerChannel || canModerate
  // The active server's custom-emoji map (name → id), used to render `:name:` inline.
  // Undefined for #general / DMs (no server) so `:name:` stays literal there.
  const activeEmoji = activeServerId ? serverEmoji[Number(activeServerId)] : undefined

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

  // Fetch the active server's custom emoji once and cache it by server id (don't refetch
  // on every channel switch within the same server). Drives `:name:` → inline image.
  useEffect(() => {
    if (!activeServerId) return
    const sid = Number(activeServerId)
    if (serverEmoji[sid]) return // already cached
    let live = true
    listServerEmoji(token, sid)
      .then((list) => {
        if (!live) return
        const map = new Map(list.map((e) => [e.name, e.id]))
        setServerEmoji((cur) => ({ ...cur, [sid]: map }))
      })
      .catch(() => {})
    return () => {
      live = false
    }
  }, [activeServerId, token, serverEmoji])

  // Keep my own status label + emoji in sync from whichever member list includes me.
  useEffect(() => {
    const mine =
      memberList.find((m) => m.userId === user.id) ??
      membersOf?.members.find((m) => m.userId === user.id)
    if (!mine) return
    if ((mine.status ?? '') !== myStatus) setMyStatus_(mine.status ?? '')
    if ((mine.statusEmoji ?? '') !== myStatusEmoji) setMyStatusEmoji_(mine.statusEmoji ?? '')
    if ((mine.about ?? '') !== myAbout) setMyAbout_(mine.about ?? '')
    if ((mine.pronouns ?? '') !== myPronouns) setMyPronouns_(mine.pronouns ?? '')
    // Own row reports the true self presence (incl. invisible) — keep the picker synced.
    if (mine.presence && mine.presence !== myPresence) setMyPresence_(mine.presence)
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
    void fetchMutedChannels(token)
      .then((ids) => live && setMutedChannels(new Set(ids)))
      .catch(() => {})
    const timer = setInterval(load, 10000)
    return () => {
      live = false
      clearInterval(timer)
    }
  }, [token])

  // Mute/unmute a channel for myself: optimistic toggle, then persist + refetch unreads so
  // a now-muted channel's dot clears (or a now-unmuted one reappears) immediately.
  const toggleChannelMute = async (id: number) => {
    const willMute = !mutedChannels.has(id)
    setMutedChannels((prev) => {
      const next = new Set(prev)
      if (willMute) next.add(id)
      else next.delete(id)
      return next
    })
    try {
      await setChannelMuted(token, id, willMute)
      const cs = await fetchUnreads(token)
      setUnread(new Map(cs.map((c) => [c.id, c.mentions])))
    } catch {
      // Roll back the optimistic toggle on failure.
      setMutedChannels((prev) => {
        const next = new Set(prev)
        if (willMute) next.delete(id)
        else next.add(id)
        return next
      })
    }
  }

  // Browser tab badge (Discord-style): reflect unread/mention state in document.title so
  // a backgrounded tab signals activity — "(N) • Opencord" when you have @mentions, a
  // "● Opencord" dot for plain unreads, else just "Opencord". Excludes the active channel.
  useEffect(() => {
    let mentions = 0
    let unreadCount = 0
    unread.forEach((m, id) => {
      if (id === channelId) return
      unreadCount++
      mentions += m
    })
    const base = 'Opencord'
    document.title = mentions > 0 ? `(${mentions}) • ${base}` : unreadCount > 0 ? `● ${base}` : base
    return () => {
      document.title = base // reset on unmount (logout) so the login screen isn't badged
    }
  }, [unread, channelId])

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
    setThreads(null)
    setActiveThread(null) // leaving a thread for a normal channel
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
              className={`channel-item${c.id === channelId ? ' active' : ''}${isUnread(c.id) ? ' unread' : ''}${mutedChannels.has(c.id) ? ' muted' : ''}`}
              onClick={() => selectChannel(c.id)}
            >
              <span className="hash">#</span>
              <span className="item-name" title={c.name}>
                {c.name}
              </span>
              {unreadIndicator(c.id)}
            </button>
          ))}
        </nav>
        <button className="add-channel" onClick={addChannel}>
          + New channel
        </button>

        <div className="sidebar-head">Direct Messages</div>
        <nav className="channel-list dm-list">
          {dms.map((d) => {
            const group = dmIsGroup(d)
            const title = dmTitle(d)
            const lead = dmOthers(d)[0]
            return (
              <button
                key={d.id}
                className={`channel-item${d.id === channelId ? ' active' : ''}${isUnread(d.id) ? ' unread' : ''}${mutedChannels.has(d.id) ? ' muted' : ''}`}
                onClick={() => selectChannel(d.id)}
              >
                {group ? (
                  <span className="dm-avatar dm-group-avatar" aria-hidden>
                    <svg width="13" height="13" viewBox="0 0 24 24" fill="currentColor">
                      <path d="M16 11c1.66 0 3-1.34 3-3s-1.34-3-3-3-3 1.34-3 3 1.34 3 3 3zm-8 0c1.66 0 3-1.34 3-3S9.66 5 8 5 5 6.34 5 8s1.34 3 3 3zm0 2c-2.33 0-7 1.17-7 3.5V19h14v-2.5c0-2.33-4.67-3.5-7-3.5zm8 0c-.29 0-.62.02-.97.05 1.16.84 1.97 1.97 1.97 3.45V19h6v-2.5c0-2.33-4.67-3.5-7-3.5z" />
                    </svg>
                  </span>
                ) : (
                  <Avatar token={token} userId={lead.id} username={lead.username} className="dm-avatar" />
                )}
                <span className="item-name" title={title}>
                  {title}
                </span>
                {unreadIndicator(d.id)}
              </button>
            )
          })}
        </nav>
        <button className="add-channel" onClick={() => setNewDMOpen(true)}>
          + New DM
        </button>

        <div className="sidebar-head">Servers</div>
        <nav className="channel-list server-list">
          {servers.map((s) => (
            <div key={s.id} className="server-group">
              <div className="server-name">
                <span className="server-name-text" title={s.name}>
                  {s.name}
                </span>{' '}
                <span className="server-id">#{s.id}</span>
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
            {inThread && activeThread?.parentId != null && (
              <button
                className="link thread-back"
                onClick={() => selectChannel(activeThread.parentId as number)}
                title="Back to the channel"
              >
                ←
              </button>
            )}
            <span className="channel">
              {inThread
                ? `🧵 ${activeChannelName ?? '…'}`
                : activeDM
                  ? dmIsGroup(activeDM)
                    ? dmTitle(activeDM)
                    : `@${dmTitle(activeDM)}`
                  : `#${activeChannelName ?? '…'}`}
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
          {channelId != null && !activeDM && !inThread && (
            <button className="link threads-open" onClick={() => void openThreads()}>
              🧵 threads
            </button>
          )}
          {channelId != null && (
            <button
              className="link channel-mute-toggle"
              onClick={() => void toggleChannelMute(channelId)}
              title={
                mutedChannels.has(channelId)
                  ? 'Unmute this channel (show its notifications again)'
                  : 'Mute this channel (no unread/mention/tab badges)'
              }
              data-muted={mutedChannels.has(channelId)}
            >
              {mutedChannels.has(channelId) ? '🔕 muted' : '🔔 mute'}
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
              placeholder="Search… (try from:user has:link before:2024-01-31)"
              title="Filters: from:<user>, has:link, has:image, has:file, before:<YYYY-MM-DD>, after:<YYYY-MM-DD>"
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
            />
          </form>
          <div className="meta">
            <span className={connected ? 'dot online' : 'dot offline'} />
            {online} online
            <button
              type="button"
              className="self-chip"
              title="User settings"
              aria-label="user settings"
              onClick={() => setSettingsOpen(true)}
            >
              <span className="self-chip-avatar">
                <Avatar
                  token={token}
                  userId={user.id}
                  username={user.username}
                  className="avatar avatar-self"
                  bust={avatarVersion}
                />
                <span
                  className={`presence-pip presence-${myPresence} self-chip-pip`}
                  aria-hidden
                />
              </span>
              <span className="self-chip-name">{user.username}</span>
              <span className="self-chip-gear" aria-hidden>
                ⚙
              </span>
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
              className={`link voice-screen-toggle${localScreen && localVideoKind === 'screen' ? ' on' : ''}`}
              onClick={() => void toggleScreenShare()}
              title="Share your screen (up to 4K/60 — with system audio if you allow it)"
              data-sharing={localScreen != null && localVideoKind === 'screen'}
            >
              {localScreen && localVideoKind === 'screen' ? 'stop sharing' : '🖥 share screen'}
            </button>
            <button
              className={`link voice-camera-toggle${localScreen && localVideoKind === 'camera' ? ' on' : ''}`}
              onClick={() => void toggleCamera()}
              title="Turn your camera on (others in the call see your video)"
              data-camera={localScreen != null && localVideoKind === 'camera'}
            >
              {localScreen && localVideoKind === 'camera' ? 'stop camera' : '📹 camera'}
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
              <div
                className="screen-tile"
                data-screen-self={localVideoKind === 'screen' ? true : undefined}
                data-camera-self={localVideoKind === 'camera' ? true : undefined}
              >
                <video
                  className={`screen-video${localVideoKind === 'camera' ? ' mirror' : ''}`}
                  autoPlay
                  muted
                  playsInline
                  style={{ objectFit: screenFit }}
                  ref={(el) => {
                    if (el && el.srcObject !== localScreen) el.srcObject = localScreen
                  }}
                />
                <div className="screen-tile-bar">
                  <span className="screen-tile-name">
                    {localVideoKind === 'camera' ? 'Your camera' : 'You are sharing'}
                  </span>
                  <button
                    type="button"
                    className="link screen-fullscreen"
                    onClick={(e) =>
                      void e.currentTarget.closest('.screen-tile')?.querySelector('video')?.requestFullscreen?.()
                    }
                    title="Fullscreen"
                    aria-label="fullscreen this video"
                  >
                    ⛶
                  </button>
                  {/* Screen-audio mixing controls — only the screen carries audio. */}
                  {localVideoKind === 'screen' && (
                    <>
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
                    </>
                  )}
                </div>
              </div>
            )}
            {voicePeers
              .filter((p) => p.sharingScreen && p.screenStream)
              .map((p) => (
                <div
                  key={p.id}
                  className="screen-tile"
                  data-screen-peer={p.id}
                  data-video-kind={p.videoKind}
                >
                  {/* Remote camera is NOT mirrored — you see others as they are
                      (only your own self-view is mirrored). */}
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
                    <span className="screen-tile-name">
                      {p.username || `user ${p.id}`}’s {p.videoKind === 'camera' ? 'camera' : 'screen'}
                    </span>
                    {/* Only a screen carries audio — a camera has none. */}
                    {p.videoKind === 'screen' && (
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
                    )}
                    <button
                      type="button"
                      className="link screen-fullscreen"
                      onClick={(e) =>
                        void e.currentTarget.closest('.screen-tile')?.querySelector('video')?.requestFullscreen?.()
                      }
                      title="Fullscreen"
                      aria-label={`fullscreen ${p.username || `user ${p.id}`}'s video`}
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
                    setEmojiManager(null)
                    setEmojiName('')
                    setEmojiFile(null)
                    setEmojiError('')
                  }}
                >
                  ✕ close
                </button>
              </div>
              {/* Server settings (any member). Dedicated classes (NOT .member-row /
                  .invites-head / .bans-head) so this never collides with the QA +
                  behavioural selectors those carry. Rename is admin+, delete owner-only,
                  leave is for any non-owner member. */}
              {myRoleInPanel && (
                <div className="server-settings">
                  <span className="server-settings-head">⚙ Server settings</span>
                  {(myRoleInPanel === 'owner' || myRoleInPanel === 'admin') && (
                    <button
                      className="link rename-server-btn"
                      onClick={() =>
                        void renameServerPanel(
                          membersOf.serverId,
                          servers.find((s) => s.id === membersOf.serverId)?.name ?? '',
                        )
                      }
                    >
                      rename
                    </button>
                  )}
                  {(myRoleInPanel === 'owner' || myRoleInPanel === 'admin') && (
                    <button
                      className="link manage-roles-btn"
                      onClick={() => setRolesManagerFor(membersOf.serverId)}
                    >
                      manage roles
                    </button>
                  )}
                  {myRoleInPanel === 'owner' ? (
                    <button
                      className="link delete-server-btn"
                      onClick={() =>
                        void deleteServerPanel(
                          membersOf.serverId,
                          servers.find((s) => s.id === membersOf.serverId)?.name ?? '',
                        )
                      }
                    >
                      delete server
                    </button>
                  ) : (
                    <button
                      className="link leave-server-btn"
                      onClick={() =>
                        void leaveServerPanel(
                          membersOf.serverId,
                          servers.find((s) => s.id === membersOf.serverId)?.name ?? '',
                        )
                      }
                    >
                      leave server
                    </button>
                  )}
                </div>
              )}
              {membersOf.members.map((mb) => (
                <div key={mb.userId} className={`member-row${mb.online ? '' : ' offline'}`}>
                  <span className="avatar-presence">
                    <Avatar token={token} userId={mb.userId} username={mb.username} />
                    <span
                      className={`presence-dot ${mb.presence ?? (mb.online ? 'online' : 'offline')}`}
                      title={mb.presence ?? (mb.online ? 'online' : 'offline')}
                    />
                  </span>
                  <span className="member-id">
                    <span className="author" style={mb.color ? { color: mb.color } : undefined}>
                      {mb.username}
                    </span>
                    {(mb.status || mb.statusEmoji) && (
                      <span className="member-status" title={mb.status || ''}>
                        {mb.statusEmoji && <span className="status-emoji">{mb.statusEmoji}</span>}
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
                  {/* Transfer ownership: only the owner may hand the server to another
                      member; they then become an admin (server enforces it). */}
                  {iAmServerOwner && mb.role !== 'owner' && mb.userId !== user.id && (
                    <button
                      className="link transfer-owner-btn"
                      onClick={() =>
                        void transferOwnership(membersOf.serverId, mb.userId, mb.username)
                      }
                    >
                      make owner
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
              {/* Custom-emoji manager (admin view): list the server's emoji, upload a new
                  one (name + image), and delete one. A successful upload/delete refreshes
                  the per-server `:name:` cache so the renderer picks it up live (no reload).
                  Dedicated emoji-manager-* classes throughout (NOT .member-row /
                  .invites-head / .bans-head) so this never collides with those QA +
                  behavioural selectors. */}
              {emojiManager !== null && (
                <>
                  <div className="emoji-manager-head">Emoji ({emojiManager.length})</div>
                  <form
                    className="emoji-manager-form"
                    onSubmit={(e) => {
                      e.preventDefault()
                      void uploadEmojiFromManager(membersOf.serverId)
                    }}
                  >
                    <input
                      className="emoji-manager-name"
                      aria-label="emoji name"
                      placeholder="emoji_name"
                      maxLength={32}
                      value={emojiName}
                      onChange={(e) => setEmojiName(e.target.value)}
                    />
                    <input
                      className="emoji-manager-file"
                      type="file"
                      accept="image/*"
                      aria-label="emoji image"
                      onChange={(e) => {
                        setEmojiFile(e.target.files?.[0] ?? null)
                        setEmojiError('')
                      }}
                    />
                    <button
                      type="submit"
                      className="link emoji-upload-btn"
                      disabled={emojiUploading}
                    >
                      {emojiUploading ? 'Uploading…' : 'Upload'}
                    </button>
                  </form>
                  <span className="emoji-manager-hint">
                    Name: 2–32 chars, lowercase letters, numbers, or underscores. Use it as{' '}
                    <code>:name:</code> in chat. Image ≤256 KiB.
                  </span>
                  {emojiError && (
                    <div className="emoji-manager-error" role="alert">
                      {emojiError}
                    </div>
                  )}
                  {emojiManager.length === 0 && (
                    <div className="emoji-manager-empty">
                      No custom emoji yet — upload one to use it as <code>:name:</code> in chat.
                    </div>
                  )}
                  {emojiManager.map((em) => (
                    <div key={em.id} className="emoji-manager-row">
                      <EmojiImg
                        token={token}
                        id={em.id}
                        alt={`:${em.name}:`}
                        className="emoji-manager-img"
                      />
                      <code className="emoji-manager-code">:{em.name}:</code>
                      <button
                        className="link emoji-delete-btn"
                        onClick={() =>
                          void deleteEmojiFromManager(membersOf.serverId, em.id, em.name)
                        }
                      >
                        delete
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
                      <span className="author" style={m.authorColor ? { color: m.authorColor } : undefined}>
                        {m.username}
                      </span>
                      <span className="time">{messageTimestamp(new Date(m.createdAt))}</span>
                    </div>
                    <div className="body">{m.deleted ? m.body : renderMarkdown(m.body, { me: user.username, emoji: activeEmoji, token })}</div>
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
                      <span className="author" style={m.authorColor ? { color: m.authorColor } : undefined}>
                        {m.username}
                      </span>
                      <span className="time">{messageTimestamp(new Date(m.createdAt))}</span>
                    </div>
                    <div className="body">
                      {m.deleted ? m.body : renderMarkdown(m.body, { me: user.username, emoji: activeEmoji, token })}
                    </div>
                  </div>
                </div>
              ))}
            </div>
          )}
          {membersOf === null && searchResults === null && pins === null && threads !== null && (
            <div className="search-results">
              <div className="search-results-head">
                <span>
                  🧵 {threads.length} thread{threads.length === 1 ? '' : 's'}
                </span>
                <span>
                  <button className="link" onClick={() => void startThread(channelId as number)}>
                    + New thread
                  </button>{' '}
                  <button className="link" onClick={closeThreads}>
                    ✕ close
                  </button>
                </span>
              </div>
              {threads.length === 0 && <div className="search-empty">No threads yet — start one.</div>}
              {threads.map((t) => (
                <button
                  key={t.id}
                  className="thread-row"
                  onClick={() => selectThread(t)}
                  title={`Open “${t.name}”`}
                >
                  <span className="thread-row-icon" aria-hidden>
                    🧵
                  </span>
                  <span className="thread-row-name">{t.name}</span>
                </button>
              ))}
            </div>
          )}
          {membersOf === null && searchResults === null && pins === null && threads === null && (
            <div className="channel-intro">
              <div className="channel-intro-icon" aria-hidden>
                {inThread ? '🧵' : activeDM ? (dmIsGroup(activeDM) ? '👥' : '@') : '#'}
              </div>
              <h2 className="channel-intro-title">
                {inThread
                  ? activeChannelName
                  : activeDM
                    ? dmTitle(activeDM)
                    : `Welcome to #${activeChannelName ?? ''}!`}
              </h2>
              <p className="channel-intro-sub">
                {inThread
                  ? `This is the start of the “${activeChannelName ?? ''}” thread.`
                  : activeDM
                    ? dmIsGroup(activeDM)
                      ? `This is the beginning of your group conversation with ${dmTitle(activeDM)}.`
                      : `This is the beginning of your direct message history with @${dmTitle(activeDM)}.`
                    : `This is the start of the #${activeChannelName ?? ''} channel.`}
              </p>
            </div>
          )}
          {membersOf === null &&
            searchResults === null &&
            pins === null &&
            threads === null &&
            // Hide blocked users' messages entirely. Filter FIRST so the date-divider +
            // grouping logic below computes over the VISIBLE list (a hidden message can't
            // break a run or leave an orphaned divider). Live WS messages from a blocked
            // user are filtered here too — no special-casing needed.
            visibleMessages(messages, blocked).map((m, i, visible) => {
            const prev = i > 0 ? visible[i - 1] : null
            // First message of a new calendar day (Discord-style date divider). The
            // very first message also starts a day (prev === null).
            const newDay =
              !prev || new Date(m.createdAt).toDateString() !== new Date(prev.createdAt).toDateString()
            // Group consecutive messages from the same author within 5 min (Discord-style):
            // hide the repeated avatar + name. A deleted message OR a new day breaks the run.
            const grouped =
              !!prev &&
              !newDay &&
              !m.deleted &&
              !prev.deleted &&
              prev.userId === m.userId &&
              new Date(m.createdAt).getTime() - new Date(prev.createdAt).getTime() < 5 * 60 * 1000
            return (
              <Fragment key={m.id}>
              {newDay && (
                <div className="day-divider" role="separator" aria-label={dayLabel(new Date(m.createdAt))}>
                  <span>{dayLabel(new Date(m.createdAt))}</span>
                </div>
              )}
              <div
                id={`msg-${m.id}`}
                className={`message${m.deleted ? ' deleted' : ''}${grouped ? ' grouped' : ''}${m.id === flashId ? ' flash' : ''}`}
              >
                {grouped ? (
                  // Grouped continuation rows hide the avatar/name; Discord surfaces a
                  // compact timestamp in the gutter on hover so you can still place the message.
                  <div className="avatar-spacer">
                    <span className="hover-time">{shortTime(new Date(m.createdAt))}</span>
                  </div>
                ) : (
                  <button
                    type="button"
                    className="avatar-link"
                    title={`View ${m.username}'s profile`}
                    aria-label={`View ${m.username}'s profile`}
                    onClick={() => void openUserProfile(m.userId)}
                  >
                    <Avatar token={token} userId={m.userId} username={m.username} />
                  </button>
                )}
                <div className="message-content">
                  {!grouped && (
                    <div className="message-head">
                      <button
                        type="button"
                        className="author author-link"
                        style={m.authorColor ? { color: m.authorColor } : undefined}
                        onClick={() => void openUserProfile(m.userId)}
                      >
                        {m.username}
                      </button>
                      <span className="time">{messageTimestamp(new Date(m.createdAt))}</span>
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
                      {!activeDM && !inThread && (
                        <button onClick={() => threadFromMessage(m)}>
                          {m.threadId != null ? 'open thread' : 'thread'}
                        </button>
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
                    <div className="body">{m.deleted ? m.body : renderMarkdown(m.body, { me: user.username, emoji: activeEmoji, token })}</div>
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
                      {/* The active server's custom emoji — react with one as `custom:{id}`. */}
                      {activeEmoji &&
                        activeEmoji.size > 0 &&
                        [...activeEmoji.entries()].map(([name, id]) => (
                          <button
                            key={`custom:${id}`}
                            className="emoji-option"
                            title={`:${name}:`}
                            data-emoji-name={name}
                            onClick={() => {
                              void toggleReaction(m, `custom:${id}`)
                              setPickerFor(null)
                            }}
                          >
                            <EmojiImg token={token} id={id} alt={`:${name}:`} />
                          </button>
                        ))}
                    </div>
                  )}
                  {!m.deleted && m.threadId != null && (
                    <button
                      className="thread-chip"
                      onClick={() =>
                        selectThread({
                          id: m.threadId as number,
                          name: m.threadName ?? 'thread',
                          kind: 'thread',
                          createdAt: '',
                          parentId: m.channelId,
                        })
                      }
                      title={`Open the “${m.threadName}” thread`}
                    >
                      🧵 {m.threadName}
                    </button>
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
                            {r.emoji.startsWith('custom:') ? (
                              <EmojiImg
                                token={token}
                                id={Number(r.emoji.slice(7))}
                                alt="custom emoji"
                              />
                            ) : (
                              <span className="emoji">{r.emoji}</span>
                            )}
                            <span className="rcount">{r.count}</span>
                          </button>
                        )
                      })}
                    </div>
                  )}
                </div>
              </div>
              </Fragment>
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
          {/* Custom-emoji picker — only when the active server has custom emoji. Inserts
              `:name:` at the caret. Separate from the per-message reaction palette. */}
          {activeEmoji && activeEmoji.size > 0 && (
            <div className="emoji-picker-wrap" ref={emojiPickerRef}>
              <button
                type="button"
                className="emoji-picker-btn"
                aria-label="insert custom emoji"
                aria-expanded={emojiPickerOpen}
                title="Custom emoji"
                disabled={!connected || !canPost}
                onClick={() => setEmojiPickerOpen((o) => !o)}
              >
                🙂
              </button>
              {emojiPickerOpen && (
                <div className="emoji-picker-popover" role="listbox" aria-label="custom emoji">
                  {[...activeEmoji.entries()].map(([name, id]) => (
                    <button
                      type="button"
                      key={id}
                      role="option"
                      aria-selected={false}
                      className="emoji-picker-item"
                      data-emoji-name={name}
                      title={`:${name}:`}
                      aria-label={`:${name}:`}
                      // mousedown so the textarea keeps focus / caret for the splice.
                      onMouseDown={(e) => {
                        e.preventDefault()
                        insertEmojiShortcode(name)
                      }}
                    >
                      <EmojiImg token={token} id={id} alt={`:${name}:`} />
                    </button>
                  ))}
                </div>
              )}
            </div>
          )}
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
                    : inThread
                      ? `Message 🧵 ${activeChannelName ?? ''}`
                      : activeDM
                        ? dmIsGroup(activeDM)
                          ? `Message ${dmTitle(activeDM)}`
                          : `Message @${dmTitle(activeDM)}`
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
                    role="button"
                    tabIndex={0}
                    title={`View ${mb.username}'s profile`}
                    onClick={() => setProfileMember(mb)}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter' || e.key === ' ') {
                        e.preventDefault()
                        setProfileMember(mb)
                      }
                    }}
                  >
                    <span className="avatar-presence">
                      <Avatar token={token} userId={mb.userId} username={mb.username} />
                      <span
                        className={`presence-dot ${mb.presence ?? (mb.online ? 'online' : 'offline')}`}
                        title={mb.presence ?? (mb.online ? 'online' : 'offline')}
                      />
                    </span>
                    <span className="member-id">
                      <span className="author" style={mb.color ? { color: mb.color } : undefined}>
                        {mb.username}
                      </span>
                      {(mb.status || mb.statusEmoji) && (
                        <span className="member-status" title={mb.status || ''}>
                          {mb.statusEmoji && <span className="status-emoji">{mb.statusEmoji}</span>}
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

      {settingsOpen && (
        <Settings
          token={token}
          user={user}
          myPresence={myPresence}
          myStatus={myStatus}
          myStatusEmoji={myStatusEmoji}
          myAbout={myAbout}
          myPronouns={myPronouns}
          avatarVersion={avatarVersion}
          onAvatarPicked={onAvatarPicked}
          onSaveStatus={saveStatus}
          onSaveProfile={saveProfile}
          onChangePresence={changePresence}
          audioInputs={audioInputs}
          audioOutputs={audioOutputs}
          inputDevice={inputDevice}
          outputDevice={outputDevice}
          onChangeInputDevice={changeInputDevice}
          onChangeOutputDevice={changeOutputDevice}
          onRefreshDevices={refreshDevices}
          onSetMasterVolume={(v) => voiceRef.current?.setMasterVolume(v)}
          onSetInputVolume={(v) => voiceRef.current?.setInputVolume(v)}
          onUnblock={async (id) => {
            // Unblock on the server, then refresh Chat's hide set so the unblocked
            // user's messages reappear immediately (keeps both views consistent).
            await unblockUser(token, id)
            await refreshBlocked()
          }}
          onClose={() => setSettingsOpen(false)}
        />
      )}

      {profileMember && (
        <ProfileCard
          member={profileMember}
          token={token}
          selfId={user.id}
          blocked={blocked.has(profileMember.userId)}
          onToggleBlock={(id) => void toggleBlock(id)}
          onClose={() => setProfileMember(null)}
          canManageRoles={canModerate}
          serverRoles={serverRoles}
          onAssignRole={(uid, rid) => void assignRole(uid, rid)}
          onUnassignRole={(uid, rid) => void unassignRole(uid, rid)}
        />
      )}
      {newDMOpen && (
        <NewGroupModal token={token} onClose={() => setNewDMOpen(false)} onCreated={addDM} />
      )}
      {rolesManagerFor !== null && (
        <RolesManagerModal
          token={token}
          serverId={rolesManagerFor}
          onClose={() => setRolesManagerFor(null)}
          onChanged={() => void refreshRolesAndMembers()}
        />
      )}
    </div>
  )
}
