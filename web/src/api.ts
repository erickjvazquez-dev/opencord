import type {
  Channel,
  ChannelCategory,
  ChannelUnread,
  DMChannel,
  Invite,
  Message,
  Server,
  ServerBan,
  ServerMember,
  User,
} from './types'

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

// Rename a server (owner/admin only; the server enforces who may act).
export async function renameServer(
  token: string,
  serverId: number,
  name: string,
): Promise<Server> {
  const res = await fetch(`/api/servers/${serverId}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
    body: JSON.stringify({ name }),
  })
  const data = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error((data as { error?: string }).error || 'could not rename server')
  return data as Server
}

// Delete a server (owner only; destructive — the server enforces it).
export async function deleteServer(token: string, serverId: number): Promise<void> {
  const res = await fetch(`/api/servers/${serverId}`, {
    method: 'DELETE',
    headers: { Authorization: `Bearer ${token}` },
  })
  if (!res.ok && res.status !== 204) {
    const data = await res.json().catch(() => ({}))
    throw new Error((data as { error?: string }).error || 'could not delete server')
  }
}

// Leave a server (any non-owner member; the owner can't — the server enforces it).
export async function leaveServer(token: string, serverId: number): Promise<void> {
  const res = await fetch(`/api/servers/${serverId}/leave`, {
    method: 'POST',
    headers: { Authorization: `Bearer ${token}` },
  })
  if (!res.ok && res.status !== 204) {
    const data = await res.json().catch(() => ({}))
    throw new Error((data as { error?: string }).error || 'could not leave server')
  }
}

// Mint an invite. maxUses (optional, 1–1000) caps how many members the code admits;
// omit it for an unlimited code (the default).
export async function createInvite(
  token: string,
  serverId: number,
  maxUses?: number,
): Promise<string> {
  const res = await fetch(`/api/servers/${serverId}/invites`, {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${token}`,
      ...(maxUses != null ? { 'Content-Type': 'application/json' } : {}),
    },
    body: maxUses != null ? JSON.stringify({ maxUses }) : undefined,
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

// List a server's active invite codes (admin-gated; a non-admin gets 403).
export async function fetchServerInvites(token: string, serverId: number): Promise<Invite[]> {
  const res = await fetch(`/api/servers/${serverId}/invites`, {
    headers: { Authorization: `Bearer ${token}` },
  })
  if (!res.ok) {
    const data = await res.json().catch(() => ({}))
    throw new Error((data as { error?: string }).error || 'could not load invites')
  }
  return res.json()
}

// Revoke an invite code so it can no longer be redeemed (admin-gated).
export async function revokeServerInvite(
  token: string,
  serverId: number,
  code: string,
): Promise<void> {
  const res = await fetch(`/api/servers/${serverId}/invites/${encodeURIComponent(code)}`, {
    method: 'DELETE',
    headers: { Authorization: `Bearer ${token}` },
  })
  if (!res.ok && res.status !== 204) {
    const data = await res.json().catch(() => ({}))
    throw new Error((data as { error?: string }).error || 'could not revoke invite')
  }
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

// Transfer server ownership to another member (owner only; server enforces it).
export async function transferServerOwnership(
  token: string,
  serverId: number,
  userId: number,
): Promise<void> {
  const res = await fetch(`/api/servers/${serverId}/transfer`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
    body: JSON.stringify({ userId }),
  })
  if (!res.ok && res.status !== 204) {
    const data = await res.json().catch(() => ({}))
    throw new Error((data as { error?: string }).error || 'could not transfer ownership')
  }
}

// The caller's accessible channels that have unread messages, each with a count of
// unread @-mentions (drives sidebar dots + the red mention badge).
export async function fetchUnreads(token: string): Promise<ChannelUnread[]> {
  const res = await fetch('/api/unreads', { headers: { Authorization: `Bearer ${token}` } })
  if (!res.ok) return []
  const data = (await res.json()) as { channels?: ChannelUnread[] }
  return data.channels ?? []
}

// Mark a channel read up to its latest message (fire-and-forget).
export async function markChannelRead(token: string, channelId: number): Promise<void> {
  await fetch(`/api/channels/${channelId}/read`, {
    method: 'POST',
    headers: { Authorization: `Bearer ${token}` },
  }).catch(() => {})
}

// Set the caller's own custom status ("" clears it). The server trims + caps it.
export async function setMyStatus(token: string, status: string): Promise<void> {
  const res = await fetch('/api/me/status', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
    body: JSON.stringify({ status }),
  })
  if (!res.ok && res.status !== 204) {
    const data = await res.json().catch(() => ({}))
    throw new Error((data as { error?: string }).error || 'could not set status')
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

// Ban a member from a server (owner/admin only). Removes them AND blocks rejoining
// until unbanned; the server enforces who may act and who may be banned.
export async function banServerMember(
  token: string,
  serverId: number,
  userId: number,
  reason: string,
): Promise<void> {
  const res = await fetch(`/api/servers/${serverId}/bans`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
    body: JSON.stringify({ userId, reason }),
  })
  if (!res.ok && res.status !== 204) {
    const data = await res.json().catch(() => ({}))
    throw new Error((data as { error?: string }).error || 'could not ban member')
  }
}

// Lift a member's ban (owner/admin only) so they may rejoin via an invite.
export async function unbanServerMember(
  token: string,
  serverId: number,
  userId: number,
): Promise<void> {
  const res = await fetch(`/api/servers/${serverId}/bans/${userId}`, {
    method: 'DELETE',
    headers: { Authorization: `Bearer ${token}` },
  })
  if (!res.ok && res.status !== 204) {
    const data = await res.json().catch(() => ({}))
    throw new Error((data as { error?: string }).error || 'could not unban member')
  }
}

// List a server's banned users (admin-gated; a non-admin gets 403).
export async function fetchServerBans(token: string, serverId: number): Promise<ServerBan[]> {
  const res = await fetch(`/api/servers/${serverId}/bans`, {
    headers: { Authorization: `Bearer ${token}` },
  })
  if (!res.ok) {
    const data = await res.json().catch(() => ({}))
    throw new Error((data as { error?: string }).error || 'could not load bans')
  }
  return res.json()
}

// Timeout (temporarily mute) a member for durationSeconds (owner/admin only). The
// server clamps the duration; a muted member can't post until it expires or is cleared.
export async function timeoutServerMember(
  token: string,
  serverId: number,
  userId: number,
  durationSeconds: number,
): Promise<void> {
  const res = await fetch(`/api/servers/${serverId}/timeouts`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
    body: JSON.stringify({ userId, durationSeconds }),
  })
  if (!res.ok && res.status !== 204) {
    const data = await res.json().catch(() => ({}))
    throw new Error((data as { error?: string }).error || 'could not time out member')
  }
}

