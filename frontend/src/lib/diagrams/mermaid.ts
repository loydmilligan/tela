import { getTheme, subscribeToTheme } from '../theme'

// Milkdown-free mermaid render core. SINGLE SOURCE shared by the editor's
// mermaid decoration (milkdown-mermaid.ts) and the read-only view renderer.
// mermaid is heavy, so it's lazy-imported on first render (own chunk). The
// returned element self-renders and is content-keyed via the cache, so the same
// diagram never re-renders. Colours come from the tela design tokens (mermaid's
// `base` theme + themeVariables read at render time) and re-render on theme
// switch, so diagrams stay theme-dynamic — same pattern as the chart core
// (lib/diagrams/chart.ts). See docs/view-edit-split.md.

// Keyed by `${theme}\n${code}` — the same diagram renders once per theme (its
// colours differ), so a light SVG is never served on the dark theme.
const svgCache = new Map<string, string>()

type MermaidApi = {
  initialize: (config: Record<string, unknown>) => void
  render: (id: string, code: string) => Promise<{ svg: string }>
}
let mermaidPromise: Promise<MermaidApi> | null = null

function getMermaid(): Promise<MermaidApi> {
  if (!mermaidPromise) {
    mermaidPromise = import('mermaid').then(
      (m) => m.default as unknown as MermaidApi,
    )
  }
  return mermaidPromise
}

// Resolve the diagram palette from the live design tokens. mermaid rejects
// `var()` in themeVariables (it only accepts literal colours), so we read the
// computed hex off :root and feed those in — which also makes the palette track
// whichever [data-theme] is active (light/dark/warm/future).
function readTheme() {
  const s = getComputedStyle(document.documentElement)
  const g = (n: string, fb: string) => s.getPropertyValue(n).trim() || fb
  return {
    accent: g('--accent', '#4f46e5'),
    accentFg: g('--accent-fg', '#ffffff'),
    surface1: g('--surface-1', '#ffffff'),
    surface2: g('--surface-2', '#f7f7f8'),
    surface3: g('--surface-3', '#ececef'),
    textPrimary: g('--text-primary', '#16161a'),
    textMuted: g('--text-muted', '#5b5b66'),
    border: g('--border-subtle', '#e4e4e9'),
    font: g('--font-sans', 'ui-sans-serif, system-ui, sans-serif'),
  }
}

// A tuned `base` theme: nodes sit on --surface-3 with an --accent border and
// --text-primary labels; edges use --text-muted; subgraphs use --surface-2.
// Softer edges (curve: basis), looser spacing, and a subtle node shadow lift it
// out of stock-mermaid territory. suppressErrorRendering keeps a parse error from
// injecting mermaid's own bomb SVG into the DOM (we show a clean inline message).
function buildConfig(tk: ReturnType<typeof readTheme>): Record<string, unknown> {
  return {
    startOnLoad: false,
    securityLevel: 'strict',
    suppressErrorRendering: true,
    theme: 'base',
    fontFamily: tk.font,
    themeVariables: {
      fontFamily: tk.font,
      fontSize: '15px',
      background: tk.surface2,
      primaryColor: tk.surface3,
      primaryBorderColor: tk.accent,
      primaryTextColor: tk.textPrimary,
      secondaryColor: tk.surface2,
      tertiaryColor: tk.surface1,
      mainBkg: tk.surface3,
      nodeBorder: tk.accent,
      nodeTextColor: tk.textPrimary,
      lineColor: tk.textMuted,
      textColor: tk.textPrimary,
      titleColor: tk.textPrimary,
      edgeLabelBackground: tk.surface1,
      clusterBkg: tk.surface2,
      clusterBorder: tk.border,
      // sequence diagrams
      actorBkg: tk.surface3,
      actorBorder: tk.accent,
      actorTextColor: tk.textPrimary,
      actorLineColor: tk.textMuted,
      signalColor: tk.textMuted,
      signalTextColor: tk.textPrimary,
      labelBoxBkgColor: tk.surface2,
      labelBoxBorderColor: tk.border,
      labelTextColor: tk.textPrimary,
      loopTextColor: tk.textPrimary,
      activationBkgColor: tk.surface3,
      activationBorderColor: tk.accent,
      sequenceNumberColor: tk.accentFg,
      noteBkgColor: tk.surface2,
      noteBorderColor: tk.border,
      noteTextColor: tk.textPrimary,
    },
    flowchart: {
      curve: 'basis',
      nodeSpacing: 50,
      rankSpacing: 58,
      padding: 12,
      htmlLabels: true,
      useMaxWidth: true,
    },
    // mermaid id-scopes every themeCSS selector (`#<id> .node rect …`), so these
    // win over page CSS without !important. themeVariables can't express corner
    // radius / shadow — inject them. The foreignObject rule is load-bearing: with
    // htmlLabels the label text is a <p>, and our prose `p { font-size / margin }`
    // leaks in and renders it larger than the box mermaid measured (at the label
    // container's size) → text overflows / clips. Reset it back to the container's
    // metrics so rendered == measured.
    themeCSS:
      '.node rect,.node polygon{rx:6px;ry:6px}' +
      '.node rect,.node polygon,.node circle,.node ellipse,.node path{filter:drop-shadow(0 1px 1.5px rgba(0,0,0,.10))}' +
      'foreignObject p{font-size:inherit;line-height:inherit;margin:0;padding:0}',
  }
}

async function renderMermaid(code: string, tk: ReturnType<typeof readTheme>): Promise<string> {
  const mermaid = await getMermaid()
  // Re-apply config each render so the active theme's colours are picked up.
  // initialize() just sets global config; concurrent renders share one theme.
  mermaid.initialize(buildConfig(tk))
  const id = 'tela-mmd-' + Math.random().toString(36).slice(2)
  const { svg } = await mermaid.render(id, code)
  return svg
}

// One theme subscription re-renders every live diagram with fresh token colours.
const renderers = new WeakMap<HTMLElement, () => void>()
let themeWired = false
function wireThemeOnce() {
  if (themeWired) return
  themeWired = true
  subscribeToTheme(() => {
    document.querySelectorAll<HTMLElement>('.tela-mermaid').forEach((el) => {
      renderers.get(el)?.()
    })
  })
}

export function buildMermaidElement(code: string): HTMLElement {
  wireThemeOnce()
  const dom = document.createElement('div')
  dom.className = 'tela-mermaid'
  dom.setAttribute('contenteditable', 'false')

  const render = () => {
    const key = getTheme() + '\n' + code
    const cached = svgCache.get(key)
    if (cached) {
      dom.classList.remove('tela-mermaid-error')
      dom.innerHTML = cached
      return
    }
    // Keep any already-rendered diagram visible while the re-theme renders;
    // only show the placeholder on a truly empty (first) mount.
    if (!dom.innerHTML) dom.textContent = 'Rendering diagram…'
    void renderMermaid(code, readTheme())
      .then((svg) => {
        svgCache.set(key, svg)
        dom.classList.remove('tela-mermaid-error')
        dom.innerHTML = svg
      })
      .catch((err: unknown) => {
        dom.classList.add('tela-mermaid-error')
        dom.textContent =
          err instanceof Error ? err.message : 'Could not render diagram'
      })
  }
  renderers.set(dom, render)
  render()
  return dom
}
