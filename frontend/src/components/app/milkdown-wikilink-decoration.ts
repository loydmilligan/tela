import { $prose } from '@milkdown/kit/utils'
import { Plugin } from '@milkdown/kit/prose/state'
import { Decoration, DecorationSet } from '@milkdown/kit/prose/view'
import type { Node as ProseNode } from '@milkdown/kit/prose/model'

// Decorates every link mark whose href starts with `tela://page/` with the
// `tela-wikilink` class so styling — and M5.2d's broken-state handling — can
// target wikilinks distinctly from plain markdown links. Rebuilt on every doc
// change; cost is one pass over inline nodes per transaction.
export const wikilinkDecorationPlugin = $prose(
  () =>
    new Plugin({
      state: {
        init: (_, { doc }) => buildWikilinkDecorations(doc),
        apply: (tr, old) =>
          tr.docChanged ? buildWikilinkDecorations(tr.doc) : old,
      },
      props: {
        decorations(state) {
          return this.getState(state)
        },
      },
    }),
)

function buildWikilinkDecorations(doc: ProseNode): DecorationSet {
  const decos: Decoration[] = []
  doc.descendants((node, pos) => {
    if (!node.isInline) return
    const link = node.marks.find((m) => m.type.name === 'link')
    if (!link) return
    const href = link.attrs.href
    if (typeof href !== 'string' || !href.startsWith('tela://page/')) return
    decos.push(
      Decoration.inline(pos, pos + node.nodeSize, { class: 'tela-wikilink' }),
    )
  })
  return DecorationSet.create(doc, decos)
}
