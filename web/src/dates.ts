// Small date helpers for the chat UI, kept out of the big Chat component so they
// can be unit-tested in isolation (no React import needed).

// Discord-style day divider label: "Today" / "Yesterday" for the two most recent
// days, otherwise a full local date ("June 17, 2026"). `now` is injectable so the
// boundary behaviour is testable without depending on the wall clock.
export function dayLabel(d: Date, now: Date = new Date()): string {
  const yesterday = new Date(now)
  yesterday.setDate(now.getDate() - 1)
  if (d.toDateString() === now.toDateString()) return 'Today'
  if (d.toDateString() === yesterday.toDateString()) return 'Yesterday'
  return d.toLocaleDateString(undefined, { month: 'long', day: 'numeric', year: 'numeric' })
}

// Compact time for the message gutter (Discord shows HH:MM with no seconds, e.g.
// "9:41 AM" / "21:41"). Uses the viewer's locale.
export function shortTime(d: Date): string {
  return d.toLocaleTimeString(undefined, { hour: 'numeric', minute: '2-digit' })
}

// Full message-header timestamp, Discord-style: "Today at 9:41 AM" / "Yesterday at
// 9:41 AM" / "June 15, 2026 at 9:41 AM" — no seconds (the old toLocaleTimeString()
// showed "9:41:30 AM", which Discord never does).
export function messageTimestamp(d: Date, now: Date = new Date()): string {
  return `${dayLabel(d, now)} at ${shortTime(d)}`
}
