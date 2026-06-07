import { useQuery } from '@tanstack/react-query'
import { api } from '../api'

// Recent-changes feed for the home dashboard: the latest edit per page across
// every space the caller can reach. Backed by GET /api/recent-changes
// (page_revisions, gated through space_access).

export interface RecentChange {
  page_id: number
  title: string
  space_id: number
  space_name: string
  author_username: string | null
  updated_at: string
}

export const recentChangesKeys = {
  all: ['recent-changes'] as const,
  list: (variant: string) => [...recentChangesKeys.all, variant] as const,
}

// Variants of the same feed:
//   default       → every accessible page's latest edit ("Recent changes")
//   { mine }      → only the caller's own edits ("My recent edits")
//   { source }    → only agent/MCP edits ("Changes by your AI")
export function useRecentChanges(opts?: { mine?: boolean; source?: 'agent' }) {
  const params = new URLSearchParams()
  if (opts?.mine) params.set('mine', '1')
  if (opts?.source) params.set('source', opts.source)
  const qs = params.toString()
  return useQuery({
    queryKey: recentChangesKeys.list(qs || 'all'),
    queryFn: async () => {
      const { changes } = await api<{ changes: RecentChange[] }>(
        `/api/recent-changes${qs ? `?${qs}` : ''}`,
      )
      return changes
    },
    staleTime: 15_000,
  })
}
