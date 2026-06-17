// Desktop notifications (Web Notifications API) for the web client. Discord's
// default low-noise policy: only notify when the tab is UNFOCUSED and a new message
// in the active channel is either a DM (any message) or @-mentions you. Off by
// default; the user opts in via the Settings → Notifications toggle, which requests
// OS permission on that click (a user gesture — browsers reject silent requests).
//
// Rule A — self-hostable, zero required cost, degrade gracefully: every entry point
// here is a no-op when the API is unsupported or permission is denied. No telemetry.
// The decision is split into a PURE function (shouldNotify) so it's unit-testable
// without a real Notification object or a focused/hidden DOM.

const KEY = 'opencord.notify.desktop'

// localStorage can throw (private mode / disabled); never let a pref read break the
// message handler. Mirrors voiceSettings' try/catch helpers.
function read(key: string): string | null {
  try {
    return localStorage.getItem(key)
  } catch {
    return null
  }
}
function write(key: string, val: string): void {
  try {
    localStorage.setItem(key, val)
  } catch {
    /* storage unavailable — the pref just doesn't persist this session */
  }
}

// The desktop-notify pref defaults to FALSE (Discord enables it only after the user
// opts in + grants OS permission). Only the literal '1' means on.
export function getDesktopNotify(): boolean {
  return read(KEY) === '1'
}
export function setDesktopNotify(on: boolean): void {
  write(KEY, on ? '1' : '0')
}

// True if a message body mentions the viewer — same rule the markdown renderer uses
// to highlight a self-mention (markdown.tsx): a word-bounded `@username`
// (case-insensitive), or `@everyone` / `@here` (which ping everyone). The `(?!\w)`
// trailing boundary stops `@meelsewhere` from matching `@me`. The username is escaped
// so a name with regex metacharacters can't break the pattern (Rule B).
export function mentionsMe(body: string, username: string): boolean {
  if (!body) return false
  if (/@(?:everyone|here)(?!\w)/i.test(body)) return true
  if (!username) return false
  const esc = username.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  return new RegExp(`@${esc}(?!\\w)`, 'i').test(body)
}

// PURE decision: notify iff the feature is on, the OS granted permission, the tab is
// hidden (Discord only notifies when you're not looking), the message isn't your own,
// and it's a DM or it mentions you. Unit-tested exhaustively; the live handler just
// feeds it the current state.
export function shouldNotify(opts: {
  enabled: boolean
  permission: string
  hidden: boolean
  isMine: boolean
  isDM: boolean
  mentionsMe: boolean
}): boolean {
  return (
    opts.enabled &&
    opts.permission === 'granted' &&
    opts.hidden &&
    !opts.isMine &&
    (opts.isDM || opts.mentionsMe)
  )
}

// Request OS permission for notifications. Must be called from a user gesture (the
// Settings toggle click). Returns the resulting permission, or 'denied' when the API
// is unsupported (so callers can treat unsupported the same as refused — Rule A).
export async function requestNotifyPermission(): Promise<string> {
  if (typeof window === 'undefined' || !('Notification' in window)) return 'denied'
  try {
    return await Notification.requestPermission()
  } catch {
    return 'denied'
  }
}

// Show a desktop notification — a no-op unless the API exists and permission is
// granted. Clicking it focuses the window (Discord-style). Wrapped in try/catch so a
// throw (e.g. some browsers require a service worker on mobile) never propagates into
// the WS message handler.
export function showNotification(title: string, body: string): void {
  try {
    if (typeof window === 'undefined' || !('Notification' in window)) return
    if (Notification.permission !== 'granted') return
    // tag: 'opencord' collapses rapid-fire notifications into one (no spam stack).
    const n = new Notification(title, { body, tag: 'opencord' })
    n.onclick = () => {
      try {
        window.focus()
      } catch {
        /* focus can be blocked; the click still dismisses the notification */
      }
    }
  } catch {
    /* notification construction failed — degrade silently (Rule A) */
  }
}
