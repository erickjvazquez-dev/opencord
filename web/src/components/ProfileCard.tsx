import { useEffect } from 'react'
import { Avatar } from './Avatar'
import type { ServerMember } from '../types'

// ProfileCard is the Discord-style user profile shown when you click a member: avatar,
// name (+ presence dot), pronouns, custom status, and the About Me bio. A centered
// overlay (reuses the settings-overlay pattern for robustness — no fragile popover
// positioning). Esc or an overlay click closes it. All text is React-escaped.
//
// When viewing ANOTHER user's card, a Block/Unblock action appears (reflecting `blocked`);
// it's hidden on your own card. `selfId` identifies the viewer; `onToggleBlock` is wired
// only when blocking is available (own-card omits it).
export function ProfileCard({
  member,
  token,
  selfId,
  blocked,
  onToggleBlock,
  onClose,
}: {
  member: ServerMember
  token: string
  selfId: number
  blocked?: boolean
  onToggleBlock?: (userId: number) => void
  onClose: () => void
}) {
  const isSelf = member.userId === selfId
  const canBlock = !isSelf && !!onToggleBlock
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  const presence = member.presence || (member.online ? 'online' : 'offline')

  return (
    <div
      className="profile-overlay"
      role="dialog"
      aria-modal="true"
      aria-label={`${member.username}'s profile`}
      onClick={onClose}
    >
      <div className="profile-card" onClick={(e) => e.stopPropagation()}>
        <button
          type="button"
          className="settings-close profile-close"
          aria-label="close profile"
          title="Close (Esc)"
          onClick={onClose}
        >
          ✕
        </button>
        <div className="profile-head">
          <span className="profile-avatar-wrap">
            <Avatar
              token={token}
              userId={member.userId}
              username={member.username}
              className="avatar avatar-profile"
            />
            <span className={`presence-pip presence-${presence} profile-avatar-pip`} aria-hidden />
          </span>
          <div className="profile-identity">
            <div className="profile-name">{member.username}</div>
            {member.pronouns && <div className="profile-pronouns">{member.pronouns}</div>}
            {(member.status || member.statusEmoji) && (
              <div className="profile-status">
                {member.statusEmoji && <span className="status-emoji">{member.statusEmoji}</span>}
                {member.status}
              </div>
            )}
          </div>
        </div>
        {member.about ? (
          <div className="profile-section">
            <div className="profile-section-label">About Me</div>
            <div className="profile-about">{member.about}</div>
          </div>
        ) : (
          <div className="profile-empty">No About Me yet.</div>
        )}
        {canBlock && (
          <div className="profile-actions">
            <button
              type="button"
              className={`settings-btn profile-block-btn${blocked ? '' : ' danger'}`}
              data-blocked={blocked ? 'true' : 'false'}
              onClick={() => onToggleBlock?.(member.userId)}
            >
              {blocked ? 'Unblock' : 'Block'}
            </button>
          </div>
        )}
      </div>
    </div>
  )
}
