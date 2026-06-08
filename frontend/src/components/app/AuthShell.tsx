import type { ReactNode } from 'react'
import { ThemeSwitcher } from '../ThemeSwitcher'
import { BrandMark } from '../BrandMark'
import { useHostContext } from '../../lib/queries/host-context'

// Shared chrome for the unauthenticated auth surfaces (login / register /
// verify / forgot / reset). Mirrors the original login page layout so all the
// auth pages read as one family: a thin branded header and a centered card.
export function AuthShell({ children }: { children: ReactNode }) {
  // On an org's custom domain, white-label the header with the org name; keep
  // tela branding on the canonical host. Degrades to tela while loading / on
  // error (org null).
  const org = useHostContext().data?.org ?? null
  return (
    <div className="min-h-dvh flex flex-col bg-[var(--surface-1)] text-[var(--text-primary)]">
      <header className="flex items-center justify-between px-[var(--space-6)] py-[var(--space-3)] border-b border-[var(--border-subtle)] shrink-0">
        {org ? (
          <span className="m-0 inline-flex items-center gap-[var(--space-2)] text-[length:var(--text-lg)] leading-[var(--leading-tight)] font-[family-name:var(--font-sans)] text-[var(--text-primary)]">
            {org.name}
          </span>
        ) : (
          /* The landing lives at "/" (served by Caddy, not an SPA route), so
             this is a real navigation with the ?home=1 hatch — not a router
             Link, which would resolve "/" in-app and bounce back. */
          <a
            href="/?home=1"
            aria-label="tela — home"
            className="m-0 inline-flex items-center gap-[var(--space-2)] text-[length:var(--text-lg)] leading-[var(--leading-tight)] font-[family-name:var(--font-sans)] text-[var(--text-primary)] no-underline"
          >
            <BrandMark size={22} />
            tela
          </a>
        )}
        <ThemeSwitcher />
      </header>
      <main className="relative flex-1 flex items-center justify-center p-[var(--space-7)]">
        <div aria-hidden className="tela-auth-backdrop" />
        <div className="relative w-full max-w-[25rem]">{children}</div>
      </main>
    </div>
  )
}

// AuthField is a labelled form row. labelSlot renders at the far end of the
// label line (e.g. a "Forgot password?" link) so the label + helper align.
export function AuthField({
  id,
  label,
  labelSlot,
  children,
}: {
  id: string
  label: string
  labelSlot?: ReactNode
  children: ReactNode
}) {
  return (
    <div className="flex flex-col gap-[var(--space-2)]">
      <div className="flex items-center justify-between gap-[var(--space-2)]">
        <label
          htmlFor={id}
          className="text-[length:var(--text-sm)] text-[var(--text-muted)]"
        >
          {label}
        </label>
        {labelSlot}
      </div>
      {children}
    </div>
  )
}

// AuthFooterLink is the small centered line under a card (cross-links between
// sign-in / register).
export function AuthFooterLink({ children }: { children: ReactNode }) {
  return (
    <p className="m-0 mt-[var(--space-5)] text-center text-[length:var(--text-sm)] text-[var(--text-muted)]">
      {children}
    </p>
  )
}
