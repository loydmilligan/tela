import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../api'
import type { OrgBranding } from '../types'

// An org's branding — logo, accent, and recommended deck variant. Themes the
// white-label login/app shell AND is inherited by the org's slide decks. The
// logo is always stored IN tela (upload or import-from-URL) and served from
// tela's origin, so the deck renderer can always reach it. Org-admin gated.

export const orgBrandingKeys = {
  all: ['org-branding'] as const,
  detail: (orgId: number) => [...orgBrandingKeys.all, orgId] as const,
}

// GET /api/orgs/{id}/branding.
export function useOrgBranding(orgId: number) {
  return useQuery({
    queryKey: orgBrandingKeys.detail(orgId),
    queryFn: async () => api<OrgBranding>(`/api/orgs/${orgId}/branding`),
    staleTime: 30_000,
  })
}

// PUT /api/orgs/{id}/branding — sets accent + recommended deck variant, and
// optionally imports a logo from a URL (tela fetches + stores it once). Echoes
// back the updated branding.
export function usePutOrgBranding(orgId: number) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (input: { accent: string; deck_variant: string; logo_import_url?: string }) =>
      api<OrgBranding>(`/api/orgs/${orgId}/branding`, {
        method: 'PUT',
        body: JSON.stringify(input),
      }),
    onSuccess: (saved) => qc.setQueryData(orgBrandingKeys.detail(orgId), saved),
  })
}

// POST /api/orgs/{id}/branding/logo — upload an image file (raw bytes). The
// File's own type rides as Content-Type; the backend validates by sniffing.
export function useUploadOrgLogo(orgId: number) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (file: File) =>
      api<OrgBranding>(`/api/orgs/${orgId}/branding/logo`, {
        method: 'POST',
        body: file,
        headers: { 'Content-Type': file.type || 'application/octet-stream' },
      }),
    onSuccess: (saved) => qc.setQueryData(orgBrandingKeys.detail(orgId), saved),
  })
}

// DELETE /api/orgs/{id}/branding/logo — clear the stored logo.
export function useDeleteOrgLogo(orgId: number) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async () =>
      api<OrgBranding>(`/api/orgs/${orgId}/branding/logo`, { method: 'DELETE' }),
    onSuccess: (saved) => qc.setQueryData(orgBrandingKeys.detail(orgId), saved),
  })
}
