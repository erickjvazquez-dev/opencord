import React, { type ReactNode } from 'react'

// A deliberately small, XSS-safe Markdown subset for chat messages. It returns
// React elements (never an HTML string / dangerouslySetInnerHTML), so React
// escapes all text and only a fixed set of safe tags/components is ever emitted:
//
//   ```fenced code```   `inline code`   **bold**   *italic*  _italic_
//   ~~strike~~   ||spoiler||   @mention   and  > blockquote  lines
//
// Anything else — including raw HTML like <script>…</script> — renders as literal
// text (Rule B: treat message bodies as hostile; never inject markup from them).

// ctx threads a key counter (siblings need unique keys) and the viewer's own
// username (so a mention of them can be highlighted) through the recursion.
type Ctx = { n: number; me?: string }

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
  kind?: 'mention'
}

// Order matters: inline code first (its content is literal), then spoiler, then
// bold before the single-* italic rule so `**x**` isn't eaten as italic. Mention
// is last; earliest-match-by-index decides between different delimiters anyway, so
// e.g. `@x` inside `` `@x` `` stays literal (the code match starts earlier).
const INLINE_RULES: InlineRule[] = [
  { re: /`([^`\n]+)`/, el: 'code', literal: true },
  { re: /\|\|([^|\n]+)\|\|/, el: Spoiler },
  { re: /\*\*([^*\n]+)\*\*/, el: 'strong' },
  { re: /~~([^~\n]+)~~/, el: 'del' },
  { re: /\*([^*\n]+)\*/, el: 'em' },
  { re: /_([^_\n]+)_/, el: 'em' },
  { re: /@([A-Za-z0-9_]{2,32})/, kind: 'mention' },
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

  if (rule.kind === 'mention') {
    const name = m[1]
    const isMe = !!ctx.me && name.toLowerCase() === ctx.me.toLowerCase()
    out.push(
      React.createElement(
        'span',
        { key: `md${ctx.n++}`, className: isMe ? 'mention mention-me' : 'mention' },
        '@' + name,
      ),
    )
  } else {
    const children = rule.literal ? [m[1]] : renderInline(m[1], ctx)
    out.push(React.createElement(rule.el as string, { key: `md${ctx.n++}` }, ...children))
  }
  out.push(...renderInline(text.slice(m.index + m[0].length), ctx))
  return out
}

// QUOTE matches a leading `>` (with an optional space) on a line.
const QUOTE = /^>\s?/

// renderBlocks does line-level parsing on a (code-block-free) chunk: consecutive
// `>`-prefixed lines become one <blockquote>; everything else is inline-rendered as
// a run (newlines preserved by the body's white-space: pre-wrap).
function renderBlocks(text: string, ctx: Ctx): ReactNode[] {
  const lines = text.split('\n')
  const out: ReactNode[] = []
  let i = 0
  while (i < lines.length) {
    if (QUOTE.test(lines[i])) {
      const quoted: string[] = []
      while (i < lines.length && QUOTE.test(lines[i])) {
        quoted.push(lines[i].replace(QUOTE, ''))
        i++
      }
      out.push(
        React.createElement('blockquote', { key: `md${ctx.n++}` }, ...renderInline(quoted.join('\n'), ctx)),
      )
    } else {
      const para: string[] = []
      while (i < lines.length && !QUOTE.test(lines[i])) {
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

export function renderMarkdown(text: string, opts?: { me?: string }): ReactNode {
  if (!text) return text
  const ctx: Ctx = { n: 0, me: opts?.me }
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
