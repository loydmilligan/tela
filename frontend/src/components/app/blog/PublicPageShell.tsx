import type { ReactNode } from 'react'
import { PublicTopbar } from './PublicTopbar'

// The shared frame for the no-login public surfaces — the space front page, the
// author home, the handle home, and /discover. Full-height column, sticky
// topbar, a centered content container. Defined once so every surface reads as
// one site and the frame never drifts between them.
export function PublicPageShell({ children }: { children: ReactNode }) {
  return (
    <div className="flex min-h-dvh flex-col bg-[var(--surface-1)] text-[var(--text-primary)]">
      <PublicTopbar />
      <main className="flex-1">
        <div className="mx-auto w-full max-w-[60rem] px-[var(--space-6)] py-[var(--space-8)]">
          {children}
        </div>
      </main>
    </div>
  )
}
