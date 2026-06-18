import { useEffect, useRef, useState } from 'react'
import { createGroupDM } from '../api'
import { parseIdentifiers } from '../dm'
import type { DMChannel } from '../types'

// NewGroupModal is the Discord-style "create DM / group" dialog. You add people by typing a
// username or user id and pressing Enter or comma (each becomes a removable chip); one
// person makes a 1:1, two or more make a group (the server caps a group at 10 members).
// Reuses the settings-overlay backdrop; Esc, an overlay click, or ✕ closes it. The server
// re-validates every identifier — unknown user, a block, or the cap surface as an inline error.
const MAX_OTHERS = 9

export function NewGroupModal({
  token,
  onClose,
  onCreated,
}: {
  token: string
  onClose: () => void
  onCreated: (dm: DMChannel) => void
}) {
  const [members, setMembers] = useState<string[]>([])
  const [draft, setDraft] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    inputRef.current?.focus()
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  // Commit whatever's in the draft (and any comma/space-separated paste) into chips, deduped
  // against the existing list and capped. Returns the resulting member list.
  const commitDraft = (): string[] => {
    const next = [...members]
    for (const id of parseIdentifiers(draft)) {
      if (!next.includes(id) && next.length < MAX_OTHERS) next.push(id)
    }
    setMembers(next)
    setDraft('')
    return next
  }

  const onDraftKey = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter' || e.key === ',') {
      e.preventDefault()
      commitDraft()
    } else if (e.key === 'Backspace' && draft === '' && members.length > 0) {
      setMembers(members.slice(0, -1))
    }
  }

  const removeMember = (id: string) => setMembers(members.filter((m) => m !== id))

  const submit = async () => {
    const all = commitDraft()
    if (all.length === 0) {
      setError('Add at least one person.')
      return
    }
    setBusy(true)
    setError(null)
    try {
      const dm = await createGroupDM(token, all)
      onCreated(dm)
      onClose()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'could not create group DM')
      setBusy(false)
    }
  }

  // The pending count includes a non-empty draft that hasn't been turned into a chip yet.
  const pending = members.length + (draft.trim() ? 1 : 0)
  const createLabel = pending > 1 ? 'Create Group' : 'Create DM'

  return (
    <div className="settings-overlay" role="dialog" aria-modal="true" aria-label="New direct message" onClick={onClose}>
      <div className="group-modal" onClick={(e) => e.stopPropagation()}>
        <button className="settings-close" onClick={onClose} aria-label="Close">
          ✕
        </button>
        <h2 className="group-modal-title">New Direct Message</h2>
        <p className="group-modal-sub">Add one person, or several for a group (up to {MAX_OTHERS}).</p>

        <div className="group-chips" onClick={() => inputRef.current?.focus()}>
          {members.map((m) => (
            <span className="group-chip" key={m}>
              {m}
              <button
                type="button"
                className="group-chip-x"
                onClick={() => removeMember(m)}
                aria-label={`Remove ${m}`}
              >
                ✕
              </button>
            </span>
          ))}
          <input
            ref={inputRef}
            className="group-chip-input"
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={onDraftKey}
            placeholder={members.length ? 'Add another…' : 'Username or user ID'}
            disabled={busy || members.length >= MAX_OTHERS}
            aria-label="Add a person by username or id"
          />
        </div>

        {error && (
          <p className="group-modal-error" role="alert">
            {error}
          </p>
        )}

        <div className="group-modal-actions">
          <button className="settings-btn" onClick={onClose} disabled={busy}>
            Cancel
          </button>
          <button className="settings-btn primary" onClick={submit} disabled={busy || pending === 0}>
            {busy ? 'Creating…' : createLabel}
          </button>
        </div>
      </div>
    </div>
  )
}
