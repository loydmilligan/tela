import { useEffect } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../api'
import { subscribeToPageMutation } from '../pageMutationEvent'

// Per-space view (GET /api/spaces/{id}/overview) — a content-first hub + a
// maintenance/health rollup, one read for the tabbed space landing.

export interface SpaceTopPage {
  id: number
  title: string
  children: number
  updated_at: string
}
export interface SpaceMiniPage {
  id: number
  title: string
  updated_at?: string
}
export interface SpaceDisputed {
  id: number
  title: string
  n: number
}
export interface SpaceReviewPage {
  id: number
  title: string
  age_days: number
  every: number
}
export interface SpaceDuplicate {
  page_a: number
  title_a: string
  page_b: number
  title_b: string
  space_a: number
  space_b: number
  similarity: number
}
export interface SpaceHealth {
  disputed: SpaceDisputed[]
  review_overdue: SpaceReviewPage[]
  orphans: SpaceMiniPage[]
  duplicates: SpaceDuplicate[]
}
export interface SpaceOverview {
  pages: number
  top_level: SpaceTopPage[]
  recent: SpaceMiniPage[]
  health: SpaceHealth
}

export function useSpaceOverview(spaceId: number | null | undefined) {
  const qc = useQueryClient()
  useEffect(() => {
    if (spaceId == null) return
    return subscribeToPageMutation(() => {
      void qc.invalidateQueries({ queryKey: ['space-overview', spaceId] })
    })
  }, [qc, spaceId])
  return useQuery({
    queryKey: ['space-overview', spaceId ?? -1],
    queryFn: () => api<SpaceOverview>(`/api/spaces/${spaceId}/overview`),
    enabled: spaceId != null,
  })
}
