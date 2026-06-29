import { TextSelection } from '@milkdown/kit/prose/state'
import type { EditorState, Transaction } from '@milkdown/kit/prose/state'
import type { Node as ProseNode, NodeType } from '@milkdown/kit/prose/model'
import type { EditorView } from '@milkdown/kit/prose/view'

// Shared block-insertion mechanics for every slash-menu / block inserter.
//
// Why this exists: `Transaction.replaceSelectionWith` has subtle positioning —
// it replaces an empty enclosing textblock, splits a non-empty one, or appends
// at the nearest valid level, so the inserted node's final position is NOT
// simply `selection.from`. Each block inserter used to rediscover that position
// itself, and the two that placed the caret inside the new block (callout,
// collapsible) did it by matching on text CONTENT — "the last callout whose
// body is empty", "the last details whose summary equals the placeholder".
// That collides with a pre-existing identical block: insert a second empty
// callout and the walk targets the first one. This locates the node by POSITION
// instead (the first node of its type at/after the insertion point), which is
// robust regardless of content, and funnels every inserter through one caret
// rule so the per-block offset arithmetic is gone.

export type InsertCaret =
  // First text position inside the inserted node's LAST child — the body you
  // immediately type into. Right for empty scaffolds (callout, pull-quote, and
  // collapsible, whose body is the second child after the summary).
  | 'inside'
  // Leave ProseMirror's own post-insert selection untouched. Right for
  // pre-filled / multi-region scaffolds the user clicks into (tabs, kanban,
  // stat grid, timeline, calendar) and for block atoms (math, mermaid, chart,
  // embed).
  | 'none'

export interface InsertBlockOptions {
  caret?: InsertCaret
}

// The node we just inserted = the first node of `type` at or after the mapped
// insertion point. `replaceSelectionWith` can split the surrounding textblock
// and push our node a token past where the cursor was, so we scan forward from
// the mapped cursor position — never matching on text content or needing a
// marker attr.
function locateInserted(
  doc: ProseNode,
  type: NodeType,
  lowerBound: number,
): number | null {
  let found: number | null = null
  doc.descendants((node, pos) => {
    if (found != null) return false
    if (node.type === type && pos >= lowerBound) {
      found = pos
      return false
    }
    return true
  })
  return found
}

// Build the transaction that inserts `node` at the current selection and parks
// the caret per `opts.caret`. Pure over EditorState (no EditorView) so the
// positioning logic is unit-testable without a DOM; `insertBlock` is the thin
// view-side wrapper that dispatches it.
export function buildInsertBlock(
  state: EditorState,
  node: ProseNode,
  opts: InsertBlockOptions = {},
): Transaction {
  const caret = opts.caret ?? 'inside'
  const from = state.selection.from
  const tr = state.tr.replaceSelectionWith(node)
  if (caret === 'inside') {
    const pos = locateInserted(tr.doc, node.type, tr.mapping.map(from, -1))
    if (pos != null) {
      // Resolve just inside the node's closing boundary and bias backward — for
      // an empty-body scaffold that lands the caret in the (empty) body, whether
      // the body is the only child (callout, pull-quote) or the last child after
      // a summary (collapsible). No per-block offset arithmetic.
      tr.setSelection(
        TextSelection.near(tr.doc.resolve(pos + node.nodeSize - 1), -1),
      )
    }
  }
  return tr.scrollIntoView()
}

// Insert `node` at the selection, dispatch, and refocus. The single entry point
// every block inserter funnels through.
export function insertBlock(
  view: EditorView,
  node: ProseNode,
  opts: InsertBlockOptions = {},
): void {
  view.dispatch(buildInsertBlock(view.state, node, opts))
  view.focus()
}
