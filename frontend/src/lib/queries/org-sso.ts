import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../api'

// Mirrors backend orgSSODTO. The client_secret is write-only — the read API
// never returns it.
export interface OrgSSO {
  configured: boolean
  issuer: string
  client_id: string
  enforced: boolean
}

export interface PutOrgSSOInput {
  issuer: string
  client_id: string
  client_secret: string
  enforced: boolean
}

export const orgSSOKeys = {
  all: ['org-sso'] as const,
  detail: (orgId: number) => [...orgSSOKeys.all, orgId] as const,
}

// GET /api/orgs/{id}/sso — the org's OIDC connection (minus secret). Instance-
// admin only. Pass null to disable the query (dialog closed).
export function useOrgSSO(orgId: number | null) {
  return useQuery({
    queryKey: orgSSOKeys.detail(orgId ?? 0),
    queryFn: async () => {
      const { sso } = await api<{ sso: OrgSSO }>(`/api/orgs/${orgId}/sso`)
      return sso
    },
    enabled: orgId != null,
    staleTime: 30_000,
  })
}

export function usePutOrgSSO(orgId: number) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (input: PutOrgSSOInput) => {
      const { sso } = await api<{ sso: OrgSSO }>(`/api/orgs/${orgId}/sso`, {
        method: 'PUT',
        body: JSON.stringify(input),
      })
      return sso
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: orgSSOKeys.detail(orgId) })
    },
  })
}

export function useDeleteOrgSSO(orgId: number) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async () => {
      await api<void>(`/api/orgs/${orgId}/sso`, { method: 'DELETE' })
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: orgSSOKeys.detail(orgId) })
    },
  })
}
