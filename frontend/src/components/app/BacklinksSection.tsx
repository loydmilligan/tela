import { useMemo } from 'react'
import { Link } from '@tanstack/react-router'
import { FileText } from 'lucide-react'
import { useBacklinks } from '../../lib/queries/pages'
import { HighlightedSnippet } from '../../lib/highlightSnippet'
import { disambiguateBreadcrumbs } from '../../lib/disambiguateBreadcrumbs'
import { cn } from '../../lib/utils'

interface BacklinksSectionProps {
  pageId: number
}

export function BacklinksSection({ pageId }: BacklinksSectionProps) {
  const { data, isLoading, isError } = useBacklinks(pageId)
  const rows = useMemo(() => disambiguateBreadcrumbs(data ?? []), [data])

  // Loading / error / empty all render nothing — backlinks are non-essential
  // UX, never block the page or add visual noise.
  if (isLoading || isError) return null
  if (rows.length === 0) return null

  const headerCopy =
    rows.length === 1 ? '1 page links here' : `${rows.length} pages link here`

  return (
    <section
      aria-labelledby={`backlinks-${pageId}`}
      className="flex flex-col gap-[var(--space-2)] pt-[var(--space-4)] border-t border-[var(--border-subtle)]"
    >
      <h2
        id={`backlinks-${pageId}`}
        className="m-0 text-[length:var(--text-xs)] uppercase tracking-wider text-[var(--text-muted)] font-[family-name:var(--font-sans)]"
      >
        {headerCopy}
      </h2>
      <ul className="m-0 p-0 list-none flex flex-col gap-[1px]">
        {rows.map((row) => (
          <li key={row.item.page_id} className="m-0 p-0 list-none">
            <Link
              to="/spaces/$spaceId/pages/$pageId"
              params={{
                spaceId: row.item.space_id,
                pageId: row.item.page_id,
              }}
              className={cn(
                'group block w-full no-underline',
                'flex items-start gap-[var(--space-3)]',
                'px-[var(--space-3)] py-[var(--space-2)]',
                'rounded-[var(--radius-sm)]',
                'hover:bg-[var(--surface-2)]',
                'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent)]',
              )}
            >
              <FileText
                aria-hidden
                width={14}
                height={14}
                className="mt-[2px] shrink-0 text-[var(--text-muted)] group-hover:text-[var(--text-primary)]"
              />
              <span className="flex-1 min-w-0 flex flex-col gap-[2px]">
                <span className="flex items-center gap-[var(--space-2)] min-w-0">
                  <span className="truncate text-[length:var(--text-sm)] text-[var(--text-primary)] font-medium font-[family-name:var(--font-sans)]">
                    {row.item.title || 'Untitled'}
                  </span>
                  {row.showSpaceChip ? (
                    <span
                      className={cn(
                        'shrink-0',
                        'font-[family-name:var(--font-sans)]',
                        'text-[length:var(--text-xs)] leading-[var(--leading-tight)]',
                        'text-[var(--text-muted)]',
                        'bg-[var(--surface-1)] border border-[var(--border-subtle)]',
                        'rounded-[var(--radius-sm)]',
                        'px-[var(--space-2)] py-[1px]',
                      )}
                    >
                      {row.item.space_name}
                    </span>
                  ) : null}
                </span>
                <span className="truncate text-[length:var(--text-xs)] text-[var(--text-muted)] font-[family-name:var(--font-sans)]">
                  {row.breadcrumbLabel}
                </span>
                {row.item.snippet ? (
                  <span className="truncate text-[length:var(--text-xs)] text-[var(--text-muted)] font-[family-name:var(--font-sans)]">
                    <HighlightedSnippet snippet={row.item.snippet} />
                  </span>
                ) : null}
              </span>
            </Link>
          </li>
        ))}
      </ul>
    </section>
  )
}
