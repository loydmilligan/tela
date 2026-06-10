// Pure, Milkdown-free `<details>` collapsible grouping for the VIEW parse
// stack (remark-stack.ts). The editor needs a structurally different pass
// (milkdown-collapsibles.ts) because milkdown's remarkHtmlTransformer wraps
// every block-level html chunk in a paragraph before it runs; here the
// opener/closer arrive as raw `html` siblings straight from remark. Matching
// rules are kept identical (same regexes, depth-aware closer scan, inline or
// next-sibling summary, nested re-processing) so view and edit agree on what
// counts as a collapsible.
//
// Output: a `details` mdast node `{ open: boolean, summary: string,
// children: [...body blocks] }` the view renderer maps to a native <details>.

import type { MdastNode } from './callouts'

// Anchored to start-of-value / end-of-value, mirroring the editor's anchors,
// so a stray `<details>` inside a longer html chunk doesn't trip the matcher.
const DETAILS_OPEN_RE = /^\s*<details(\s+[^>]*)?>/i
const DETAILS_CLOSE_RE = /<\/details>\s*$/i
// First `<summary>…</summary>` text content. Intentionally simple (no
// nested-tag handling) — the slash menu and the serializer both emit
// plain-text summaries.
const SUMMARY_INLINE_RE = /<summary[^>]*>([\s\S]*?)<\/summary>/i

function htmlValue(node: MdastNode): string | null {
  return node.type === 'html' && typeof node.value === 'string'
    ? node.value
    : null
}

function detailsHasOpenAttr(openingValue: string): boolean {
  const m = openingValue.match(DETAILS_OPEN_RE)
  const attrs = m?.[1] ?? ''
  return /(^|\s)open(\s|=|$)/i.test(attrs)
}

interface DetailsMatch {
  closingIdx: number
  summary: string
  open: boolean
  bodyStart: number
}

function tryMatchDetailsAt(
  children: MdastNode[],
  startIdx: number,
): DetailsMatch | null {
  const openingValue = htmlValue(children[startIdx])
  if (openingValue == null || !DETAILS_OPEN_RE.test(openingValue)) return null
  // A single html chunk holding both opener and closer (no blank lines) is
  // left alone — the editor doesn't collapse that form either.
  if (DETAILS_CLOSE_RE.test(openingValue)) return null

  // Depth-aware scan for the matching closer so nested details pair up.
  let closingIdx = -1
  let depth = 0
  for (let i = startIdx + 1; i < children.length; i++) {
    const v = htmlValue(children[i])
    if (v == null) continue
    if (DETAILS_OPEN_RE.test(v)) {
      if (!DETAILS_CLOSE_RE.test(v)) depth += 1
    } else if (DETAILS_CLOSE_RE.test(v)) {
      if (depth === 0) {
        closingIdx = i
        break
      }
      depth -= 1
    }
  }
  if (closingIdx === -1) return null

  // Summary: inline with the opener (canonical serializer form), else the
  // next sibling html node.
  let summary = ''
  let bodyStart = startIdx + 1
  const inline = openingValue.match(SUMMARY_INLINE_RE)
  if (inline) {
    summary = inline[1].trim()
  } else if (startIdx + 1 < closingIdx) {
    const next = htmlValue(children[startIdx + 1])
    const m = next?.match(SUMMARY_INLINE_RE)
    if (m) {
      summary = m[1].trim()
      bodyStart = startIdx + 2
    }
  }

  return {
    closingIdx,
    summary,
    open: detailsHasOpenAttr(openingValue),
    bodyStart,
  }
}

export function transformCollapsiblesInMdast(node: MdastNode): void {
  if (!Array.isArray(node.children)) return

  const inChildren = node.children
  const outChildren: MdastNode[] = []

  let i = 0
  while (i < inChildren.length) {
    const child = inChildren[i]
    const match = tryMatchDetailsAt(inChildren, i)
    if (match) {
      const body = inChildren.slice(match.bodyStart, match.closingIdx)
      // Re-run over the body as a sibling group so a NESTED details — whose
      // opener/closer are separate body siblings — collapses too (and each
      // body child recurses).
      const bodyContainer: MdastNode = { type: 'root', children: body }
      transformCollapsiblesInMdast(bodyContainer)
      outChildren.push({
        type: 'details',
        open: match.open,
        summary: match.summary,
        children: bodyContainer.children ?? [],
      })
      i = match.closingIdx + 1
      continue
    }
    transformCollapsiblesInMdast(child)
    outChildren.push(child)
    i++
  }

  node.children = outChildren
}

// Raw unified/remark attacher.
export function collapsiblesRemark() {
  return (tree: unknown) => {
    transformCollapsiblesInMdast(tree as unknown as MdastNode)
  }
}
