import { useMemo } from 'react'
import { Link } from '@tanstack/react-router'
import { FileText, Sparkles } from 'lucide-react'
import { useRelatedPages } from '../../lib/queries/pages'
import { useSpaces } from '../../lib/queries/spaces'
import { cn } from '../../lib/utils'

interface RelatedPagesSectionProps {
  pageId: number
  // The space the source page lives in — drives the cross-space chip (a related
  // page in a *different* space is worth flagging; same-space is the common case).
  spaceId: number
}

// RelatedPagesSection — "Related pages" in the page view's below-content zone.
// The semantic counterpart to Backlinks: where Backlinks shows pages a human
// explicitly [[linked]], this shows pages that are *about the same thing*,
// computed from embeddings (GET /api/pages/{id}/related — one pgvector query, no
// model call). An always-open section, not a collapsible, because discovery is a
// front feature of reading — it surfaces the connections nobody got around to
// drawing. Renders nothing on loading / error / empty / unconfigured-embedder, so
// it never adds noise to a page with no semantic neighbours.
export function RelatedPagesSection({ pageId, spaceId }: RelatedPagesSectionProps) {
  const { data, isLoading, isError } = useRelatedPages(pageId)
  const spacesQuery = useSpaces()
  const spaceName = useMemo(() => {
    const m = new Map<number, string>()
    for (const s of spacesQuery.data ?? []) m.set(s.id, s.name)
    return m
  }, [spacesQuery.data])

  const rows = data ?? []
  // Normalise the meter to the strongest neighbour so the bars always use the
  // full width — raw cosine similarities cluster in a narrow high band, which
  // would render every bar near-full and meaningless.
  const top = rows.length > 0 ? rows[0].similarity : 1

  if (isLoading || isError) return null
  if (rows.length === 0) return null

  return (
    <section
      aria-labelledby={`related-pages-${pageId}`}
      className="flex flex-col gap-[var(--space-2)] pt-[var(--space-4)] border-t border-[var(--border-subtle)]"
    >
      <h2
        id={`related-pages-${pageId}`}
        className="m-0 flex items-center gap-[var(--space-2)] text-[length:var(--text-xs)] uppercase tracking-wider text-[var(--text-muted)] font-[family-name:var(--font-sans)]"
      >
        <Sparkles aria-hidden width={13} height={13} className="text-[var(--accent)]" />
        Related pages
      </h2>
      <ul className="m-0 p-0 list-none flex flex-col gap-[1px]">
        {rows.map((row) => {
          const crossSpace = row.space_id !== spaceId
          const fraction = top > 0 ? Math.max(row.similarity / top, 0.08) : 0
          return (
            <li key={row.page_id} className="m-0 p-0 list-none">
              <Link
                to="/spaces/$spaceId/pages/$pageId/{-$slug}"
                params={{
                  spaceId: row.space_id,
                  pageId: row.page_id,
                  slug: undefined,
                }}
                className={cn(
                  'group block w-full no-underline',
                  'flex items-center gap-[var(--space-3)]',
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
                  className="shrink-0 text-[var(--text-muted)] group-hover:text-[var(--text-primary)]"
                />
                <span className="flex flex-1 items-center gap-[var(--space-2)] min-w-0">
                  <span className="truncate text-[length:var(--text-sm)] text-[var(--text-primary)] font-medium font-[family-name:var(--font-sans)]">
                    {row.title || 'Untitled'}
                  </span>
                  {crossSpace && spaceName.has(row.space_id) ? (
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
                      {spaceName.get(row.space_id)}
                    </span>
                  ) : null}
                </span>
                {/* Similarity meter — a quiet signal of how close the match is,
                    normalised to the top neighbour. Decorative, so aria-hidden. */}
                <span
                  aria-hidden
                  title={`${Math.round(row.similarity * 100)}% similar`}
                  className="shrink-0 h-[3px] w-[var(--space-8)] rounded-full bg-[var(--surface-3)] overflow-hidden"
                >
                  <span
                    className="block h-full rounded-full bg-[color-mix(in_oklch,var(--accent)_70%,transparent)]"
                    style={{ width: `${Math.round(fraction * 100)}%` }}
                  />
                </span>
              </Link>
            </li>
          )
        })}
      </ul>
    </section>
  )
}
