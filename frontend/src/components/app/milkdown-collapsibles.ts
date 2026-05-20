import { $ctx, $nodeSchema, $prose, $remark } from '@milkdown/kit/utils'
import { Plugin } from '@milkdown/kit/prose/state'

// M13.1 — collapsibles via raw `<details><summary>` HTML pass-through.
// Two schema nodes (`details` + `details_summary`) materialize the GitHub-style
// disclosure widget inside the editor; round-trip serializes back to the same
// raw HTML so it remains portable plain markdown (`<details>...</details>` is
// universally rendered by every markdown viewer).
//
// Three pieces wired together:
// 1. `collapsiblesRemarkPlugin` — mdast transformer that runs AFTER milkdown's
//    `remarkHtmlTransformer` (commonmark preset, line 41-64). That transformer
//    wraps every block-level `html` mdast node in a paragraph (so it slots
//    into the existing inline `htmlSchema`). We undo that wrapping for the
//    `<details>` / `</details>` openers/closers by walking the parent's
//    children array, detecting paragraph[html("<details...>")] ... paragraph[
//    html("</details>")] brackets, and rewriting the bracketed range into a
//    structured `details` mdast node carrying a `details_summary` child + the
//    body content as further block children.
// 2. `detailsSchema` + `detailsSummarySchema` — `$nodeSchema` definitions.
//    `details.content = 'details_summary block+'` enforces "summary first,
//    then at least one body block"; PM's content matcher rejects malformed
//    paste/parse cases automatically. `toDOM` emits the native `<details>` +
//    `<summary>` structure so the browser's built-in disclosure toggle keeps
//    working inside contenteditable — clicks on the summary fire the standard
//    HTMLDetailsElement `toggle` event without us wiring any JS.
//    `toMarkdown` re-emits raw HTML sibling nodes (opening tag + summary
//    embedded, body content as block children, closing tag) so the markdown
//    is exactly the GitHub-renderable form.
//
// Note: the `open` attribute is intentionally NOT preserved across save/reload.
// Toggle state is session-only — matches the brief ("persistent open-state in
// URL" is out of scope for v0). Re-opening a saved page renders all details
// closed; the user toggles them open as needed.

export const COLLAPSIBLE_DEFAULT_SUMMARY = 'Click to expand'

interface MdastNode {
  type: string
  value?: string
  children?: MdastNode[]
  [k: string]: unknown
}

// Recognize a "paragraph wrapper around a single html mdast node" — the post-
// remarkHtmlTransformer shape of every block-level raw-HTML chunk in the
// source markdown. We use this both to find `<details>` openers/closers and
// to inspect the inner html value's content.
function paragraphHtmlValue(node: MdastNode): string | null {
  if (node.type !== 'paragraph') return null
  if (!Array.isArray(node.children) || node.children.length !== 1) return null
  const inner = node.children[0]
  if (inner.type !== 'html' || typeof inner.value !== 'string') return null
  return inner.value
}

// Anchors are anchored to start-of-line so a stray `<details>` inside a code
// block or inside a longer html chunk doesn't trip the matcher (remark's
// codeblock + inline-code paths short-circuit before reaching our walker, but
// belt-and-suspenders).
const DETAILS_OPEN_RE = /^\s*<details(\s+[^>]*)?>/i
const DETAILS_CLOSE_RE = /<\/details>\s*$/i
// Extract the first `<summary>…</summary>` text content. Intentionally simple
// (no nested-tag handling) — the slash menu inserts plain-text summaries and
// the common manual-edit path is also plain text. If a user pastes rich
// markup into the summary it survives as raw text; we don't try to be clever
// here.
const SUMMARY_INLINE_RE = /<summary[^>]*>([\s\S]*?)<\/summary>/i

interface DetailsMatch {
  closingIdx: number
  summary: string
  // Index range of body children (siblings between opening and closing,
  // inclusive of start, exclusive of end). May be empty if the body is empty
  // — we'll inject an empty paragraph in that case to satisfy `block+`.
  bodyStart: number
  bodyEnd: number
  // True iff the second sibling is a paragraph[html("<summary>…</summary>")]
  // that should be consumed (not included in the body). When summary is
  // embedded in the opening html value, this is false.
  consumeSecondAsSummary: boolean
}

