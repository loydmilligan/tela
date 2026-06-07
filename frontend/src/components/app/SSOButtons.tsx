import { useState } from 'react'
import { Github } from 'lucide-react'
import { Button } from '../ui/button'
import { Input } from '../ui/input'
import { useSSOProviders } from '../../lib/queries/auth'

// Federated sign-in options on the login screen. Each social button is a plain
// anchor to a BACKEND start route (Button asChild) — a hard navigation so the
// browser hits the server's OAuth redirect, not the SPA router. Org SSO is a
// by-domain affordance: the user enters their work email and we bounce to the
// org connection resolved from its domain.
export function SSOButtons({ next }: { next: string }) {
  const { data } = useSSOProviders()
  const providers = data?.providers ?? []
  const orgSSO = data?.org_sso ?? false

  if (providers.length === 0 && !orgSSO) return null

  const start = (provider: string) =>
    `/api/auth/sso/${provider}/start?next=${encodeURIComponent(next)}`

  return (
    <div className="flex flex-col gap-[var(--space-3)]">
      <div className="flex items-center gap-[var(--space-3)] text-[length:var(--text-xs)] text-[var(--text-muted)]">
        <span className="h-px flex-1 bg-[var(--border-subtle)]" />
        or
        <span className="h-px flex-1 bg-[var(--border-subtle)]" />
      </div>

      {providers.map((p) => (
        <Button key={p.name} asChild variant="secondary" size="lg">
          <a href={start(p.name)} className="no-underline">
            <ProviderIcon name={p.name} />
            Continue with {p.label}
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
        variant="ghost"
        size="lg"
        onClick={() => setOpen(true)}
      >
        Sign in with SSO
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
        value={email}
        onChange={(e) => setEmail(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Enter') {
            e.preventDefault()
            go()
          }
        }}
      />
      <Button type="button" variant="secondary" size="lg" onClick={go}>
        Continue with SSO
      </Button>
    </div>
  )
}

function ProviderIcon({ name }: { name: string }) {
  if (name === 'github') return <Github size={18} aria-hidden />
  if (name === 'google') return <GoogleMark />
  if (name === 'microsoft') return <MicrosoftMark />
  return null
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
