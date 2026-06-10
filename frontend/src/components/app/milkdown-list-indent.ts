import { $prose } from '@milkdown/kit/utils'
import { keymap } from '@milkdown/kit/prose/keymap'
import { liftListItem, sinkListItem } from '@milkdown/kit/prose/schema-list'
import type { Command } from '@milkdown/kit/prose/state'

// Tab / Shift-Tab nest & un-nest list items — the indentation people expect from
// Tab in a list. Nested lists are real markdown, so they round-trip through
// pages.body (a paragraph-indent attribute couldn't). The list commands return
// false when the selection isn't inside a list, so the keymap falls through and
// Tab keeps moving focus out of the editor — no accessibility regression.
//
// This is deliberately NOT @milkdown/plugin-indent: that one binds Tab to insert
// two literal spaces and swallows every Tab press (incl. focus-out), which is
// both surprising and a11y-hostile. We only intercept Tab when it does something
// meaningful (a list item to sink/lift).
const nest: Command = (state, dispatch, view) =>
  sinkListItem(state.schema.nodes.list_item)(state, dispatch, view)
const unnest: Command = (state, dispatch, view) =>
  liftListItem(state.schema.nodes.list_item)(state, dispatch, view)

export const listIndentKeymap = $prose(() =>
  keymap({ Tab: nest, 'Shift-Tab': unnest }),
)
