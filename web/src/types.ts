export interface User {
  id: number
  username: string
}

export interface Channel {
  id: number
  name: string
  createdAt: string
  // 'everyone' or 'admins' (read-only); present for server channels.
  postPolicy?: string
  // Short channel description shown in the header; present for server channels.
  topic?: string
  // Per-channel post cooldown for non-admins, in seconds (0/absent = off).
  slowmodeSeconds?: number
  // Groups the channel under a server category; absent = uncategorized.
  categoryId?: number
  // Threads (v0.8): kind='thread' marks a thread; parentId is the channel it hangs off.
  // Absent/'' for a normal channel.
  kind?: string
  parentId?: number
}

// A named, collapsible grouping of a server's channels (Discord-style category).
export interface ChannelCategory {
  id: number
  serverId: number
  name: string
  createdAt: string
}

// A direct-message channel as seen by one participant: the channel id plus the
// *other* member(s) in it. DM messages flow through the same per-channel WS as channels.
// `user` is the other member of a 1:1 DM (mirrors users[0]); `users` is every other member
// — one entry for a 1:1, two or more for a group DM (v0.6, slice 1 backend; rendering lands
// in slice 2).
export interface DMChannel {
  id: number
  createdAt: string
  user: { id: number; username: string }
  users?: { id: number; username: string }[]
}

// A server (guild) groups channels under a shared membership. Its channels are
// fetched separately (GET /api/servers/{id}/channels) and are members-only.
export interface Server {
  id: number
  name: string
  ownerId: number
  createdAt: string
  // The requesting user's role in this server ('owner'|'admin'|'member').
  role?: string
}

// A server member with their role (owner | admin | member).
export interface ServerMember {
  userId: number
  username: string
  role: string
  online?: boolean
  // Effective presence as the viewer sees it: online | idle | dnd | offline.
  // (Others see invisible/disconnected as offline; you see your own true state.)
  presence?: string
  status?: string
  // Optional short emoji shown before the status line; absent = none.
  statusEmoji?: string
  // Set (ISO timestamp) while the member is timed out (muted); absent = not muted.
  timeoutUntil?: string
  // Profile (shown on the profile card): a longer About Me + short pronouns; absent = none.
  about?: string
  pronouns?: string
  // Custom colored roles (v0.7): `color` is the member's top role color (#RGB/#RRGGBB) used
  // to tint their name; `roleIds` are the cosmetic role ids they hold (top position first).
  color?: string
  roleIds?: number[]
}

// A custom, cosmetic, server-scoped colored role (v0.7) — separate from the
// owner/admin/member permission tier. A member's display color is their top role's color.
export interface Role {
  id: number
  serverId: number
  name: string
  color: string
  position: number
}

// A custom emoji owned by a server. Server-scoped: a `:name:` only renders as an
// image when that name exists in the CURRENT server's set; the image bytes are served
// (bearer-authed) from /api/emoji/{id}.
export interface ServerEmoji {
  id: number
  serverId: number
  name: string
  createdBy: number
  createdAt: string
}

// A banned user as shown to an admin in the bans list (server moderation).
export interface ServerBan {
  userId: number
  username: string
  reason?: string
  bannedAt: string
}

// An active invite code as shown to an admin in the invites list (server management).
// `expiresAt` is absent for legacy never-expire codes; `maxUses` absent = unlimited.
export interface Invite {
  code: string
  createdBy: number
  creatorName: string
  createdAt: string
  expiresAt?: string
  maxUses?: number
  uses: number
}

// A channel with unread messages; `mentions` counts unread @-mentions of you (red badge).
export interface ChannelUnread {
  id: number
  mentions: number
}

export interface Reaction {
  emoji: string
  count: number
  // True only when the server knows the viewer reacted (history load). The live
  // `reaction` broadcast is count-only (mine=false); the client tracks mine itself.
  mine?: boolean
}

// A file/image attached to a message. `url` is the access-gated serve endpoint
// (/api/attachments/{id}); the client fetches it with its bearer token and renders
// via an object URL, so the session JWT never leaks into an <img src>.
export interface Attachment {
  id: number
  filename: string
  contentType: string
  size: number
  url: string
}

export interface Message {
  id: number
  channelId: number
  userId: number
  username: string
  body: string
  createdAt: string
  editedAt?: string
  deleted?: boolean
  pinned?: boolean
  reactions?: Reaction[]
  // The author's top custom-role color (#RGB/#RRGGBB) in the channel's server (v0.7),
  // tinting their name. Absent for DM/global channels or an uncolored author.
  authorColor?: string
  // Reply reference: the id of the message this one replies to, plus a denormalized
  // author + body snippet for the quoted preview (all unset when not a reply).
  replyTo?: number
  replyToAuthor?: string
  replyToBody?: string
  // Files/images carried by this message (unset when none).
  attachments?: Attachment[]
}

export interface ServerEvent {
  type:
    | 'history'
    | 'message'
    | 'message-edited'
    | 'message-deleted'
    | 'message-pinned'
    | 'reaction'
    | 'typing'
    | 'presence'
    | 'error'
    // Mesh-voice signaling, relayed verbatim and stamped with the sender (`from`).
    | 'voice-join'
    | 'voice-leave'
    | 'voice-signal'
    | 'voice-screen'
    // Per-user push: you were removed from a server (serverId).
    | 'server-removed'
    // Per-member push: a server you're in was renamed (serverId + name).
    | 'server-renamed'
  message?: Message
  history?: Message[]
  username?: string
  online?: number
  error?: string
  // Voice signaling fields: who sent it, who it targets, and the opaque WebRTC
  // SDP/ICE payload (only present on voice-* events).
  from?: number
  target?: number
  signal?: unknown
  // Screen share (voice-screen): on=true started sharing (streamId = the screen
  // MediaStream id, so its tracks are told apart from the mic); absent/false = stopped.
  on?: boolean
  streamId?: string
  // server-removed: which server the user was removed from.
  // server-renamed: which server (serverId) and its new name.
  serverId?: number
  name?: string
}
