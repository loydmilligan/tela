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
  list: (mine: boolean) => [...recentChangesKeys.all, mine ? 'mine' : 'all'] as const,
}

// mine=true narrows to pages the caller themselves edited ("My recent edits");
// otherwise it's the team-wide feed of every accessible page's latest edit.
export function useRecentChanges(opts?: { mine?: boolean }) {
  const mine = opts?.mine ?? false
  return useQuery({
    queryKey: recentChangesKeys.list(mine),
    queryFn: async () => {
      const { changes } = await api<{ changes: RecentChange[] }>(
        `/api/recent-changes${mine ? '?mine=1' : ''}`,
      )
      return changes
    },
    staleTime: 15_000,
  })
}
