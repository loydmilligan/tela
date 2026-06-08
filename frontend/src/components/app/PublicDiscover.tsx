import { Link } from '@tanstack/react-router'
import { Compass, FileText, FrownIcon } from 'lucide-react'
import {
  DISCOVER_PAGE_SIZE,
  usePublicDiscover,
  type DiscoverSort,
  type DiscoverSpace,
} from '../../lib/queries/public'
import { useHeadMeta } from '../../lib/useHeadMeta'
import { avatarStyle, monogram } from '../../lib/blog'
import { relativeTimeFromSqlite } from '../../lib/relativeTime'
import { Button } from '../ui/button'
import { EmptyState } from '../ui/empty-state'
import { PublicTopbar } from './blog/PublicTopbar'
import { PublicMasthead, MetaDot } from './blog/PublicMasthead'

// The cross-tenant public-space directory at /discover — a login-free "network"
// view of every public space on the instance. Same no-login chrome as the space
// front page and /u/{handle}, so the whole public surface reads as one site.
// Data comes from GET /api/public/discover via a raw-fetch hook (never api()).
export function PublicDiscover({
  sort,
  offset,
  onSort,
  onOffset,
}: {
  sort: DiscoverSort
  offset: number
  onSort: (s: DiscoverSort) => void
  onOffset: (o: number) => void
}) {
  const query = usePublicDiscover(sort, offset)
  const spaces = query.data?.spaces ?? []
  const page = Math.floor(offset / DISCOVER_PAGE_SIZE) + 1
  // The directory is open-ended; a full page implies there may be more.
  const hasNext = spaces.length === DISCOVER_PAGE_SIZE
  const hasPrev = offset > 0

  useHeadMeta({
    title: 'Discover — tela',
    description: 'Browse public spaces published on tela.',
    canonicalPath: '/discover',
    ogType: 'website',
  })

  return (
    <div className="flex min-h-dvh flex-col bg-[var(--surface-1)] text-[var(--text-primary)]">
      <PublicTopbar />

      <main className="flex-1">
        <div className="mx-auto w-full max-w-[60rem] px-[var(--space-6)] py-[var(--space-8)]">
          <PublicMasthead
            title="Discover"
            avatarSeed="discover"
            standfirst="Public spaces published on tela — browse the network."
            meta={
              <SortToggle sort={sort} onSort={onSort} disabled={query.isLoading} />
            }
          />

          <div className="mt-[var(--space-7)]">
            {query.isLoading ? (
              <p
                role="status"
                className="text-[length:var(--text-sm)] text-[var(--text-muted)]"
              >
                Loading…
              </p>
            ) : query.error ? (
              <EmptyState
                icon={FrownIcon}
                tone="danger"
                title="Couldn’t load spaces"
                description="Something went wrong fetching the directory. Try again in a moment."
                actions={
                  <Button variant="secondary" onClick={() => void query.refetch()}>
                    Retry
                  </Button>
                }
              />
            ) : spaces.length === 0 ? (
              <EmptyState
                icon={Compass}
                title={offset > 0 ? 'No more spaces' : 'Nothing published yet'}
                description={
                  offset > 0
                    ? 'You’ve reached the end of the directory.'
                    : 'When people publish public spaces, they’ll show up here.'
                }
                actions={
                  offset > 0 ? (
                    <Button
                      variant="secondary"
                      onClick={() => onOffset(Math.max(0, offset - DISCOVER_PAGE_SIZE))}
                    >
                      Back
                    </Button>
                  ) : undefined
                }
              />
            ) : (
              <>
                <ul className="grid list-none grid-cols-1 gap-[var(--space-5)] p-0 sm:grid-cols-2 lg:grid-cols-3">
                  {spaces.map((s) => (
                    <li key={s.id}>
                      <SpaceCard space={s} />
                    </li>
                  ))}
                </ul>

                {(hasPrev || hasNext) && (
                  <nav
                    aria-label="Pagination"
                    className="mt-[var(--space-7)] flex items-center justify-between"
                  >
                    <Button
                      variant="secondary"
                      size="sm"
                      disabled={!hasPrev}
                      onClick={() => onOffset(Math.max(0, offset - DISCOVER_PAGE_SIZE))}
                    >
                      Previous
                    </Button>
                    <span className="text-[length:var(--text-sm)] text-[var(--text-muted)]">
                      Page {page}
                    </span>
                    <Button
                      variant="secondary"
                      size="sm"
                      disabled={!hasNext}
                      onClick={() => onOffset(offset + DISCOVER_PAGE_SIZE)}
                    >
                      Next
                    </Button>
                  </nav>
                )}
              </>
            )}
          </div>
        </div>
      </main>
    </div>
  )
}

