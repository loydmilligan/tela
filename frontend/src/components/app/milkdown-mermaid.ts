import { $prose } from '@milkdown/kit/utils'
import { Plugin, PluginKey } from '@milkdown/kit/prose/state'
import { Decoration, DecorationSet } from '@milkdown/kit/prose/view'
import type { Node as ProseNode } from '@milkdown/kit/prose/model'
import { editorViewCtx } from '@milkdown/kit/core'
import type { Ctx } from '@milkdown/ctx'

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
const svgCache = new Map<string, string>()

type MermaidApi = {
  initialize: (config: Record<string, unknown>) => void
  render: (id: string, code: string) => Promise<{ svg: string }>
}
let mermaidPromise: Promise<MermaidApi> | null = null

async function getMermaid(): Promise<MermaidApi> {
  if (!mermaidPromise) {
    mermaidPromise = import('mermaid').then((m) => {
      const api = m.default as unknown as MermaidApi
      api.initialize({ startOnLoad: false, securityLevel: 'strict', theme: 'neutral' })
      return api
    })
  }
  return mermaidPromise
}

async function renderMermaid(code: string): Promise<string> {
  const mermaid = await getMermaid()
  const id = 'tela-mmd-' + Math.random().toString(36).slice(2)
  const { svg } = await mermaid.render(id, code)
  return svg
}

function buildWidget(code: string): HTMLElement {
  const dom = document.createElement('div')
  dom.className = 'tela-mermaid'
  dom.setAttribute('contenteditable', 'false')
  const cached = svgCache.get(code)
  if (cached) {
    dom.innerHTML = cached
    return dom
  }
  dom.textContent = 'Rendering diagram…'
  void renderMermaid(code)
    .then((svg) => {
      svgCache.set(code, svg)
      dom.innerHTML = svg
    })
    .catch((err: unknown) => {
      dom.classList.add('tela-mermaid-error')
      dom.textContent =
        err instanceof Error ? err.message : 'Could not render diagram'
    })
  return dom
}

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
        Decoration.widget(pos + node.nodeSize, () => buildWidget(code), {
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
