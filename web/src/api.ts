import type { Channel, DMChannel, Message, Server, ServerMember, User } from './types'

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

export async function fetchServers(token: string): Promise<Server[]> {
  const res = await fetch('/api/servers', { headers: { Authorization: `Bearer ${token}` } })
  if (!res.ok) throw new Error('could not load servers')
  return res.json()
}

export async function createServer(token: string, name: string): Promise<Server> {
  const res = await fetch('/api/servers', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
    body: JSON.stringify({ name }),
  })
  const data = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error((data as { error?: string }).error || 'could not create server')
  return data as Server
}

export async function createInvite(token: string, serverId: number): Promise<string> {
  const res = await fetch(`/api/servers/${serverId}/invites`, {
    method: 'POST',
    headers: { Authorization: `Bearer ${token}` },
  })
  const data = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error((data as { error?: string }).error || 'could not create invite')
  return (data as { code: string }).code
}

export async function redeemInvite(token: string, code: string): Promise<Server> {
  const res = await fetch(`/api/invites/${encodeURIComponent(code)}`, {
    method: 'POST',
    headers: { Authorization: `Bearer ${token}` },
  })
  const data = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error((data as { error?: string }).error || 'could not redeem invite')
  return data as Server
}

export async function fetchServerMembers(token: string, serverId: number): Promise<ServerMember[]> {
  const res = await fetch(`/api/servers/${serverId}/members`, {
    headers: { Authorization: `Bearer ${token}` },
  })
  if (!res.ok) throw new Error('could not load members')
  return res.json()
}

export async function setServerMemberRole(
  token: string,
  serverId: number,
  userId: number,
  role: string,
): Promise<void> {
  const res = await fetch(`/api/servers/${serverId}/roles`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
    body: JSON.stringify({ userId, role }),
  })
  if (!res.ok && res.status !== 204) {
    const data = await res.json().catch(() => ({}))
    throw new Error((data as { error?: string }).error || 'could not change role')
  }
}

// Kick a member out of a server (owner/admin only; server enforces who may act).
export async function kickServerMember(
  token: string,
  serverId: number,
  userId: number,
): Promise<void> {
  const res = await fetch(`/api/servers/${serverId}/members/${userId}`, {
    method: 'DELETE',
    headers: { Authorization: `Bearer ${token}` },
  })
  if (!res.ok && res.status !== 204) {
    const data = await res.json().catch(() => ({}))
    throw new Error((data as { error?: string }).error || 'could not kick member')
  }
}

export async function fetchServerChannels(token: string, serverId: number): Promise<Channel[]> {
  const res = await fetch(`/api/servers/${serverId}/channels`, {
    headers: { Authorization: `Bearer ${token}` },
  })
  if (!res.ok) throw new Error('could not load server channels')
  return res.json()
}

export async function createServerChannel(
  token: string,
  serverId: number,
  name: string,
): Promise<Channel> {
  const res = await fetch(`/api/servers/${serverId}/channels`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
    body: JSON.stringify({ name }),
  })
  const data = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error((data as { error?: string }).error || 'could not create channel')
  return data as Channel
}

export async function setChannelPolicy(
  token: string,
  channelId: number,
  postPolicy: string,
): Promise<void> {
  const res = await fetch(`/api/channels/${channelId}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
    body: JSON.stringify({ postPolicy }),
  })
  if (!res.ok && res.status !== 204) {
    const data = await res.json().catch(() => ({}))
    throw new Error((data as { error?: string }).error || 'could not change channel policy')
  }
}

export async function setMessagePinned(
  token: string,
  messageId: number,
  pinned: boolean,
): Promise<void> {
  const res = await fetch(`/api/messages/${messageId}/pin`, {
    method: pinned ? 'PUT' : 'DELETE',
    headers: { Authorization: `Bearer ${token}` },
  })
  if (!res.ok && res.status !== 204) {
    const data = await res.json().catch(() => ({}))
    throw new Error((data as { error?: string }).error || 'could not change pin')
  }
}

export async function setChannelTopic(
  token: string,
  channelId: number,
  topic: string,
): Promise<void> {
  const res = await fetch(`/api/channels/${channelId}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
    body: JSON.stringify({ topic }),
  })
  if (!res.ok && res.status !== 204) {
    const data = await res.json().catch(() => ({}))
    throw new Error((data as { error?: string }).error || 'could not set channel topic')
  }
}

