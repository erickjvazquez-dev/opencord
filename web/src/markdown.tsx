import React, { type ReactNode } from 'react'

// A deliberately small, XSS-safe Markdown subset for chat messages. It returns
// React elements (never an HTML string / dangerouslySetInnerHTML), so React
// escapes all text and only a fixed set of safe tags is ever emitted:
//
//   ```fenced code```   `inline code`   **bold**   ~~strike~~   *italic*  _italic_
//
// Anything else — including raw HTML like <script>…</script> — renders as literal
// text (Rule B: treat message bodies as hostile; never inject markup from them).

type InlineRule = { re: RegExp; tag: 'strong' | 'em' | 'del' | 'code'; literal?: boolean }

// Order matters: inline code first (its content is literal), then bold before the
// single-* italic rule so `**x**` isn't eaten as italic.
const INLINE_RULES: InlineRule[] = [
  { re: /`([^`\n]+)`/, tag: 'code', literal: true },
  { re: /\*\*([^*\n]+)\*\*/, tag: 'strong' },
  { re: /~~([^~\n]+)~~/, tag: 'del' },
  { re: /\*([^*\n]+)\*/, tag: 'em' },
  { re: /_([^_\n]+)_/, tag: 'em' },
]

// renderInline walks a (code-block-free) string, repeatedly applying the earliest
// matching inline rule and recursing into the match's content. `counter` hands out
// keys that are unique across the whole message (siblings need unique keys).
function renderInline(text: string, counter: { n: number }): ReactNode[] {
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
  const children = rule.literal ? [m[1]] : renderInline(m[1], counter)
  out.push(React.createElement(rule.tag, { key: `md${counter.n++}` }, ...children))
  out.push(...renderInline(text.slice(m.index + m[0].length), counter))
  return out
}

// ```…``` fenced code blocks (optional ```lang line). Pulled out before inline
// parsing; their contents are literal.
const FENCE = /```[^\n]*\n?([\s\S]*?)```/g

export function renderMarkdown(text: string): ReactNode {
  if (!text) return text
  const counter = { n: 0 }
  const nodes: ReactNode[] = []
  let last = 0
  let m: RegExpExecArray | null
  FENCE.lastIndex = 0
  while ((m = FENCE.exec(text)) !== null) {
    if (m.index > last) nodes.push(...renderInline(text.slice(last, m.index), counter))
    nodes.push(
      React.createElement(
        'pre',
        { key: `md${counter.n++}` },
        React.createElement('code', null, m[1]),
      ),
    )
    last = m.index + m[0].length
  }
  if (last < text.length) nodes.push(...renderInline(text.slice(last), counter))
  return nodes
}
