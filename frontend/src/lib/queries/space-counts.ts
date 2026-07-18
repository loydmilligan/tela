import { useQuery } from '@tanstack/react-query'
import { api } from '../api'

// Per-space sidebar counts (GET /api/spaces/counts) — one batched read of total
// + disputed pages for every readable space, backing the sidebar badges (#26).
// One request for the whole tree, not one per space.

export interface SpaceCount {
  space_id: number
  total: number
  disputed: number
}

export function useSpaceCounts() {
  return useQuery({
    queryKey: ['space-counts'],
    queryFn: async () => {
      const r = await api<{ spaces: SpaceCount[] }>('/api/spaces/counts')
      return r.spaces ?? []
    },
    staleTime: 60_000, // counts drift slowly; don't refetch on every focus
  })
}
