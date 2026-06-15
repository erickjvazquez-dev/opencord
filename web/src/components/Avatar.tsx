import { useEffect, useState } from 'react'
import { avatarColor, avatarTextColor, initials } from '../avatar'
import { fetchAttachment } from '../api'

// Module-level cache keyed by userId: an object URL (the user's avatar image) or
// null = "no avatar, render initials". Avoids refetching the same user's avatar for
// every message they posted. Persists for the session.
const cache = new Map<number, string | null>()

// Avatar renders a user's uploaded picture, falling back to deterministic initials
// when they have none (a 404 from the serve endpoint). The bytes are fetched WITH
// the bearer token (the serve endpoint is auth-gated), so no token leaks into the
// DOM. Pass `bust` (a changing number) to force a cache-busted refetch after the
// viewer changes their OWN avatar.
export function Avatar({
  token,
  userId,
  username,
  className = 'avatar',
  bust,
}: {
  token: string
  userId: number
  username: string
  className?: string
  bust?: number
}) {
  const [url, setUrl] = useState<string | null>(() => (bust ? null : (cache.get(userId) ?? null)))

  useEffect(() => {
    if (!bust && cache.has(userId)) {
      setUrl(cache.get(userId) ?? null)
      return
    }
    let cancelled = false
    const path = `/api/users/${userId}/avatar${bust ? `?v=${bust}` : ''}`
    fetchAttachment(token, path)
      .then((blob) => {
        const objUrl = URL.createObjectURL(blob)
        cache.set(userId, objUrl)
        if (!cancelled) setUrl(objUrl)
      })
      .catch(() => {
        cache.set(userId, null) // no avatar (404) → initials, and don't refetch
        if (!cancelled) setUrl(null)
      })
    return () => {
      cancelled = true
    }
  }, [token, userId, bust])

  if (url) {
    return <img className={`${className} avatar-img`} src={url} alt={username} />
  }
  return (
    <div
      className={className}
      style={{ backgroundColor: avatarColor(username), color: avatarTextColor(username) }}
      aria-hidden
    >
      {initials(username)}
    </div>
  )
}
