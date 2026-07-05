import { useQuery } from '@tanstack/react-query'
import { api } from '../api'
import type { AccessAuditEntry } from '../types'

export const accessAuditKeys = {
  all: ['access-audit'] as const,
  list: (includeAdmins: boolean) =>
    [...accessAuditKeys.all, 'list', { includeAdmins }] as const,
}

// GET /api/admin/access-audit — most recent access-control changes. Instance-admin
// only; mounted from an admin-gated tab. Changes made by instance admins are hidden
// by default; includeAdmins re-includes them (?include_admins=1).
export function useAccessAudit(includeAdmins = false) {
  return useQuery({
    queryKey: accessAuditKeys.list(includeAdmins),
    queryFn: async () => {
      const { entries } = await api<{ entries: AccessAuditEntry[] }>(
        `/api/admin/access-audit${includeAdmins ? '?include_admins=1' : ''}`,
      )
      return entries
    },
    staleTime: 15_000,
  })
}
