import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../api'
import type { AdminUserRow } from '../types'
import type { RecentChange } from './recent-changes'
import { authKeys } from './auth'

export const adminUserKeys = {
  all: ['admin-users'] as const,
  list: () => [...adminUserKeys.all, 'list'] as const,
  activity: (id: number) => [...adminUserKeys.all, 'activity', id] as const,
}

// One user's recent edits, instance-wide (GET /api/admin/users/{id}/activity).
// Instance-admin only; unlike the home feed it isn't scoped to the caller's
// space access. `enabled` defers the fetch until the activity drawer opens.
export function useAdminUserActivity(userId: number, enabled: boolean) {
  return useQuery({
    queryKey: adminUserKeys.activity(userId),
    enabled,
    queryFn: async () => {
      const { changes } = await api<{ changes: RecentChange[] }>(
        `/api/admin/users/${userId}/activity`,
      )
      return changes
    },
    staleTime: 15_000,
  })
}

// Lists every user (active + inactive) for the instance-admin Settings tab.
// Sorted by username ASC on the backend. 403 to non-admins surfaces as the
// query erroring — the UI only mounts this hook from an admin-gated tab so
// that path should not fire in practice.
export function useAdminUsers() {
  return useQuery({
    queryKey: adminUserKeys.list(),
    queryFn: async () => {
      const { users } = await api<{ users: AdminUserRow[] }>(
        '/api/admin/users',
      )
      return users
    },
    staleTime: 30_000,
  })
}

export interface CreateAdminUserInput {
  username: string
  // Optional. Admin-created accounts with an email are treated as
  // pre-confirmed (no verification email is sent).
  email?: string
  password: string
  is_instance_admin?: boolean
}

export function useCreateAdminUser() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (input: CreateAdminUserInput) => {
      const { user } = await api<{ user: AdminUserRow }>('/api/admin/users', {
        method: 'POST',
        body: JSON.stringify(input),
      })
      return user
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: adminUserKeys.list() })
    },
  })
}

// PATCH /api/admin/users/{id}. Backend accepts any subset of these fields;
// at least one must be present (server returns 400 `bad_request` otherwise).
// Password reset or is_active=false wipes ALL sessions for the target user
// in the same tx.
export interface UpdateAdminUserInput {
  id: number
  is_active?: boolean
  is_instance_admin?: boolean
  password?: string
}

export function useUpdateAdminUser() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (input: UpdateAdminUserInput) => {
      const { id, ...body } = input
      const { user } = await api<{ user: AdminUserRow }>(
        `/api/admin/users/${id}`,
        { method: 'PATCH', body: JSON.stringify(body) },
      )
      return user
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: adminUserKeys.list() })
      // Defensive: if the patch ever targeted the caller (the UI hides
      // self-actions, but the backend would also reject), invalidate /me
      // so any cached state stays consistent.
      void qc.invalidateQueries({ queryKey: authKeys.me() })
    },
  })
}
