import { $prose } from '@milkdown/kit/utils'
import { Plugin, PluginKey } from '@milkdown/kit/prose/state'
import { Decoration, DecorationSet } from '@milkdown/kit/prose/view'
import type { Node as ProseNode } from '@milkdown/kit/prose/model'
import { editorViewCtx } from '@milkdown/kit/core'
import type { Ctx } from '@milkdown/ctx'
import { buildQueryPreview } from '../../lib/blocks/query-spec'
import { insertBlock } from '../../lib/milkdown/insert-block'

// Props query block. A ` ```query ` fenced code block carries a small YAML spec
// (where / space / columns / sort / limit); the READ view renders it as a live
// table of pages whose props match (props @> where), gated through the caller's
// space_access. A Dataview analog — see milkdown-chart.ts for the same
// fenced-block + decoration shape.
//
// In the EDITOR this decoration is a static, non-interactive PREVIEW of the query
// beneath the still-editable source (the chart/field stance): the live fetch +
// table is a read-view concern (QueryBlockView in MarkdownView), so the editor
// stays free of data-fetching. The canonical markdown is a plain code block, so
// it round-trips and degrades to readable source anywhere the widget isn't shown.
//
// Render core (spec parse + preview DOM) lives in lib/blocks/query-spec.ts,
// Milkdown-free and shared with the read view. Re-exported so importers keep a
// single path.
export { buildQueryPreview }

const queryKey = new PluginKey('tela-query')

function buildDecorations(doc: ProseNode): DecorationSet {
  const decos: Decoration[] = []
  doc.descendants((node, pos) => {
    if (
      node.type.name === 'code_block' &&
      String(node.attrs.language ?? '').toLowerCase() === 'query'
    ) {
      const code = node.textContent
      if (code.trim().length === 0) return
      decos.push(
        Decoration.widget(pos + node.nodeSize, () => buildQueryPreview(code), {
          side: 1,
          key: `query:${code}`,
        }),
      )
    }
  })
  return DecorationSet.create(doc, decos)
}

export const queryPlugin = $prose(() => {
  return new Plugin({
    key: queryKey,
    props: {
      decorations(state) {
        return buildDecorations(state.doc)
      },
    },
  })
})

// Slash inserter: a `query` code block seeded with a by-type listing — the
// canonical starting query.
export function insertQuery(ctx: Ctx) {
  const view = ctx.get(editorViewCtx)
  const codeType = view.state.schema.nodes.code_block
  if (!codeType) return
  const starter =
    'where: { type: doc }\nspace: here\ncolumns: [title, status, updated]\nsort: -updated\nlimit: 25'
  const node = codeType.create(
    { language: 'query' },
    view.state.schema.text(starter),
  )
  insertBlock(view, node, { caret: 'none' })
}
