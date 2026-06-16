import { useQuery } from '@tanstack/react-query'
import { api } from '../api'

// Admin "Issues" view over browser error reports (client.error events grouped by
// fingerprint). See backend/internal/api/admin_client_errors.go.

export interface ClientErrorGroup {
  fingerprint: string
  kind: string
  message: string
  count: number
  users: number
  first_seen: string
  last_seen: string
  sample: string // full detail of the latest occurrence (incl. stack)
}

export interface ClientErrorOccurrence {
  id: number
  actor_label: string
  detail: string
  ip: string
  created_at: string
}

export const clientErrorKeys = {
  groups: ['client-errors'] as const,
  occurrences: (fp: string) => ['client-errors', fp] as const,
}

// GET /api/admin/client-errors — grouped issues, most-recently-seen first.
export function useClientErrorGroups() {
  return useQuery({
    queryKey: clientErrorKeys.groups,
    queryFn: async () => {
      const { groups } = await api<{ groups: ClientErrorGroup[] }>('/api/admin/client-errors')
      return groups
    },
    staleTime: 15_000,
  })
}

// GET /api/admin/client-errors/{fingerprint} — recent occurrences of one issue.
// Enabled only once a row is expanded.
export function useClientErrorOccurrences(fingerprint: string | null) {
  return useQuery({
    queryKey: clientErrorKeys.occurrences(fingerprint ?? ''),
    enabled: fingerprint != null,
    queryFn: async () => {
      const { occurrences } = await api<{ occurrences: ClientErrorOccurrence[] }>(
        `/api/admin/client-errors/${encodeURIComponent(fingerprint!)}`,
      )
      return occurrences
    },
    staleTime: 15_000,
  })
}
