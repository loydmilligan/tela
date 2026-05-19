import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../api'
import type { SessionRow } from '../types'

export const sessionKeys = {
  all: ['sessions'] as const,
  list: () => [...sessionKeys.all, 'list'] as const,
}

// Active sessions for the authenticated user. Backed by
// GET /api/users/me/sessions; rows are returned sorted last_seen_at DESC
// with `current=true` flagging the request cookie's own session.
export function useSessions() {
  return useQuery({
    queryKey: sessionKeys.list(),
    queryFn: async () => {
      const { sessions } = await api<{ sessions: SessionRow[] }>(
        '/api/users/me/sessions',
      )
      return sessions
    },
    staleTime: 30_000,
  })
}

// Revoke one session by id. Backend refuses the current session with 400
// (`code: bad_request`) — the UI disables the button on the current row so
// that branch shouldn't fire, but the error surfaces if it does.
export function useRevokeSession() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (id: string) => {
      await api<void>(`/api/users/me/sessions/${encodeURIComponent(id)}`, {
        method: 'DELETE',
      })
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: sessionKeys.list() })
    },
  })
}

// "Logout everywhere" — kills every session for the caller except the one
// whose cookie made this request.
export function useLogoutEverywhere() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async () => {
      await api<void>('/api/users/me/sessions', { method: 'DELETE' })
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: sessionKeys.list() })
    },
  })
}

// Change the caller's own password. Backend revokes every OTHER session
// for this user in the same tx on success, so we invalidate the sessions
// query to surface that. 401 on wrong old password; 400 on too-short new
// password (also enforced client-side before submit).
export interface ChangePasswordInput {
  old_password: string
  new_password: string
}

export function useChangePassword() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (input: ChangePasswordInput) => {
      await api<void>('/api/users/me/password', {
        method: 'POST',
        body: JSON.stringify(input),
      })
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: sessionKeys.list() })
    },
  })
}
