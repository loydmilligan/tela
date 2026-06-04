import { useQuery } from '@tanstack/react-query'
import { api } from '../api'
import type { AccessAuditEntry } from '../types'

export const accessAuditKeys = {
  all: ['access-audit'] as const,
  list: () => [...accessAuditKeys.all, 'list'] as const,
}

// GET /api/admin/access-audit — most recent access-control changes. Instance-admin
// only; mounted from an admin-gated tab.
export function useAccessAudit() {
  return useQuery({
    queryKey: accessAuditKeys.list(),
    queryFn: async () => {
      const { entries } = await api<{ entries: AccessAuditEntry[] }>(
        '/api/admin/access-audit',
      )
      return entries
    },
    staleTime: 15_000,
  })
}
