import { useQuery } from '@tanstack/react-query'
import type { HostContext } from '../types'

// Sensible canonical-host default: the org is absent and every standard sign-in
// method is on. A transient fetch failure degrades to this so the login screen
// always renders something usable (never strands behind a white-label probe).
const DEFAULT_HOST_CONTEXT: HostContext = {
  org: null,
  login: {
    password_enabled: true,
    social_enabled: true,
    org_sso_available: false,
  },
}

// GET /api/host-context — PUBLIC, host-derived. Raw fetch (not api()) since
// there's no session to gate and api() would treat a transient failure as an
// auth bounce. Never throws: any failure degrades to DEFAULT_HOST_CONTEXT.
export async function fetchHostContext(): Promise<HostContext> {
  try {
    const res = await fetch('/api/host-context')
    if (!res.ok) return DEFAULT_HOST_CONTEXT
    return (await res.json()) as HostContext
  } catch {
    return DEFAULT_HOST_CONTEXT
  }
}

// Host-derived white-labeling context for the login surface. Cacheable and
// host-stable for the page's lifetime, so staleTime: Infinity (mirrors
// useSSOProviders). Degrades to DEFAULT_HOST_CONTEXT while loading / on error.
export function useHostContext() {
  return useQuery({
    queryKey: ['host-context'],
    queryFn: fetchHostContext,
    staleTime: Infinity,
    retry: false,
    initialData: DEFAULT_HOST_CONTEXT,
  })
}
