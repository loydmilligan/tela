import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../api'
import type { Org, OrgMember, OrgRole } from '../types'

export const orgKeys = {
  all: ['orgs'] as const,
  list: () => [...orgKeys.all, 'list'] as const,
  members: (orgId: number) => [...orgKeys.all, orgId, 'members'] as const,
}

// GET /api/orgs — the caller's orgs (instance-admins see every org).
export function useOrgs() {
  return useQuery({
    queryKey: orgKeys.list(),
    queryFn: async () => {
      const { orgs } = await api<{ orgs: Org[] }>('/api/orgs')
      return orgs
    },
    staleTime: 30_000,
  })
}

export interface CreateOrgInput {
  name: string
  slug?: string
}

export function useCreateOrg() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (input: CreateOrgInput) => {
      const { org } = await api<{ org: Org }>('/api/orgs', {
        method: 'POST',
        body: JSON.stringify(input),
      })
      return org
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: orgKeys.list() })
    },
  })
}

export interface UpdateOrgInput {
  id: number
  name?: string
  slug?: string
}

export function useUpdateOrg() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async ({ id, ...body }: UpdateOrgInput) => {
      const { org } = await api<{ org: Org }>(`/api/orgs/${id}`, {
        method: 'PATCH',
        body: JSON.stringify(body),
      })
      return org
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: orgKeys.list() })
    },
  })
}

export function useDeleteOrg() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (id: number) => {
      await api<void>(`/api/orgs/${id}`, { method: 'DELETE' })
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: orgKeys.list() })
    },
  })
}

// GET /api/orgs/{id}/members — any member (or instance-admin) may read.
export function useOrgMembers(orgId: number | null | undefined) {
  return useQuery({
    queryKey: orgKeys.members(orgId ?? -1),
    queryFn: async () => {
      const { members } = await api<{ members: OrgMember[] }>(
        `/api/orgs/${orgId}/members`,
      )
      return members
    },
    enabled: orgId != null,
    staleTime: 30_000,
  })
}

export interface AddOrgMemberInput {
  orgId: number
  identifier: string
  org_role: OrgRole
}

export function useAddOrgMember() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async ({ orgId, identifier, org_role }: AddOrgMemberInput) => {
      const { member } = await api<{ member: OrgMember }>(
        `/api/orgs/${orgId}/members`,
        { method: 'POST', body: JSON.stringify({ identifier, org_role }) },
      )
      return member
    },
    onSuccess: (_member, { orgId }) => {
      void qc.invalidateQueries({ queryKey: orgKeys.members(orgId) })
      void qc.invalidateQueries({ queryKey: orgKeys.list() })
    },
  })
}

export interface UpdateOrgMemberInput {
  orgId: number
  userId: number
  org_role: OrgRole
}

export function useUpdateOrgMember() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async ({ orgId, userId, org_role }: UpdateOrgMemberInput) => {
      const { member } = await api<{ member: OrgMember }>(
        `/api/orgs/${orgId}/members/${userId}`,
        { method: 'PATCH', body: JSON.stringify({ org_role }) },
      )
      return member
    },
    onSuccess: (_member, { orgId }) => {
      void qc.invalidateQueries({ queryKey: orgKeys.members(orgId) })
    },
  })
}

export interface RemoveOrgMemberInput {
  orgId: number
  userId: number
}

export function useRemoveOrgMember() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async ({ orgId, userId }: RemoveOrgMemberInput) => {
      await api<void>(`/api/orgs/${orgId}/members/${userId}`, {
        method: 'DELETE',
      })
    },
    onSuccess: (_void, { orgId }) => {
      void qc.invalidateQueries({ queryKey: orgKeys.members(orgId) })
      void qc.invalidateQueries({ queryKey: orgKeys.list() })
    },
  })
}
