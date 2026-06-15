import { useQuery } from '@tanstack/react-query'
import { api } from '../api'
import type { AdminUsage, FeedbackEntry } from '../types'

export const adminUsageKeys = {
  usage: ['admin-usage'] as const,
  feedback: ['admin-feedback'] as const,
}

// GET /api/admin/usage — instance-wide usage overview. Instance-admin only.
export function useAdminUsage() {
  return useQuery({
    queryKey: adminUsageKeys.usage,
    queryFn: () => api<AdminUsage>('/api/admin/usage'),
    staleTime: 30_000,
  })
}

// GET /api/admin/feedback — submitted feedback, newest first. Instance-admin only.
export function useAdminFeedback() {
  return useQuery({
    queryKey: adminUsageKeys.feedback,
    queryFn: async () => {
      const { feedback } = await api<{ feedback: FeedbackEntry[] }>('/api/admin/feedback')
      return feedback
    },
    staleTime: 15_000,
  })
}
