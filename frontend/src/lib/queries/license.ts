import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../api'

// Self-host Enterprise license (internal/ee). Instance-admin only. The raw key is
// never returned — only this status summary.
export interface LicenseStatus {
  customer: string
  tier: string
  seats: number
  features: string[]
  issued_at?: string
  expires_at?: string
  valid: boolean
}

export interface LicenseInfo {
  license: LicenseStatus
  // true → the key is pinned via TELA_LICENSE_KEY (env); the UI is read-only.
  env_locked: boolean
  // Present (self-host only) when the key is seated: active users vs licensed
  // seats, for the soft over-seat notice. Features stay on regardless.
  seat_usage?: { used: number; licensed: number }
}

const licenseKey = ['admin', 'license'] as const

// GET /api/admin/license — current license status + whether it's env-pinned.
export function useLicense() {
  return useQuery({
    queryKey: licenseKey,
    queryFn: () => api<LicenseInfo>('/api/admin/license'),
    staleTime: 60_000,
  })
}

// PUT /api/admin/license — install/replace the key (verified server-side first).
export function useSetLicense() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (token: string) =>
      api<{ license: LicenseStatus }>('/api/admin/license', {
        method: 'PUT',
        body: JSON.stringify({ token }),
      }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: licenseKey })
    },
  })
}

// DELETE /api/admin/license — remove the key (downgrade to Community).
export function useClearLicense() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () => api<void>('/api/admin/license', { method: 'DELETE' }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: licenseKey })
    },
  })
}
