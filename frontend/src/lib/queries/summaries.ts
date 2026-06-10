import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../api'

// Mirrors the backend summaries status payloads (GET /api/summaries/status).
export interface SpaceSummaries {
  space_id: number
  name: string
  pages: number
  summarized: number
  stale: number
  failed: number
  last_generated: string
}

export type PageSummaryStatus =
  | 'fresh'
  | 'stale'
  | 'missing'
  | 'failed'
  | 'locked'
  | 'empty'

export interface PageSummary {
  page_id: number
  title: string
  status: PageSummaryStatus
  generated_at: string
  model: string
  last_error: string
  updated_at: string
}

export const summariesKeys = {
  all: ['summaries'] as const,
  summary: () => [...summariesKeys.all, 'summary'] as const,
  space: (id: number) => [...summariesKeys.all, 'space', id] as const,
}

// Per-space summary-health rollup across every space the caller can access.
// Drives the Settings "Summaries" view. Short staleTime so counts reflect the
// background worker settling without hammering the server.
export function useSummaries() {
  return useQuery({
    queryKey: summariesKeys.summary(),
    queryFn: () =>
      api<{ enabled: boolean; model: string; spaces: SpaceSummaries[] }>(
        '/api/summaries/status',
      ),
    staleTime: 60_000,
  })
}

// Per-page summary status within one space. Enabled only when a space is in view.
export function useSpaceSummaries(spaceId: number | null | undefined) {
  return useQuery({
    queryKey: spaceId != null ? summariesKeys.space(spaceId) : summariesKeys.space(-1),
    queryFn: () =>
      api<{ enabled: boolean; pages: PageSummary[] }>(
        `/api/summaries/status?space_id=${spaceId}`,
      ),
    enabled: spaceId != null,
    staleTime: 60_000,
  })
}

// Queues regeneration for every stale/missing/failed page in a space, then
// refreshes the summaries views.
export function useSummarizeSpace() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (spaceId: number) =>
      api<{ queued: number }>(`/api/spaces/${spaceId}/summarize`, {
        method: 'POST',
      }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: summariesKeys.all })
    },
  })
}
