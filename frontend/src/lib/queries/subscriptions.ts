import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../api'

// Follow/subscribe to a page or space → opts into its change notifications.
// Backed by /api/{pages|spaces}/{id}/subscription.

export type SubscribableKind = 'page' | 'space'

export const subscriptionKeys = {
  all: ['subscriptions'] as const,
  one: (kind: SubscribableKind, id: number) => [...subscriptionKeys.all, kind, id] as const,
}

function subPath(kind: SubscribableKind, id: number) {
  return `/api/${kind === 'page' ? 'pages' : 'spaces'}/${id}/subscription`
}

export function useSubscription(kind: SubscribableKind, id: number | null | undefined) {
  return useQuery({
    queryKey: id != null ? subscriptionKeys.one(kind, id) : subscriptionKeys.one(kind, -1),
    queryFn: async () => {
      const { subscribed } = await api<{ subscribed: boolean }>(subPath(kind, id as number))
      return subscribed
    },
    enabled: id != null,
  })
}

export function useToggleSubscription(kind: SubscribableKind, id: number) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (subscribed: boolean) => {
      await api<void>(subPath(kind, id), { method: subscribed ? 'DELETE' : 'POST' })
      return !subscribed
    },
    onSuccess: (nowSubscribed) => {
      qc.setQueryData(subscriptionKeys.one(kind, id), nowSubscribed)
    },
  })
}
