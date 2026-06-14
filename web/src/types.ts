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
}

// A direct-message channel as seen by one participant: the channel id plus the
// *other* user in it. DM messages flow through the same per-channel WS as channels.
export interface DMChannel {
  id: number
  createdAt: string
  user: { id: number; username: string }
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
}

export interface Reaction {
  emoji: string
  count: number
  // True only when the server knows the viewer reacted (history load). The live
  // `reaction` broadcast is count-only (mine=false); the client tracks mine itself.
  mine?: boolean
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
  reactions?: Reaction[]
}

export interface ServerEvent {
  type:
    | 'history'
    | 'message'
    | 'message-edited'
    | 'message-deleted'
    | 'reaction'
    | 'typing'
    | 'presence'
    | 'error'
  message?: Message
  history?: Message[]
  username?: string
  online?: number
  error?: string
}
