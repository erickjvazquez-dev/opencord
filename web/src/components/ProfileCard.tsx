import { useEffect } from 'react'
import { Avatar } from './Avatar'
import type { Role, ServerMember } from '../types'

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
  canManageRoles,
  serverRoles,
  onAssignRole,
  onUnassignRole,
}: {
  member: ServerMember
  token: string
  selfId: number
  blocked?: boolean
  onToggleBlock?: (userId: number) => void
  onClose: () => void
  // Custom colored roles (v0.7): when an admin views a server member, they can toggle the
  // server's roles on/off here. All optional → the card stays usable in DM/non-admin contexts.
  canManageRoles?: boolean
  serverRoles?: Role[]
  onAssignRole?: (userId: number, roleId: number) => void
  onUnassignRole?: (userId: number, roleId: number) => void
}) {
  const isSelf = member.userId === selfId
  const canBlock = !isSelf && !!onToggleBlock
  const assigned = new Set(member.roleIds ?? [])
  const showRoles = !!canManageRoles && !!serverRoles && serverRoles.length > 0
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
            <div className="profile-name" style={member.color ? { color: member.color } : undefined}>
              {member.username}
            </div>
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
        {showRoles && (
          <div className="profile-section">
            <div className="profile-section-label">Roles</div>
            <div className="profile-roles">
              {serverRoles!.map((r) => {
                const on = assigned.has(r.id)
                return (
                  <button
                    key={r.id}
                    type="button"
                    className={`profile-role-chip${on ? ' on' : ''}`}
                    data-role={r.id}
                    aria-pressed={on}
                    style={on ? { background: r.color, borderColor: r.color } : { borderColor: r.color, color: r.color }}
                    onClick={() => (on ? onUnassignRole : onAssignRole)?.(member.userId, r.id)}
                    title={on ? `Remove ${r.name}` : `Add ${r.name}`}
                  >
                    {on ? '✓ ' : '+ '}
                    {r.name}
                  </button>
                )
              })}
            </div>
          </div>
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
