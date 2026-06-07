import { useEffect, useRef } from 'react'
import {
  forceCenter,
  forceCollide,
  forceLink,
  forceManyBody,
  forceSimulation,
  forceX,
  forceY,
  type Simulation,
  type SimulationLinkDatum,
  type SimulationNodeDatum,
} from 'd3-force'
import { subscribeToTheme } from '../../lib/theme'
import { buildSpacePalette, type GraphLink, type GraphNode } from '../../lib/queries/graph'

// Interactive force-directed graph on a <canvas>: d3-force does the layout, a
// thin hand-rolled renderer + pointer layer does the rest (pan / wheel-zoom /
// node-drag / hover-focus / click-to-navigate). Canvas (not SVG) so a few
// hundred nodes stay smooth; all colors come from CSS tokens read off the live
// computed style, so it re-themes with the app. Node size = degree, color =
// space; "link" edges are solid, "tree" (hierarchy) edges are dashed.

interface SimNode extends SimulationNodeDatum {
  id: number
  spaceId: number
  title: string
  deg: number
  r: number
}
interface SimLink extends SimulationLinkDatum<SimNode> {
  kind: 'link' | 'tree'
}

export interface PageGraphProps {
  nodes: GraphNode[]
  links: GraphLink[]
  // Which edge kinds to draw (layout always uses both so positions stay stable).
  showLinks: boolean
  showTree: boolean
  // Ring + always-label this node (the page the graph is focused on).
  currentId?: number | null
  // Search highlight: when non-null, matched nodes pop and the rest dim.
  matchedIds?: Set<number> | null
  onNavigate: (id: number) => void
}

const MIN_K = 0.2
const MAX_K = 4
const CLICK_SLOP = 4 // px of movement under which a pointerup counts as a click

function nodeRadius(deg: number): number {
  return Math.min(4 + Math.sqrt(deg) * 1.8, 16)
}

