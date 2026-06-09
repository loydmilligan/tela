import type { ReactNode } from 'react'
import { Monogram } from './Monogram'

// The identity block atop a public blog surface — a generated monogram avatar
// (deterministic tint, since there's no uploaded image), the name, an optional
// standfirst (space description / author bio), and a meta line (byline, counts).
// Shared by the space front page and /u/{handle} so both read as one site.
export function PublicMasthead({
  title,
  avatarSeed,
  standfirst,
  meta,
}: {
  title: string
  avatarSeed: string
  standfirst?: string
  meta?: ReactNode
}) {
  return (
    <header className="flex flex-col items-start gap-[var(--space-4)]">
      <Monogram name={title} seed={avatarSeed} size="md" />
      <div className="flex flex-col gap-[var(--space-2)]">
        <h1 className="m-0 font-[family-name:var(--font-sans)] text-[length:var(--text-3xl)] font-semibold leading-[var(--leading-tight)] tracking-[-0.02em] text-[var(--text-primary)]">
          {title}
        </h1>
        {standfirst ? (
          <p className="m-0 max-w-[42rem] text-[length:var(--text-lg)] leading-[var(--leading-normal)] text-[var(--text-muted)]">
            {standfirst}
          </p>
        ) : null}
        {meta ? (
          <div className="mt-[var(--space-1)] flex flex-wrap items-center gap-x-[var(--space-2)] gap-y-[var(--space-1)] text-[length:var(--text-sm)] text-[var(--text-muted)]">
            {meta}
          </div>
        ) : null}
      </div>
    </header>
  )
}

// A subtle dot separator for meta rows (date · reading time, byline · counts).
export function MetaDot() {
  return (
    <span
      aria-hidden
      className="inline-block size-[3px] rounded-full bg-[var(--text-muted)] opacity-60"
    />
  )
}
