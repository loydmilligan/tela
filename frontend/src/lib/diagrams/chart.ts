import { subscribeToTheme } from '../theme'

// Milkdown-free chart render core. SINGLE SOURCE shared by the editor's chart
// decoration (milkdown-chart.ts) and the read-only view renderer. ECharts (SVG)
// + the YAML parser are lazy-imported on first render (own chunk). Colours come
// from the --chart-* design tokens and re-render on theme switch. The returned
// element self-renders. See docs/view-edit-split.md.
//
// Supported `type`: bar, grouped-bar, stacked-bar, line, area, scatter, pie,
// donut. xy/category charts take `x` (categories) + `series: [{name, data}]`;
// pie/donut take `data: [{label, value}]`.

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
