import { useEffect, useRef, useState, type FormEvent } from 'react'
import {
  addReaction,
  createChannel,
  deleteMessage,
  editMessage,
  fetchChannels,
  fetchDMs,
  openDM,
  removeReaction,
} from '../api'
import type { Channel, DMChannel, Message, Reaction, ServerEvent, User } from '../types'

// Quick-react palette (Discord-style). Small by design; a full picker is later.
const QUICK_EMOJIS = ['👍', '❤️', '😂', '🎉', '😮', '😢']
// Local key for "the viewer reacted with this emoji on this message".
const rkey = (msgId: number, emoji: string) => `${msgId}:${emoji}`

// Apply a count delta for one emoji to a message's reaction list (immutably):
// inserts a chip when adding the first, drops it when the last is removed.
function applyDelta(reactions: Reaction[] | undefined, emoji: string, delta: number): Reaction[] {
  const out = [...(reactions ?? [])]
  const i = out.findIndex((r) => r.emoji === emoji)
  if (i >= 0) {
    const count = out[i].count + delta
    if (count <= 0) out.splice(i, 1)
    else out[i] = { ...out[i], count }
  } else if (delta > 0) {
    out.push({ emoji, count: delta })
  }
  return out
}

// Deterministic avatar color + initials from a username (no uploaded avatars yet).
function avatarColor(name: string): string {
  let h = 0
  for (let i = 0; i < name.length; i++) h = (h * 31 + name.charCodeAt(i)) % 360
  return `hsl(${h}, 55%, 45%)`
}
function initials(name: string): string {
  return name.slice(0, 2).toUpperCase()
}

