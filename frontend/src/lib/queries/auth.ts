import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ApiError, api } from '../api'
import { pageKeys } from './pages'
import { spaceKeys } from './spaces'

// Mirrors backend/internal/api/auth.go's authUserDTO.
export interface AuthUser {
  id: number
  username: string
  is_instance_admin: boolean
}

export interface LoginInput {
  username: string
  password: string
}

export const authKeys = {
  all: ['auth'] as const,
  me: () => [...authKeys.all, 'me'] as const,
}

// Probe the session cookie. The HttpOnly cookie isn't readable from JS, so the
// only honest way to know whether we're logged in is to ask /api/auth/me.
// 401 → null (the canonical "no session" state); other failures bubble up.
export async function fetchMe(): Promise<AuthUser | null> {
  try {
    const { user } = await api<{ user: AuthUser }>('/api/auth/me')
    return user
  } catch (err) {
    if (err instanceof ApiError && err.status === 401) return null
    throw err
  }
}

export function useMe() {
  return useQuery({
    queryKey: authKeys.me(),
    queryFn: fetchMe,
    retry: false,
    staleTime: Infinity,
  })
}

export function useLogin() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (input: LoginInput) => {
      const { user } = await api<{ user: AuthUser }>('/api/auth/login', {
        method: 'POST',
        body: JSON.stringify(input),
      })
      return user
    },
    onSuccess: (user) => {
      // Seed the me cache so AppLayout's beforeLoad doesn't issue an extra
      // round-trip on the post-login redirect.
      qc.setQueryData(authKeys.me(), user)
    },
  })
}

export function useLogout() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async () => {
      await api<void>('/api/auth/logout', { method: 'POST' })
    },
    onSettled: () => {
      // Drop the auth state immediately, then sweep any user-scoped data so
      // the next signed-in user doesn't see the previous user's cached
      // spaces/pages. Other namespaces (search, etc.) flow through the same
      // queries on their next mount.
      qc.setQueryData(authKeys.me(), null)
      qc.removeQueries({ queryKey: spaceKeys.all })
      qc.removeQueries({ queryKey: pageKeys.all })
    },
  })
}

// Validate a `?next=` param before bouncing back into the app. We only accept
// in-app paths (`/foo`), and explicitly reject protocol-relative `//evil.com`
// and absolute / scheme-bearing URLs. Empty / unrecognised values → null so
// callers can fall back to '/'.
export function sanitizeNextPath(raw: unknown): string | null {
  if (typeof raw !== 'string') return null
  if (raw.length === 0) return null
  if (!raw.startsWith('/')) return null
  if (raw.startsWith('//')) return null
  return raw
}
