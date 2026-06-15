// Deterministic avatar colour + initials from a username — the fallback shown when
// a user has no uploaded avatar (and briefly while one loads).
function avatarHue(name: string): number {
  let h = 0
  for (let i = 0; i < name.length; i++) h = (h * 31 + name.charCodeAt(i)) % 360
  return h
}

export function avatarColor(name: string): string {
  return `hsl(${avatarHue(name)}, 55%, 45%)`
}

// Initials colour picked for WCAG-AA contrast on the generated background: white on
// dark hues (blue/red/purple), black on bright ones (yellow/green/cyan) — so the
// initials are always readable regardless of the user's hue.
export function avatarTextColor(name: string): string {
  const h = avatarHue(name) / 360
  const s = 0.55
  const l = 0.45
  const a = s * Math.min(l, 1 - l)
  const f = (n: number) => {
    const k = (n + h * 12) % 12
    return l - a * Math.max(-1, Math.min(k - 3, 9 - k, 1))
  }
  const lin = (c: number) => (c <= 0.03928 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4))
  const lum = 0.2126 * lin(f(0)) + 0.7152 * lin(f(8)) + 0.0722 * lin(f(4))
  return lum > 0.18 ? '#000000' : '#ffffff'
}

export function initials(name: string): string {
  return name.slice(0, 2).toUpperCase()
}