function tryMatchDetailsAt(
  children: MdastNode[],
  startIdx: number,
): DetailsMatch | null {
  const opening = children[startIdx]
  const openingValue = paragraphHtmlValue(opening)
  if (openingValue == null) return null
  if (!DETAILS_OPEN_RE.test(openingValue)) return null

  // Look for the matching closing </details>. Conservative single-level scan
  // (no nested details support in v0 — nested details was a stretch goal in
  // the brief, not a blocker; complex nesting is rare and the failure mode
  // is graceful: the inner details just renders as plain raw HTML).
  let closingIdx = -1
  for (let i = startIdx + 1; i < children.length; i++) {
    const v = paragraphHtmlValue(children[i])
    if (v != null && DETAILS_CLOSE_RE.test(v)) {
      closingIdx = i
      break
    }
  }
  if (closingIdx === -1) return null

  // Extract summary: prefer inline-with-opening, fall back to next sibling
  // paragraph[html("<summary>…</summary>")].
  let summary = ''
  let consumeSecondAsSummary = false
  const inlineMatch = openingValue.match(SUMMARY_INLINE_RE)
  if (inlineMatch) {
    summary = inlineMatch[1].trim()
  } else if (startIdx + 1 < closingIdx) {
    const nextValue = paragraphHtmlValue(children[startIdx + 1])
    if (nextValue) {
      const m = nextValue.match(SUMMARY_INLINE_RE)
      if (m) {
        summary = m[1].trim()
        consumeSecondAsSummary = true
      }
    }
  }

  const bodyStart = startIdx + (consumeSecondAsSummary ? 2 : 1)
  return {
    closingIdx,
    summary,
    bodyStart,
    bodyEnd: closingIdx,
    consumeSecondAsSummary,
  }
}

function transformCollapsiblesInMdast(node: MdastNode): void {
  if (!Array.isArray(node.children)) return

  const inChildren = node.children
  const outChildren: MdastNode[] = []

  let i = 0
  while (i < inChildren.length) {
    const child = inChildren[i]
    const match = tryMatchDetailsAt(inChildren, i)
    if (match) {
      const summaryNode: MdastNode = {
        type: 'details_summary',
        children:
          match.summary.length > 0
            ? [{ type: 'text', value: match.summary }]
            : [],
      }
      const bodyChildren = inChildren.slice(match.bodyStart, match.bodyEnd)
      // Schema requires `block+` after summary. Empty bodies get a placeholder
      // paragraph so PM's content matcher accepts the parsed node.
      if (bodyChildren.length === 0) {
        bodyChildren.push({ type: 'paragraph', children: [] })
      }
      // Recurse into each body child so nested transforms (e.g. a callout
      // inside a collapsible) still fire.
      for (const b of bodyChildren) {
        transformCollapsiblesInMdast(b)
      }
      outChildren.push({
        type: 'details',
        children: [summaryNode, ...bodyChildren],
      })
      i = match.closingIdx + 1
      continue
    }
    // Recurse into non-details children so nested details inside e.g. a
    // blockquote still get transformed.
    transformCollapsiblesInMdast(child)
    outChildren.push(child)
    i++
  }

  node.children = outChildren
}

export const collapsiblesRemarkPlugin = $remark(
  'telaCollapsibles',
  () => () => (tree) => {
    transformCollapsiblesInMdast(tree as unknown as MdastNode)
  },
)

// Escape a few minimum HTML metacharacters so `<` / `&` / `>` inside summary
// text don't break the round-trip. Quotes are NOT escaped because the summary
// content is between `<summary>` open/close tags (not inside an attribute),
// so quotes are textually fine. Hash, slash, etc. all pass through.
function escapeHtmlText(s: string): string {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
}

export const detailsSummarySchema = $nodeSchema('details_summary', () => ({
  content: 'inline*',
  defining: true,
  parseDOM: [{ tag: 'summary' }],
  toDOM: () => ['summary', { class: 'tela-details-summary' }, 0],
  parseMarkdown: {
    match: ({ type }) => type === 'details_summary',
    runner: (state, node, type) => {
      state.openNode(type)
      state.next((node as MdastNode).children ?? [])
      state.closeNode()
    },
  },
  toMarkdown: {
    match: (node) => node.type.name === 'details_summary',
    // Parent `details` runner consumes the summary directly via node.firstChild
    // (see below) and emits it as part of the opening html. If for some reason
    // PM tries to serialize a stray `details_summary` outside a `details`
    // (shouldn't happen under normal conditions thanks to `content: 'details_summary block+'`
    // on the parent), emit a defensive `<summary>` html fragment so the
    // markdown still round-trips losslessly.
    runner: (state, node) => {
      state.addNode(
        'html',
        undefined,
        `<summary>${escapeHtmlText(node.textContent)}</summary>`,
      )
    },
  },
}))

