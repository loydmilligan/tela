import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../api'
import type { OrgLoginSettings } from '../types'

// Which sign-in methods an org's custom-domain login screen offers. Org-admin
// gated. The backend rejects (400 bad_request) disabling BOTH password and
// social when the org has no SSO configured.

export const orgLoginSettingsKeys = {
  all: ['org-login-settings'] as const,
  detail: (orgId: number) => [...orgLoginSettingsKeys.all, orgId] as const,
}

// GET /api/orgs/{id}/login-settings.
export function useOrgLoginSettings(orgId: number) {
  return useQuery({
    queryKey: orgLoginSettingsKeys.detail(orgId),
    queryFn: async () =>
      api<OrgLoginSettings>(`/api/orgs/${orgId}/login-settings`),
    staleTime: 30_000,
  })
}

// PUT /api/orgs/{id}/login-settings — echoes back the saved settings.
export function usePutOrgLoginSettings(orgId: number) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (input: OrgLoginSettings) =>
      api<OrgLoginSettings>(`/api/orgs/${orgId}/login-settings`, {
        method: 'PUT',
        body: JSON.stringify(input),
      }),
    onSuccess: (saved) => {
      qc.setQueryData(orgLoginSettingsKeys.detail(orgId), saved)
    },
  })
}