// Ask the server for a voice transport for this channel. Returns {sfu:false} when
// no SFU is configured (the client then uses mesh), else a LiveKit url + token.
export async function voiceToken(
  token: string,
  channelId: number,
): Promise<{ sfu: boolean; url?: string; room?: string; token?: string }> {
  const res = await fetch(`/api/voice/token?channel=${channelId}`, {
    method: 'POST',
    headers: { Authorization: `Bearer ${token}` },
  })
  if (!res.ok) return { sfu: false } // any error → fall back to mesh
  return res.json()
}

export async function setChannelSlowmode(
  token: string,
  channelId: number,
  slowmodeSeconds: number,
): Promise<void> {
  const res = await fetch(`/api/channels/${channelId}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
    body: JSON.stringify({ slowmodeSeconds }),
  })
  if (!res.ok && res.status !== 204) {
    const data = await res.json().catch(() => ({}))
    throw new Error((data as { error?: string }).error || 'could not set slowmode')
  }
}

export async function searchMessages(
  token: string,
  channelId: number,
  q: string,
): Promise<Message[]> {
  const res = await fetch(
    `/api/messages/search?channel=${channelId}&q=${encodeURIComponent(q)}`,
    { headers: { Authorization: `Bearer ${token}` } },
  )
  const data = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error((data as { error?: string }).error || 'could not search')
  return data as Message[]
}

export async function fetchPins(token: string, channelId: number): Promise<Message[]> {
  const res = await fetch(`/api/messages/pins?channel=${channelId}`, {
    headers: { Authorization: `Bearer ${token}` },
  })
  const data = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error((data as { error?: string }).error || 'could not load pins')
  return data as Message[]
}

export async function fetchDMs(token: string): Promise<DMChannel[]> {
  const res = await fetch('/api/dms', { headers: { Authorization: `Bearer ${token}` } })
  if (!res.ok) throw new Error('could not load DMs')
  return res.json()
}

// `identifier` may be a username or a numeric user id (email once accounts store one).
export async function openDM(token: string, identifier: string): Promise<DMChannel> {
  const res = await fetch('/api/dms', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
    body: JSON.stringify({ identifier }),
  })
  const data = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error((data as { error?: string }).error || 'could not open DM')
  return data as DMChannel
}

// Send a message carrying file/image attachments (multipart). Body is optional when
// files are present; the server broadcasts the finished message over the WS, so the
// caller relies on the WS echo to render it (same as a plain message). `replyTo` is
// optional. Returns nothing; throws with the server's error on failure.
export async function sendAttachments(
  token: string,
  channelId: number,
  body: string,
  files: File[],
  replyTo?: number,
): Promise<void> {
  const form = new FormData()
  form.append('channelId', String(channelId))
  if (body) form.append('body', body)
  if (replyTo != null) form.append('replyTo', String(replyTo))
  for (const f of files) form.append('files', f)
  // NOTE: don't set Content-Type — the browser sets the multipart boundary.
  const res = await fetch('/api/messages', {
    method: 'POST',
    headers: { Authorization: `Bearer ${token}` },
    body: form,
  })
  if (!res.ok && res.status !== 201) {
    const data = await res.json().catch(() => ({}))
    throw new Error((data as { error?: string }).error || `could not send (${res.status})`)
  }
}

// Fetch an access-gated attachment's bytes with the bearer token and return a blob
// (the caller wraps it in an object URL). Keeps the JWT out of any <img src>/URL.
export async function fetchAttachment(token: string, url: string): Promise<Blob> {
  const res = await fetch(url, { headers: { Authorization: `Bearer ${token}` } })
  if (!res.ok) throw new Error(`could not load attachment (${res.status})`)
  return res.blob()
}

// Upload the caller's own avatar (multipart, field "file"). The server derives the
// user from the JWT — you can only ever set your own. Throws with the server's error.
export async function uploadAvatar(token: string, file: File): Promise<void> {
  const form = new FormData()
  form.append('file', file)
  const res = await fetch('/api/avatar', {
    method: 'POST',
    headers: { Authorization: `Bearer ${token}` },
    body: form,
  })
  if (!res.ok) {
    const data = await res.json().catch(() => ({}))
    throw new Error((data as { error?: string }).error || `could not upload avatar (${res.status})`)
  }
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
