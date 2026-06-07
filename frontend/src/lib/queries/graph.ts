import { useEffect } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../api'
import { subscribeToPageMutation } from '../pageMutationEvent'

// Graph view data — pages the caller can see plus the edges between them. Two
// edge kinds: "link" (wikilink/reference) and "tree" (parent→child). Backed by
// GET /api/graph (backend/internal/api/graph.go).

export interface GraphNode {
  id: number
  space_id: number
  space_name: string
  title: string
  // Ancestor page titles, top→down (excludes the page and the space).
  breadcrumb: string[]
  updated_at: string
  // Count of outgoing wikilinks whose target no longer exists.
  broken: number
}

export interface GraphLink {
  source: number
  target: number
  kind: 'link' | 'tree'
}

export interface GraphData {
  nodes: GraphNode[]
  links: GraphLink[]
}

// Per-space color assignment, reusing the 8 collab-cursor palette tokens (so it
// stays on-theme and re-themes for free). Stable ordering by space id means the
// canvas renderer and the DOM legend agree on which space gets which color.
const PALETTE_SIZE = 8

export interface SpaceColor {
  spaceId: number
  spaceName: string
  varName: string
}

export function buildSpacePalette(nodes: GraphNode[]): SpaceColor[] {
  const seen = new Map<number, string>()
  for (const n of nodes) if (!seen.has(n.space_id)) seen.set(n.space_id, n.space_name)
  return [...seen.entries()]
    .sort((a, b) => a[0] - b[0])
    .map(([spaceId, spaceName], i) => ({
      spaceId,
      spaceName,
      varName: `--collab-cursor-${(i % PALETTE_SIZE) + 1}`,
    }))
}

// BFS the (undirected) graph from a seed page out to `depth` hops. Used to scope
// the full graph down to a page's neighborhood (focus mode + on-page local graph).
export function neighborhood(
  seed: number,
  links: GraphLink[],
  depth: number,
): Set<number> {
  const adj = new Map<number, number[]>()
  for (const l of links) {
    ;(adj.get(l.source) ?? adj.set(l.source, []).get(l.source)!).push(l.target)
    ;(adj.get(l.target) ?? adj.set(l.target, []).get(l.target)!).push(l.source)
  }
  const seen = new Set<number>([seed])
  let frontier = [seed]
  for (let i = 0; i < depth; i++) {
    const next: number[] = []
    for (const id of frontier)
      for (const nb of adj.get(id) ?? [])
        if (!seen.has(nb)) {
          seen.add(nb)
          next.push(nb)
        }
    frontier = next
  }
  return seen
}

export function useGraph(spaceId?: number) {
  const qc = useQueryClient()
  useEffect(() => {
    return subscribeToPageMutation(() => {
      void qc.invalidateQueries({ queryKey: ['graph'] })
    })
  }, [qc])
  return useQuery({
    queryKey: ['graph', spaceId ?? 'all'],
    queryFn: () =>
      api<GraphData>(
        spaceId != null ? `/api/graph?space_id=${spaceId}` : '/api/graph',
      ),
  })
}
