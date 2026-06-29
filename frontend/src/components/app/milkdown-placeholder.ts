import { $prose } from '@milkdown/kit/utils'
import { Plugin, PluginKey } from '@milkdown/kit/prose/state'
import { Decoration, DecorationSet } from '@milkdown/kit/prose/view'

// Discoverability cue: when the caret sits on an empty top-level paragraph, show
// a muted "Type / for commands" hint (Notion-style). This is how block editors
// expose the slash menu without a persistent toolbar — the affordance lives in
// the empty line itself. Rendered via a node decoration + CSS `::before`
// (`content: attr(data-placeholder)`), so it never enters the document or the
// canonical markdown.

export const PLACEHOLDER_TEXT = 'Type / for commands…'

const key = new PluginKey('tela-placeholder')

export const placeholderPlugin = $prose(() => {
  return new Plugin({
    key,
    props: {
      decorations(state) {
        const { selection, doc } = state
        // Only with a collapsed caret (not while selecting).
        if (!selection.empty) return null
        const $from = selection.$from
        // Only a top-level paragraph (depth 1) that is empty — keep the hint to
        // the document's own empty lines, not empty cells inside callouts/lists.
        if ($from.depth !== 1) return null
        const node = $from.parent
        if (node.type.name !== 'paragraph' || node.content.size > 0) return null
        const pos = $from.before(1)
        return DecorationSet.create(doc, [
          Decoration.node(pos, pos + node.nodeSize, {
            class: 'tela-placeholder',
            'data-placeholder': PLACEHOLDER_TEXT,
          }),
        ])
      },
    },
  })
})
