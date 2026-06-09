import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../api'
import type { SpaceMember } from '../types'
import { spaceKeys } from './spaces'
import { spaceAccessKeys } from './space-grants'

export const memberKeys = {
  all: ['members'] as const,
  list: (spaceId: number) => [...memberKeys.all, spaceId] as const,
}

// GET /api/spaces/{spaceId}/members — any member of the space may read.
// Backend orders rows owner → editor → viewer (role ASC) then username ASC.
export function useSpaceMembers(spaceId: number | null | undefined) {
  return useQuery({
    queryKey: memberKeys.list(spaceId ?? -1),
    queryFn: async () => {
      const { members } = await api<{ members: SpaceMember[] }>(
        `/api/spaces/${spaceId}/members`,
      )
      return members
    },
    enabled: spaceId != null,
    // Inherits the global SWR staleTime; membership is invalidated by the
    // add/update/remove-member mutations, so a longer window is safe.
  })
}

export interface AddSpaceMemberInput {
  spaceId: number
  username: string
  role: SpaceMember['role']
}

export function useAddSpaceMember() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async ({ spaceId, username, role }: AddSpaceMemberInput) => {
      const { member } = await api<{ member: SpaceMember }>(
        `/api/spaces/${spaceId}/members`,
        { method: 'POST', body: JSON.stringify({ username, role }) },
      )
      return member
    },
    onSuccess: (_member, { spaceId }) => {
      void qc.invalidateQueries({ queryKey: memberKeys.list(spaceId) })
      void qc.invalidateQueries({ queryKey: spaceAccessKeys.list(spaceId) })
      // my_role on the space detail may have changed (e.g. own role edited).
      void qc.invalidateQueries({ queryKey: spaceKeys.detail(spaceId) })
    },
  })
}

export interface UpdateSpaceMemberInput {
  spaceId: number
  userId: number
  role: SpaceMember['role']
}

export function useUpdateSpaceMember() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async ({ spaceId, userId, role }: UpdateSpaceMemberInput) => {
      const { member } = await api<{ member: SpaceMember }>(
        `/api/spaces/${spaceId}/members/${userId}`,
        { method: 'PATCH', body: JSON.stringify({ role }) },
      )
      return member
    },
    onSuccess: (_member, { spaceId }) => {
      void qc.invalidateQueries({ queryKey: memberKeys.list(spaceId) })
      void qc.invalidateQueries({ queryKey: spaceAccessKeys.list(spaceId) })
      // my_role on the space detail may have changed (e.g. own role edited).
      void qc.invalidateQueries({ queryKey: spaceKeys.detail(spaceId) })
    },
  })
}

export interface RemoveSpaceMemberInput {
  spaceId: number
  userId: number
  // Set when the caller is removing themselves (self-leave). Drives a
  // spaceKeys.list() invalidation so the space disappears from the sidebar
  // without a hard reload.
  isSelf?: boolean
}

export function useRemoveSpaceMember() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async ({ spaceId, userId }: RemoveSpaceMemberInput) => {
      await api<void>(`/api/spaces/${spaceId}/members/${userId}`, {
        method: 'DELETE',
      })
    },
    onSuccess: (_void, { spaceId, isSelf }) => {
      void qc.invalidateQueries({ queryKey: memberKeys.list(spaceId) })
      void qc.invalidateQueries({ queryKey: spaceAccessKeys.list(spaceId) })
      // my_role on the space detail may have changed (e.g. own role edited).
      void qc.invalidateQueries({ queryKey: spaceKeys.detail(spaceId) })
      if (isSelf) {
        void qc.invalidateQueries({ queryKey: spaceKeys.list() })
      }
    },
  })
}
