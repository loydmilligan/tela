import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../api'
import type { CreateSpaceInput, Space, SpaceRole, UpdateSpaceInput } from '../types'

export const spaceKeys = {
  all: ['spaces'] as const,
  lists: () => [...spaceKeys.all, 'list'] as const,
  list: () => [...spaceKeys.lists()] as const,
  details: () => [...spaceKeys.all, 'detail'] as const,
  detail: (id: number) => [...spaceKeys.details(), id] as const,
}

export function useSpaces() {
  return useQuery({
    queryKey: spaceKeys.list(),
    queryFn: async () => {
      const { spaces } = await api<{ spaces: Space[] }>('/api/spaces')
      return spaces
    },
  })
}

export function useSpace(id: number | null | undefined) {
  return useQuery({
    queryKey: id != null ? spaceKeys.detail(id) : spaceKeys.detail(-1),
    queryFn: async () => {
      const { space } = await api<{ space: Space }>(`/api/spaces/${id}`)
      return space
    },
    enabled: id != null,
  })
}

// The caller's effective role on a space, from the `my_role` field of
// GET /api/spaces/{id} (backend space_access view: direct ∪ org ∪ group).
// This is THE way to role-gate UI — never derive "my role" by finding the
// caller in useSpaceMembers: a user reaching the space via an org/group grant
// is absent from the direct members list, which would misread an effective
// viewer as an editor (and an effective owner as not-owner).
//
// `resolved` is false until the role is known; treat unresolved as no-edit.
export function useSpaceRole(spaceId: number | null | undefined): {
  role: SpaceRole | null
  resolved: boolean
  isViewer: boolean
  isOwner: boolean
} {
  const space = useSpace(spaceId)
  const role = space.data?.my_role ?? null
  return {
    role,
    resolved: role != null,
    isViewer: role === 'viewer',
    isOwner: role === 'owner',
  }
}

export function useCreateSpace() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (input: CreateSpaceInput) => {
      const { space } = await api<{ space: Space }>('/api/spaces', {
        method: 'POST',
        body: JSON.stringify(input),
      })
      return space
    },
    onMutate: async (input) => {
      await qc.cancelQueries({ queryKey: spaceKeys.lists() })
      const previous = qc.getQueryData<Space[]>(spaceKeys.list())
      const optimistic: Space = {
        id: -Date.now(),
        name: input.name,
        slug: input.slug ?? '',
        created_at: '',
        updated_at: '',
      }
      qc.setQueryData<Space[]>(spaceKeys.list(), (curr) =>
        curr ? [...curr, optimistic].sort((a, b) => a.name.localeCompare(b.name)) : [optimistic],
      )
      return { previous, optimisticId: optimistic.id }
    },
    onError: (_err, _input, ctx) => {
      if (ctx?.previous) qc.setQueryData(spaceKeys.list(), ctx.previous)
    },
    onSuccess: (created, _input, ctx) => {
      qc.setQueryData<Space[]>(spaceKeys.list(), (curr) => {
        if (!curr) return [created]
        const swapped = curr.map((s) => (s.id === ctx?.optimisticId ? created : s))
        return swapped.sort((a, b) => a.name.localeCompare(b.name))
      })
      qc.setQueryData(spaceKeys.detail(created.id), created)
    },
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: spaceKeys.lists() })
    },
  })
}

export function useUpdateSpace() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async ({ id, ...patch }: UpdateSpaceInput & { id: number }) => {
      const { space } = await api<{ space: Space }>(`/api/spaces/${id}`, {
        method: 'PATCH',
        body: JSON.stringify(patch),
      })
      return space
    },
    onSuccess: (updated) => {
      qc.setQueryData(spaceKeys.detail(updated.id), updated)
      qc.setQueryData<Space[]>(spaceKeys.list(), (curr) =>
        curr
          ? curr
              .map((s) => (s.id === updated.id ? updated : s))
              .sort((a, b) => a.name.localeCompare(b.name))
          : curr,
      )
    },
  })
}

// POST /api/spaces/{id}/transfer — move a space's owning account to an org
// (org_id) or back to personal (org_id: null). Owner-only on the backend.
export interface TransferSpaceInput {
  id: number
  org_id: number | null
}

export function useTransferSpace() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async ({ id, org_id }: TransferSpaceInput) => {
      const { space } = await api<{ space: Space }>(
        `/api/spaces/${id}/transfer`,
        { method: 'POST', body: JSON.stringify({ org_id }) },
      )
      return space
    },
    onSuccess: (updated) => {
      qc.setQueryData(spaceKeys.detail(updated.id), updated)
      void qc.invalidateQueries({ queryKey: spaceKeys.lists() })
      void qc.invalidateQueries({ queryKey: spaceKeys.detail(updated.id) })
    },
  })
}

export function useDeleteSpace() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (id: number) => {
      await api<void>(`/api/spaces/${id}`, { method: 'DELETE' })
      return id
    },
    onSuccess: (id) => {
      qc.setQueryData<Space[]>(spaceKeys.list(), (curr) => curr?.filter((s) => s.id !== id))
      qc.removeQueries({ queryKey: spaceKeys.detail(id) })
    },
  })
}
