import type {
  Channel,
  ChannelCategory,
  ChannelUnread,
  DMChannel,
  Invite,
  Message,
  Role,
  Server,
  ServerBan,
  ServerEmoji,
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

// Leave a group DM (≥3 members). 204 on success; throws with the server's message otherwise.
export async function leaveGroupDM(token: string, dmId: number): Promise<void> {
  const res = await fetch(`/api/dms/${dmId}/leave`, {
    method: 'POST',
    headers: { Authorization: `Bearer ${token}` },
  })
  if (!res.ok && res.status !== 204) {
    const data = await res.json().catch(() => ({}))
    throw new Error((data as { error?: string }).error || 'could not leave the group DM')
  }
}

// Add a member (by username or user id) to a group DM. 204 on success.
export async function addGroupDMMember(
  token: string,
  dmId: number,
  identifier: string,
): Promise<void> {
  const res = await fetch(`/api/dms/${dmId}/members`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
    body: JSON.stringify({ identifier }),
  })
  if (!res.ok && res.status !== 204) {
    const data = await res.json().catch(() => ({}))
    throw new Error((data as { error?: string }).error || 'could not add to the group')
  }
}

// Rename a group DM (any member may rename). Pass an empty string to clear the name and
// fall back to the member-list title. 204 on success; throws with the server's message.
export async function renameGroupDM(token: string, dmId: number, name: string): Promise<void> {
  const res = await fetch(`/api/dms/${dmId}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
    body: JSON.stringify({ name }),
  })
  if (!res.ok && res.status !== 204) {
    const data = await res.json().catch(() => ({}))
    throw new Error((data as { error?: string }).error || 'could not rename the group')
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

// Voice presence across a server's channels (v0.9 slice 2): channelId → user ids in voice.
// Only channels with an active call appear. Powers the sidebar "🔊 N" badges.
export async function fetchServerVoicePresence(
  token: string,
  serverId: number,
): Promise<Record<number, number[]>> {
  const res = await fetch(`/api/servers/${serverId}/voice-presence`, {
    headers: { Authorization: `Bearer ${token}` },
  })
  if (!res.ok) throw new Error('could not load voice presence')
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

// --- Custom colored roles (v0.7) ---

// List a server's cosmetic colored roles (members), highest position first.
export async function listServerRoles(token: string, serverId: number): Promise<Role[]> {
  const res = await fetch(`/api/servers/${serverId}/custom-roles`, {
    headers: { Authorization: `Bearer ${token}` },
  })
  if (!res.ok) throw new Error('could not load roles')
  return res.json()
}

// Create a colored role {name, color, hoist} (admin). Returns the new role.
export async function createServerRole(
  token: string,
  serverId: number,
  name: string,
  color: string,
  hoist = false,
): Promise<Role> {
  const res = await fetch(`/api/servers/${serverId}/custom-roles`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
    body: JSON.stringify({ name, color, hoist }),
  })
  const data = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error((data as { error?: string }).error || 'could not create role')
  return data as Role
}

// Rename/recolor/re-hoist a role (admin).
export async function updateServerRole(
  token: string,
  serverId: number,
  roleId: number,
  name: string,
  color: string,
  hoist = false,
): Promise<void> {
  const res = await fetch(`/api/servers/${serverId}/custom-roles/${roleId}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
    body: JSON.stringify({ name, color, hoist }),
  })
  if (!res.ok && res.status !== 204) {
    const data = await res.json().catch(() => ({}))
    throw new Error((data as { error?: string }).error || 'could not update role')
  }
}

// Delete a role (admin); assignments cascade away.
export async function deleteServerRole(token: string, serverId: number, roleId: number): Promise<void> {
  const res = await fetch(`/api/servers/${serverId}/custom-roles/${roleId}`, {
    method: 'DELETE',
    headers: { Authorization: `Bearer ${token}` },
  })
  if (!res.ok && res.status !== 204) {
    const data = await res.json().catch(() => ({}))
    throw new Error((data as { error?: string }).error || 'could not delete role')
  }
}

// Assign (PUT) / unassign (DELETE) a role to/from a member (admin).
export async function assignServerRole(
  token: string,
  serverId: number,
  userId: number,
  roleId: number,
): Promise<void> {
  const res = await fetch(`/api/servers/${serverId}/members/${userId}/custom-roles/${roleId}`, {
    method: 'PUT',
    headers: { Authorization: `Bearer ${token}` },
  })
  if (!res.ok && res.status !== 204) {
    const data = await res.json().catch(() => ({}))
    throw new Error((data as { error?: string }).error || 'could not assign role')
  }
}

