import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../api'
import { orgKeys } from './orgs'

// Organization email invitations (self-serve teams). The accept page is public
// (token-authenticated); managing + accepting are session-gated.

export interface InviteInfo {
  valid: boolean
  org_name?: string
  email?: string
}

export interface OrgInvite {
  id: number
  email: string
  org_role: string
  created_at?: string
  expires_at?: string
}

const inviteKeys = {
  one: (token: string) => ['invite', token] as const,
  list: (orgId: number) => [...orgKeys.all, orgId, 'invites'] as const,
}

// GET /api/invites/{token} — public; renders the accept page for a logged-out
// invitee. `valid:false` when missing/expired (never an error).
export function useInvite(token: string) {
  return useQuery({
    queryKey: inviteKeys.one(token),
    queryFn: () => api<InviteInfo>(`/api/invites/${encodeURIComponent(token)}`),
    enabled: !!token,
    retry: false,
  })
}

// POST /api/me/accept-invite — the logged-in invitee joins the org.
export function useAcceptInvite() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (token: string) =>
      api<{ org?: unknown }>('/api/me/accept-invite', {
        method: 'POST',
        body: JSON.stringify({ token }),
      }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: orgKeys.list() }),
  })
}

// GET /api/orgs/{id}/invites — pending invites (org admin).
export function useOrgInvites(orgId: number | null | undefined) {
  return useQuery({
    queryKey: inviteKeys.list(orgId ?? -1),
    queryFn: async () => (await api<{ invites: OrgInvite[] }>(`/api/orgs/${orgId}/invites`)).invites,
    enabled: orgId != null,
  })
}

// POST /api/orgs/{id}/invites — invite a teammate by email (org admin).
export function useCreateOrgInvite(orgId: number) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: { email: string; org_role?: string }) =>
      api<{ invite: OrgInvite }>(`/api/orgs/${orgId}/invites`, {
        method: 'POST',
        body: JSON.stringify(input),
      }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: inviteKeys.list(orgId) }),
  })
}

// DELETE /api/orgs/{id}/invites/{inviteId} — revoke a pending invite (org admin).
export function useRevokeOrgInvite(orgId: number) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (inviteId: number) =>
      api<void>(`/api/orgs/${orgId}/invites/${inviteId}`, { method: 'DELETE' }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: inviteKeys.list(orgId) }),
  })
}
