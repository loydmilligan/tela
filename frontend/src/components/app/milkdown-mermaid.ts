import { $prose } from '@milkdown/kit/utils'
import { Plugin, PluginKey } from '@milkdown/kit/prose/state'
import { Decoration, DecorationSet } from '@milkdown/kit/prose/view'
import type { Node as ProseNode } from '@milkdown/kit/prose/model'
import { editorViewCtx } from '@milkdown/kit/core'
import type { Ctx } from '@milkdown/ctx'
import { buildMermaidElement } from '../../lib/diagrams/mermaid'

// Mermaid diagrams: a ` ```mermaid ` code block renders its diagram beneath the
// (still-editable) source. Canonical markdown — GitHub renders the same fence
// — so it round-trips as a plain code block and degrades to readable code
// where mermaid isn't supported.
//
// Rendered via a widget decoration after each mermaid code block (additive —
// the source code block, with its prism highlighting, is untouched). The
// widget is content-keyed so PM reuses the rendered SVG until the source
// changes (no flicker, no re-render of unchanged diagrams). mermaid is heavy,
// so it's lazy-imported on first render — it lands in its own chunk.

const mermaidKey = new PluginKey('tela-mermaid')

function buildDecorations(doc: ProseNode): DecorationSet {
  const decos: Decoration[] = []
  doc.descendants((node, pos) => {
    if (
      node.type.name === 'code_block' &&
      String(node.attrs.language ?? '').toLowerCase() === 'mermaid'
    ) {
      const code = node.textContent
      if (code.trim().length === 0) return
      decos.push(
        Decoration.widget(pos + node.nodeSize, () => buildMermaidElement(code), {
          side: 1,
          key: `mermaid:${code}`,
        }),
      )
    }
  })
  return DecorationSet.create(doc, decos)
}

export const mermaidPlugin = $prose(() => {
  return new Plugin({
    key: mermaidKey,
    props: {
      decorations(state) {
        return buildDecorations(state.doc)
      },
    },
  })
})

// Slash-menu inserter: a mermaid code block seeded with a starter diagram.
export function insertMermaid(ctx: Ctx) {
  const view = ctx.get(editorViewCtx)
  const codeType = view.state.schema.nodes.code_block
  if (!codeType) return
  const starter = 'graph TD\n  A[Start] --> B[End]'
  const node = codeType.create({ language: 'mermaid' }, view.state.schema.text(starter))
  view.dispatch(view.state.tr.replaceSelectionWith(node).scrollIntoView())
  view.focus()
}
