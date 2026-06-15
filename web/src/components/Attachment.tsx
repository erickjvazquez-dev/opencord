import { useEffect, useState } from 'react'
import type { Attachment } from '../types'
import { fetchAttachment } from '../api'

// Only these sniffed types render inline as an image; everything else is a download
// chip (mirrors the server's inline allowlist).
const INLINE_IMAGE = /^image\/(png|jpeg|gif|webp)$/

// humanSize renders a byte count compactly (e.g. "2.4 MB").
function humanSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  const units = ['KB', 'MB', 'GB']
  let v = bytes / 1024
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v.toFixed(v < 10 ? 1 : 0)} ${units[i]}`
}

// AttachmentView renders one attachment: an inline image (for the image allowlist)
// or a downloadable file chip. Both fetch the bytes WITH the bearer token (the URL
// is access-gated) and use an object URL, so the session JWT never appears in the DOM.
export function AttachmentView({ token, attachment }: { token: string; attachment: Attachment }) {
  const [objectUrl, setObjectUrl] = useState<string | null>(null)
  const [failed, setFailed] = useState(false)
  const isImage = INLINE_IMAGE.test(attachment.contentType)

  // Eagerly load images so they render inline; non-images load on click (download).
  useEffect(() => {
    if (!isImage) return
    let created: string | null = null
    let cancelled = false
    fetchAttachment(token, attachment.url)
      .then((blob) => {
        if (cancelled) return
        created = URL.createObjectURL(blob)
        setObjectUrl(created)
      })
      .catch(() => {
        if (!cancelled) setFailed(true)
      })
    return () => {
      cancelled = true
      if (created) URL.revokeObjectURL(created)
    }
  }, [token, attachment.url, isImage])

  const download = async () => {
    try {
      const blob = await fetchAttachment(token, attachment.url)
      const u = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = u
      a.download = attachment.filename
      document.body.appendChild(a)
      a.click()
      a.remove()
      setTimeout(() => URL.revokeObjectURL(u), 10_000)
    } catch {
      setFailed(true)
    }
  }

  if (isImage && !failed) {
    return (
      <a
        className="attachment-image-link"
        href={objectUrl ?? undefined}
        target="_blank"
        rel="noreferrer"
        aria-label={`open image ${attachment.filename}`}
      >
        {objectUrl ? (
          <img
            className="attachment-image"
            src={objectUrl}
            alt={attachment.filename}
            loading="lazy"
          />
        ) : (
          <span className="attachment-loading">loading image…</span>
        )}
      </a>
    )
  }

  return (
    <button type="button" className="attachment-file" onClick={() => void download()}>
      <span className="attachment-icon" aria-hidden>
        📎
      </span>
      <span className="attachment-meta">
        <span className="attachment-name">{attachment.filename}</span>
        <span className="attachment-size">
          {humanSize(attachment.size)}
          {failed ? ' · failed' : ''}
        </span>
      </span>
    </button>
  )
}

// AttachmentList renders a message's attachments below its body.
export function AttachmentList({
  token,
  attachments,
}: {
  token: string
  attachments: Attachment[]
}) {
  if (!attachments.length) return null
  return (
    <div className="attachments">
      {attachments.map((a) => (
        <AttachmentView key={a.id} token={token} attachment={a} />
      ))}
    </div>
  )
}
