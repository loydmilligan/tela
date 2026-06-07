import { $prose } from '@milkdown/kit/utils'
import { Plugin, PluginKey } from '@milkdown/kit/prose/state'
import { Decoration, DecorationSet } from '@milkdown/kit/prose/view'
import type { Node as ProseNode } from '@milkdown/kit/prose/model'
import { editorViewCtx } from '@milkdown/kit/core'
import type { Ctx } from '@milkdown/ctx'
import { subscribeToTheme } from '../../lib/theme'

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

interface EChartsInstance {
  setOption: (option: unknown, notMerge?: boolean) => void
  resize: () => void
  dispose: () => void
}
interface EChartsModule {
  init: (
    dom: HTMLElement,
    theme: unknown,
    opts: { renderer: string },
  ) => EChartsInstance
}
interface YamlModule {
  load: (input: string) => unknown
}

let echartsPromise: Promise<EChartsModule> | null = null
function getECharts(): Promise<EChartsModule> {
  if (!echartsPromise) {
    echartsPromise = import('echarts') as unknown as Promise<EChartsModule>
  }
  return echartsPromise
}
let yamlPromise: Promise<YamlModule> | null = null
function getYaml(): Promise<YamlModule> {
  if (!yamlPromise) {
    yamlPromise = import('js-yaml') as unknown as Promise<YamlModule>
  }
  return yamlPromise
}

interface ChartSpec {
  type?: string
  title?: string
  x?: (string | number)[]
  categories?: (string | number)[]
  series?: { name?: string; data: unknown[] }[]
  data?: { label?: string; name?: string; value: number }[]
}

function readTokens() {
  const s = getComputedStyle(document.documentElement)
  const g = (n: string) => s.getPropertyValue(n).trim()
  return {
    palette: [
      g('--chart-1'),
      g('--chart-2'),
      g('--chart-3'),
      g('--chart-4'),
      g('--chart-5'),
      g('--chart-6'),
    ].filter(Boolean),
    text: g('--text-primary') || '#16161a',
    muted: g('--text-muted') || '#5b5b66',
    grid: g('--chart-grid') || '#e4e4e9',
    axis: g('--chart-axis') || '#5b5b66',
    surface: g('--surface-1') || '#ffffff',
    font: g('--font-sans') || 'sans-serif',
  }
}

const norm = (t: string) => t.toLowerCase().replace(/_/g, '-')

function buildOption(
  spec: ChartSpec,
  tk: ReturnType<typeof readTokens>,
): unknown {
  const type = norm(spec.type || 'bar')
  const title = spec.title
    ? {
        text: spec.title,
        left: 'center',
        textStyle: {
          color: tk.text,
          fontFamily: tk.font,
          fontSize: 14,
          fontWeight: 600,
        },
      }
    : undefined
  const base = {
    color: tk.palette,
    textStyle: { color: tk.text, fontFamily: tk.font },
    title,
    animation: false,
  }

  if (type === 'pie' || type === 'donut') {
    const data = (spec.data || []).map((d) => ({
      name: d.label ?? d.name ?? '',
      value: d.value,
    }))
    return {
      ...base,
      tooltip: { trigger: 'item' },
      legend: {
        bottom: 0,
        textStyle: { color: tk.muted, fontFamily: tk.font },
      },
      series: [
        {
          type: 'pie',
          radius: type === 'donut' ? ['45%', '70%'] : '62%',
          center: ['50%', spec.title ? '54%' : '50%'],
          data,
          label: { color: tk.text, fontFamily: tk.font },
          itemStyle: { borderColor: tk.surface, borderWidth: 2 },
        },
      ],
    }
  }

  const cats = spec.x || spec.categories || []
  const seriesIn = spec.series || []
  const multi = seriesIn.length > 1
  const isBar = type.includes('bar')
  const isScatter = type === 'scatter'
  const axis = {
    axisLine: { lineStyle: { color: tk.grid } },
    axisTick: { show: false },
    axisLabel: { color: tk.axis, fontFamily: tk.font },
    splitLine: { lineStyle: { color: tk.grid } },
  }
  return {
    ...base,
    tooltip: { trigger: isScatter ? 'item' : 'axis' },
    legend: multi
      ? { bottom: 0, textStyle: { color: tk.muted, fontFamily: tk.font } }
      : { show: false },
    grid: {
      left: 8,
      right: 16,
      top: spec.title ? 44 : 16,
      bottom: multi ? 36 : 8,
      containLabel: true,
    },
    xAxis: isScatter
      ? { type: 'value', ...axis }
      : { type: 'category', data: cats, boundaryGap: isBar, ...axis },
    yAxis: { type: 'value', ...axis },
    series: seriesIn.map((srs) => ({
      name: srs.name,
      type: type === 'area' ? 'line' : isScatter ? 'scatter' : isBar ? 'bar' : 'line',
      data: srs.data,
      areaStyle: type === 'area' ? { opacity: 0.15 } : undefined,
      smooth: type === 'line' || type === 'area' ? true : undefined,
      showSymbol: type === 'line' || type === 'area' ? false : undefined,
      stack: type === 'stacked-bar' ? 'total' : undefined,
      symbolSize: isScatter ? 10 : undefined,
      barMaxWidth: isBar ? 40 : undefined,
    })),
  }
}

// One theme subscription re-renders every live chart with fresh token colours.
const renderers = new WeakMap<HTMLElement, () => void>()
let themeWired = false
function wireThemeOnce() {
  if (themeWired) return
  themeWired = true
  subscribeToTheme(() => {
    document.querySelectorAll<HTMLElement>('.tela-chart').forEach((el) => {
      renderers.get(el)?.()
    })
  })
}

// Exported so the Storybook story renders a real chart through the same path.
export function buildChartWidget(code: string): HTMLElement {
  wireThemeOnce()
  const dom = document.createElement('div')
  dom.className = 'tela-chart'
  dom.setAttribute('contenteditable', 'false')
  const canvas = document.createElement('div')
  canvas.className = 'tela-chart-canvas'
  dom.appendChild(canvas)

  let chart: EChartsInstance | null = null
  const render = () => {
    void Promise.all([getECharts(), getYaml()])
      .then(([echarts, yaml]) => {
        const spec = (yaml.load(code) ?? {}) as ChartSpec
        if (typeof spec !== 'object' || Array.isArray(spec)) {
          throw new Error('chart spec must be a YAML mapping')
        }
        if (!chart) chart = echarts.init(canvas, null, { renderer: 'svg' })
        chart.setOption(buildOption(spec, readTokens()), true)
        dom.classList.remove('tela-chart-error')
      })
      .catch((e: unknown) => {
        dom.classList.add('tela-chart-error')
        canvas.textContent =
          e instanceof Error ? e.message : 'Could not render chart'
      })
  }
  renderers.set(dom, render)
  render()

  const ro = new ResizeObserver(() => chart?.resize())
  ro.observe(canvas)
  return dom
}

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
  view.dispatch(view.state.tr.replaceSelectionWith(node).scrollIntoView())
  view.focus()
}
