import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../api'
import type { SpaceGrant } from '../types'

export const spaceGrantKeys = {
  all: ['space-grants'] as const,
  list: (spaceId: number) => [...spaceGrantKeys.all, spaceId] as const,
}

// GET /api/spaces/{id}/grants — the org grants on a space. Any member may read.
export function useSpaceGrants(spaceId: number | null | undefined) {
  return useQuery({
    queryKey: spaceGrantKeys.list(spaceId ?? -1),
    queryFn: async () => {
      const { grants } = await api<{ grants: SpaceGrant[] }>(
        `/api/spaces/${spaceId}/grants`,
      )
      return grants
    },
    enabled: spaceId != null,
    staleTime: 30_000,
  })
}

export interface AddSpaceGrantInput {
  spaceId: number
  org_id: number
  role: SpaceGrant['role']
}

export function useAddSpaceGrant() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async ({ spaceId, org_id, role }: AddSpaceGrantInput) => {
      const { grant } = await api<{ grant: SpaceGrant }>(
        `/api/spaces/${spaceId}/grants`,
        { method: 'POST', body: JSON.stringify({ org_id, role }) },
      )
      return grant
    },
    onSuccess: (_grant, { spaceId }) => {
      void qc.invalidateQueries({ queryKey: spaceGrantKeys.list(spaceId) })
    },
  })
}

export interface UpdateSpaceGrantInput {
  spaceId: number
  grantId: number
  role: SpaceGrant['role']
}

export function useUpdateSpaceGrant() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async ({ spaceId, grantId, role }: UpdateSpaceGrantInput) => {
      const { grant } = await api<{ grant: SpaceGrant }>(
        `/api/spaces/${spaceId}/grants/${grantId}`,
        { method: 'PATCH', body: JSON.stringify({ role }) },
      )
      return grant
    },
    onSuccess: (_grant, { spaceId }) => {
      void qc.invalidateQueries({ queryKey: spaceGrantKeys.list(spaceId) })
    },
  })
}

export interface RemoveSpaceGrantInput {
  spaceId: number
  grantId: number
}

export function useRemoveSpaceGrant() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async ({ spaceId, grantId }: RemoveSpaceGrantInput) => {
      await api<void>(`/api/spaces/${spaceId}/grants/${grantId}`, {
        method: 'DELETE',
      })
    },
    onSuccess: (_void, { spaceId }) => {
      void qc.invalidateQueries({ queryKey: spaceGrantKeys.list(spaceId) })
    },
  })
}
