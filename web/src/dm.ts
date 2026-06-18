import type { DMChannel } from './types'

export interface DMMember {
  id: number
  username: string
}

// The "other" members of a DM as seen by the caller. The server sends `users` (one entry
// for a 1:1, two or more for a group DM); fall back to the legacy singular `user` for any
// older payload that predates the slice-1 backend.
export function dmOthers(dm: DMChannel): DMMember[] {
  return dm.users && dm.users.length > 0 ? dm.users : [dm.user]
}

// A group DM has more than one other member. (Unnamed group DMs are titled by their
// members, like Discord — see dmTitle.)
export function dmIsGroup(dm: DMChannel): boolean {
  return dmOthers(dm).length > 1
}

// The sidebar/header title for a DM: the single other's username for a 1:1, else every
// other member's username comma-joined — Discord's default for an *unnamed* group DM.
export function dmTitle(dm: DMChannel): string {
  return dmOthers(dm)
    .map((u) => u.username)
    .join(', ')
}

// Parse the group-create input into a clean identifier list: split a typed/pasted string on
// commas and whitespace, trim, drop blanks, and dedupe (preserving order). The server
// re-resolves + re-validates every identifier; this is just client-side convenience.
export function parseIdentifiers(raw: string): string[] {
  const out: string[] = []
  const seen = new Set<string>()
  for (const tok of raw.split(/[\s,]+/)) {
    const t = tok.trim()
    if (!t || seen.has(t)) continue
    seen.add(t)
    out.push(t)
  }
  return out
}