export async function unassignServerRole(
  token: string,
  serverId: number,
  userId: number,
  roleId: number,
): Promise<void> {
  const res = await fetch(`/api/servers/${serverId}/members/${userId}/custom-roles/${roleId}`, {
    method: 'DELETE',
    headers: { Authorization: `Bearer ${token}` },
  })
  if (!res.ok && res.status !== 204) {
    const data = await res.json().catch(() => ({}))
    throw new Error((data as { error?: string }).error || 'could not unassign role')
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

// The ids of channels the caller has muted (a muted channel never shows unread/mention/tab badges).
export async function fetchMutedChannels(token: string): Promise<number[]> {
  const res = await fetch('/api/muted-channels', { headers: { Authorization: `Bearer ${token}` } })
  if (!res.ok) return []
  const data = (await res.json()) as { channels?: number[] }
  return data.channels ?? []
}

// Mute or unmute a channel for the caller (POST = mute, DELETE = unmute).
export async function setChannelMuted(token: string, channelId: number, muted: boolean): Promise<void> {
  await fetch(`/api/channels/${channelId}/mute`, {
    method: muted ? 'POST' : 'DELETE',
    headers: { Authorization: `Bearer ${token}` },
  })
}

// Set the caller's own custom status + optional emoji ("" clears each). The server
// trims + caps both.
export async function setMyStatus(token: string, status: string, statusEmoji = ''): Promise<void> {
  const res = await fetch('/api/me/status', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
    body: JSON.stringify({ status, statusEmoji }),
  })
  if (!res.ok && res.status !== 204) {
    const data = await res.json().catch(() => ({}))
    throw new Error((data as { error?: string }).error || 'could not set status')
  }
}

// Fetch a user's PUBLIC profile (about, pronouns, status, presence) for the profile card.
// Returns null if not found / on error so the caller can no-op. The shape matches the
// fields ProfileCard reads (a ServerMember-compatible subset).
export async function fetchUserProfile(token: string, userId: number): Promise<ServerMember | null> {
  const res = await fetch(`/api/users/${userId}/profile`, {
    headers: { Authorization: `Bearer ${token}` },
  })
  if (!res.ok) return null
  return (await res.json()) as ServerMember
}

// Set the caller's own profile: About Me + pronouns ("" clears each). Capped server-side.
export async function setMyProfile(token: string, about: string, pronouns: string): Promise<void> {
  const res = await fetch('/api/me/profile', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
    body: JSON.stringify({ about, pronouns }),
  })
  if (!res.ok && res.status !== 204) {
    const data = await res.json().catch(() => ({}))
    throw new Error((data as { error?: string }).error || 'could not set profile')
  }
}

// Set the caller's own presence state (online | idle | dnd | invisible). The server
// normalizes any unknown value to "online".
export async function setMyPresence(token: string, presence: string): Promise<void> {
  const res = await fetch('/api/me/presence', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
    body: JSON.stringify({ presence }),
  })
  if (!res.ok && res.status !== 204) {
    const data = await res.json().catch(() => ({}))
    throw new Error((data as { error?: string }).error || 'could not set presence')
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

// List a server's custom emoji (members). Drives `:name:` → inline image rendering;
// the names are server-scoped, so this is fetched per active server.
export async function listServerEmoji(token: string, serverId: number): Promise<ServerEmoji[]> {
  const res = await fetch(`/api/servers/${serverId}/emoji`, {
    headers: { Authorization: `Bearer ${token}` },
  })
  if (!res.ok) throw new Error('could not load server emoji')
  return res.json()
}

// Upload a custom emoji to a server (admin-gated; multipart `name` + `file`). The
// server validates the name (lowercase `[a-z0-9_]{2,32}`, unique per server) and the
// image (≤256 KiB). Surfaces the server's error message on non-2xx (e.g. 409 name
// taken / 400 invalid / 413 too big). Returns the created record.
export async function uploadServerEmoji(
  token: string,
  serverId: number,
  name: string,
  file: File,
): Promise<ServerEmoji> {
  const form = new FormData()
  form.append('name', name)
  form.append('file', file)
  // NOTE: don't set Content-Type — the browser sets the multipart boundary.
  const res = await fetch(`/api/servers/${serverId}/emoji`, {
    method: 'POST',
    headers: { Authorization: `Bearer ${token}` },
    body: form,
  })
  const data = await res.json().catch(() => ({}))
  if (!res.ok && res.status !== 201) {
    throw new Error((data as { error?: string }).error || `could not upload emoji (${res.status})`)
  }
  return data as ServerEmoji
}

// Delete a server's custom emoji (admin-gated). Mirrors the other admin DELETEs.
export async function deleteServerEmoji(
  token: string,
  serverId: number,
  emojiId: number,
): Promise<void> {
  const res = await fetch(`/api/servers/${serverId}/emoji/${emojiId}`, {
    method: 'DELETE',
    headers: { Authorization: `Bearer ${token}` },
  })
  if (!res.ok && res.status !== 204) {
    const data = await res.json().catch(() => ({}))
    throw new Error((data as { error?: string }).error || 'could not delete emoji')
  }
}

export async function createServerChannel(
  token: string,
  serverId: number,
  name: string,
  categoryId?: number,
  kind?: 'public' | 'voice',
): Promise<Channel> {
  const body: { name: string; categoryId?: number; kind?: string } = { name }
  if (categoryId != null) body.categoryId = categoryId
  if (kind === 'voice') body.kind = 'voice' // omit for text channels (server defaults to public)
  const res = await fetch(`/api/servers/${serverId}/channels`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
    body: JSON.stringify(body),
  })
  const data = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error((data as { error?: string }).error || 'could not create channel')
  return data as Channel
}

