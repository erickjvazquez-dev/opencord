export interface User {
  id: number
  username: string
}

export interface Channel {
  id: number
  name: string
  createdAt: string
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
}

export interface ServerEvent {
  type: 'history' | 'message' | 'message-edited' | 'message-deleted' | 'typing' | 'presence' | 'error'
  message?: Message
  history?: Message[]
  username?: string
  online?: number
  error?: string
}
