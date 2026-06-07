import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../api'

// Per-user notification preferences: an (event_type × channel) on/off matrix.
// Opt-out — the backend returns the full matrix defaulting to enabled.

export interface NotificationPref {
  event_type: string
  channel: string
  enabled: boolean
}

export const notificationPrefKeys = {
  all: ['notification-prefs'] as const,
}

export function useNotificationPrefs() {
  return useQuery({
    queryKey: notificationPrefKeys.all,
    queryFn: async () => {
      const { prefs } = await api<{ prefs: NotificationPref[] }>(
        '/api/users/me/notification-prefs',
      )
      return prefs
    },
  })
}

export function useUpdateNotificationPref() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (pref: NotificationPref) => {
      await api<NotificationPref>('/api/users/me/notification-prefs', {
        method: 'PUT',
        body: JSON.stringify(pref),
      })
      return pref
    },
    onMutate: async (pref) => {
      await qc.cancelQueries({ queryKey: notificationPrefKeys.all })
      const prev = qc.getQueryData<NotificationPref[]>(notificationPrefKeys.all)
      qc.setQueryData<NotificationPref[]>(notificationPrefKeys.all, (curr) =>
        curr?.map((p) =>
          p.event_type === pref.event_type && p.channel === pref.channel ? pref : p,
        ),
      )
      return { prev }
    },
    onError: (_e, _p, ctx) => {
      if (ctx?.prev) qc.setQueryData(notificationPrefKeys.all, ctx.prev)
    },
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: notificationPrefKeys.all })
    },
  })
}
