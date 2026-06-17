import { useEffect, useState } from 'react'
import { fetchAttachment } from '../api'

// Emoji are served auth-gated (GET /api/emoji/{id} needs the bearer token) and
// <img src> can't send headers — so fetch the bytes WITH the token and render a
// blob URL, cached by id (session-lived), exactly like Avatar does. Falls back to
// the literal :name: text while loading / on error so it's never invisible.
const cache = new Map<number, string | null>()

export function EmojiImg({ token, id, alt, className = 'emoji-inline' }: {
  token: string; id: number; alt: string; className?: string
}) {
  const [url, setUrl] = useState<string | null>(() => cache.get(id) ?? null)
  useEffect(() => {
    if (cache.has(id)) { setUrl(cache.get(id) ?? null); return }
    let cancelled = false
    fetchAttachment(token, `/api/emoji/${id}`)
      .then((blob) => { const u = URL.createObjectURL(blob); cache.set(id, u); if (!cancelled) setUrl(u) })
      .catch(() => { cache.set(id, null); if (!cancelled) setUrl(null) })
    return () => { cancelled = true }
  }, [token, id])
  if (url) return <img className={className} src={url} alt={alt} title={alt} data-emoji-id={id} />
  return <span className="emoji-fallback" data-emoji-id={id} title={alt}>{alt}</span>
}