// Clear a member's timeout early (owner/admin only).
export async function clearMemberTimeout(
  token: string,
  serverId: number,
  userId: number,
): Promise<void> {
  const res = await fetch(`/api/servers/${serverId}/timeouts/${userId}`, {
    method: 'DELETE',
    headers: { Authorization: `Bearer ${token}` },
  })
  if (!res.ok && res.status !== 204) {
    const data = await res.json().catch(() => ({}))
    throw new Error((data as { error?: string }).error || 'could not clear timeout')
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
  categoryId?: number,
): Promise<Channel> {
  const res = await fetch(`/api/servers/${serverId}/channels`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
    body: JSON.stringify(categoryId == null ? { name } : { name, categoryId }),
  })
  const data = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error((data as { error?: string }).error || 'could not create channel')
  return data as Channel
}

// List a server's channel categories (members).
export async function fetchChannelCategories(
  token: string,
  serverId: number,
): Promise<ChannelCategory[]> {
  const res = await fetch(`/api/servers/${serverId}/categories`, {
    headers: { Authorization: `Bearer ${token}` },
  })
  if (!res.ok) throw new Error('could not load categories')
  return res.json()
}

// Create a channel category (admin-gated).
export async function createChannelCategory(
  token: string,
  serverId: number,
  name: string,
): Promise<ChannelCategory> {
  const res = await fetch(`/api/servers/${serverId}/categories`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
    body: JSON.stringify({ name }),
  })
  const data = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error((data as { error?: string }).error || 'could not create category')
  return data as ChannelCategory
}

// Delete a category (admin-gated). Its channels survive and become uncategorized.
export async function deleteChannelCategory(
  token: string,
  serverId: number,
  categoryId: number,
): Promise<void> {
  const res = await fetch(`/api/servers/${serverId}/categories/${categoryId}`, {
    method: 'DELETE',
    headers: { Authorization: `Bearer ${token}` },
  })
  if (!res.ok && res.status !== 204) {
    const data = await res.json().catch(() => ({}))
    throw new Error((data as { error?: string }).error || 'could not delete category')
  }
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
): Promise<{
  sfu: boolean
  url?: string
  room?: string
  token?: string
  iceServers?: RTCIceServer[]
}> {
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
