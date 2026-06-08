import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../api'
import type { Plan, Usage } from '../types'
import { adminUserKeys } from './admin-users'
import { orgKeys } from './orgs'

// Metering & tiers. usage = an account's plan + live consumption; plans = the
// tier catalog (for comparison UI); setPlan = instance-admin assignment (there's
// no self-serve billing).
export const billingKeys = {
  all: ['billing'] as const,
  myUsage: () => [...billingKeys.all, 'usage', 'me'] as const,
  orgUsage: (orgId: number) => [...billingKeys.all, 'usage', 'org', orgId] as const,
  plans: () => [...billingKeys.all, 'plans'] as const,
}

// GET /api/usage — the caller's personal-account plan + usage.
export function useMyUsage() {
  return useQuery({
    queryKey: billingKeys.myUsage(),
    queryFn: () => api<Usage>('/api/usage'),
    staleTime: 30_000,
  })
}

// GET /api/orgs/{id}/usage — an org's plan + usage (any member may read).
export function useOrgUsage(orgId: number | null | undefined) {
  return useQuery({
    queryKey: billingKeys.orgUsage(orgId ?? -1),
    queryFn: () => api<Usage>(`/api/orgs/${orgId}/usage`),
    enabled: orgId != null,
    staleTime: 30_000,
  })
}

// GET /api/plans — every tier, for the plan-comparison UI.
export function usePlans() {
  return useQuery({
    queryKey: billingKeys.plans(),
    queryFn: async () => {
      const { plans } = await api<{ plans: Plan[] }>('/api/plans')
      return plans
    },
    staleTime: 5 * 60_000,
  })
}

export interface SetPlanInput {
  account_kind: 'user' | 'org'
  account_id: number
  plan_key: string
}

// PATCH /api/admin/plan — instance-admin only. Invalidates the affected
// account's usage so the panel reflects the new tier immediately.
export function useSetPlan() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: SetPlanInput) =>
      api<Usage>('/api/admin/plan', {
        method: 'PATCH',
        body: JSON.stringify(input),
      }),
    onSuccess: (updated) => {
      // Usage cards for the affected account.
      if (updated.account_kind === 'org') {
        void qc.invalidateQueries({ queryKey: billingKeys.orgUsage(updated.account_id) })
      } else {
        void qc.invalidateQueries({ queryKey: billingKeys.myUsage() })
      }
      // The admin Users + Orgs lists now carry plan_key — refresh their badges.
      void qc.invalidateQueries({ queryKey: orgKeys.list() })
      void qc.invalidateQueries({ queryKey: adminUserKeys.list() })
    },
  })
}
