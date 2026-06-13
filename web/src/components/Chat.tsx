import { useEffect, useRef, useState, type FormEvent } from 'react'
import { createChannel, deleteMessage, editMessage, fetchChannels } from '../api'
import type { Channel, Message, ServerEvent, User } from '../types'

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
  const [channelId, setChannelId] = useState<number | null>(null)
  const [messages, setMessages] = useState<Message[]>([])
  const [online, setOnline] = useState(0)
  const [connected, setConnected] = useState(false)
  const [draft, setDraft] = useState('')
  const [editingId, setEditingId] = useState<number | null>(null)
  const [editDraft, setEditDraft] = useState('')
  const [typing, setTyping] = useState<string[]>([])
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
  }, [token])

  useEffect(() => {
    if (channelId == null) return
    setMessages([])
    setEditingId(null)
    setTyping([])
    const proto = location.protocol === 'https:' ? 'wss' : 'ws'
    const ws = new WebSocket(
      `${proto}://${location.host}/ws?token=${encodeURIComponent(token)}&channel=${channelId}`,
    )
    wsRef.current = ws
    ws.onopen = () => setConnected(true)
    ws.onclose = () => setConnected(false)
    ws.onmessage = (ev) => {
      const data = JSON.parse(ev.data) as ServerEvent
      if (data.type === 'history' && data.history) setMessages(data.history)
      else if (data.type === 'message' && data.message) {
        const m = data.message
        setMessages((prev) => [...prev, m])
      } else if (data.type === 'message-edited' && data.message) {
        const m = data.message
        setMessages((prev) => prev.map((x) => (x.id === m.id ? m : x)))
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

  const current = channels.find((c) => c.id === channelId)

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
      </aside>

      <div className="chat">
        <header className="chat-header">
          <div className="brand">
            Opencord <span className="channel">#{current?.name ?? '…'}</span>
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
          {messages.map((m) => (
            <div key={m.id} className={m.deleted ? 'message deleted' : 'message'}>
              <div className="avatar" style={{ backgroundColor: avatarColor(m.username) }} aria-hidden>
                {initials(m.username)}
              </div>
              <div className="message-content">
                <div className="message-head">
                  <span className="author">{m.username}</span>
                  <span className="time">{new Date(m.createdAt).toLocaleTimeString()}</span>
                  {m.editedAt && !m.deleted && <span className="edited">(edited)</span>}
                  {!m.deleted && m.userId === user.id && editingId !== m.id && (
                    <span className="msg-actions">
                      <button onClick={() => startEdit(m)}>edit</button>
                      <button onClick={() => remove(m)}>delete</button>
                    </span>
                  )}
                </div>
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
              </div>
            </div>
          ))}
          <div ref={bottomRef} />
        </main>

        {typing.length > 0 && (
          <div className="typing">
            {typing.join(', ')} {typing.length === 1 ? 'is' : 'are'} typing…
          </div>
        )}
        <form className="composer" onSubmit={send}>
          <input
            placeholder={connected ? `Message #${current?.name ?? ''}` : 'connecting…'}
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
