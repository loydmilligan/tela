import { QueryClient, QueryCache, MutationCache } from '@tanstack/react-query'
import { ApiError } from './api'
import { reportClientError, type ClientErrorKind } from './client-errors'

// The wide net for *handled* failures: every query/mutation in the app routes
// its error through these caches, so a single hook captures any failed data
// fetch or write — the bulk of "something didn't work for this user" that the
// uncaught-exception handlers (client-errors.ts) never see because TanStack
// Query catches it and renders a quiet error state.
//
// We report only genuine breakage and skip the expected/handled cases, so the
// events feed stays signal, not noise:
//   - 401          — session expiry; api.ts already bounces to /login.
//   - other 4xx    — "you can't do that" (403/404/409/422/429); handled by the UI.
//   - rag/llm 503  — feature-disabled sentinels the UI renders as "unavailable".
// What's left — network failures (status 0) and server errors (5xx) — is the
// "the app or server actually misbehaved" tier worth surfacing.
function reportableApiError(err: unknown): boolean {
  if (err instanceof ApiError) {
    if (err.status === 401) return false
    if (err.code === 'rag_disabled' || err.code === 'llm_disabled') return false
    return err.status === 0 || err.status >= 500
  }
  // A non-ApiError escaping a query/mutation is unexpected by definition.
  return true
}

function reportQueryFailure(
  kind: ClientErrorKind,
  err: unknown,
  key: unknown,
): void {
  if (!reportableApiError(err)) return
  const message =
    err instanceof ApiError
      ? `${err.status} ${err.code}: ${err.message}`
      : err instanceof Error
        ? err.message
        : String(err)
  let context: string | undefined
  try {
    context = `${kind}: ${JSON.stringify(key)}`
  } catch {
    context = kind
  }
  reportClientError({
    kind,
    message,
    stack: err instanceof Error ? err.stack : undefined,
    component: context,
  })
}

export const queryClient = new QueryClient({
  queryCache: new QueryCache({
    onError: (err, query) => reportQueryFailure('query', err, query.queryKey),
  }),
  mutationCache: new MutationCache({
    onError: (err, _vars, _ctx, mutation) =>
      reportQueryFailure('mutation', err, mutation.options.mutationKey ?? 'mutation'),
  }),
  defaultOptions: {
    queries: {
      // SWR window. At the old 30s every page revisit within the 5-min gcTime
      // was "stale" and fired a background refetch — so a single sidebar click
      // re-hit 7-10 endpoints over the ~240ms Cloudflare-tunnel floor even
      // though the data was already cached. 2 min makes rapid back-and-forth
      // navigation paint instantly from cache with no network. It's safe
      // because every list/detail query is invalidated by its own mutation
      // (create/update/delete/move) and live page content arrives over the Yjs
      // websocket, not these REST reads.
      staleTime: 120_000,
      // Keep cache entries warm well past the stale window so navigating back
      // after a break still paints instantly, then revalidates.
      gcTime: 1_800_000,
      retry: 1,
      refetchOnWindowFocus: false,
    },
    mutations: {
      retry: 0,
    },
  },
})
