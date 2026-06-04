import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../api'
import type { Group, GroupMember, MyGroup } from '../types'
import { orgKeys } from './orgs'

export const groupKeys = {
  all: ['groups'] as const,
  mine: () => [...groupKeys.all, 'mine'] as const,
  list: (orgId: number) => [...groupKeys.all, orgId] as const,
  members: (groupId: number) => [...groupKeys.all, 'members', groupId] as const,
}

// GET /api/groups — every group the caller can grant a space to (for the share
// picker). Instance-admins see all.
export function useMyGroups() {
  return useQuery({
    queryKey: groupKeys.mine(),
    queryFn: async () => {
      const { groups } = await api<{ groups: MyGroup[] }>('/api/groups')
      return groups
    },
    staleTime: 30_000,
  })
}

// GET /api/orgs/{id}/groups — groups in an org (member or instance-admin).
export function useOrgGroups(orgId: number | null | undefined) {
  return useQuery({
    queryKey: groupKeys.list(orgId ?? -1),
    queryFn: async () => {
      const { groups } = await api<{ groups: Group[] }>(
        `/api/orgs/${orgId}/groups`,
      )
      return groups
    },
    enabled: orgId != null,
    staleTime: 30_000,
  })
}

export function useCreateGroup() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async ({ orgId, name }: { orgId: number; name: string }) => {
      const { group } = await api<{ group: Group }>(
        `/api/orgs/${orgId}/groups`,
        { method: 'POST', body: JSON.stringify({ name }) },
      )
      return group
    },
    onSuccess: (_g, { orgId }) => {
      void qc.invalidateQueries({ queryKey: groupKeys.list(orgId) })
    },
  })
}

export function useDeleteGroup() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async ({ orgId, groupId }: { orgId: number; groupId: number }) => {
      await api<void>(`/api/orgs/${orgId}/groups/${groupId}`, { method: 'DELETE' })
    },
    onSuccess: (_v, { orgId }) => {
      void qc.invalidateQueries({ queryKey: groupKeys.list(orgId) })
      void qc.invalidateQueries({ queryKey: orgKeys.list() })
    },
  })
}

// GET /api/orgs/{id}/groups/{group_id}/members.
export function useGroupMembers(
  orgId: number | null | undefined,
  groupId: number | null | undefined,
) {
  return useQuery({
    queryKey: groupKeys.members(groupId ?? -1),
    queryFn: async () => {
      const { members } = await api<{ members: GroupMember[] }>(
        `/api/orgs/${orgId}/groups/${groupId}/members`,
      )
      return members
    },
    enabled: orgId != null && groupId != null,
    staleTime: 30_000,
  })
}

export function useAddGroupMember() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async ({
      orgId,
      groupId,
      identifier,
    }: {
      orgId: number
      groupId: number
      identifier: string
    }) => {
      const { member } = await api<{ member: GroupMember }>(
        `/api/orgs/${orgId}/groups/${groupId}/members`,
        { method: 'POST', body: JSON.stringify({ identifier }) },
      )
      return member
    },
    onSuccess: (_m, { orgId, groupId }) => {
      void qc.invalidateQueries({ queryKey: groupKeys.members(groupId) })
      void qc.invalidateQueries({ queryKey: groupKeys.list(orgId) })
    },
  })
}

export function useRemoveGroupMember() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async ({
      orgId,
      groupId,
      userId,
    }: {
      orgId: number
      groupId: number
      userId: number
    }) => {
      await api<void>(`/api/orgs/${orgId}/groups/${groupId}/members/${userId}`, {
        method: 'DELETE',
      })
    },
    onSuccess: (_v, { orgId, groupId }) => {
      void qc.invalidateQueries({ queryKey: groupKeys.members(groupId) })
      void qc.invalidateQueries({ queryKey: groupKeys.list(orgId) })
    },
  })
}