export function Chat({
  token,
  user,
  onLogout,
}: {
  token: string
  user: User
  onLogout: () => void
}) {
  const [channels, setChannels] = useState<Channel[]>([])
  const [dms, setDms] = useState<DMChannel[]>([])
  const [channelId, setChannelId] = useState<number | null>(null)
  const [messages, setMessages] = useState<Message[]>([])
  const [online, setOnline] = useState(0)
  const [connected, setConnected] = useState(false)
  const [draft, setDraft] = useState('')
  const [editingId, setEditingId] = useState<number | null>(null)
  const [editDraft, setEditDraft] = useState('')
  const [typing, setTyping] = useState<string[]>([])
  // Emojis the viewer has reacted with, as "msgId:emoji" keys. The live `reaction`
  // broadcast is count-only (mine=false), so we own this locally; seeded from history.
  const [myReactions, setMyReactions] = useState<Set<string>>(new Set())
  const [pickerFor, setPickerFor] = useState<number | null>(null)
  const wsRef = useRef<WebSocket | null>(null)
  const bottomRef = useRef<HTMLDivElement>(null)
  const typingTimers = useRef<Record<string, ReturnType<typeof setTimeout>>>({})
  const lastTypingSent = useRef(0)

  useEffect(() => {
    fetchChannels(token)
      .then((cs) => {
        setChannels(cs)
        setChannelId((cur) => cur ?? cs.find((c) => c.name === 'general')?.id ?? cs[0]?.id ?? null)
      })
      .catch(() => {})
    fetchDMs(token).then(setDms).catch(() => {})
  }, [token])

  useEffect(() => {
    if (channelId == null) return
    setMessages([])
    setEditingId(null)
    setTyping([])
    setMyReactions(new Set())
    setPickerFor(null)
    const proto = location.protocol === 'https:' ? 'wss' : 'ws'
    const ws = new WebSocket(
      `${proto}://${location.host}/ws?token=${encodeURIComponent(token)}&channel=${channelId}`,
    )
    wsRef.current = ws
    ws.onopen = () => setConnected(true)
    ws.onclose = () => setConnected(false)
    ws.onmessage = (ev) => {
      const data = JSON.parse(ev.data) as ServerEvent
      if (data.type === 'history' && data.history) {
        const hist = data.history
        setMessages(hist)
        // Seed "mine" from the server's per-viewer flags (history is viewer-scoped).
        const mine = new Set<string>()
        for (const m of hist) for (const r of m.reactions ?? []) if (r.mine) mine.add(rkey(m.id, r.emoji))
        setMyReactions(mine)
      } else if (data.type === 'message' && data.message) {
        const m = data.message
        setMessages((prev) => [...prev, m])
      } else if (data.type === 'message-edited' && data.message) {
        const m = data.message
        // Server's edited message carries no reactions — keep the ones we have.
        setMessages((prev) =>
          prev.map((x) => (x.id === m.id ? { ...x, ...m, reactions: m.reactions ?? x.reactions } : x)),
        )
      } else if (data.type === 'reaction' && data.message) {
        // Count-only broadcast: take the server's authoritative counts; "mine" is
        // overlaid from myReactions at render time.
        const m = data.message
        setMessages((prev) =>
          prev.map((x) => (x.id === m.id ? { ...x, reactions: m.reactions ?? [] } : x)),
        )
      } else if (data.type === 'message-deleted' && data.message) {
        const id = data.message.id
        setMessages((prev) =>
          prev.map((x) => (x.id === id ? { ...x, deleted: true, body: '[deleted]' } : x)),
        )
      } else if (data.type === 'typing' && data.username && data.username !== user.username) {
        const who = data.username
        setTyping((prev) => (prev.includes(who) ? prev : [...prev, who]))
        clearTimeout(typingTimers.current[who])
        typingTimers.current[who] = setTimeout(() => {
          setTyping((prev) => prev.filter((u) => u !== who))
          delete typingTimers.current[who]
        }, 3000)
      } else if (data.type === 'presence') setOnline(data.online ?? 0)
    }
    return () => {
      ws.close()
      Object.values(typingTimers.current).forEach(clearTimeout)
      typingTimers.current = {}
    }
  }, [token, channelId, user.username])

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [messages])

  const send = (e: FormEvent) => {
    e.preventDefault()
    const body = draft.trim()
    if (!body || wsRef.current?.readyState !== WebSocket.OPEN) return
    wsRef.current.send(JSON.stringify({ body }))
    setDraft('')
  }

  const addChannel = async () => {
    const name = window.prompt('New channel name (2-32 chars: a-z, 0-9, _ or -):')?.trim()
    if (!name) return
    try {
      const c = await createChannel(token, name)
      setChannels((cs) => [...cs, c])
      setChannelId(c.id)
    } catch (err) {
      window.alert(err instanceof Error ? err.message : 'could not create channel')
    }
  }

  const startDM = async () => {
    const username = window.prompt('Direct message which user? (their username)')?.trim()
    if (!username) return
    try {
      const dm = await openDM(token, username)
      setDms((cur) => (cur.some((d) => d.id === dm.id) ? cur : [...cur, dm]))
      setChannelId(dm.id)
    } catch (err) {
      window.alert(err instanceof Error ? err.message : 'could not open DM')
    }
  }

  const startEdit = (m: Message) => {
    setEditingId(m.id)
    setEditDraft(m.body)
  }
  const cancelEdit = () => {
    setEditingId(null)
    setEditDraft('')
  }
  const submitEdit = async (id: number) => {
    const body = editDraft.trim()
    if (!body) return
    try {
      await editMessage(token, id, body) // the WS broadcast updates the list
    } catch (err) {
      window.alert(err instanceof Error ? err.message : 'could not edit message')
    } finally {
      cancelEdit()
    }
  }
  const remove = async (m: Message) => {
    if (!window.confirm('Delete this message?')) return
    try {
      await deleteMessage(token, m.id) // the WS broadcast marks it deleted
    } catch (err) {
      window.alert(err instanceof Error ? err.message : 'could not delete message')
    }
  }

  // Toggle the viewer's reaction on a message. Optimistically flips local "mine"
  // and nudges the count; the WS `reaction` broadcast reconciles counts. Reverts
  // on HTTP failure.
  const toggleReaction = async (m: Message, emoji: string) => {
    const key = rkey(m.id, emoji)
    const mineNow = myReactions.has(key)
    const delta = mineNow ? -1 : 1
    setMyReactions((prev) => {
      const next = new Set(prev)
      if (mineNow) next.delete(key)
      else next.add(key)
      return next
    })
    setMessages((prev) =>
      prev.map((x) => (x.id === m.id ? { ...x, reactions: applyDelta(x.reactions, emoji, delta) } : x)),
    )
    try {
      if (mineNow) await removeReaction(token, m.id, emoji)
      else await addReaction(token, m.id, emoji)
    } catch (err) {
      setMyReactions((prev) => {
        const next = new Set(prev)
        if (mineNow) next.add(key)
        else next.delete(key)
        return next
      })
      setMessages((prev) =>
        prev.map((x) =>
          x.id === m.id ? { ...x, reactions: applyDelta(x.reactions, emoji, -delta) } : x,
        ),
      )
      window.alert(err instanceof Error ? err.message : 'could not update reaction')
    }
  }

  const current = channels.find((c) => c.id === channelId)
  const activeDM = dms.find((d) => d.id === channelId)

  return (
    <div className="app">
      <aside className="sidebar">
        <div className="sidebar-head">Channels</div>
        <nav className="channel-list">
          {channels.map((c) => (
            <button
              key={c.id}
              className={c.id === channelId ? 'channel-item active' : 'channel-item'}
              onClick={() => setChannelId(c.id)}
            >
              <span className="hash">#</span>
              {c.name}
            </button>
          ))}
        </nav>
        <button className="add-channel" onClick={addChannel}>
          + New channel
        </button>

        <div className="sidebar-head">Direct Messages</div>
        <nav className="channel-list dm-list">
          {dms.map((d) => (
            <button
              key={d.id}
              className={d.id === channelId ? 'channel-item active' : 'channel-item'}
              onClick={() => setChannelId(d.id)}
            >
              <span
                className="dm-avatar"
                style={{ backgroundColor: avatarColor(d.user.username) }}
                aria-hidden
              >
                {initials(d.user.username)}
              </span>
              {d.user.username}
            </button>
          ))}
        </nav>
        <button className="add-channel" onClick={startDM}>
          + New DM
        </button>
      </aside>

      <div className="chat">
        <header className="chat-header">
          <div className="brand">
            Opencord{' '}
            <span className="channel">
              {activeDM ? `@${activeDM.user.username}` : `#${current?.name ?? '…'}`}
            </span>
          </div>
          <div className="meta">
            <span className={connected ? 'dot online' : 'dot offline'} />
            {online} online · {user.username}
            <button className="link" onClick={onLogout}>
              log out
            </button>
          </div>
        </header>

        <main className="messages">
          {messages.map((m, i) => {
            const prev = i > 0 ? messages[i - 1] : null
            // Group consecutive messages from the same author within 5 min (Discord-style):
            // hide the repeated avatar + name. A deleted message breaks the run.
            const grouped =
              !!prev &&
              !m.deleted &&
              !prev.deleted &&
              prev.userId === m.userId &&
              new Date(m.createdAt).getTime() - new Date(prev.createdAt).getTime() < 5 * 60 * 1000
            return (
              <div
                key={m.id}
                className={`message${m.deleted ? ' deleted' : ''}${grouped ? ' grouped' : ''}`}
              >
                {grouped ? (
                  <div className="avatar-spacer" aria-hidden />
                ) : (
                  <div
                    className="avatar"
                    style={{ backgroundColor: avatarColor(m.username) }}
                    aria-hidden
                  >
                    {initials(m.username)}
                  </div>
                )}
                <div className="message-content">
                  {!grouped && (
                    <div className="message-head">
                      <span className="author">{m.username}</span>
                      <span className="time">{new Date(m.createdAt).toLocaleTimeString()}</span>
                      {m.editedAt && !m.deleted && <span className="edited">(edited)</span>}
                    </div>
                  )}
                  {!m.deleted && editingId !== m.id && (
                    <span className="msg-actions">
                      {m.userId === user.id && (
                        <>
                          <button onClick={() => startEdit(m)}>edit</button>
                          <button onClick={() => remove(m)}>delete</button>
                        </>
                      )}
                      <button onClick={() => setPickerFor((p) => (p === m.id ? null : m.id))}>
                        react
                      </button>
                    </span>
                  )}
                  {editingId === m.id ? (
                    <div className="edit-row">
                      <input
                        autoFocus
                        value={editDraft}
                        onChange={(e) => setEditDraft(e.target.value)}
                        onKeyDown={(e) => {
                          if (e.key === 'Enter') void submitEdit(m.id)
                          else if (e.key === 'Escape') cancelEdit()
                        }}
                      />
                      <button onClick={() => void submitEdit(m.id)}>save</button>
                      <button className="link" onClick={cancelEdit}>
                        cancel
                      </button>
                    </div>
                  ) : (
                    <div className="body">{m.body}</div>
                  )}
                  {pickerFor === m.id && !m.deleted && (
                    <div className="emoji-picker">
                      {QUICK_EMOJIS.map((e) => (
                        <button
                          key={e}
                          className="emoji-option"
                          onClick={() => {
                            void toggleReaction(m, e)
                            setPickerFor(null)
                          }}
                        >
                          {e}
                        </button>
                      ))}
                    </div>
                  )}
                  {!m.deleted && (m.reactions?.length ?? 0) > 0 && (
                    <div className="reactions">
                      {m.reactions!.map((r) => {
                        const mine = myReactions.has(rkey(m.id, r.emoji))
                        return (
                          <button
                            key={r.emoji}
                            className={mine ? 'reaction mine' : 'reaction'}
                            aria-pressed={mine}
                            onClick={() => void toggleReaction(m, r.emoji)}
                          >
                            <span className="emoji">{r.emoji}</span>
                            <span className="rcount">{r.count}</span>
                          </button>
                        )
                      })}
                    </div>
                  )}
                </div>
              </div>
            )
          })}
          <div ref={bottomRef} />
        </main>

        {typing.length > 0 && (
          <div className="typing">
            {typing.join(', ')} {typing.length === 1 ? 'is' : 'are'} typing…
          </div>
        )}
        <form className="composer" onSubmit={send}>
          <input
            placeholder={
              connected
                ? activeDM
                  ? `Message @${activeDM.user.username}`
                  : `Message #${current?.name ?? ''}`
                : 'connecting…'
            }
            value={draft}
            onChange={(e) => {
              setDraft(e.target.value)
              const now = Date.now()
              if (wsRef.current?.readyState === WebSocket.OPEN && now - lastTypingSent.current > 2000) {
                lastTypingSent.current = now
                wsRef.current.send(JSON.stringify({ type: 'typing' }))
              }
            }}
            disabled={!connected}
          />
          <button disabled={!connected || !draft.trim()}>Send</button>
        </form>
      </div>
    </div>
  )
}