export function PageGraph({
  nodes,
  links,
  showLinks,
  showTree,
  currentId,
  matchedIds,
  onNavigate,
}: PageGraphProps) {
  const canvasRef = useRef<HTMLCanvasElement>(null)
  const simRef = useRef<Simulation<SimNode, SimLink> | null>(null)
  const nodeCacheRef = useRef<Map<number, SimNode>>(new Map())
  const simNodesRef = useRef<SimNode[]>([])
  const simLinksRef = useRef<SimLink[]>([])
  const adjRef = useRef<Map<number, Set<number>>>(new Map())
  const transformRef = useRef({ k: 1, x: 0, y: 0 })
  const hoveredRef = useRef<number | null>(null)
  const colorsRef = useRef<Record<string, string>>({})
  // Latest render-affecting props, read inside the (stable) draw loop.
  const propsRef = useRef({ showLinks, showTree, currentId, matchedIds })
  propsRef.current = { showLinks, showTree, currentId, matchedIds }
  const navRef = useRef(onNavigate)
  navRef.current = onNavigate

  // Resolve all token colors off the live computed style; refresh on theme flip.
  useEffect(() => {
    const el = canvasRef.current
    if (!el) return
    const read = () => {
      const cs = getComputedStyle(el)
      const get = (v: string) => cs.getPropertyValue(v).trim()
      const palette = buildSpacePalette(nodes)
      const spaceColors: Record<string, string> = {}
      for (const p of palette) spaceColors[`space:${p.spaceId}`] = get(p.varName)
      colorsRef.current = {
        ...spaceColors,
        accent: get('--accent'),
        text: get('--text-primary'),
        muted: get('--text-muted'),
        edge: get('--border-strong'),
        surface: get('--surface-1'),
      }
      draw()
    }
    read()
    return subscribeToTheme(read)
  }, [nodes])

  // (Re)build the simulation when the data changes. Node objects are cached by
  // id so x/y survive a rebuild (stable layout across filter/toggle changes).
  useEffect(() => {
    const cache = nodeCacheRef.current
    const deg = new Map<number, number>()
    for (const l of links) {
      deg.set(l.source, (deg.get(l.source) ?? 0) + 1)
      deg.set(l.target, (deg.get(l.target) ?? 0) + 1)
    }
    const live = new Set(nodes.map((n) => n.id))
    for (const id of [...cache.keys()]) if (!live.has(id)) cache.delete(id)
    const simNodes = nodes.map((n) => {
      const existing = cache.get(n.id)
      const d = deg.get(n.id) ?? 0
      if (existing) {
        existing.spaceId = n.space_id
        existing.title = n.title
        existing.deg = d
        existing.r = nodeRadius(d)
        return existing
      }
      const created: SimNode = {
        id: n.id,
        spaceId: n.space_id,
        title: n.title,
        deg: d,
        r: nodeRadius(d),
      }
      cache.set(n.id, created)
      return created
    })
    const byId = new Map(simNodes.map((n) => [n.id, n]))
    const simLinks: SimLink[] = links
      .filter((l) => byId.has(l.source) && byId.has(l.target))
      .map((l) => ({ source: l.source, target: l.target, kind: l.kind }))

    const adj = new Map<number, Set<number>>()
    for (const l of links) {
      if (!adj.has(l.source)) adj.set(l.source, new Set())
      if (!adj.has(l.target)) adj.set(l.target, new Set())
      adj.get(l.source)!.add(l.target)
      adj.get(l.target)!.add(l.source)
    }
    adjRef.current = adj
    simNodesRef.current = simNodes
    simLinksRef.current = simLinks

    let sim = simRef.current
    if (!sim) {
      sim = forceSimulation<SimNode, SimLink>()
        .force('charge', forceManyBody<SimNode>().strength(-180))
        .force(
          'link',
          forceLink<SimNode, SimLink>()
            .id((d) => d.id)
            .distance(60)
            .strength(0.5),
        )
        .force('x', forceX<SimNode>().strength(0.04))
        .force('y', forceY<SimNode>().strength(0.04))
        .force('collide', forceCollide<SimNode>((d) => d.r + 4))
        .force('center', forceCenter(0, 0))
      sim.on('tick', draw)
      simRef.current = sim
    }
    sim.nodes(simNodes)
    sim.force<ReturnType<typeof forceLink<SimNode, SimLink>>>('link')?.links(simLinks)
    sim.alpha(0.7).restart()
    return () => {}
  }, [nodes, links])

  // Tear the simulation down only on unmount.
  useEffect(() => {
    return () => {
      simRef.current?.stop()
      simRef.current = null
    }
  }, [])

  // --- rendering ----------------------------------------------------------
  function draw() {
    const canvas = canvasRef.current
    if (!canvas) return
    const ctx = canvas.getContext('2d')
    if (!ctx) return
    const dpr = window.devicePixelRatio || 1
    const w = canvas.clientWidth
    const h = canvas.clientHeight
    if (canvas.width !== w * dpr || canvas.height !== h * dpr) {
      canvas.width = w * dpr
      canvas.height = h * dpr
    }
    const { k, x, y } = transformRef.current
    const c = colorsRef.current
    const { showLinks: sl, showTree: st, currentId: cur, matchedIds: matched } =
      propsRef.current
    const hovered = hoveredRef.current
    const adj = adjRef.current
    const focusSet =
      hovered != null
        ? new Set<number>([hovered, ...(adj.get(hovered) ?? [])])
        : null

    ctx.save()
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0)
    ctx.clearRect(0, 0, w, h)
    // World transform: center the origin, then apply pan/zoom.
    ctx.translate(w / 2 + x, h / 2 + y)
    ctx.scale(k, k)

    const dimmed = (id: number): boolean => {
      if (focusSet) return !focusSet.has(id)
      if (matched) return !matched.has(id)
      return false
    }

    // Edges.
    for (const l of simLinksRef.current) {
      if (l.kind === 'link' && !sl) continue
      if (l.kind === 'tree' && !st) continue
      const s = l.source as SimNode
      const t = l.target as SimNode
      if (s.x == null || t.x == null) continue
      const lit =
        focusSet != null && (focusSet.has(s.id) && focusSet.has(t.id))
      ctx.beginPath()
      ctx.moveTo(s.x, s.y!)
      ctx.lineTo(t.x!, t.y!)
      ctx.strokeStyle = l.kind === 'tree' ? c.muted : c.edge
      ctx.globalAlpha = (focusSet ? (lit ? 0.9 : 0.06) : 0.35) * (l.kind === 'tree' ? 0.8 : 1)
      ctx.lineWidth = (lit ? 1.5 : 1) / k
      if (l.kind === 'tree') ctx.setLineDash([3 / k, 3 / k])
      else ctx.setLineDash([])
      ctx.stroke()
    }
    ctx.setLineDash([])

    // Nodes.
    for (const n of simNodesRef.current) {
      if (n.x == null) continue
      const isDim = dimmed(n.id)
      ctx.globalAlpha = isDim ? 0.2 : 1
      ctx.beginPath()
      ctx.arc(n.x, n.y!, n.r, 0, Math.PI * 2)
      ctx.fillStyle = c[`space:${n.spaceId}`] ?? c.accent
      ctx.fill()
      if (n.id === cur || (matched && matched.has(n.id)) || n.id === hovered) {
        ctx.lineWidth = 2 / k
        ctx.strokeStyle = c.accent
        ctx.stroke()
      }
    }
    ctx.globalAlpha = 1

    // Labels: hovered + its neighbors, the focused node, search matches, and —
    // when zoomed in — everything. Drawn after nodes so they sit on top.
    ctx.font = `${12 / k}px ui-sans-serif, system-ui, sans-serif`
    ctx.fillStyle = c.text
    ctx.textAlign = 'center'
    ctx.textBaseline = 'top'
    for (const n of simNodesRef.current) {
      if (n.x == null) continue
      const labelled =
        n.id === cur ||
        n.id === hovered ||
        (focusSet?.has(n.id) ?? false) ||
        (matched?.has(n.id) ?? false) ||
        k > 1.6
      if (!labelled) continue
      ctx.globalAlpha = dimmed(n.id) ? 0.25 : 1
      const label = n.title.length > 28 ? n.title.slice(0, 27) + '…' : n.title || 'Untitled'
      ctx.fillText(label, n.x, n.y! + n.r + 3 / k)
    }
    ctx.globalAlpha = 1
    ctx.restore()
  }

  // --- pointer interaction ------------------------------------------------
  useEffect(() => {
    const canvas = canvasRef.current
    if (!canvas) return

    const toWorld = (clientX: number, clientY: number) => {
      const rect = canvas.getBoundingClientRect()
      const { k, x, y } = transformRef.current
      return {
        x: (clientX - rect.left - rect.width / 2 - x) / k,
        y: (clientY - rect.top - rect.height / 2 - y) / k,
      }
    }
    const hitTest = (wx: number, wy: number): SimNode | null => {
      let best: SimNode | null = null
      let bestD = Infinity
      for (const n of simNodesRef.current) {
        if (n.x == null) continue
        const dx = n.x - wx
        const dy = n.y! - wy
        const d = dx * dx + dy * dy
        const hit = n.r + 4
        if (d < hit * hit && d < bestD) {
          bestD = d
          best = n
        }
      }
      return best
    }

    let mode: 'none' | 'pan' | 'drag' = 'none'
    let dragNode: SimNode | null = null
    let downX = 0
    let downY = 0
    let panStart = { x: 0, y: 0, tx: 0, ty: 0 }

    const onPointerDown = (e: PointerEvent) => {
      canvas.setPointerCapture(e.pointerId)
      downX = e.clientX
      downY = e.clientY
      const wpt = toWorld(e.clientX, e.clientY)
      const hit = hitTest(wpt.x, wpt.y)
      if (hit) {
        mode = 'drag'
        dragNode = hit
        simRef.current?.alphaTarget(0.3).restart()
        hit.fx = hit.x
        hit.fy = hit.y
      } else {
        mode = 'pan'
        const t = transformRef.current
        panStart = { x: e.clientX, y: e.clientY, tx: t.x, ty: t.y }
      }
    }
    const onPointerMove = (e: PointerEvent) => {
      if (mode === 'none') {
        const wpt = toWorld(e.clientX, e.clientY)
        const hit = hitTest(wpt.x, wpt.y)
        const id = hit?.id ?? null
        if (id !== hoveredRef.current) {
          hoveredRef.current = id
          canvas.style.cursor = id != null ? 'pointer' : 'default'
          draw()
        }
        return
      }
      if (mode === 'drag' && dragNode) {
        const wpt = toWorld(e.clientX, e.clientY)
        dragNode.fx = wpt.x
        dragNode.fy = wpt.y
      } else if (mode === 'pan') {
        transformRef.current.x = panStart.tx + (e.clientX - panStart.x)
        transformRef.current.y = panStart.ty + (e.clientY - panStart.y)
        draw()
      }
    }
    const onPointerUp = (e: PointerEvent) => {
      canvas.releasePointerCapture(e.pointerId)
      const wasClick =
        Math.abs(e.clientX - downX) + Math.abs(e.clientY - downY) < CLICK_SLOP
      if (mode === 'drag' && dragNode) {
        dragNode.fx = null
        dragNode.fy = null
        simRef.current?.alphaTarget(0)
        if (wasClick) navRef.current(dragNode.id)
      }
      mode = 'none'
      dragNode = null
    }
    const onWheel = (e: WheelEvent) => {
      e.preventDefault()
      const t = transformRef.current
      const rect = canvas.getBoundingClientRect()
      // Pointer position relative to the world origin (canvas center + pan).
      const px = e.clientX - rect.left - rect.width / 2 - t.x
      const py = e.clientY - rect.top - rect.height / 2 - t.y
      const factor = Math.exp(-e.deltaY * 0.0015)
      const k = Math.max(MIN_K, Math.min(MAX_K, t.k * factor))
      const ratio = k / t.k
      t.x -= px * (ratio - 1)
      t.y -= py * (ratio - 1)
      t.k = k
      draw()
    }

    canvas.addEventListener('pointerdown', onPointerDown)
    canvas.addEventListener('pointermove', onPointerMove)
    canvas.addEventListener('pointerup', onPointerUp)
    canvas.addEventListener('wheel', onWheel, { passive: false })
    const onResize = () => draw()
    window.addEventListener('resize', onResize)
    return () => {
      canvas.removeEventListener('pointerdown', onPointerDown)
      canvas.removeEventListener('pointermove', onPointerMove)
      canvas.removeEventListener('pointerup', onPointerUp)
      canvas.removeEventListener('wheel', onWheel)
      window.removeEventListener('resize', onResize)
    }
  }, [])

  // Redraw when render-only props (toggles / search / focus) change.
  useEffect(() => {
    draw()
  }, [showLinks, showTree, currentId, matchedIds])

  return (
    <canvas
      ref={canvasRef}
      className="block h-full w-full touch-none select-none"
      aria-label="Page graph"
    />
  )
}