// Recent / Popular sort toggle, rendered in the masthead meta row. Mirrors the
// TagBar chip styling on the space index so the public surfaces stay consistent.
function SortToggle({
  sort,
  onSort,
  disabled,
}: {
  sort: DiscoverSort
  onSort: (s: DiscoverSort) => void
  disabled?: boolean
}) {
  const chip =
    'rounded-[var(--radius-sm)] border px-[var(--space-3)] py-[2px] text-[length:var(--text-xs)] transition-colors duration-[var(--duration-fast)] disabled:opacity-50'
  const on = 'border-[var(--accent)] bg-[var(--accent)] text-[var(--text-inverse)]'
  const off =
    'border-[var(--border-subtle)] text-[var(--text-muted)] hover:border-[var(--border-strong)] hover:text-[var(--text-primary)]'
  const opts: { value: DiscoverSort; label: string }[] = [
    { value: 'recent', label: 'Recent' },
    { value: 'popular', label: 'Popular' },
  ]
  return (
    <div role="group" aria-label="Sort" className="flex items-center gap-[var(--space-2)]">
      {opts.map((o) => (
        <button
          key={o.value}
          type="button"
          disabled={disabled}
          aria-pressed={sort === o.value}
          onClick={() => onSort(o.value)}
          className={`${chip} ${sort === o.value ? on : off}`}
        >
          {o.label}
        </button>
      ))}
    </div>
  )
}

// One space in the directory grid. The whole card links into the space's public
// reader; the owner handle is a separate nested link to /u/{handle}. A generated
// monogram avatar gives every space a deterministic identity (no uploaded image).
function SpaceCard({ space }: { space: DiscoverSpace }) {
  const name = space.name || 'Untitled space'
  return (
    <div
      className={[
        'group relative flex h-full flex-col gap-[var(--space-3)] rounded-[var(--radius-lg)] border border-[var(--border-subtle)]',
        'bg-[var(--surface-1)] p-[var(--space-5)] transition-all duration-[var(--duration-fast)]',
        'hover:border-[var(--border-strong)] hover:bg-[var(--surface-2)]',
        'focus-within:border-[var(--border-strong)]',
      ].join(' ')}
    >
      <div className="flex items-center gap-[var(--space-3)]">
        <span
          aria-hidden
          className="grid size-[2.5rem] shrink-0 place-items-center rounded-[var(--radius-md)] font-[family-name:var(--font-sans)] text-[length:var(--text-base)] font-semibold leading-none select-none"
          style={avatarStyle(space.slug || name)}
        >
          {monogram(name)}
        </span>
        <Link
          to="/public/spaces/$spaceId"
          params={{ spaceId: space.id }}
          search={{ tag: undefined }}
          className={[
            'min-w-0 font-[family-name:var(--font-sans)] text-[length:var(--text-lg)] font-semibold leading-[var(--leading-tight)]',
            'tracking-[-0.01em] text-[var(--text-primary)] no-underline transition-colors duration-[var(--duration-fast)]',
            // Stretched link: the whole card is clickable, owner link sits above.
            'after:absolute after:inset-0 hover:text-[var(--accent)]',
            'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent)]',
          ].join(' ')}
        >
          <span className="line-clamp-2">{name}</span>
        </Link>
      </div>

      {space.description ? (
        <p className="m-0 line-clamp-3 text-[length:var(--text-sm)] leading-[var(--leading-normal)] text-[var(--text-muted)]">
          {space.description}
        </p>
      ) : null}

      <div className="mt-auto flex flex-wrap items-center gap-x-[var(--space-2)] gap-y-[var(--space-1)] pt-[var(--space-2)] text-[length:var(--text-xs)] text-[var(--text-muted)]">
        {space.owner_handle ? (
          <>
            <Link
              to="/u/$username"
              params={{ username: space.owner_handle }}
              // relative z-index lifts this above the card's stretched ::after.
              className="relative z-10 font-medium text-[var(--text-muted)] no-underline hover:text-[var(--accent)]"
            >
              @{space.owner_handle}
            </Link>
            <MetaDot />
          </>
        ) : null}
        <span className="inline-flex items-center gap-[var(--space-1)]">
          <FileText size="0.9em" aria-hidden />
          {space.page_count} {space.page_count === 1 ? 'page' : 'pages'}
        </span>
        {space.updated_at ? (
          <>
            <MetaDot />
            <span>Updated {relativeTimeFromSqlite(space.updated_at)}</span>
          </>
        ) : null}
      </div>
    </div>
  )
}
