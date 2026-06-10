import { $prose } from '@milkdown/kit/utils'
import { keymap } from '@milkdown/kit/prose/keymap'
import { liftListItem, sinkListItem } from '@milkdown/kit/prose/schema-list'
import type { Command, EditorState } from '@milkdown/kit/prose/state'
import type { NodeType } from '@milkdown/kit/prose/model'

// Tab / Shift-Tab nest & un-nest list items — the indentation people expect from
// Tab in a list. Nested lists are real markdown, so they round-trip through
// pages.body (a paragraph-indent attribute couldn't).
//
// Crucially we CONSUME Tab/Shift-Tab whenever the caret is inside a list, even
// when there's nothing to do — e.g. Tab on the FIRST item (markdown can't nest
// it: no preceding sibling to nest under). Without that, the key fell through to
// the browser default and moved focus OUT of the editor onto the next tabbable
// element (the "Connections" panel), which reads as "Tab doesn't work". Outside
// a list we deliberately DON'T consume it, so Tab still moves focus normally —
// no accessibility regression.
//
// This is deliberately NOT @milkdown/plugin-indent: that one binds Tab to insert
// two literal spaces and swallows EVERY Tab (incl. focus-out everywhere), which
// is both surprising and a11y-hostile.

function inListItem(state: EditorState, itemType: NodeType): boolean {
  const { $from } = state.selection
  for (let d = $from.depth; d > 0; d--) {
    if ($from.node(d).type === itemType) return true
  }
  return false
}

const nest: Command = (state, dispatch, view) => {
  const itemType = state.schema.nodes.list_item
  return (
    sinkListItem(itemType)(state, dispatch, view) || inListItem(state, itemType)
  )
}
const unnest: Command = (state, dispatch, view) => {
  const itemType = state.schema.nodes.list_item
  return (
    liftListItem(itemType)(state, dispatch, view) || inListItem(state, itemType)
  )
}

export const listIndentKeymap = $prose(() =>
  keymap({ Tab: nest, 'Shift-Tab': unnest }),
)
