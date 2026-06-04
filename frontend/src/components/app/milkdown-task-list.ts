import { $prose } from '@milkdown/kit/utils'
import { Plugin } from '@milkdown/kit/prose/state'
import { commandsCtx, editorViewCtx } from '@milkdown/kit/core'
import { wrapInBulletListCommand } from '@milkdown/kit/preset/commonmark'
import type { Ctx } from '@milkdown/ctx'

// Task lists (GFM `- [ ] / - [x]`). The heavy lifting already ships in the gfm
// preset: `extendListItemSchemaForTask` adds a nullable `checked` attr to
// `list_item` (null = plain bullet, boolean = task), `wrapInTaskListInputRule`
// converts `[ ] ` / `[x] ` typed at the start of a list item, and the
// markdown round-trip is handled by the preset's parse/serialize runners. The
// checkbox visual is drawn in editor.css off `li[data-item-type="task"]
// [data-checked]`.
//
// Two pieces the preset does NOT give us, added here:
//  1. `taskCheckboxPlugin` — clicking the rendered checkbox toggles `checked`.
//     The checkbox is a CSS `::before` in the list item's left gutter, so it
//     has no DOM node of its own; a click there reports the host <li> as the
//     event target (pseudo-elements don't receive events). Clicking the item's
//     text reports the inner <p> instead. That difference IS the hit test:
//     target.tagName === 'LI' means "the gutter / checkbox was clicked".
//  2. `insertTaskList` — the slash-menu inserter (the preset only exposes the
//     input rule, no wrap-in-task command).

export const taskCheckboxPlugin = $prose(() => {
  return new Plugin({
    props: {
      handleClick: (view, _pos, event) => {
        // Viewer / share / disconnected modes render read-only; the checkbox is
        // inert there (matches a published doc — you read it, you don't tick it).
        if (!view.editable) return false
        const target = event.target
        if (!(target instanceof HTMLElement)) return false
        if (target.tagName !== 'LI' || target.dataset.itemType !== 'task') {
          return false
        }
        // Tie the toggle to the exact <li> clicked rather than the click pos
        // heuristic, so the gutter of a parent task never flips a nested one.
        const pos = view.posAtDOM(target, 0)
        if (pos < 0) return false
        const $pos = view.state.doc.resolve(pos)
        for (let depth = $pos.depth; depth >= 0; depth--) {
          const node = $pos.node(depth)
          if (node.type.name === 'list_item' && node.attrs.checked != null) {
            const nodePos = $pos.before(depth)
            view.dispatch(
              view.state.tr.setNodeMarkup(nodePos, undefined, {
                ...node.attrs,
                checked: !node.attrs.checked,
              }),
            )
            return true
          }
        }
        return false
      },
    },
  })
})

// Slash-menu inserter: wrap the current paragraph into a bullet list (reusing
// the commonmark command so list nesting / lift behaviour stays consistent),
// then promote the resulting item to an unchecked task by setting `checked`.
// The serializer emits `- [ ] ` from there; typing in the item works as usual.
export function insertTaskList(ctx: Ctx) {
  ctx.get(commandsCtx).call(wrapInBulletListCommand.key)
  const view = ctx.get(editorViewCtx)
  const { state } = view
  const { $from } = state.selection
  for (let depth = $from.depth; depth >= 0; depth--) {
    const node = $from.node(depth)
    if (node.type.name === 'list_item' && node.attrs.checked == null) {
      const nodePos = $from.before(depth)
      view.dispatch(
        state.tr.setNodeMarkup(nodePos, undefined, {
          ...node.attrs,
          checked: false,
        }),
      )
      break
    }
  }
}
