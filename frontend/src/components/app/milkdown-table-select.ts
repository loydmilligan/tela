import { Plugin } from '@milkdown/kit/prose/state'
import type { EditorState } from '@milkdown/kit/prose/state'
import { CellSelection, TableMap, findTable } from '@milkdown/kit/prose/tables'

// Table edge-selection guard. prosemirror-tables extends a CellSelection with
// Shift+Arrow correctly *inside* a table, but at the table's edge its handler
// bails (returns false) — so the browser's native Shift+Arrow takes over and
// **collapses the whole CellSelection to a caret**: the user grows a selection
// down a column, presses once more at the bottom row, and the entire highlight
// vanishes. Same on the left/right/top edges.
//
// This plugin runs ahead of prosemirror-tables (prepended in milkdown-editor):
// when a Shift+Arrow would push a CellSelection past the edge of its table, we
// swallow the key (return true) so the selection simply stays put instead of
// disappearing. Inside the table we return false and let prosemirror-tables do
// its normal cell-by-cell extension. Editable-only, single + collab (it only
// reads state and swallows a key — no transaction, nothing for collab to sync).

type Axis = 'horiz' | 'vert'

// Is the CellSelection's head cell already in the last/first row|column for the
// given direction — i.e. would extending exit the table?
function atTableEdge(state: EditorState, axis: Axis, dir: 1 | -1): boolean {
  const sel = state.selection
  if (!(sel instanceof CellSelection)) return false
  const table = findTable(sel.$headCell)
  if (!table) return false
  const map = TableMap.get(table.node)
  const rect = map.findCell(sel.$headCell.pos - table.start)
  if (axis === 'vert') {
    return dir > 0 ? rect.bottom >= map.height : rect.top <= 0
  }
  return dir > 0 ? rect.right >= map.width : rect.left <= 0
}

function guard(axis: Axis, dir: 1 | -1) {
  return (state: EditorState): boolean => atTableEdge(state, axis, dir)
}

export function createTableEdgeSelectPlugin(): Plugin {
  const handlers: Record<string, (s: EditorState) => boolean> = {
    'Shift-ArrowDown': guard('vert', 1),
    'Shift-ArrowUp': guard('vert', -1),
    'Shift-ArrowRight': guard('horiz', 1),
    'Shift-ArrowLeft': guard('horiz', -1),
  }
  return new Plugin({
    props: {
      handleKeyDown: (view, event) => {
        if (!event.shiftKey) return false
        const key =
          event.key === 'ArrowDown'
            ? 'Shift-ArrowDown'
            : event.key === 'ArrowUp'
              ? 'Shift-ArrowUp'
              : event.key === 'ArrowRight'
                ? 'Shift-ArrowRight'
                : event.key === 'ArrowLeft'
                  ? 'Shift-ArrowLeft'
                  : ''
        const h = handlers[key]
        return h ? h(view.state) : false
      },
    },
  })
}