// List a channel's threads (GET /api/channels/{id}/threads), newest first.
export async function fetchThreads(token: string, channelId: number): Promise<Channel[]> {
  const res = await fetch(`/api/channels/${channelId}/threads`, {
    headers: { Authorization: `Bearer ${token}` },
  })
  if (!res.ok) throw new Error('could not load threads')
  return res.json()
}

// Start a thread off a channel (POST /api/channels/{id}/threads {name, fromMessageId?}).
// fromMessageId anchors the thread to a message so its source shows a thread reference.
export async function createThread(
  token: string,
  channelId: number,
  name: string,
  fromMessageId?: number,
): Promise<Channel> {
  const res = await fetch(`/api/channels/${channelId}/threads`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
    body: JSON.stringify(fromMessageId == null ? { name } : { name, fromMessageId }),
  })
  const data = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error((data as { error?: string }).error || 'could not create thread')
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

// Fetch a page of history OLDER than `before` (a message id) for the channel — used to load more
// history when the reader pages up. Oldest-first within the page (same shape as the WS history);
// an empty array means the channel start has been reached.
export async function fetchMessagesBefore(
  token: string,
  channelId: number,
  before: number,
): Promise<Message[]> {
  const res = await fetch(`/api/messages?channel=${channelId}&before=${before}`, {
    headers: { Authorization: `Bearer ${token}` },
  })
  if (!res.ok) throw new Error(`could not load older messages (${res.status})`)
  return (await res.json()) as Message[]
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

// Block a user (POST → 204). Server-enforced symmetric DM block; identity is derived
// from the JWT (never the payload). Idempotent on the server (re-blocking is also 204).
export async function blockUser(token: string, userId: number): Promise<void> {
  const res = await fetch(`/api/users/${userId}/block`, {
    method: 'POST',
    headers: { Authorization: `Bearer ${token}` },
  })
  if (!res.ok && res.status !== 204) {
    const data = await res.json().catch(() => ({}))
    throw new Error((data as { error?: string }).error || 'could not block user')
  }
}

// Unblock a user (DELETE → 204).
export async function unblockUser(token: string, userId: number): Promise<void> {
  const res = await fetch(`/api/users/${userId}/block`, {
    method: 'DELETE',
    headers: { Authorization: `Bearer ${token}` },
  })
  if (!res.ok && res.status !== 204) {
    const data = await res.json().catch(() => ({}))
    throw new Error((data as { error?: string }).error || 'could not unblock user')
  }
}

// The users I've blocked (id + username), so the client can hide their messages and
// render a manageable block list.
export async function listBlocked(token: string): Promise<{ id: number; username: string }[]> {
  const res = await fetch('/api/me/blocks', { headers: { Authorization: `Bearer ${token}` } })
  if (!res.ok) throw new Error('could not load blocked users')
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

// Start a group DM with `identifiers` (each a username or numeric user id). One identifier
// resolves to the idempotent 1:1; 2..9 make a group. The server rejects a block with any
// member, unknown users, and the 10-member cap.
export async function createGroupDM(token: string, identifiers: string[]): Promise<DMChannel> {
  const res = await fetch('/api/dms/group', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
    body: JSON.stringify({ identifiers }),
  })
  const data = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error((data as { error?: string }).error || 'could not create group DM')
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
