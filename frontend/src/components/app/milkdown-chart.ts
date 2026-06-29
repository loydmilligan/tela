import { $prose } from '@milkdown/kit/utils'
import { Plugin, PluginKey } from '@milkdown/kit/prose/state'
import { Decoration, DecorationSet } from '@milkdown/kit/prose/view'
import type { Node as ProseNode } from '@milkdown/kit/prose/model'
import { editorViewCtx } from '@milkdown/kit/core'
import type { Ctx } from '@milkdown/ctx'
import { buildChartWidget } from '../../lib/diagrams/chart'
import { insertBlock } from '../../lib/milkdown/insert-block'

// The chart render core lives in lib/diagrams/chart.ts (Milkdown-free, shared
// with the view renderer). This file keeps the editor decoration + slash insert.
// Re-exported so the Storybook story (and any importer) keeps its import path.
export { buildChartWidget }

// M19 — chart block. A ` ```chart ` fenced code block carrying a small YAML
// spec renders an interactive chart (hover tooltips, clickable legend) beneath
// the still-editable source — same pattern as the mermaid block. ECharts (SVG
// renderer) + the YAML parser are lazy-imported on first render (own chunk), so
// pages without a chart pay nothing. Colours come from the --chart-* design
// tokens and re-render on theme switch, so charts stay theme-dynamic. The
// canonical markdown is a plain code block, so it round-trips and degrades to
// readable source where charts aren't supported.
//
// Supported `type`: bar, grouped-bar, stacked-bar, line, area, scatter, pie,
// donut. xy/category charts take `x` (categories) + `series: [{name, data}]`;
// pie/donut take `data: [{label, value}]`.

const chartKey = new PluginKey('tela-chart')


function buildDecorations(doc: ProseNode): DecorationSet {
  const decos: Decoration[] = []
  doc.descendants((node, pos) => {
    if (
      node.type.name === 'code_block' &&
      String(node.attrs.language ?? '').toLowerCase() === 'chart'
    ) {
      const code = node.textContent
      if (code.trim().length === 0) return
      decos.push(
        Decoration.widget(pos + node.nodeSize, () => buildChartWidget(code), {
          side: 1,
          key: `chart:${code}`,
        }),
      )
    }
  })
  return DecorationSet.create(doc, decos)
}

export const chartPlugin = $prose(() => {
  return new Plugin({
    key: chartKey,
    props: {
      decorations(state) {
        return buildDecorations(state.doc)
      },
    },
  })
})

// Slash inserter: a `chart` code block seeded with a starter bar-chart spec.
export function insertChart(ctx: Ctx) {
  const view = ctx.get(editorViewCtx)
  const codeType = view.state.schema.nodes.code_block
  if (!codeType) return
  const starter =
    'type: bar\ntitle: Quarterly revenue\nx: [Q1, Q2, Q3, Q4]\nseries:\n  - name: Revenue\n    data: [120, 145, 180, 210]'
  const node = codeType.create(
    { language: 'chart' },
    view.state.schema.text(starter),
  )
  insertBlock(view, node, { caret: 'none' })
}
