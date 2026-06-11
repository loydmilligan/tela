import { useEffect, useRef, useState } from 'react'
import {
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
import { parseSqliteTs } from '../../lib/types'
import { relativeTimeFromSqlite } from '../../lib/relativeTime'
import { buildSpacePalette, type GraphLink, type GraphNode } from '../../lib/queries/graph'

// Interactive force-directed graph on a <canvas>: d3-force does the layout, a
// thin hand-rolled renderer + pointer layer does the rest (pan / wheel-zoom /
// node-drag / hover-focus / click-to-navigate / zoom-to-fit). Canvas (not SVG)
// so a few hundred nodes stay smooth; all colors come from CSS tokens read off
// the live computed style, so it re-themes with the app. Node size = degree,
// color = space; same-space nodes are pulled into clusters. "link" edges are
// solid + arrowed, "tree" (hierarchy) edges are dashed.

interface SimNode extends SimulationNodeDatum {
  id: number
  spaceId: number
  spaceName: string
  breadcrumb: string[]
  title: string
  updatedAt: string
  deg: number
  r: number
  ageT: number // 0 = newest, 1 = oldest (for recency tint)
  broken: number
}
interface SimLink extends SimulationLinkDatum<SimNode> {
  kind: 'link' | 'tree' | 'semantic'
}

// Surfaced to the React overlay when a node is hovered long enough.
interface HoverCard {
  x: number // container-relative position
  y: number
  flipX: boolean // place left of the cursor (near right edge)
  flipY: boolean // place above the cursor (near bottom edge)
  title: string
  location: string // "Space › …ancestors", collapsed when deep
  updatedAt: string
  broken: number
  linksOut: number
  linksIn: number
  children: number
}

// Build the card's one-line location: space + ancestor path, collapsed to
// `first › … › last` past 3 segments so a deep tree never wraps or squashes.
function locationLine(spaceName: string, breadcrumb: string[]): string {
  const segs = [spaceName, ...breadcrumb]
  const shown =
    segs.length > 3 ? [segs[0], '…', segs[segs.length - 1]] : segs
  return shown.join(' › ')
}

export interface PageGraphProps {
  nodes: GraphNode[]
  links: GraphLink[]
  showLinks: boolean
  showTree: boolean
  // Embedding-similarity overlay. Optional (defaults off) so the on-page
  // LocalGraph — authored edges only — needs no change.
  showSemantic?: boolean
  recency: boolean
  currentId?: number | null
  matchedIds?: Set<number> | null
  onNavigate: (id: number) => void
  // Changes to (re)fit the view to the current layout (Fit button / focus change).
  fitSignal?: string | number
}

const MIN_K = 0.2
const MAX_K = 4
const CLICK_SLOP = 4

function nodeRadius(deg: number): number {
  return Math.min(4 + Math.sqrt(deg) * 1.8, 16)
}

export function PageGraph({
  nodes,
  links,
  showLinks,
  showTree,
  showSemantic = false,
  recency,
  currentId,
  matchedIds,
  onNavigate,
  fitSignal,
}: PageGraphProps) {
  const canvasRef = useRef<HTMLCanvasElement>(null)
  const simRef = useRef<Simulation<SimNode, SimLink> | null>(null)
  const nodeCacheRef = useRef<Map<number, SimNode>>(new Map())
  const simNodesRef = useRef<SimNode[]>([])
  const simLinksRef = useRef<SimLink[]>([])
  const adjRef = useRef<Map<number, Set<number>>>(new Map())
  const centroidRef = useRef<Map<number, { x: number; y: number }>>(new Map())
  const transformRef = useRef({ k: 1, x: 0, y: 0 })
  const hoveredRef = useRef<number | null>(null)
  const colorsRef = useRef<Record<string, string>>({})
  const needsFitRef = useRef(true)
  const [card, setCard] = useState<HoverCard | null>(null)
  const cardTimerRef = useRef<number | undefined>(undefined)
  const propsRef = useRef({ showLinks, showTree, showSemantic, recency, currentId, matchedIds })
  propsRef.current = { showLinks, showTree, showSemantic, recency, currentId, matchedIds }
  const navRef = useRef(onNavigate)
  navRef.current = onNavigate

  // Resolve token colors off the live computed style; refresh on theme flip.
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
        danger: get('--danger'),
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
      // Node size = authored connectivity; semantic overlay edges don't resize
      // nodes (so toggling the lens doesn't reflow the whole layout).
      if (l.kind === 'semantic') continue
      deg.set(l.source, (deg.get(l.source) ?? 0) + 1)
      deg.set(l.target, (deg.get(l.target) ?? 0) + 1)
    }
    // Recency: map each node's updated_at to [0,1] across the visible set.
    let minTs = Infinity
    let maxTs = -Infinity
    const ts = new Map<number, number>()
    for (const n of nodes) {
      const raw = parseSqliteTs(n.updated_at).getTime()
      const t = Number.isNaN(raw) ? 0 : raw
      ts.set(n.id, t)
      if (t < minTs) minTs = t
      if (t > maxTs) maxTs = t
    }
    const span = maxTs - minTs

    const live = new Set(nodes.map((n) => n.id))
    for (const id of [...cache.keys()]) if (!live.has(id)) cache.delete(id)
    const simNodes = nodes.map((n) => {
      const d = deg.get(n.id) ?? 0
      const ageT = span > 0 ? 1 - ((ts.get(n.id) ?? minTs) - minTs) / span : 0
      const existing = cache.get(n.id)
      if (existing) {
        Object.assign(existing, {
          spaceId: n.space_id,
          spaceName: n.space_name,
          breadcrumb: n.breadcrumb,
          title: n.title,
          updatedAt: n.updated_at,
          deg: d,
          r: nodeRadius(d),
          ageT,
          broken: n.broken,
        })
        return existing
      }
      const created: SimNode = {
        id: n.id,
        spaceId: n.space_id,
        spaceName: n.space_name,
        breadcrumb: n.breadcrumb,
        title: n.title,
        updatedAt: n.updated_at,
        deg: d,
        r: nodeRadius(d),
        ageT,
        broken: n.broken,
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

    // Per-space cluster centroids on a ring, so same-space pages group up.
    const spaceIds = [...new Set(simNodes.map((n) => n.spaceId))].sort((a, b) => a - b)
    const R = Math.max(120, simNodes.length * 4)
    const centroids = new Map<number, { x: number; y: number }>()
    spaceIds.forEach((sid, i) => {
      if (spaceIds.length === 1) centroids.set(sid, { x: 0, y: 0 })
      else {
        const a = (2 * Math.PI * i) / spaceIds.length
        centroids.set(sid, { x: Math.cos(a) * R, y: Math.sin(a) * R })
      }
    })
    centroidRef.current = centroids

    simNodesRef.current = simNodes
    simLinksRef.current = simLinks
    needsFitRef.current = true

    let sim = simRef.current
    if (!sim) {
      sim = forceSimulation<SimNode, SimLink>()
        .force('charge', forceManyBody<SimNode>().strength(-180))
        .force(
          'link',
          forceLink<SimNode, SimLink>().id((d) => d.id).distance(55).strength(0.45),
        )
        .force('collide', forceCollide<SimNode>((d) => d.r + 4))
      sim.on('tick', onTick)
      simRef.current = sim
    }
    // Cluster forces read the current centroid map (reassigned each rebuild).
    sim
      .force('x', forceX<SimNode>((d) => centroidRef.current.get(d.spaceId)?.x ?? 0).strength(0.12))
      .force('y', forceY<SimNode>((d) => centroidRef.current.get(d.spaceId)?.y ?? 0).strength(0.12))
    sim.nodes(simNodes)
    sim.force<ReturnType<typeof forceLink<SimNode, SimLink>>>('link')?.links(simLinks)
    sim.alpha(0.8).restart()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [nodes, links])

  useEffect(() => {
    return () => {
      simRef.current?.stop()
      simRef.current = null
    }
  }, [])

  // Refit on external signal (Fit button / focus change).
  useEffect(() => {
    if (fitSignal === undefined) return
    fitToView()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [fitSignal])

  function onTick() {
    // Auto-fit once the freshly (re)heated layout has settled.
    if (needsFitRef.current && (simRef.current?.alpha() ?? 1) < 0.12) {
      needsFitRef.current = false
      fitToView()
      return
    }
    draw()
  }

  function fitToView() {
    const canvas = canvasRef.current
    const ns = simNodesRef.current
    if (!canvas || ns.length === 0) return
    let minX = Infinity
    let maxX = -Infinity
    let minY = Infinity
    let maxY = -Infinity
    for (const n of ns) {
      if (n.x == null) continue
      minX = Math.min(minX, n.x)
      maxX = Math.max(maxX, n.x)
      minY = Math.min(minY, n.y!)
      maxY = Math.max(maxY, n.y!)
    }
    if (!Number.isFinite(minX)) return
    const w = canvas.clientWidth
    const h = canvas.clientHeight
    const pad = 80
    const gw = Math.max(maxX - minX, 1)
    const gh = Math.max(maxY - minY, 1)
    const k = Math.max(MIN_K, Math.min(MAX_K, Math.min((w - pad) / gw, (h - pad) / gh)))
    const cx = (minX + maxX) / 2
    const cy = (minY + maxY) / 2
    transformRef.current = { k, x: -cx * k, y: -cy * k }
    draw()
  }

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
    const p = propsRef.current
    const hovered = hoveredRef.current
    const adj = adjRef.current
    const focusSet =
      hovered != null ? new Set<number>([hovered, ...(adj.get(hovered) ?? [])]) : null

    ctx.save()
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0)
    ctx.clearRect(0, 0, w, h)
    ctx.translate(w / 2 + x, h / 2 + y)
    ctx.scale(k, k)

    const dimmed = (id: number): boolean => {
      if (focusSet) return !focusSet.has(id)
      if (p.matchedIds) return !p.matchedIds.has(id)
      return false
    }

    // Edges.
    for (const l of simLinksRef.current) {
      if (l.kind === 'link' && !p.showLinks) continue
      if (l.kind === 'tree' && !p.showTree) continue
      if (l.kind === 'semantic' && !p.showSemantic) continue
      const s = l.source as SimNode
      const t = l.target as SimNode
      if (s.x == null || t.x == null) continue
      const lit = focusSet != null && focusSet.has(s.id) && focusSet.has(t.id)
      // Semantic edges: a soft dashed accent thread (no arrowhead — they're
      // undirected affinity, not authored references), fainter than real links so
      // the authored graph still reads as primary. A semantic edge with no link
      // beneath it is a visible "should these be connected?" — the structural hole.
      if (l.kind === 'semantic') {
        ctx.beginPath()
        ctx.moveTo(s.x, s.y!)
        ctx.lineTo(t.x!, t.y!)
        ctx.strokeStyle = c.accent
        ctx.globalAlpha = focusSet ? (lit ? 0.6 : 0.04) : 0.32
        ctx.lineWidth = (lit ? 1.4 : 1) / k
        ctx.setLineDash([2 / k, 3 / k])
        ctx.stroke()
        continue
      }
      ctx.beginPath()
      ctx.moveTo(s.x, s.y!)
      ctx.lineTo(t.x!, t.y!)
      ctx.strokeStyle = l.kind === 'tree' ? c.muted : c.edge
      ctx.globalAlpha = (focusSet ? (lit ? 0.9 : 0.05) : 0.3) * (l.kind === 'tree' ? 0.8 : 1)
      ctx.lineWidth = (lit ? 1.5 : 1) / k
      ctx.setLineDash(l.kind === 'tree' ? [3 / k, 3 / k] : [])
      ctx.stroke()
      // Arrowhead for reference (link) edges, at the target's rim.
      if (l.kind === 'link') {
        const dx = t.x! - s.x
        const dy = t.y! - s.y!
        const len = Math.hypot(dx, dy) || 1
        const ux = dx / len
        const uy = dy / len
        const tipX = t.x! - ux * (t.r + 1.5 / k)
        const tipY = t.y! - uy * (t.r + 1.5 / k)
        const ah = 5 / k
        ctx.setLineDash([])
        ctx.beginPath()
        ctx.moveTo(tipX, tipY)
        ctx.lineTo(tipX - ux * ah + -uy * ah * 0.6, tipY - uy * ah + ux * ah * 0.6)
        ctx.lineTo(tipX - ux * ah + uy * ah * 0.6, tipY - uy * ah - ux * ah * 0.6)
        ctx.closePath()
        ctx.fillStyle = c.edge
        ctx.fill()
      }
    }
    ctx.setLineDash([])

    // Nodes.
    for (const n of simNodesRef.current) {
      if (n.x == null) continue
      let alpha = dimmed(n.id) ? 0.2 : 1
      if (p.recency && !dimmed(n.id)) alpha *= 1 - 0.65 * n.ageT
      ctx.globalAlpha = alpha
      ctx.beginPath()
      ctx.arc(n.x, n.y!, n.r, 0, Math.PI * 2)
      ctx.fillStyle = c[`space:${n.spaceId}`] ?? c.accent
      ctx.fill()
      // Broken-link warning ring.
      if (n.broken > 0) {
        ctx.lineWidth = 2 / k
        ctx.strokeStyle = c.danger
        ctx.stroke()
      }
      if (
        n.id === p.currentId ||
        (p.matchedIds && p.matchedIds.has(n.id)) ||
        n.id === hovered
      ) {
        ctx.lineWidth = 2 / k
        ctx.strokeStyle = c.accent
        ctx.stroke()
      }
    }
    ctx.globalAlpha = 1

    // Labels with a halo for legibility over edges.
    ctx.font = `${12 / k}px ui-sans-serif, system-ui, sans-serif`
    ctx.textAlign = 'center'
    ctx.textBaseline = 'top'
    ctx.lineJoin = 'round'
    ctx.lineWidth = 3 / k
    for (const n of simNodesRef.current) {
      if (n.x == null) continue
      const labelled =
        n.id === p.currentId ||
        n.id === hovered ||
        (focusSet?.has(n.id) ?? false) ||
        (p.matchedIds?.has(n.id) ?? false) ||
        k > 1.6
      if (!labelled) continue
      ctx.globalAlpha = dimmed(n.id) ? 0.25 : 1
      const label = n.title.length > 28 ? n.title.slice(0, 27) + '…' : n.title || 'Untitled'
      const ly = n.y! + n.r + 3 / k
      ctx.strokeStyle = c.surface
      ctx.strokeText(label, n.x, ly)
      ctx.fillStyle = c.text
      ctx.fillText(label, n.x, ly)
    }
    ctx.globalAlpha = 1
    ctx.restore()
  }

  // Connection counts for the hover card, derived from the visible edges.
  function connectionCounts(id: number) {
    let linksOut = 0
    let linksIn = 0
    let children = 0
    for (const l of simLinksRef.current) {
      const s = (l.source as SimNode).id
      const t = (l.target as SimNode).id
      if (l.kind === 'link') {
        if (s === id) linksOut++
        else if (t === id) linksIn++
      } else if (l.kind === 'tree' && s === id) {
        children++
      }
    }
    return { linksOut, linksIn, children }
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

    const hideCard = () => {
      window.clearTimeout(cardTimerRef.current)
      setCard(null)
    }

    const onPointerDown = (e: PointerEvent) => {
      hideCard()
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
          // Hover card: hide on change, then show after a short dwell.
          hideCard()
          if (hit) {
            const rect = canvas.getBoundingClientRect()
            const cx = e.clientX - rect.left
            const cy = e.clientY - rect.top
            const flipX = cx > rect.width - 232
            const flipY = cy > rect.height - 132
            const node = hit
            cardTimerRef.current = window.setTimeout(() => {
              setCard({
                x: cx,
                y: cy,
                flipX,
                flipY,
                title: node.title,
                location: locationLine(node.spaceName, node.breadcrumb),
                updatedAt: node.updatedAt,
                broken: node.broken,
                ...connectionCounts(node.id),
              })
            }, 260)
          }
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
    const onPointerLeave = () => {
      hideCard()
      if (hoveredRef.current != null) {
        hoveredRef.current = null
        canvas.style.cursor = 'default'
        draw()
      }
    }
    const onWheel = (e: WheelEvent) => {
      hideCard()
      e.preventDefault()
      const t = transformRef.current
      const rect = canvas.getBoundingClientRect()
      const px = e.clientX - rect.left - rect.width / 2 - t.x
      const py = e.clientY - rect.top - rect.height / 2 - t.y
      const k = Math.max(MIN_K, Math.min(MAX_K, t.k * Math.exp(-e.deltaY * 0.0015)))
      const ratio = k / t.k
      t.x -= px * (ratio - 1)
      t.y -= py * (ratio - 1)
      t.k = k
      draw()
    }

    canvas.addEventListener('pointerdown', onPointerDown)
    canvas.addEventListener('pointermove', onPointerMove)
    canvas.addEventListener('pointerup', onPointerUp)
    canvas.addEventListener('pointerleave', onPointerLeave)
    canvas.addEventListener('wheel', onWheel, { passive: false })
    const onResize = () => draw()
    window.addEventListener('resize', onResize)
    return () => {
      canvas.removeEventListener('pointerdown', onPointerDown)
      canvas.removeEventListener('pointermove', onPointerMove)
      canvas.removeEventListener('pointerup', onPointerUp)
      canvas.removeEventListener('pointerleave', onPointerLeave)
      canvas.removeEventListener('wheel', onWheel)
      window.removeEventListener('resize', onResize)
    }

  }, [])

  useEffect(() => {
    draw()

  }, [showLinks, showTree, showSemantic, recency, currentId, matchedIds])

  return (
    <div className="relative h-full w-full">
      <canvas
        ref={canvasRef}
        className="block h-full w-full touch-none select-none"
        aria-label="Page graph"
      />
      {card ? <NodeHoverCard card={card} /> : null}
    </div>
  )
}

function NodeHoverCard({ card }: { card: HoverCard }) {
  const parts: string[] = []
  if (card.linksOut) parts.push(`${card.linksOut} link${card.linksOut === 1 ? '' : 's'}`)
  if (card.linksIn) parts.push(`${card.linksIn} backlink${card.linksIn === 1 ? '' : 's'}`)
  if (card.children) parts.push(`${card.children} child${card.children === 1 ? '' : 'ren'}`)
  const left = card.flipX ? card.x - 220 - 12 : card.x + 12
  const top = card.flipY ? undefined : card.y + 12
  const bottom = card.flipY ? `calc(100% - ${card.y - 12}px)` : undefined
  return (
    <div className="tela-graph-nodecard" style={{ left, top, bottom }}>
      <p className="tela-graph-nodecard-title">{card.title || 'Untitled'}</p>
      <p className="tela-graph-nodecard-sub" title={card.location}>{card.location}</p>
      <p className="tela-graph-nodecard-meta">
        {parts.length ? parts.join(' · ') : 'No connections'}
      </p>
      {card.broken > 0 ? (
        <p className="tela-graph-nodecard-broken">
          {card.broken} broken link{card.broken === 1 ? '' : 's'}
        </p>
      ) : null}
      <p className="tela-graph-nodecard-meta">Updated {relativeTimeFromSqlite(card.updatedAt)}</p>
    </div>
  )
}
