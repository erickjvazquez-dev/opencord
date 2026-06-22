// Emoji `:`-autocomplete helpers (composer parity with Discord). Pure functions so
// the regression-delicate caret/splice logic is unit-testable in isolation — the
// React wiring in Chat.tsx stays thin. Mirrors the `@`-mention system (activeMention
// / acceptMention) one-for-one, with custom-emoji NAMES as the source instead of
// usernames. v1 covers custom server emoji only (the map already loaded for `:name:`
// rendering); a unicode-name table is a later enhancement.

// If the caret sits inside a `:partial` emoji shortcode being typed, return the
// partial `query` and the index where its `:` starts (so the token can be replaced
// on accept). The `:` must begin the text or follow whitespace; at least 2 name
// chars (letters/digits/_/-) must run from it to the caret — so a lone `:`, an
// emoticon like `:)`, or a completed `:name:` (closing colon isn't a name char)
// does NOT trigger. Mirrors activeMention exactly.
export function activeEmojiToken(
  text: string,
  caret: number,
): { query: string; start: number } | null {
  const upto = text.slice(0, Math.max(0, caret))
  const m = /(?:^|\s):([\w-]{2,})$/.exec(upto)
  if (!m) return null
  return { query: m[1], start: caret - m[1].length - 1 }
}

// Filter custom-emoji names by the partial: case-insensitive startsWith, stable
// (insertion) order, capped. Empty query returns everything (capped).
export function matchEmojiNames(names: Iterable<string>, query: string, cap = 8): string[] {
  const q = query.toLowerCase()
  const out: string[] = []
  for (const n of names) {
    if (n.toLowerCase().startsWith(q)) {
      out.push(n)
      if (out.length >= cap) break
    }
  }
  return out
}

// Replace the `:partial` token (range = {start, len} from activeEmojiToken) with
// `:name: ` (note the closing colon + trailing space) and return the new draft and
// the caret position to restore after it. Mirrors acceptMention's splice.
export function spliceEmoji(
  draft: string,
  range: { start: number; len: number },
  name: string,
): { next: string; caret: number } {
  const inserted = `:${name}: `
  const before = draft.slice(0, range.start)
  const next = before + inserted + draft.slice(range.start + range.len)
  return { next, caret: before.length + inserted.length }
}
