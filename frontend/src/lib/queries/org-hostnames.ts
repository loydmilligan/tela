import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../api'
import type { OrgHostname } from '../types'

// Custom login domains (vanity hostnames) attached to an org — the org's
// white-labeled sign-in surface. Distinct from the email-domain auto-join
// mappings in org-domains.ts. Org-admin gated.

export const orgHostnameKeys = {
  all: ['org-hostnames'] as const,
  list: (orgId: number) => [...orgHostnameKeys.all, orgId] as const,
}

// GET /api/orgs/{id}/hostnames — every custom login domain on the org.
export function useOrgHostnames(orgId: number) {
  return useQuery({
    queryKey: orgHostnameKeys.list(orgId),
    queryFn: async () => {
      const { hostnames } = await api<{ hostnames: OrgHostname[] }>(
        `/api/orgs/${orgId}/hostnames`,
      )
      return hostnames
    },
    staleTime: 30_000,
  })
}

// POST /api/orgs/{id}/hostnames — attach a hostname. 400 bad_request
// (invalid/apex), 409 conflict (already taken).
export function useAddOrgHostname(orgId: number) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (hostname: string) => {
      const { hostname: created } = await api<{ hostname: OrgHostname }>(
        `/api/orgs/${orgId}/hostnames`,
        { method: 'POST', body: JSON.stringify({ hostname }) },
      )
      return created
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: orgHostnameKeys.list(orgId) })
    },
  })
}

// POST /api/orgs/{id}/hostnames/{hostname}/verify — run DNS verification.
// 400 verification_failed when the TXT record isn't found/doesn't match.
// (Instance admins are force-verified by the backend, skipping DNS.)
export function useVerifyOrgHostname(orgId: number) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (hostname: string) => {
      const { hostname: verified } = await api<{ hostname: OrgHostname }>(
        `/api/orgs/${orgId}/hostnames/${encodeURIComponent(hostname)}/verify`,
        { method: 'POST' },
      )
      return verified
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: orgHostnameKeys.list(orgId) })
    },
  })
}

// DELETE /api/orgs/{id}/hostnames/{hostname} — detach a hostname.
export function useDeleteOrgHostname(orgId: number) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (hostname: string) => {
      await api<void>(
        `/api/orgs/${orgId}/hostnames/${encodeURIComponent(hostname)}`,
        { method: 'DELETE' },
      )
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: orgHostnameKeys.list(orgId) })
    },
  })
}
