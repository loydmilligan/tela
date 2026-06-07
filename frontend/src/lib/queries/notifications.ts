import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../api'

// Notifications inbox. The unread count drives the header bell badge (polled);
// the list backs the bell's panel. See docs/notifications.md.

export interface NotificationItem {
  id: number
  type: string
  actor_username: string | null
  subject_kind: string
  subject_id: number
  space_id: number | null
  data: Record<string, unknown>
  read: boolean
  created_at: string
}

export const notificationKeys = {
  all: ['notifications'] as const,
  list: () => [...notificationKeys.all, 'list'] as const,
  unread: () => [...notificationKeys.all, 'unread'] as const,
}

export function useNotifications() {
  return useQuery({
    queryKey: notificationKeys.list(),
    queryFn: async () => {
      const { notifications } = await api<{ notifications: NotificationItem[] }>(
        '/api/notifications',
      )
      return notifications
    },
    staleTime: 15_000,
  })
}

export function useUnreadCount() {
  return useQuery({
    queryKey: notificationKeys.unread(),
    queryFn: async () => {
      const { count } = await api<{ count: number }>('/api/notifications/unread-count')
      return count
    },
    // Poll so the badge stays fresh without a realtime channel (v1).
    refetchInterval: 30_000,
    staleTime: 15_000,
  })
}

export function useMarkNotificationRead() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (id: number) => {
      await api<void>(`/api/notifications/${id}/read`, { method: 'POST' })
      return id
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: notificationKeys.all })
    },
  })
}

export function useMarkAllNotificationsRead() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async () => {
      await api<void>('/api/notifications/read-all', { method: 'POST' })
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: notificationKeys.all })
    },
  })
}
