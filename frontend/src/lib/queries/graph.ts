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
