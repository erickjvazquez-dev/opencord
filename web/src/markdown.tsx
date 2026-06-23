import React, { type ReactNode } from 'react'
import { EmojiImg } from './components/EmojiImg'

// A deliberately small, XSS-safe Markdown subset for chat messages. It returns
// React elements (never an HTML string / dangerouslySetInnerHTML), so React
// escapes all text and only a fixed set of safe tags/components is ever emitted:
//
//   ```fenced code```   `inline code`   **bold**   *italic*  _italic_  __underline__
//   ***bold italic***
//   ~~strike~~   ||spoiler||   @mention   :custom_emoji:   and  > blockquote  lines
//
// Anything else — including raw HTML like <script>…</script> — renders as literal
// text (Rule B: treat message bodies as hostile; never inject markup from them).

// ctx threads a key counter (siblings need unique keys), the viewer's own username
// (so a mention of them can be highlighted), and the current server's custom-emoji
// map (name → emoji id) through the recursion.
type Ctx = { n: number; me?: string; emoji?: ReadonlyMap<string, number>; token?: string }

// Spoiler — hidden until clicked (Discord-style). Content is React-escaped.
function Spoiler({ children }: { children?: ReactNode }): React.ReactElement {
  const [shown, setShown] = React.useState(false)
  return React.createElement(
    'span',
    {
      className: shown ? 'spoiler shown' : 'spoiler',
      onClick: shown ? undefined : () => setShown(true),
      role: 'button',
      tabIndex: 0,
      title: shown ? undefined : 'Click to reveal',
    },
    children,
  )
}


type InlineRule = {
  re: RegExp
  el?: string | React.ComponentType<{ children?: ReactNode }>
  literal?: boolean
  kind?: 'mention' | 'link' | 'masklink' | 'emoji' | 'bolditalic'
}

