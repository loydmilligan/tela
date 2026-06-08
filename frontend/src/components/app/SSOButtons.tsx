import { useState } from 'react'
import { Building2 } from 'lucide-react'
import { Button } from '../ui/button'
import { Input } from '../ui/input'
import { useSSOProviders } from '../../lib/queries/auth'
import { useHostContext } from '../../lib/queries/host-context'

// Most-recognized first, so the list reads in a familiar order regardless of
// the backend's alphabetical sort.
const PROVIDER_ORDER = ['google', 'microsoft', 'github']

// Federated sign-in options on the login screen. Each social button is a plain
// anchor to a BACKEND start route (Button asChild) — a hard navigation so the
// browser hits the server's OAuth redirect, not the SPA router. Org SSO is a
// by-domain affordance: the user enters their work email and we bounce to the
// org connection resolved from its domain.
export function SSOButtons({ next }: { next: string }) {
  const { data } = useSSOProviders()
  const orgSSO = data?.org_sso ?? false
  // On a custom domain that's disabled social sign-in, suppress the social
  // provider buttons — the org-SSO by-domain affordance stays. Degrades to
  // "social on" while host context loads / on error.
  const socialEnabled = useHostContext().data?.login.social_enabled ?? true
  const providers = socialEnabled
    ? [...(data?.providers ?? [])].sort(
        (a, b) =>
          (PROVIDER_ORDER.indexOf(a.name) + 1 || 99) -
          (PROVIDER_ORDER.indexOf(b.name) + 1 || 99),
      )
    : []

  if (providers.length === 0 && !orgSSO) return null

  const start = (provider: string) =>
    `/api/auth/sso/${provider}/start?next=${encodeURIComponent(next)}`

  return (
    <div className="flex flex-col gap-[var(--space-3)]">
      <div className="flex items-center gap-[var(--space-3)] text-[length:var(--text-xs)] text-[var(--text-muted)]">
        <span className="h-px flex-1 bg-[var(--border-subtle)]" />
        Or continue with
        <span className="h-px flex-1 bg-[var(--border-subtle)]" />
      </div>

      {providers.map((p) => (
        <Button
          key={p.name}
          asChild
          variant="secondary"
          size="lg"
          className="font-medium hover:shadow-[var(--shadow-sm)]"
        >
          <a href={start(p.name)} className="no-underline">
            <ProviderIcon name={p.name} />
            {p.label}
          </a>
        </Button>
      ))}

      {orgSSO ? <OrgSSO next={next} /> : null}
    </div>
  )
}

// OrgSSO collects a work email and bounces to the org connection mapped to its
// domain. The backend 404s a domain with no SSO; we surface that inline.
function OrgSSO({ next }: { next: string }) {
  const [open, setOpen] = useState(false)
  const [email, setEmail] = useState('')

  if (!open) {
    return (
      <Button
        type="button"
        variant="secondary"
        size="lg"
        onClick={() => setOpen(true)}
        className="font-medium hover:shadow-[var(--shadow-sm)]"
      >
        <Building2 size={18} aria-hidden />
        Single sign-on (SSO)
      </Button>
    )
  }

  const go = () => {
    const value = email.trim()
    if (!value) return
    window.location.assign(
      `/api/auth/sso/org/start?email=${encodeURIComponent(value)}&next=${encodeURIComponent(next)}`,
    )
  }

  return (
    <div className="flex flex-col gap-[var(--space-2)]">
      <Input
        type="email"
        autoFocus
        placeholder="you@company.com"
        autoComplete="email"
        aria-label="Work email for single sign-on"
        value={email}
        onChange={(e) => setEmail(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Enter') {
            e.preventDefault()
            go()
          }
        }}
      />
      <Button
        type="button"
        variant="secondary"
        size="lg"
        onClick={go}
        className="font-medium hover:shadow-[var(--shadow-sm)]"
      >
        Continue with SSO
      </Button>
    </div>
  )
}

function ProviderIcon({ name }: { name: string }) {
  if (name === 'github') return <GitHubMark />
  if (name === 'google') return <GoogleMark />
  if (name === 'microsoft') return <MicrosoftMark />
  return null
}

// Brand marks are logo assets (lucide dropped its brand icons over trademark
// concerns), so they're inline SVGs. GitHub's mark is monochrome — currentColor.
function GitHubMark() {
  return (
    <svg width="18" height="18" viewBox="0 0 16 16" fill="currentColor" aria-hidden>
      <path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.01 8.01 0 0 0 16 8c0-4.42-3.58-8-8-8Z" />
    </svg>
  )
}

// Brand marks below are logo assets (not themeable UI), so their official
// colors are intentionally inline rather than design tokens.
function GoogleMark() {
  return (
    <svg width="18" height="18" viewBox="0 0 18 18" aria-hidden>
      <path
        fill="#4285F4"
        d="M17.64 9.2c0-.64-.06-1.25-.16-1.84H9v3.48h4.84a4.14 4.14 0 0 1-1.8 2.72v2.26h2.92c1.71-1.57 2.68-3.89 2.68-6.62Z"
      />
      <path
        fill="#34A853"
        d="M9 18c2.43 0 4.47-.8 5.96-2.18l-2.92-2.26c-.81.54-1.84.86-3.04.86-2.34 0-4.32-1.58-5.03-3.7H.96v2.33A9 9 0 0 0 9 18Z"
      />
      <path
        fill="#FBBC05"
        d="M3.97 10.72a5.4 5.4 0 0 1 0-3.44V4.95H.96a9 9 0 0 0 0 8.1l3.01-2.33Z"
      />
      <path
        fill="#EA4335"
        d="M9 3.58c1.32 0 2.5.46 3.44 1.35l2.58-2.58C13.47.89 11.43 0 9 0A9 9 0 0 0 .96 4.95l3.01 2.33C4.68 5.16 6.66 3.58 9 3.58Z"
      />
    </svg>
  )
}

function MicrosoftMark() {
  return (
    <svg width="18" height="18" viewBox="0 0 18 18" aria-hidden>
      <path fill="#F25022" d="M0 0h8.5v8.5H0z" />
      <path fill="#7FBA00" d="M9.5 0H18v8.5H9.5z" />
      <path fill="#00A4EF" d="M0 9.5h8.5V18H0z" />
      <path fill="#FFB900" d="M9.5 9.5H18V18H9.5z" />
    </svg>
  )
}
