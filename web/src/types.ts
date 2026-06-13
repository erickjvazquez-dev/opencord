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
}

export interface ServerEvent {
  type: 'history' | 'message' | 'presence' | 'error'
  message?: Message
  history?: Message[]
  online?: number
  error?: string
}
