// Per-channel composer drafts persisted to localStorage so they survive a page reload
// (Discord parity — building on the in-memory per-channel drafts). The stored value is
// UNTRUSTED: a user can hand-edit localStorage, so parse defensively (Rule 15) — ignore
// corrupt JSON, skip non-numeric keys and non-string/empty values, and BOUND both the
// per-draft length and the channel count so a tampered or huge entry can never bloat memory
// or wedge the composer. Drafts are plain text (rendered into a <textarea> / React-escaped
// markdown), so there is no markup-injection vector — the only risks are size/shape, which
// these bounds close.

export const DRAFT_MAX_LEN = 4000 // ~ the WS message cap (4 KiB); anything longer is junk
export const DRAFT_MAX_CHANNELS = 50 // cap distinct channels kept in storage

// parseStoredDrafts turns a raw localStorage string into a bounded channelId→text map.
// Returns an empty map for null/corrupt/wrong-shaped input (never throws).
export function parseStoredDrafts(raw: string | null): Map<number, string> {
  const out = new Map<number, string>()
  if (!raw) return out
  let obj: unknown
  try {
    obj = JSON.parse(raw)
  } catch {
    return out // corrupt JSON → no drafts, not a crash
  }
  if (!obj || typeof obj !== 'object' || Array.isArray(obj)) return out
  for (const [k, v] of Object.entries(obj as Record<string, unknown>)) {
    const id = Number(k)
    if (!Number.isInteger(id) || typeof v !== 'string' || v === '') continue
    out.set(id, v.slice(0, DRAFT_MAX_LEN))
    if (out.size >= DRAFT_MAX_CHANNELS) break
  }
  return out
}

// serializeDrafts produces the bounded JSON to write back: drop empty drafts, keep at most
// the most-recently-inserted DRAFT_MAX_CHANNELS, and clamp each to DRAFT_MAX_LEN.
export function serializeDrafts(drafts: Map<number, string>): string {
  const entries = [...drafts.entries()].filter(([, v]) => v).slice(-DRAFT_MAX_CHANNELS)
  const obj: Record<string, string> = {}
  for (const [id, v] of entries) obj[String(id)] = v.slice(0, DRAFT_MAX_LEN)
  return JSON.stringify(obj)
}
