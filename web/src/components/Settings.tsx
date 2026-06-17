import { useEffect, useRef, useState } from 'react'
import { Avatar } from './Avatar'
import type { User } from '../types'

type Tab = 'account' | 'voice'

// Settings is the Discord-style User Settings overlay. It consolidates the account
// controls that used to be scattered in the chat header (avatar, custom status +
// emoji, presence) behind a single ⚙ entry point, with a left tab rail. No new
// endpoints — it reuses the handlers passed down from Chat (POST /avatar via
// onAvatarPicked, PUT /me/status via onSaveStatus, PUT /me/presence via
// onChangePresence). Esc or an overlay click closes it.
export function Settings({
  token,
  user,
  myPresence,
  myStatus,
  myStatusEmoji,
  avatarVersion,
  onAvatarPicked,
  onSaveStatus,
  onChangePresence,
  onClose,
}: {
  token: string
  user: User
  myPresence: string
  myStatus: string
  myStatusEmoji: string
  avatarVersion: number
  onAvatarPicked: (file: File | undefined) => void | Promise<void>
  onSaveStatus: (status: string, emoji: string) => void | Promise<void>
  onChangePresence: (next: string) => void | Promise<void>
  onClose: () => void
}) {
  const [tab, setTab] = useState<Tab>('account')
  // Local, editable copies of the status fields; seeded from props and re-synced
  // if the upstream value changes while the modal is open. Save commits them.
  const [statusText, setStatusText] = useState(myStatus)
  const [statusEmoji, setStatusEmoji] = useState(myStatusEmoji)
  const [saving, setSaving] = useState(false)
  const [saved, setSaved] = useState(false)
  const avatarInputRef = useRef<HTMLInputElement>(null)

  // Esc closes the modal (Discord-style).
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  // Re-sync local fields if the status changes upstream (e.g. another tab/session).
  useEffect(() => setStatusText(myStatus), [myStatus])
  useEffect(() => setStatusEmoji(myStatusEmoji), [myStatusEmoji])

  const dirty = statusText.trim() !== myStatus || statusEmoji.trim() !== myStatusEmoji

  const saveStatus = async () => {
    setSaving(true)
    setSaved(false)
    try {
      await onSaveStatus(statusText.trim(), statusEmoji.trim())
      setSaved(true)
    } finally {
      setSaving(false)
    }
  }

  return (
    <div
      className="settings-overlay"
      role="dialog"
      aria-modal="true"
      aria-label="User settings"
      onClick={onClose}
    >
      <div className="settings-modal" onClick={(e) => e.stopPropagation()}>
        <nav className="settings-nav" aria-label="settings sections">
          <div className="settings-nav-head">User Settings</div>
          <button
            type="button"
            className={`settings-tab${tab === 'account' ? ' active' : ''}`}
            onClick={() => setTab('account')}
          >
            My Account
          </button>
          <button
            type="button"
            className="settings-tab disabled"
            disabled
            title="Coming soon"
          >
            Voice &amp; Video <span className="settings-soon">Soon</span>
          </button>
        </nav>

        <div className="settings-body">
          <button
            type="button"
            className="settings-close"
            aria-label="close settings"
            title="Close (Esc)"
            onClick={onClose}
          >
            ✕
          </button>

          {tab === 'account' && (
            <section className="settings-section" aria-label="My Account">
              <h2 className="settings-title">My Account</h2>

              {/* Avatar + username */}
              <div className="settings-row settings-avatar-row">
                <span className="settings-avatar-preview">
                  <Avatar
                    token={token}
                    userId={user.id}
                    username={user.username}
                    className="avatar avatar-settings"
                    bust={avatarVersion}
                  />
                  <span
                    className={`presence-pip presence-${myPresence} settings-avatar-pip`}
                    aria-hidden
                  />
                </span>
                <div className="settings-avatar-meta">
                  <div className="settings-username">{user.username}</div>
                  <input
                    ref={avatarInputRef}
                    type="file"
                    accept="image/png,image/jpeg,image/gif,image/webp"
                    className="file-input-hidden"
                    onChange={(e) => {
                      // Capture the File synchronously, then clear the input so the
                      // same file can be re-picked (onChange won't fire twice otherwise).
                      void onAvatarPicked(e.target.files?.[0])
                      e.target.value = ''
                    }}
                  />
                  <button
                    type="button"
                    className="settings-btn"
                    onClick={() => avatarInputRef.current?.click()}
                  >
                    Change Avatar
                  </button>
                </div>
              </div>

              {/* Custom status + emoji */}
              <div className="settings-field">
                <label className="settings-label" htmlFor="settings-status">
                  Custom Status
                </label>
                <div className="settings-status-row">
                  <input
                    id="settings-status-emoji"
                    className="settings-input settings-emoji-input"
                    aria-label="status emoji"
                    placeholder="🙂"
                    maxLength={16}
                    value={statusEmoji}
                    onChange={(e) => setStatusEmoji(e.target.value)}
                  />
                  <input
                    id="settings-status"
                    className="settings-input"
                    aria-label="custom status"
                    placeholder="What's happening?"
                    maxLength={128}
                    value={statusText}
                    onChange={(e) => setStatusText(e.target.value)}
                  />
                  <button
                    type="button"
                    className="settings-btn primary"
                    onClick={() => void saveStatus()}
                    disabled={saving || !dirty}
                  >
                    {saving ? 'Saving…' : saved && !dirty ? 'Saved' : 'Save'}
                  </button>
                </div>
              </div>

              {/* Presence */}
              <div className="settings-field">
                <label className="settings-label" htmlFor="settings-presence">
                  Presence
                </label>
                <span className="presence-pill settings-presence">
                  <span className={`presence-pip presence-${myPresence}`} />
                  <select
                    id="settings-presence"
                    className="presence-select"
                    value={myPresence}
                    onChange={(e) => void onChangePresence(e.target.value)}
                    aria-label="set your presence"
                  >
                    <option value="online">Online</option>
                    <option value="idle">Idle</option>
                    <option value="dnd">Do Not Disturb</option>
                    <option value="invisible">Invisible</option>
                  </select>
                </span>
              </div>
            </section>
          )}
        </div>
      </div>
    </div>
  )
}
