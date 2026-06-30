// Milkdown-free mermaid render core. SINGLE SOURCE shared by the editor's
// mermaid decoration (milkdown-mermaid.ts) and the read-only view renderer.
// mermaid is heavy, so it's lazy-imported on first render (own chunk). The
// returned element self-renders and is content-keyed via the cache, so the same
// diagram never re-renders. See docs/view-edit-split.md.

const svgCache = new Map<string, string>()

type MermaidApi = {
  initialize: (config: Record<string, unknown>) => void
  render: (id: string, code: string) => Promise<{ svg: string }>
}
let mermaidPromise: Promise<MermaidApi> | null = null

function getMermaid(): Promise<MermaidApi> {
  if (!mermaidPromise) {
    mermaidPromise = import('mermaid').then((m) => {
      const api = m.default as unknown as MermaidApi
      // suppressErrorRendering: on a parse error mermaid otherwise injects its own
      // "Syntax error in text" bomb SVG into the DOM (orphaned to <body> when it
      // can't find its temp container). We want render() to just throw so our
      // catch shows a clean inline message in the block instead.
      api.initialize({
        startOnLoad: false,
        securityLevel: 'strict',
        theme: 'neutral',
        suppressErrorRendering: true,
      })
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

export function buildMermaidElement(code: string): HTMLElement {
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
