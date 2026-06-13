import { useEffect, useRef, useState, type FormEvent } from 'react'
import type { Message, ServerEvent, User } from '../types'

export function Chat({
  token,
  user,
  onLogout,
}: {
  token: string
  user: User
  onLogout: () => void
}) {
  const [messages, setMessages] = useState<Message[]>([])
  const [online, setOnline] = useState(0)
  const [connected, setConnected] = useState(false)
  const [draft, setDraft] = useState('')
  const wsRef = useRef<WebSocket | null>(null)
  const bottomRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const proto = location.protocol === 'https:' ? 'wss' : 'ws'
    const ws = new WebSocket(`${proto}://${location.host}/ws?token=${encodeURIComponent(token)}`)
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
  }, [token])

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

  return (
    <div className="chat">
      <header className="chat-header">
        <div className="brand">
          Opencord <span className="channel">#general</span>
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
          placeholder={connected ? 'Message #general' : 'connecting…'}
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          disabled={!connected}
        />
        <button disabled={!connected || !draft.trim()}>Send</button>
      </form>
    </div>
  )
}
