import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../api'
import type { OrgDomain } from '../types'

export const orgDomainKeys = {
  all: ['org-domains'] as const,
  list: () => [...orgDomainKeys.all, 'list'] as const,
}

// GET /api/admin/org-domains — every auto-join mapping. Instance-admin only.
export function useOrgDomains() {
  return useQuery({
    queryKey: orgDomainKeys.list(),
    queryFn: async () => {
      const { domains } = await api<{ domains: OrgDomain[] }>(
        '/api/admin/org-domains',
      )
      return domains
    },
    staleTime: 30_000,
  })
}

export interface CreateOrgDomainInput {
  domain: string
  org_id: number
}

export function useCreateOrgDomain() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (input: CreateOrgDomainInput) => {
      const { domain } = await api<{ domain: OrgDomain }>(
        '/api/admin/org-domains',
        { method: 'POST', body: JSON.stringify(input) },
      )
      return domain
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: orgDomainKeys.list() })
    },
  })
}

export function useDeleteOrgDomain() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (domain: string) => {
      await api<void>(`/api/admin/org-domains/${encodeURIComponent(domain)}`, {
        method: 'DELETE',
      })
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: orgDomainKeys.list() })
    },
  })
}
