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
  type: 'history' | 'message' | 'message-edited' | 'message-deleted' | 'presence' | 'error'
  message?: Message
  history?: Message[]
  online?: number
  error?: string
}