// Order matters: inline code first (its content is literal), then spoiler, then
// bold before the single-* italic rule so `**x**` isn't eaten as italic. Mention
// is last; earliest-match-by-index decides between different delimiters anyway, so
// e.g. `@x` inside `` `@x` `` stays literal (the code match starts earlier).
const INLINE_RULES: InlineRule[] = [
  { re: /`([^`\n]+)`/, el: 'code', literal: true },
  // Masked link [text](url): the URL group is http(s):// IN THE REGEX, so a
  // [x](javascript:…)/[x](data:…) simply doesn't match → renders as inert literal text
  // (never a loosenable scheme check). Before the autolink so the inner URL isn't grabbed.
  { re: /\[([^\]\n]+)\]\((https?:\/\/[^\s)]+)\)/, kind: 'masklink' },
  // Autolink only http(s):// — never javascript:/data:, so the href is always safe.
  { re: /https?:\/\/[^\s<]+/, kind: 'link' },
  { re: /\|\|([^|\n]+)\|\|/, el: Spoiler },
  // Bold+italic `***x***` BEFORE bold so the triple delimiter wins (otherwise `**`
  // matches at index 1 and `***x***` mis-renders as `*<strong>x</strong>*`).
  { re: /\*\*\*([^*\n]+)\*\*\*/, kind: 'bolditalic' },
  { re: /\*\*([^*\n]+)\*\*/, el: 'strong' },
  { re: /~~([^~\n]+)~~/, el: 'del' },
  { re: /\*([^*\n]+)\*/, el: 'em' },
  // Discord maps DOUBLE underscore to UNDERLINE (single `_` stays italic below).
  // Must precede the single-`_` rule: for `__x__` underline matches at an earlier
  // index than the single-`_` rule (which would otherwise eat it as `_<em>x</em>_`).
  { re: /__([^_\n]+)__/, el: 'u' },
  { re: /_([^_\n]+)_/, el: 'em' },
  { re: /@([A-Za-z0-9_]{2,32})/, kind: 'mention' },
  // Custom emoji `:slug:` — charset matches the backend's ValidEmojiName (lowercase
  // a-z, 0-9, _; 2–32). The {2,} minimum means `::` never matches. It only becomes an
  // image when the slug is in the current server's emoji map; otherwise it stays literal.
  { re: /:([a-z0-9_]{2,32}):/, kind: 'emoji' },
]

// renderInline applies the earliest matching inline rule and recurses into both the
// match's content and the remaining text.
function renderInline(text: string, ctx: Ctx): ReactNode[] {
  if (!text) return []
  let best: { rule: InlineRule; m: RegExpExecArray } | null = null
  for (const rule of INLINE_RULES) {
    const m = rule.re.exec(text)
    if (m && (!best || m.index < best.m.index)) best = { rule, m }
  }
  if (!best) return [text]

  const { rule, m } = best
  const out: ReactNode[] = []
  const before = text.slice(0, m.index)
  if (before) out.push(before)
  let after = text.slice(m.index + m[0].length)

  if (rule.kind === 'link') {
    // Don't swallow trailing punctuation that's clearly sentence text, e.g. "(see http://x.com)."
    let url = m[0]
    const trail = url.match(/[.,!?;:)\]]+$/)
    if (trail) {
      url = url.slice(0, -trail[0].length)
      after = trail[0] + after
    }
    out.push(
      React.createElement(
        'a',
        { key: `md${ctx.n++}`, href: url, target: '_blank', rel: 'noopener noreferrer' },
        url,
      ),
    )
  } else if (rule.kind === 'masklink') {
    // [text](url): url is guaranteed http(s) by the regex. title=the real URL is the
    // anti-phishing tell (hover reveals the true destination when the text lies). The
    // display text is inline-rendered so [**bold**](url) etc. work.
    const url = m[2]
    out.push(
      React.createElement(
        'a',
        {
          key: `md${ctx.n++}`,
          href: url,
          target: '_blank',
          rel: 'noopener noreferrer',
          title: url,
        },
        ...renderInline(m[1], ctx),
      ),
    )
  } else if (rule.kind === 'mention') {
    const name = m[1]
    const lower = name.toLowerCase()
    const isMe = !!ctx.me && lower === ctx.me.toLowerCase()
    const isAll = lower === 'everyone' || lower === 'here'
    // @everyone/@here ping you too, so they share the highlighted style; the extra
    // `mention-all` class distinguishes them in the DOM.
    const cls = isMe ? 'mention mention-me' : isAll ? 'mention mention-me mention-all' : 'mention'
    out.push(React.createElement('span', { key: `md${ctx.n++}`, className: cls }, '@' + name))
  } else if (rule.kind === 'emoji') {
    const name = m[1]
    const id = ctx.emoji?.get(name)
    if (id == null) {
      // Unknown name (or no map) → leave the literal `:name:` text untouched. Emitting
      // it here (not via `before`) means a later `:known:` in `after` still resolves.
      out.push(m[0])
    } else {
      // Auth-gated emoji bytes can't ride an <img src> (no Authorization header), so
      // EmojiImg fetches them WITH the token and renders a blob URL. React-elements only
      // (no innerHTML); the slug came from a strict [a-z0-9_] charset, so nothing is
      // attacker-injectable.
      out.push(
        React.createElement(EmojiImg, {
          key: `md${ctx.n++}`,
          token: ctx.token ?? '',
          id,
          alt: `:${name}:`,
        }),
      )
    }
  } else if (rule.kind === 'bolditalic') {
    // ***x*** → <strong><em>x</em></strong>. Emitted as nested elements (not a wrapper
    // component) so the rendered tree literally carries both tags.
    out.push(
      React.createElement(
        'strong',
        { key: `md${ctx.n++}` },
        React.createElement('em', null, ...renderInline(m[1], ctx)),
      ),
    )
  } else {
    const children = rule.literal ? [m[1]] : renderInline(m[1], ctx)
    out.push(React.createElement(rule.el as string, { key: `md${ctx.n++}` }, ...children))
  }
  out.push(...renderInline(after, ctx))
  return out
}

// QUOTE matches a leading `>` (with an optional space) on a line.
const QUOTE = /^>\s?/
const BULLET = /^[-*]\s+/ // "- item" or "* item" (the space distinguishes it from *italic*)
const NUMBERED = /^\d+\.\s+/ // "1. item"
// Discord-style line headers + subtext. The space is required, so `#channel` (no space)
// and `-item` are unaffected; empty-content (`# ` alone) falls through to a paragraph.
const HEADER = /^(#{1,3})\s+(.+)$/ // "# H1" / "## H2" / "### H3" (level = hash count)
const SUBTEXT = /^-#\s+(.+)$/ // "-# small muted subtext"

type LineKind = 'quote' | 'bullet' | 'numbered' | 'para'
function lineKind(line: string): LineKind {
  if (QUOTE.test(line)) return 'quote'
  if (BULLET.test(line)) return 'bullet'
  if (NUMBERED.test(line)) return 'numbered'
  return 'para'
}

// renderBlocks does line-level parsing on a (code-block-free) chunk: a run of `>`
// lines becomes a <blockquote>; a run of `- `/`* ` or `1. ` lines becomes a
// <ul>/<ol>; everything else is inline-rendered as a paragraph run (newlines
// preserved by the body's white-space: pre-wrap).
function renderBlocks(text: string, ctx: Ctx): ReactNode[] {
  const lines = text.split('\n')
  const out: ReactNode[] = []
  let i = 0
  while (i < lines.length) {
    // Headers + subtext are single styled lines (not runs), matched before the run logic.
    // Visual weight only (chat text, not document structure); inline content stays XSS-safe.
    const hm = HEADER.exec(lines[i])
    if (hm) {
      out.push(
        React.createElement(
          'div',
          { key: `md${ctx.n++}`, className: `md-h md-h${hm[1].length}` },
          ...renderInline(hm[2], ctx),
        ),
      )
      i++
      continue
    }
    const sm = SUBTEXT.exec(lines[i])
    if (sm) {
      out.push(
        React.createElement(
          'div',
          { key: `md${ctx.n++}`, className: 'md-subtext' },
          ...renderInline(sm[1], ctx),
        ),
      )
      i++
      continue
    }
    const kind = lineKind(lines[i])
    if (kind === 'quote') {
      const quoted: string[] = []
      while (i < lines.length && lineKind(lines[i]) === 'quote') {
        quoted.push(lines[i].replace(QUOTE, ''))
        i++
      }
      out.push(
        React.createElement('blockquote', { key: `md${ctx.n++}` }, ...renderInline(quoted.join('\n'), ctx)),
      )
    } else if (kind === 'bullet' || kind === 'numbered') {
      const re = kind === 'bullet' ? BULLET : NUMBERED
      const items: ReactNode[] = []
      while (i < lines.length && lineKind(lines[i]) === kind) {
        items.push(
          React.createElement('li', { key: `md${ctx.n++}` }, ...renderInline(lines[i].replace(re, ''), ctx)),
        )
        i++
      }
      out.push(React.createElement(kind === 'bullet' ? 'ul' : 'ol', { key: `md${ctx.n++}` }, ...items))
    } else {
      const para: string[] = []
      while (i < lines.length && lineKind(lines[i]) === 'para') {
        para.push(lines[i])
        i++
      }
      out.push(...renderInline(para.join('\n'), ctx))
    }
  }
  return out
}

// ```…``` fenced code blocks (optional ```lang line). Pulled out before block/inline
// parsing; their contents are literal.
const FENCE = /```[^\n]*\n?([\s\S]*?)```/g

export function renderMarkdown(
  text: string,
  opts?: { me?: string; emoji?: ReadonlyMap<string, number>; token?: string },
): ReactNode {
  if (!text) return text
  const ctx: Ctx = { n: 0, me: opts?.me, emoji: opts?.emoji, token: opts?.token }
  const nodes: ReactNode[] = []
  let last = 0
  let m: RegExpExecArray | null
  FENCE.lastIndex = 0
  while ((m = FENCE.exec(text)) !== null) {
    if (m.index > last) nodes.push(...renderBlocks(text.slice(last, m.index), ctx))
    nodes.push(
      React.createElement('pre', { key: `md${ctx.n++}` }, React.createElement('code', null, m[1])),
    )
    last = m.index + m[0].length
  }
  if (last < text.length) nodes.push(...renderBlocks(text.slice(last), ctx))
  return nodes
}

// highlightMatches wraps case-insensitive occurrences of `query` in <mark class="search-match">
// within the React tree renderMarkdown produced — used to bold the matched term in search results
// (Discord parity). It only re-wraps EXISTING text nodes (never injects markup from the message;
// the query is the viewer's own input, React-escaped inside the <mark>), so the XSS invariant is
// untouched. Childless nodes and custom components (EmojiImg) pass through unchanged.
export function highlightMatches(node: ReactNode, query: string): ReactNode {
  const q = query.trim()
  if (!q) return node
  const needle = q.toLowerCase()
  let n = 0
  const walk = (node: ReactNode): ReactNode => {
    if (typeof node === 'string') {
      if (!node.toLowerCase().includes(needle)) return node
      const parts: ReactNode[] = []
      const hay = node.toLowerCase()
      let i = 0
      let idx: number
      while ((idx = hay.indexOf(needle, i)) !== -1) {
        if (idx > i) parts.push(node.slice(i, idx))
        parts.push(
          React.createElement('mark', { key: `hl${n++}`, className: 'search-match' }, node.slice(idx, idx + q.length)),
        )
        i = idx + q.length
      }
      if (i < node.length) parts.push(node.slice(i))
      return parts
    }
    if (Array.isArray(node)) return node.map(walk)
    if (React.isValidElement(node)) {
      const kids = (node.props as { children?: ReactNode }).children
      if (kids == null) return node
      return React.cloneElement(node, undefined, walk(kids))
    }
    return node
  }
  return walk(node)
}
