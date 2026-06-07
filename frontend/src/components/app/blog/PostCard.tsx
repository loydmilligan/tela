import { Link } from '@tanstack/react-router'
import { coverBackground, monogram } from '../../../lib/blog'
import { pageSlug } from '../../../lib/slug'
import { postDateFromSqlite } from '../../../lib/relativeTime'
import type { BlogCardMeta } from '../../../lib/queries/public'

export interface BlogPost extends BlogCardMeta {
  id: number
  title: string
  created_at: string
  updated_at: string
}

// One post on a public index. `featured` is the lead treatment (large, cover +
// text side by side) used for the newest post; the default is a compact card
// for the grid below. The whole card is one link into the no-login reader.
export function PostCard({
  spaceId,
  post,
  featured = false,
}: {
  spaceId: number
  post: BlogPost
  featured?: boolean
}) {
  const title = post.title || 'Untitled'
  return (
    <Link
      to="/public/spaces/$spaceId/pages/$pageId/{-$slug}"
      params={{ spaceId, pageId: post.id, slug: pageSlug(title) || undefined }}
      className={[
        'group relative flex overflow-hidden rounded-[var(--radius-lg)] border border-[var(--border-subtle)]',
        'bg-[var(--surface-1)] no-underline transition-all duration-[var(--duration-fast)]',
        'hover:border-[var(--border-strong)] hover:bg-[var(--surface-2)] focus-visible:outline-none',
        'focus-visible:ring-2 focus-visible:ring-[var(--accent)]',
        featured ? 'flex-col md:flex-row' : 'flex-col',
      ].join(' ')}
    >
      <Cover title={title} cover={post.cover} featured={featured} />
      <div
        className={[
          'flex min-w-0 flex-1 flex-col gap-[var(--space-2)]',
          featured ? 'p-[var(--space-6)]' : 'p-[var(--space-4)]',
        ].join(' ')}
      >
        {post.tags && post.tags.length > 0 ? (
          <div className="flex flex-wrap gap-[var(--space-1)]">
            {post.tags.slice(0, 3).map((t) => (
              <span
                key={t}
                className="rounded-[var(--radius-xs)] bg-[var(--surface-2)] px-[var(--space-2)] py-[2px] text-[length:var(--text-xs)] text-[var(--text-muted)] group-hover:bg-[var(--surface-1)]"
              >
                {t}
              </span>
            ))}
          </div>
        ) : null}

        <h3
          className={[
            'm-0 font-[family-name:var(--font-sans)] font-semibold leading-[var(--leading-tight)] tracking-[-0.01em]',
            'text-[var(--text-primary)] transition-colors duration-[var(--duration-fast)] group-hover:text-[var(--accent)]',
            featured ? 'text-[length:var(--text-2xl)]' : 'text-[length:var(--text-lg)]',
          ].join(' ')}
        >
          {title}
        </h3>

        {post.excerpt ? (
          <p
            className={[
              'm-0 text-[length:var(--text-sm)] leading-[var(--leading-normal)] text-[var(--text-muted)]',
              featured ? 'line-clamp-3' : 'line-clamp-2',
            ].join(' ')}
          >
            {post.excerpt}
          </p>
        ) : null}

        <div className="mt-auto flex items-center gap-[var(--space-2)] pt-[var(--space-2)] text-[length:var(--text-xs)] text-[var(--text-muted)]">
          <span>{postDateFromSqlite(post.updated_at)}</span>
          <span aria-hidden className="opacity-50">
            ·
          </span>
          <span>{post.reading_minutes} min read</span>
        </div>
      </div>
    </Link>
  )
}

// The card's lead image — the author's `cover` when set, otherwise a soft
// title-seeded gradient so every card has a deliberate visual, never a blank.
function Cover({
  title,
  cover,
  featured,
}: {
  title: string
  cover?: string
  featured: boolean
}) {
  const box = featured
    ? 'md:w-[44%] md:self-stretch aspect-[16/9] md:aspect-auto md:min-h-[14rem]'
    : 'aspect-[16/9]'
  if (cover) {
    return (
      <div className={`relative shrink-0 overflow-hidden bg-[var(--surface-2)] ${box}`}>
        <img
          src={cover}
          alt=""
          loading="lazy"
          className="size-full object-cover transition-transform duration-[var(--duration-base)] group-hover:scale-[1.03]"
        />
      </div>
    )
  }
  return (
    <div
      aria-hidden
      className={`relative shrink-0 overflow-hidden ${box}`}
      style={{ background: coverBackground(title) }}
    >
      {/* Large faded monogram bleeding off the corner. Fixed size (not cqh — the
          featured cover's height comes from flex-stretch, where container units
          collapse to 0). White-on-gradient: part of the generated art, theme-
          independent like the grid lines above. */}
      <span
        className="pointer-events-none absolute right-[-0.06em] bottom-[-0.18em] text-[9.5rem] font-[family-name:var(--font-sans)] font-extrabold leading-none tracking-[-0.04em] select-none"
        style={{ color: 'rgba(255,255,255,0.22)' }}
      >
        {monogram(title)}
      </span>
    </div>
  )
}
