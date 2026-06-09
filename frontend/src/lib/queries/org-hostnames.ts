import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../api'
import type { HostnameHealth, OrgHostname } from '../types'

// Custom login domains (vanity hostnames) attached to an org — the org's
// white-labeled sign-in surface. Distinct from the email-domain auto-join
// mappings in org-domains.ts. Org-admin gated.

export const orgHostnameKeys = {
  all: ['org-hostnames'] as const,
  list: (orgId: number) => [...orgHostnameKeys.all, orgId] as const,
  health: (orgId: number, hostname: string) =>
    [...orgHostnameKeys.all, orgId, 'health', hostname] as const,
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

// GET /api/orgs/{id}/hostnames/{hostname}/health — a live DNS+HTTPS probe.
// Lazy: `enabled` defaults to false so it doesn't fire for every row on mount.
// Trigger it from a "Check" button via the returned `refetch`, or pass
// `enabled` (e.g. true for Active rows) to auto-run once. `staleTime: 0` so a
// re-check always hits the network.
export function useOrgHostnameHealth(
  orgId: number,
  hostname: string,
  enabled = false,
) {
  return useQuery({
    queryKey: orgHostnameKeys.health(orgId, hostname),
    queryFn: async () =>
      api<HostnameHealth>(
        `/api/orgs/${orgId}/hostnames/${encodeURIComponent(hostname)}/health`,
      ),
    enabled,
    staleTime: 0,
    gcTime: 0,
    retry: false,
  })
}

// POST /api/orgs/{id}/hostnames/{hostname}/admin-login — instance-admin only.
// Mints a short-lived token and returns the absolute redeem URL on the org's
// domain. Navigating to it logs the admin in there as themselves (a normal
// host-bound session), bypassing the org's own SSO-only door. Returns the URL;
// the caller does the navigation.
export function useAdminDomainLogin(orgId: number) {
  return useMutation({
    mutationFn: async (hostname: string) => {
      const { url } = await api<{ url: string }>(
        `/api/orgs/${orgId}/hostnames/${encodeURIComponent(hostname)}/admin-login`,
        { method: 'POST' },
      )
      return url
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
