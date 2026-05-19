import { forwardRef, useMemo } from 'react'
import { cn } from '../../lib/utils'
import { relativeTimeFromSqlite } from '../../lib/relativeTime'

export interface SearchResultProps {
  title: string
  // Single-line crumb (e.g. space name, optionally with a parent chain).
  // Empty string hides the row entirely; the rest of the layout still aligns.
  breadcrumb: string
  // Plain text — body-tier excerpts never contain HTML in v0. Up to ~200
  // chars; we clamp to two lines for tall rows.
  excerpt: string
  // SQLite-native `YYYY-MM-DD HH:MM:SS` UTC timestamp. Parsed inline so callers
  // don't need to pre-format.
  updatedAt: string
  onSelect: () => void
}

// Owned result-row primitive for the dedicated /search route. Composes to one
// button per hit: title + breadcrumb + clamped excerpt on the left, relative
// time on the right. Pure tokens — never reach for hex / raw px / ad-hoc radii
// (Q8). Focus ring routes through `--accent` for keyboard nav parity with
// the rest of the app.
export const SearchResult = forwardRef<HTMLButtonElement, SearchResultProps>(
  function SearchResult(
    { title, breadcrumb, excerpt, updatedAt, onSelect },
    ref,
  ) {
    const rel = useMemo(
      () => (updatedAt ? relativeTimeFromSqlite(updatedAt) : ''),
      [updatedAt],
    )
    return (
      <button
        ref={ref}
        type="button"
        onClick={onSelect}
        className={cn(
          'w-full text-left',
          'flex flex-col gap-[var(--space-1)]',
          'px-[var(--space-4)] py-[var(--space-3)]',
          'rounded-[var(--radius-md)]',
          'border border-transparent',
          'bg-transparent cursor-pointer outline-none',
          'transition-[background-color,border-color] duration-[var(--duration-fast)] ease-[var(--ease-out)]',
          'hover:bg-[var(--surface-2)]',
          'focus-visible:ring-2 focus-visible:ring-[var(--accent)] focus-visible:ring-offset-2 focus-visible:ring-offset-[var(--surface-1)]',
        )}
      >
        <span className="flex items-baseline justify-between gap-[var(--space-3)]">
          <span className="text-[length:var(--text-base)] font-medium text-[var(--text-primary)] truncate font-[family-name:var(--font-sans)]">
            {title || 'Untitled'}
          </span>
          {rel ? (
            <span className="text-[length:var(--text-xs)] text-[var(--text-muted)] shrink-0 font-[family-name:var(--font-sans)]">
              {rel}
            </span>
          ) : null}
        </span>
        {breadcrumb ? (
          <span className="text-[length:var(--text-xs)] text-[var(--text-muted)] truncate font-[family-name:var(--font-sans)]">
            {breadcrumb}
          </span>
        ) : null}
        {excerpt ? (
          <span className="text-[length:var(--text-sm)] text-[var(--text-primary)] leading-[var(--leading-relaxed)] font-[family-name:var(--font-sans)]">
            {excerpt}
          </span>
        ) : null}
      </button>
    )
  },
)
