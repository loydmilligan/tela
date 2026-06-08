import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../api'

// Instance-level runtime settings (the instance_settings substrate). Read/write
// is instance-admin only; secret-prefixed keys are never returned by the API.
export const instanceSettingsKeys = {
  all: ['instance-settings'] as const,
}

// GET /api/admin/settings — all non-secret instance settings as a flat map.
export function useInstanceSettings() {
  return useQuery({
    queryKey: instanceSettingsKeys.all,
    queryFn: async () => {
      const { settings } = await api<{ settings: Record<string, string> }>('/api/admin/settings')
      return settings
    },
    staleTime: 30_000,
  })
}

// PATCH /api/admin/settings — upsert the given keys. Returns the full updated
// map, which we write straight into the cache so the panel reflects it.
export function useUpdateInstanceSettings() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (settings: Record<string, string>) =>
      api<{ settings: Record<string, string> }>('/api/admin/settings', {
        method: 'PATCH',
        body: JSON.stringify({ settings }),
      }),
    onSuccess: ({ settings }) => {
      qc.setQueryData(instanceSettingsKeys.all, settings)
    },
  })
}
