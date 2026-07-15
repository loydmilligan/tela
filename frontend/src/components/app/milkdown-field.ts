import { $prose } from '@milkdown/kit/utils'
import { Plugin, PluginKey } from '@milkdown/kit/prose/state'
import { Decoration, DecorationSet } from '@milkdown/kit/prose/view'
import type { Node as ProseNode } from '@milkdown/kit/prose/model'
import { editorViewCtx } from '@milkdown/kit/core'
import type { Ctx } from '@milkdown/ctx'
import { buildFieldPreview } from '../../lib/blocks/field-widget'
import { insertBlock } from '../../lib/milkdown/insert-block'

// Bound-field block. A ` ```field ` fenced code block carries a small YAML-ish
// spec naming a widget type + a target props key; interacting with the widget in
// the READ view writes the chosen value back to pages.props[key] (see
// PATCH /api/pages/{id}/props + FieldWidget). The block body itself never stores
// the value — it's a pointer, mirroring how a poll keeps authoring light and
// puts the interactive surface in the read view.
//
// In the EDITOR this decoration is a static, non-interactive PREVIEW beneath the
// still-editable source (the poll's stance): a field flip is a props write that
// must route through the REST core, and props are outside the Yjs doc, so live
// in-editor interaction is deliberately deferred. The canonical markdown is a
// plain code block, so it round-trips and degrades to readable source anywhere
// the widget isn't supported.
//
// Render core (spec parse + preview DOM) lives in lib/blocks/field-widget.ts,
// Milkdown-free and shared with the read view. Re-exported so importers keep a
// single path.
export { buildFieldPreview }

const fieldKey = new PluginKey('tela-field')

function buildDecorations(doc: ProseNode): DecorationSet {
  const decos: Decoration[] = []
  doc.descendants((node, pos) => {
    if (
      node.type.name === 'code_block' &&
      String(node.attrs.language ?? '').toLowerCase() === 'field'
    ) {
      const code = node.textContent
      if (code.trim().length === 0) return
      decos.push(
        Decoration.widget(pos + node.nodeSize, () => buildFieldPreview(code), {
          side: 1,
          key: `field:${code}`,
        }),
      )
    }
  })
  return DecorationSet.create(doc, decos)
}

export const fieldPlugin = $prose(() => {
  return new Plugin({
    key: fieldKey,
    props: {
      decorations(state) {
        return buildDecorations(state.doc)
      },
    },
  })
})

// Slash inserter: a `field` code block seeded with a pass/fail/pending select —
// the canonical UAT/test-note field.
export function insertField(ctx: Ctx) {
  const view = ctx.get(editorViewCtx)
  const codeType = view.state.schema.nodes.code_block
  if (!codeType) return
  const starter =
    'prop: result\ntype: select\noptions: [pass, fail, pending]\nlabel: Result'
  const node = codeType.create(
    { language: 'field' },
    view.state.schema.text(starter),
  )
  insertBlock(view, node, { caret: 'none' })
}
