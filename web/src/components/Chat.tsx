import { useEffect, useRef, useState, type FormEvent } from 'react'
import { createChannel, fetchChannels } from '../api'
import type { Channel, Message, ServerEvent, User } from '../types'

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
  const wsRef = useRef<WebSocket | null>(null)
  const bottomRef = useRef<HTMLDivElement>(null)

  // Load the channel list once, then default to #general (or the first channel).
  useEffect(() => {
    fetchChannels(token)
      .then((cs) => {
        setChannels(cs)
        setChannelId((cur) => cur ?? cs.find((c) => c.name === 'general')?.id ?? cs[0]?.id ?? null)
      })
      .catch(() => {})
  }, [token])

  // (Re)connect whenever the selected channel changes — the new socket replays
  // that channel's history, and per-channel routing keeps rooms isolated.
  useEffect(() => {
    if (channelId == null) return
    setMessages([])
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
      } else if (data.type === 'presence') setOnline(data.online ?? 0)
    }
    return () => ws.close()
  }, [token, channelId])

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
            <div key={m.id} className="message">
              <div className="message-head">
                <span className="author">{m.username}</span>
                <span className="time">{new Date(m.createdAt).toLocaleTimeString()}</span>
              </div>
              <div className="body">{m.body}</div>
            </div>
          ))}
          <div ref={bottomRef} />
        </main>

        <form className="composer" onSubmit={send}>
          <input
            placeholder={connected ? `Message #${current?.name ?? ''}` : 'connecting…'}
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            disabled={!connected}
          />
          <button disabled={!connected || !draft.trim()}>Send</button>
        </form>
      </div>
    </div>
  )
}
