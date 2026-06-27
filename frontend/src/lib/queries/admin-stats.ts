import { useQuery } from '@tanstack/react-query'
import { api } from '../api'

// Instance-analytics dashboard payload. Mirrors backend adminStats
// (admin_stats.go). The series are dense daily buckets aligned to `days`.

export interface StatsTopPage {
  page_id: number
  space_id: number
  title: string
  space_name: string
  count: number
}
export interface StatsTopPerson {
  label: string
  count: number
}
export interface StatsTopSpace {
  space_id: number
  name: string
  count: number
}
export interface StatsKindCount {
  kind: string
  count: number
}
export interface StatsSignup {
  user_id: number
  username: string
  display_name: string
  email: string
  created_at: string
  activated: boolean
}
export interface StatsUnanswered {
  question: string
  who: string
  created_at: string
}

export interface AdminStats {
  days: string[]
  views: number[]
  edits: number[]
  logins: number[]
  asks: number[]
  errors: number[]
  users_cum: number[]
  pages_cum: number[]
  dau: number
  wau: number
  mau: number
  users: number
  spaces: number
  pages: number
  top_pages: StatsTopPage[]
  top_contributors: StatsTopPerson[]
  top_spaces: StatsTopSpace[]
  asks_30: number
  asks_answered_30: number
  errors_by_kind: StatsKindCount[]
  stale_pages: number
  orphan_pages: number
  contradictions: number
  new_users_30: number
  activated: number
  recent_signups: StatsSignup[]
  unanswered_asks: StatsUnanswered[]
}

export const adminStatsKeys = { stats: ['admin-stats'] as const }

// GET /api/admin/stats — instance-admin only.
export function useAdminStats() {
  return useQuery({
    queryKey: adminStatsKeys.stats,
    queryFn: () => api<AdminStats>('/api/admin/stats'),
    staleTime: 60_000,
  })
}
