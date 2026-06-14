import type { Channel, DMChannel, User } from './types'

interface AuthResponse {
  token: string
  user: User
}

async function postAuth(path: string, body: unknown): Promise<AuthResponse> {
  const res = await fetch(path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  const data = await res.json().catch(() => ({}))
  if (!res.ok) {
    throw new Error((data as { error?: string }).error || `request failed (${res.status})`)
  }
  return data as AuthResponse
}

export const register = (username: string, password: string) =>
  postAuth('/api/auth/register', { username, password })

export const login = (username: string, password: string) =>
  postAuth('/api/auth/login', { username, password })

export async function fetchChannels(token: string): Promise<Channel[]> {
  const res = await fetch('/api/channels', { headers: { Authorization: `Bearer ${token}` } })
  if (!res.ok) throw new Error('could not load channels')
  return res.json()
}

export async function createChannel(token: string, name: string): Promise<Channel> {
  const res = await fetch('/api/channels', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
    body: JSON.stringify({ name }),
  })
  const data = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error((data as { error?: string }).error || 'could not create channel')
  return data as Channel
}

export async function fetchDMs(token: string): Promise<DMChannel[]> {
  const res = await fetch('/api/dms', { headers: { Authorization: `Bearer ${token}` } })
  if (!res.ok) throw new Error('could not load DMs')
  return res.json()
}

export async function openDM(token: string, username: string): Promise<DMChannel> {
  const res = await fetch('/api/dms', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
    body: JSON.stringify({ username }),
  })
  const data = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error((data as { error?: string }).error || 'could not open DM')
  return data as DMChannel
}

export async function editMessage(token: string, id: number, body: string): Promise<void> {
  const res = await fetch(`/api/messages/${id}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
    body: JSON.stringify({ body }),
  })
  if (!res.ok) throw new Error('could not edit message')
}

export async function deleteMessage(token: string, id: number): Promise<void> {
  const res = await fetch(`/api/messages/${id}`, {
    method: 'DELETE',
    headers: { Authorization: `Bearer ${token}` },
  })
  if (!res.ok && res.status !== 204) throw new Error('could not delete message')
}

export async function addReaction(token: string, id: number, emoji: string): Promise<void> {
  const res = await fetch(`/api/messages/${id}/reactions`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
    body: JSON.stringify({ emoji }),
  })
  if (!res.ok) throw new Error('could not add reaction')
}

export async function removeReaction(token: string, id: number, emoji: string): Promise<void> {
  const res = await fetch(`/api/messages/${id}/reactions/${encodeURIComponent(emoji)}`, {
    method: 'DELETE',
    headers: { Authorization: `Bearer ${token}` },
  })
  if (!res.ok && res.status !== 204) throw new Error('could not remove reaction')
}
