export interface User {
  id: number
  username: string
}

export interface Channel {
  id: number
  name: string
  createdAt: string
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
