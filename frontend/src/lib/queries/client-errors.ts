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
  groups: (includeAdmins: boolean) => ['client-errors', { includeAdmins }] as const,
  occurrences: (fp: string, includeAdmins: boolean) =>
    ['client-errors', fp, { includeAdmins }] as const,
}

// GET /api/admin/client-errors — grouped issues, most-recently-seen first. Errors
// from admins' own browsers are excluded by default; includeAdmins re-includes.
export function useClientErrorGroups(includeAdmins = false) {
  return useQuery({
    queryKey: clientErrorKeys.groups(includeAdmins),
    queryFn: async () => {
      const { groups } = await api<{ groups: ClientErrorGroup[] }>(
        `/api/admin/client-errors${includeAdmins ? '?include_admins=1' : ''}`,
      )
      return groups
    },
    staleTime: 15_000,
  })
}

// GET /api/admin/client-errors/{fingerprint} — recent occurrences of one issue.
// Enabled only once a row is expanded. Shares the group view's admin filter so the
// drill-down matches the group's counts.
export function useClientErrorOccurrences(
  fingerprint: string | null,
  includeAdmins = false,
) {
  return useQuery({
    queryKey: clientErrorKeys.occurrences(fingerprint ?? '', includeAdmins),
    enabled: fingerprint != null,
    queryFn: async () => {
      const { occurrences } = await api<{ occurrences: ClientErrorOccurrence[] }>(
        `/api/admin/client-errors/${encodeURIComponent(fingerprint!)}${includeAdmins ? '?include_admins=1' : ''}`,
      )
      return occurrences
    },
    staleTime: 15_000,
  })
}
