#!/usr/bin/env node
// Rule 15 — XSS by construction. The client renders ALL user text through React (which escapes
// it) and the markdown renderer emits elements only (its autolinker is restricted to http(s),
// guarded by markdown.test.tsx). The one way to break that guarantee is to introduce a raw-HTML
// or code-eval SINK. This lint scans the whole client source and FAILS (exit 1) if one appears,
// so the codebase-wide "no unsafe sink" invariant can't silently regress. It guards the CLASS of
// bug, which is higher-leverage than asserting one more field renders inertly (they all do).
//
// It's a standalone node lint (not a vitest test) on purpose: it uses fs/path, which the browser
// tsconfig doesn't type, so keeping it out of `tsc -b` avoids breaking the production build while
// still running every QA tick (wired into qa/run.sh's pre-boot checks).
import { readdirSync, readFileSync, statSync } from 'node:fs'
import { join, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'

const SRC = join(dirname(fileURLToPath(import.meta.url)), '..', 'src')

const FORBIDDEN = [
  { re: /dangerouslySetInnerHTML\s*[=:]/, name: 'dangerouslySetInnerHTML' },
  { re: /\.innerHTML\s*=/, name: '.innerHTML =' },
  { re: /\.outerHTML\s*=/, name: '.outerHTML =' },
  { re: /insertAdjacentHTML\s*\(/, name: 'insertAdjacentHTML(' },
  { re: /document\.write\s*\(/, name: 'document.write(' },
  { re: /\beval\s*\(/, name: 'eval(' },
  { re: /new\s+Function\s*\(/, name: 'new Function(' },
]

function walk(dir) {
  const out = []
  for (const name of readdirSync(dir)) {
    if (name === 'node_modules') continue
    const p = join(dir, name)
    if (statSync(p).isDirectory()) out.push(...walk(p))
    // Production source only — not the test files (some name the sinks in assertions/comments).
    else if (/\.(ts|tsx)$/.test(name) && !/\.test\.tsx?$/.test(name)) out.push(p)
  }
  return out
}

const files = walk(SRC)
if (files.length < 10) {
  console.error(`[sink-lint] only ${files.length} source files found — bad SRC path? (${SRC})`)
  process.exit(1)
}

const hits = []
for (const f of files) {
  readFileSync(f, 'utf8')
    .split('\n')
    .forEach((line, i) => {
      // Strip // line comments so doc-references to a sink's name don't trip the guard.
      const code = line.replace(/\/\/.*$/, '')
      for (const { re, name } of FORBIDDEN) {
        if (re.test(code)) hits.push(`${f.replace(SRC, 'src')}:${i + 1} [${name}] ${line.trim()}`)
      }
    })
}

if (hits.length) {
  console.error(`[sink-lint] FORBIDDEN unsafe sink(s) found (${hits.length}):`)
  for (const h of hits) console.error('  ' + h)
  console.error('[sink-lint] all user text must render through React (escaped); no raw-HTML/eval sinks.')
  process.exit(1)
}
console.log(`[sink-lint] OK — scanned ${files.length} source files, no unsafe HTML/eval sinks.`)
