import type { ReactNode } from 'react'

// Backend's /api/search returns snippets with FTS5's snippet() builtin wrapping
// matches in literal `<mark>…</mark>` strings — and crucially it does NOT
// HTML-escape the page body before substituting the delimiters. Rendering the
// raw string via dangerouslySetInnerHTML would XSS off any `<script>` /
// `<img onerror=…>` content in a page body. So we parse the snippet client-side
// by splitting on the delimiter pair and emit native `<mark>` elements around
// plain React text — the user-controlled bits flow through React's text-only
// escaping path. See memory.md "FTS5 snippet contains raw HTML metacharacters".
const SPLIT_RE = /(<mark>.*?<\/mark>)/g
const MARK_RE = /^<mark>(.*?)<\/mark>$/

export function HighlightedSnippet({ snippet }: { snippet: string }): ReactNode {
  const parts = snippet.split(SPLIT_RE)
  return (
    <span className="tela-snippet">
      {parts.map((part, i) => {
        if (part === '') return null
        const m = part.match(MARK_RE)
        if (m) return <mark key={i}>{m[1]}</mark>
        return <span key={i}>{part}</span>
      })}
    </span>
  )
}
