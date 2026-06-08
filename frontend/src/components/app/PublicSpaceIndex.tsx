import { useMemo } from 'react'
import { Link, useSearch } from '@tanstack/react-router'
import {
  type PublicPageNode,
  type PublicSpacePayload,
} from '../../lib/queries/public'
import { useHeadMeta } from '../../lib/useHeadMeta'
import { PublicTopbar } from './blog/PublicTopbar'
import { PublicMasthead, MetaDot } from './blog/PublicMasthead'
import { PostCard } from './blog/PostCard'

interface PublicSpaceIndexProps {
  space: PublicSpacePayload
  pages: PublicPageNode[]
}

// The curated front page of a public space — a blog-style index. Top-level pages
// are the "posts" (nested pages are sub-sections, reached by navigating in),
// newest published first. The newest gets a featured lead card; the rest fall
// into a grid. A tag bar filters the list (?tag=, shareable). Chrome mirrors the
// reader/author surfaces so it all reads as one site.
export function PublicSpaceIndex({ space, pages }: PublicSpaceIndexProps) {
  // strict: false — this index renders under BOTH /public/spaces/$spaceId and
  // the unified /$handle/$spaceSlug route, so it can't bind to one route's
  // search schema. The tag filter is a plain optional string either way.
  const search = useSearch({ strict: false }) as { tag?: string }
  const activeTag =
    typeof search.tag === 'string' && search.tag.trim() ? search.tag.trim() : undefined

  const posts = useMemo(
    () =>
      pages
        .filter((p) => p.parent_id == null)
        // Newest published first — string UTC timestamps sort lexicographically.
        .sort((a, b) => b.created_at.localeCompare(a.created_at)),
    [pages],
  )

  const allTags = useMemo(() => {
    const set = new Set<string>()
    for (const p of posts) for (const t of p.tags ?? []) set.add(t)
    return [...set].sort((a, b) => a.localeCompare(b))
  }, [posts])

  const shown = useMemo(
    () => (activeTag ? posts.filter((p) => p.tags?.includes(activeTag)) : posts),
    [posts, activeTag],
  )

  const feedUrl = `/api/public/spaces/${space.id}/feed.xml`

  useHeadMeta({
    title: `${space.name}${activeTag ? ` · ${activeTag}` : ''} — tela`,
    description: space.description || `Posts from ${space.name}.`,
    canonicalPath: `/public/spaces/${space.id}`,
    image: `/api/public/spaces/${space.id}/og.png`,
    ogType: 'website',
    feedHref: feedUrl,
  })

  // Featured lead only on the unfiltered view; a filtered list is a flat grid.
  const [featured, ...rest] = shown
  const showFeatured = !activeTag && featured

  return (
    <div className="flex min-h-dvh flex-col bg-[var(--surface-1)] text-[var(--text-primary)]">
      <PublicTopbar />

      <main className="flex-1">
        <div className="mx-auto w-full max-w-[60rem] px-[var(--space-6)] py-[var(--space-8)]">
          <PublicMasthead
            title={space.name}
            avatarSeed={space.slug || space.name}
            standfirst={space.description || undefined}
            meta={
              <>
                {space.owner_handle ? (
                  <>
                    <span>
                      by{' '}
                      <Link
                        to="/u/$username"
                        params={{ username: space.owner_handle }}
                        className="font-medium text-[var(--text-primary)] no-underline hover:text-[var(--accent)]"
                      >
                        @{space.owner_handle}
                      </Link>
                    </span>
                    <MetaDot />
                  </>
                ) : null}
                <span>
                  {posts.length} {posts.length === 1 ? 'post' : 'posts'}
                </span>
                <MetaDot />
                <a
                  href={feedUrl}
                  className="text-[var(--text-muted)] no-underline hover:text-[var(--accent)]"
                >
                  RSS
                </a>
              </>
            }
          />

          {allTags.length > 0 ? (
            <TagBar spaceId={space.id} tags={allTags} active={activeTag} />
          ) : null}

          {posts.length === 0 ? (
            <p className="mt-[var(--space-8)] text-[length:var(--text-sm)] text-[var(--text-muted)]">
              Nothing published here yet.
            </p>
          ) : shown.length === 0 ? (
            <p className="mt-[var(--space-6)] text-[length:var(--text-sm)] text-[var(--text-muted)]">
              No posts tagged “{activeTag}”.
            </p>
          ) : (
            <div className="mt-[var(--space-6)] flex flex-col gap-[var(--space-5)]">
              {showFeatured ? (
                <PostCard
                  spaceId={space.id}
                  post={featured}
                  featured
                  headingLevel={2}
                />
              ) : null}
              {(() => {
                const grid = showFeatured ? rest : shown
                if (grid.length === 0) return null
                // A lone trailing card spans full width rather than sitting
                // half-empty in a 2-col grid.
                if (showFeatured && grid.length === 1)
                  return (
                    <PostCard spaceId={space.id} post={grid[0]} headingLevel={2} />
                  )
                return (
                  <div className="grid grid-cols-1 gap-[var(--space-5)] sm:grid-cols-2">
                    {grid.map((p) => (
                      <PostCard
                        key={p.id}
                        spaceId={space.id}
                        post={p}
                        headingLevel={2}
                      />
                    ))}
                  </div>
                )
              })()}
            </div>
          )}
        </div>
      </main>
    </div>
  )
}

// A row of tag filter chips. "All" clears the filter; each tag links to
// ?tag=<t>. The active chip is highlighted. Links keep the filter shareable and
// back-button friendly.
function TagBar({
  spaceId,
  tags,
  active,
}: {
  spaceId: number
  tags: string[]
  active?: string
}) {
  const chip =
    'rounded-[var(--radius-sm)] border px-[var(--space-3)] py-[2px] text-[length:var(--text-xs)] no-underline transition-colors duration-[var(--duration-fast)]'
  const on = 'border-[var(--accent)] bg-[var(--accent)] text-[var(--text-inverse)]'
  const off =
    'border-[var(--border-subtle)] text-[var(--text-muted)] hover:border-[var(--border-strong)] hover:text-[var(--text-primary)]'
  return (
    <div className="mt-[var(--space-6)] flex flex-wrap items-center gap-[var(--space-2)]">
      <Link
        to="/public/spaces/$spaceId"
        params={{ spaceId }}
        search={{ tag: undefined }}
        className={`${chip} ${!active ? on : off}`}
      >
        All
      </Link>
      {tags.map((t) => (
        <Link
          key={t}
          to="/public/spaces/$spaceId"
          params={{ spaceId }}
          search={{ tag: t }}
          className={`${chip} ${active === t ? on : off}`}
        >
          {t}
        </Link>
      ))}
    </div>
  )
}
