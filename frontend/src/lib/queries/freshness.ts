import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../api'

// Mirrors backend rag.SpaceFreshness / rag.PageFreshness.
export interface SpaceFreshness {
  space_id: number
  name: string
  pages: number
  indexed_pages: number
  stale_pages: number
  chunk_count: number
  last_indexed: string
}

export type PageIndexStatus = 'fresh' | 'stale' | 'unindexed' | 'empty'

export interface PageFreshness {
  page_id: number
  title: string
  status: PageIndexStatus
  chunk_count: number
  updated_at: string
  last_indexed: string
}

export const freshnessKeys = {
  all: ['freshness'] as const,
  summary: () => [...freshnessKeys.all, 'summary'] as const,
  space: (id: number) => [...freshnessKeys.all, 'space', id] as const,
}

// Per-space index-health rollup across every space the caller can access. Drives
// the Settings "Search index" view and the sidebar staleness dots. Short
// staleTime so dots reflect auto-reindex settling without hammering the server.
export function useFreshness() {
  return useQuery({
    queryKey: freshnessKeys.summary(),
    queryFn: () =>
      api<{ enabled: boolean; spaces: SpaceFreshness[] }>('/api/rag/freshness'),
    staleTime: 15_000,
  })
}

// Per-page index status within one space. Enabled only when a space is in view.
export function useSpaceFreshness(spaceId: number | null | undefined) {
  return useQuery({
    queryKey: spaceId != null ? freshnessKeys.space(spaceId) : freshnessKeys.space(-1),
    queryFn: () =>
      api<{ enabled: boolean; pages: PageFreshness[] }>(
        `/api/rag/freshness?space_id=${spaceId}`,
      ),
    enabled: spaceId != null,
    staleTime: 15_000,
  })
}

// Triggers a full reindex of a space, then refreshes the freshness views.
export function useReindexSpace() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (spaceId: number) =>
      api<{ indexed_pages: number; indexed_chunks: number }>(
        `/api/rag/reindex?space_id=${spaceId}`,
        { method: 'POST' },
      ),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: freshnessKeys.all })
    },
  })
}
