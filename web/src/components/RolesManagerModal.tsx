import { useEffect, useState } from 'react'
import {
  listServerRoles,
  createServerRole,
  updateServerRole,
  deleteServerRole,
} from '../api'
import type { Role } from '../types'

// A small Discord-style palette for one-click color picks (the native <input type="color">
// stays available for anything custom).
const PRESETS = [
  '#5865f2', '#e91e63', '#23a55a', '#f0b232', '#eb459e',
  '#3498db', '#e67e22', '#9b59b6', '#1abc9c', '#ed4245',
]

// RolesManagerModal is the server's cosmetic-role manager (admin only): list existing roles
// (swatch + name + delete), recolor inline, and create a new role (name + color). Reuses the
// settings-overlay backdrop; Esc / overlay / ✕ close. Every mutation calls onChanged so the
// caller can refresh members (their colors) + its own roles copy.
export function RolesManagerModal({
  token,
  serverId,
  onClose,
  onChanged,
}: {
  token: string
  serverId: number
  onClose: () => void
  onChanged: () => void
}) {
  const [roles, setRoles] = useState<Role[]>([])
  const [name, setName] = useState('')
  const [color, setColor] = useState('#5865f2')
  const [hoist, setHoist] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  const refresh = async () => {
    try {
      setRoles(await listServerRoles(token, serverId))
    } catch {
      /* leave the prior list on a transient error */
    }
  }

  useEffect(() => {
    void refresh()
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [onClose])

  const create = async () => {
    const trimmed = name.trim()
    if (!trimmed) {
      setError('Give the role a name.')
      return
    }
    setBusy(true)
    setError(null)
    try {
      await createServerRole(token, serverId, trimmed, color, hoist)
      setName('')
      setHoist(false)
      await refresh()
      onChanged()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'could not create role')
    } finally {
      setBusy(false)
    }
  }

  // Recolor preserves the role's hoist; toggleHoist flips it (both via the same PATCH).
  const recolor = async (role: Role, next: string) => {
    try {
      await updateServerRole(token, serverId, role.id, role.name, next, role.hoist ?? false)
      await refresh()
      onChanged()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'could not recolor role')
    }
  }
  const toggleHoist = async (role: Role) => {
    try {
      await updateServerRole(token, serverId, role.id, role.name, role.color, !(role.hoist ?? false))
      await refresh()
      onChanged()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'could not update role')
    }
  }

  const remove = async (role: Role) => {
    try {
      await deleteServerRole(token, serverId, role.id)
      await refresh()
      onChanged()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'could not delete role')
    }
  }

  return (
    <div className="settings-overlay" role="dialog" aria-modal="true" aria-label="Manage roles" onClick={onClose}>
      <div className="group-modal roles-modal" onClick={(e) => e.stopPropagation()}>
        <button className="settings-close" onClick={onClose} aria-label="Close">
          ✕
        </button>
        <h2 className="group-modal-title">Roles</h2>
        <p className="group-modal-sub">Create colored roles, then assign them from a member's profile.</p>

        <div className="roles-list">
          {roles.length === 0 && <p className="roles-empty">No roles yet — create one below.</p>}
          {roles.map((r) => (
            <div className="roles-row" key={r.id}>
              <input
                type="color"
                className="roles-swatch"
                value={r.color}
                onChange={(e) => void recolor(r, e.target.value)}
                aria-label={`Color for ${r.name}`}
                title="Change color"
              />
              <span className="roles-name" style={{ color: r.color }}>
                {r.name}
              </span>
              <label className="roles-hoist" title="Display members with this role in their own section">
                <input
                  type="checkbox"
                  checked={r.hoist ?? false}
                  onChange={() => void toggleHoist(r)}
                  aria-label={`Display ${r.name} separately`}
                />
                hoist
              </label>
              <button className="roles-delete" onClick={() => void remove(r)} aria-label={`Delete ${r.name}`}>
                Delete
              </button>
            </div>
          ))}
        </div>

        <div className="roles-create">
          <div className="roles-create-row">
            <input
              type="color"
              className="roles-swatch"
              value={color}
              onChange={(e) => setColor(e.target.value)}
              aria-label="New role color"
            />
            <input
              className="roles-name-input"
              value={name}
              onChange={(e) => setName(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') void create()
              }}
              placeholder="New role name"
              maxLength={32}
              disabled={busy}
              aria-label="New role name"
            />
            <button className="settings-btn primary" onClick={() => void create()} disabled={busy || !name.trim()}>
              {busy ? 'Adding…' : 'Add role'}
            </button>
          </div>
          <label className="roles-hoist roles-hoist-create" title="Display members with this role in their own section">
            <input type="checkbox" checked={hoist} onChange={(e) => setHoist(e.target.checked)} />
            Display separately (hoist)
          </label>
          <div className="roles-presets" aria-label="preset colors">
            {PRESETS.map((p) => (
              <button
                key={p}
                type="button"
                className="roles-preset"
                style={{ background: p }}
                onClick={() => setColor(p)}
                aria-label={`Use ${p}`}
                title={p}
              />
            ))}
          </div>
        </div>

        {error && (
          <p className="group-modal-error" role="alert">
            {error}
          </p>
        )}
      </div>
    </div>
  )
}
