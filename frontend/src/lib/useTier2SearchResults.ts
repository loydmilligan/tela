import { useEffect } from 'react'
import {
  keepPreviousData,
  useQuery,
  useQueryClient,
} from '@tanstack/react-query'
import { searchPages, type SearchResult } from './api'
import { subscribeToPageMutation } from './pageMutationEvent'

// Server-side title+body search driven by /api/search. Subscribes to the
// page-mutation bus so renamed / deleted pages drop out of palette results the
// instant a mutation lands. Returns `undefined` while the first query for a
// given key is in flight; `keepPreviousData` keeps later re-queries non-flickery.
export function useTier2SearchResults(
  trimmedQuery: string,
): SearchResult[] | undefined {
  const queryClient = useQueryClient()
  useEffect(
    () =>
      subscribeToPageMutation(() => {
        void queryClient.invalidateQueries({ queryKey: ['search'] })
      }),
    [queryClient],
  )
  const searchQuery = useQuery<SearchResult[]>({
    queryKey: ['search', trimmedQuery],
    queryFn: ({ signal }) =>
      searchPages(trimmedQuery, signal).then((r) => r.results),
    enabled: trimmedQuery.length > 0,
    staleTime: 30_000,
    gcTime: 5 * 60_000,
    placeholderData: keepPreviousData,
  })
  return searchQuery.data
}
