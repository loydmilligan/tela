import { useQuery } from '@tanstack/react-query'
import { api } from '../api'

// Mention directory — active users for the @-mention picker. Backed by
// GET /api/users (any member). Cached like the page list; mentions are not
// latency-sensitive enough to warrant tighter freshness.

export interface MentionUser {
  id: number
  username: string
  email?: string
}

export const userKeys = {
  all: ['users'] as const,
  list: () => [...userKeys.all, 'list'] as const,
}

export function useUsers() {
  return useQuery({
    queryKey: userKeys.list(),
    queryFn: async () => {
      const { users } = await api<{ users: MentionUser[] }>('/api/users')
      return users
    },
    staleTime: 60_000,
  })
}
