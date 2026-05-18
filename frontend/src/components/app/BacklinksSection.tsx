import { useMemo } from 'react'
import { Link } from '@tanstack/react-router'
import { FileText } from 'lucide-react'
import { useBacklinks } from '../../lib/queries/pages'
import type { Backlink } from '../../lib/types'
import { HighlightedSnippet } from '../../lib/highlightSnippet'
import { cn } from '../../lib/utils'

interface BacklinksSectionProps {
  pageId: number
}

interface BacklinkRow {
  link: Backlink
  breadcrumbLabel: string
  showSpaceChip: boolean
}

function buildRows(backlinks: Backlink[]): BacklinkRow[] {
  // Same breadcrumb-collision rule the wikilink picker uses (Q14): a label
  // held by rows from two different spaces gets the space-name chip on the
  // colliding rows so the user can disambiguate without hovering.
  const labelOf = (b: Backlink) =>
    b.breadcrumb.length === 0 ? '(space root)' : b.breadcrumb.join(' › ')
  const spacesByLabel = new Map<string, Set<number>>()
  for (const b of backlinks) {
    const label = labelOf(b)
    let set = spacesByLabel.get(label)
    if (!set) {
      set = new Set()
      spacesByLabel.set(label, set)
    }
    set.add(b.space_id)
  }
  return backlinks.map((link) => {
    const label = labelOf(link)
    const spaces = spacesByLabel.get(label)
    return {
      link,
      breadcrumbLabel: label,
      showSpaceChip: !!spaces && spaces.size > 1,
    }
  })
}

export function BacklinksSection({ pageId }: BacklinksSectionProps) {
  const { data, isLoading, isError } = useBacklinks(pageId)
  const rows = useMemo(() => buildRows(data ?? []), [data])

  // Loading / error / empty all render nothing — backlinks are non-essential
  // UX, never block the page or add visual noise. Wording matches the brief
  // verbatim ("N pages link here") even at N=1.
  if (isLoading || isError) return null
  if (rows.length === 0) return null

  return (
    <section
      aria-labelledby={`backlinks-${pageId}`}
      className="flex flex-col gap-[var(--space-2)] pt-[var(--space-4)] border-t border-[var(--border-subtle)]"
    >
      <h2
        id={`backlinks-${pageId}`}
        className="m-0 text-[length:var(--text-xs)] uppercase tracking-wider text-[var(--text-muted)] font-[family-name:var(--font-sans)]"
      >
        {rows.length} pages link here
      </h2>
      <ul className="m-0 p-0 list-none flex flex-col gap-[1px]">
        {rows.map((row) => (
          <li key={row.link.page_id} className="m-0 p-0 list-none">
            <Link
              to="/spaces/$spaceId/pages/$pageId"
              params={{
                spaceId: row.link.space_id,
                pageId: row.link.page_id,
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
                    {row.link.title || 'Untitled'}
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
                      {row.link.space_name}
                    </span>
                  ) : null}
                </span>
                <span className="truncate text-[length:var(--text-xs)] text-[var(--text-muted)] font-[family-name:var(--font-sans)]">
                  {row.breadcrumbLabel}
                </span>
                {row.link.snippet ? (
                  <span className="truncate text-[length:var(--text-xs)] text-[var(--text-muted)] font-[family-name:var(--font-sans)]">
                    <HighlightedSnippet snippet={row.link.snippet} />
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