export const detailsSchema = $nodeSchema('details', () => ({
  content: 'details_summary block+',
  group: 'block',
  defining: true,
  // ProseMirror's parseDOM walks the `<details>` element's children and resolves
  // each via its own parseDOM rule — `<summary>` lands in `details_summary` via
  // the summary schema above, body markup lands as ordinary blocks. We don't
  // need a `contentElement` selector here because PM treats the details as the
  // container and recursively parses its children.
  parseDOM: [{ tag: 'details' }],
  // toDOM is used for clipboard / parseDOM round-trips. In-editor rendering
  // goes through `detailsNodeView` below, which decides whether to set the
  // `open` attribute based on `view.editable`: editable view (live editor)
  // forces `open` so caret-routing into the body works (Chromium would
  // otherwise route typing through the summary when the body is hidden by a
  // closed <details>); read-only view (share-mode, viewer-mode) leaves it
  // closed so the user clicks the summary to expand — matching native
  // HTMLDetailsElement UX and the canonical GitHub-rendered shape.
  toDOM: () => ['details', { class: 'tela-details' }, 0],
  parseMarkdown: {
    match: ({ type }) => type === 'details',
    runner: (state, node, type) => {
      state.openNode(type)
      state.next((node as MdastNode).children ?? [])
      state.closeNode()
    },
  },
  toMarkdown: {
    match: (node) => node.type.name === 'details',
    runner: (state, node) => {
      // Pull the summary out of the first child (schema guarantees it exists
      // and is a `details_summary`). Emit it inline with the opening tag so
      // the round-trip is the GitHub-canonical form `<details><summary>X</summary>`.
      const summaryNode = node.firstChild
      const summaryText = summaryNode?.textContent ?? ''
      const safeSummary = escapeHtmlText(summaryText)
      state.addNode(
        'html',
        undefined,
        `<details><summary>${safeSummary}</summary>`,
      )
      // Emit body siblings — skip the first child (summary), which we've
      // already consumed above. forEach iterates by document position; the
      // index argument is the offset within content, NOT a 0-based child
      // counter, so we track the child counter manually.
      let childIdx = 0
      node.content.forEach((child) => {
        const isSummary = childIdx === 0
        childIdx += 1
        if (isSummary) return
        state.next(child)
      })
      state.addNode('html', undefined, '</details>')
    },
  },
}))

// Tracks whether the editor was MOUNTED in read-only mode (share-mode,
// viewer-mode). NOT the same as `view.editable`, which also flips false
// during transient collab connecting/disconnect — using `view.editable` at
// nodeView construction time misfires because the ws hasn't opened yet on
// first mount (collabStatus = 'connecting' → editable predicate returns
// false), so the nodeView would render closed in edit-mode and not
// re-open when ws connects. This ctx is set once at editor build time from
// the React `readOnly` + `wikilinkMode` props (both stable across the
// editor's lifetime — PageView keys by page id) and is read once per
// nodeView construction.
export const detailsReadOnlyCtx = $ctx(false, 'detailsReadOnly')

// NodeView for `details`. Edit mode (readOnly === false): force `open=''`
// on the <details> element so caret-routing into the body works (Chromium
// routes typing on a hidden contenteditable region through the closest
// visible editable text node, which would be the <summary>). Read-only
// (share-mode, viewer-mode): leave `open` unset — native <details> renders
// closed by default, and the user clicks the <summary> to expand.
export const detailsNodeView = $prose((ctx) => {
  return new Plugin({
    props: {
      nodeViews: {
        details: () => {
          const dom = document.createElement('details')
          dom.className = 'tela-details'
          const readOnly = ctx.get(detailsReadOnlyCtx.key)
          if (!readOnly) dom.setAttribute('open', '')
          return {
            dom,
            contentDOM: dom,
            // The `open` attribute is runtime UI state owned by the
            // user-agent + modifier-click toggle plugin (M13.5 #116). It is
            // NOT a PM schema attr — keeping it out of the doc state is the
            // whole point ("transient" per the M13.1 brief). Without this
            // ignoreMutation, PM's MutationObserver sees every open-attr
            // flip and rebuilds the nodeView, which re-runs this constructor
            // and forces `open=''` again in edit mode — snapping a
            // user-initiated close back to open.
            ignoreMutation: (m) =>
              m.type === 'attributes' && m.attributeName === 'open',
          }
        },
      },
    },
  })
})
