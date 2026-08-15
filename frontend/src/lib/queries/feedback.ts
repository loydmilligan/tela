import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../api'
import { adminUsageKeys } from './admin-usage'
import type {
  CreateFeedbackInput,
  FeedbackOptions,
  FeedbackRoutingSettings,
} from '../types'

// POST /api/feedback — submit feedback from the in-app widget. The same backend
// core powers the MCP submit_feedback tool, so human + agent reports land in one
// inbox. Returns the created row (we ignore the body; the widget only needs ok).
export function useCreateFeedback() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: CreateFeedbackInput) =>
      api<{ feedback: { id: number } }>('/api/feedback', {
        method: 'POST',
        body: JSON.stringify(input),
      }),
    onSuccess: () => {
      // If the submitter happens to be an instance admin with the inbox open,
      // surface their own entry without a manual refetch.
      void qc.invalidateQueries({ queryKey: adminUsageKeys.feedback })
    },
  })
}

// ── Phase-2a routing: composer options + admin settings ─────────────────────

export const feedbackKeys = {
  options: ['feedback', 'options'] as const,
  adminSettings: ['feedback', 'admin-settings'] as const,
}

// Composer bootstrap: the caller's permission bits + pickable recipients.
export function useFeedbackOptions(open: boolean) {
  return useQuery({
    queryKey: feedbackKeys.options,
    queryFn: () => api<FeedbackOptions>('/api/feedback/options'),
    // Only fetch once the popover opens; permissions change rarely.
    enabled: open,
    staleTime: 5 * 60 * 1000,
  })
}

export function useFeedbackAdminSettings() {
  return useQuery({
    queryKey: feedbackKeys.adminSettings,
    queryFn: () =>
      api<{ settings: FeedbackRoutingSettings }>('/api/admin/feedback/settings').then(
        (r) => r.settings,
      ),
  })
}

function invalidateRouting(qc: ReturnType<typeof useQueryClient>) {
  void qc.invalidateQueries({ queryKey: feedbackKeys.adminSettings })
  void qc.invalidateQueries({ queryKey: feedbackKeys.options })
}

export function useUpdateFeedbackSettings() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: { default_user_id?: number | null; default_group_id?: number | null }) =>
      api('/api/admin/feedback/settings', { method: 'PUT', body: JSON.stringify(input) }),
    onSuccess: () => invalidateRouting(qc),
  })
}

export function useCreateFeedbackGroup() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: { name: string; member_ids: number[] }) =>
      api('/api/admin/feedback/groups', { method: 'POST', body: JSON.stringify(input) }),
    onSuccess: () => invalidateRouting(qc),
  })
}

export function useUpdateFeedbackGroup() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, ...input }: { id: number; name?: string; member_ids?: number[] }) =>
      api(`/api/admin/feedback/groups/${id}`, { method: 'PUT', body: JSON.stringify(input) }),
    onSuccess: () => invalidateRouting(qc),
  })
}

export function useDeleteFeedbackGroup() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => api(`/api/admin/feedback/groups/${id}`, { method: 'DELETE' }),
    onSuccess: () => invalidateRouting(qc),
  })
}

export function useUpdateFeedbackPermission() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ user_id, ...input }: { user_id: number; enabled: boolean; allow_claude: boolean }) =>
      api(`/api/admin/feedback/permissions/${user_id}`, { method: 'PUT', body: JSON.stringify(input) }),
    onSuccess: () => invalidateRouting(qc),
  })
}

// Per-page issue badge: how many feedback reports reference this page.
export function useFeedbackForPage(pageId: number | null) {
  return useQuery({
    queryKey: ['feedback', 'for-page', pageId] as const,
    queryFn: () => api<{ count: number }>(`/api/feedback/for-page/${pageId}`),
    enabled: pageId != null,
    staleTime: 60 * 1000,
  })
}
