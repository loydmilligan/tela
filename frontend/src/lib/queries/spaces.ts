import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../api'
import type { CreateSpaceInput, Space, UpdateSpaceInput } from '../types'

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
