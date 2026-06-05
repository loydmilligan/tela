import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { searchSemantic, type SemanticHit } from './api'

// Semantic ("Smart results") tier for the command palette. Meaning-aware,
// chunk-level RAG search via /api/rag/search — the enrichment that streams in
// AFTER the instant client tiers, on the debounced query (never per keystroke;
// the server embeds the query once).
//
// Design notes:
//  - Driven by the DEBOUNCED query, so typing stays on the zero-network instant
//    tiers and the embed call only fires on a pause.
//  - Enhancement-only: any error (notably 503 when the server has no embedder
//    configured — feature dark) resolves to [] so the palette never breaks. We
//    also disable retries so a dark instance isn't hammered every keystroke.
//  - keepPreviousData keeps the section from flickering between queries.
export function useSemanticHits(
  debouncedQuery: string,
  enabled: boolean,
): SemanticHit[] | undefined {
  const query = useQuery<SemanticHit[]>({
    queryKey: ['semantic', debouncedQuery],
    queryFn: ({ signal }) =>
      searchSemantic(debouncedQuery, signal)
        .then((r) => r.results)
        .catch(() => []),
    enabled: enabled && debouncedQuery.length > 0,
    staleTime: 30_000,
    gcTime: 5 * 60_000,
    retry: false,
    placeholderData: keepPreviousData,
  })
  return query.data
}
