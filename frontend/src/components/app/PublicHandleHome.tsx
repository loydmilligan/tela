import { Link } from '@tanstack/react-router'
import { FileText } from 'lucide-react'
import {
  type ByHandleResponse,
  type ByHandleSpace,
} from '../../lib/queries/public'
import { useHeadMeta } from '../../lib/useHeadMeta'
import { avatarStyle, monogram } from '../../lib/blog'
import { relativeTimeFromSqlite } from '../../lib/relativeTime'
import { PublicTopbar } from './blog/PublicTopbar'
import { PublicMasthead, MetaDot } from './blog/PublicMasthead'

// The GitHub-style handle home at /{handle} — works identically for a user or
// an org. A masthead (name + @handle) over a grid of the handle's public
// spaces; each card links into that space at /{handle}/{space-slug}. Same
// no-login chrome as /discover, /u/{handle} and the space front page, so the
// whole public surface reads as one site.
export function PublicHandleHome({ data }: { data: ByHandleResponse }) {
  const { handle, name, spaces, kind } = data

  useHeadMeta({
    title: `${name} — tela`,
    description: `${name} (@${handle}) on tela.`,
    canonicalPath: `/${handle}`,
    ogType: kind === 'org' ? 'website' : 'profile',
  })

  return (
    <div className="flex min-h-dvh flex-col bg-[var(--surface-1)] text-[var(--text-primary)]">
      <PublicTopbar />

      <main className="flex-1">
        <div className="mx-auto w-full max-w-[60rem] px-[var(--space-6)] py-[var(--space-8)]">
          <PublicMasthead
            title={name}
            avatarSeed={handle}
            meta={
              <>
                <span>@{handle}</span>
                <MetaDot />
                <span>
                  {spaces.length} {spaces.length === 1 ? 'space' : 'spaces'}
                </span>
              </>
            }
          />

          <div className="mt-[var(--space-7)]">
            {spaces.length === 0 ? (
              <p className="text-[length:var(--text-sm)] text-[var(--text-muted)]">
                Nothing published yet.
              </p>
            ) : (
              <ul className="grid list-none grid-cols-1 gap-[var(--space-5)] p-0 sm:grid-cols-2 lg:grid-cols-3">
                {spaces.map((s) => (
                  <li key={s.id}>
                    <HandleSpaceCard handle={handle} space={s} />
                  </li>
                ))}
              </ul>
            )}
          </div>
        </div>
      </main>
    </div>
  )
}

// One public space on the handle home. Mirrors PublicDiscover's SpaceCard, but
// the whole card links to /{handle}/{space-slug} (the unified handle route).
function HandleSpaceCard({
  handle,
  space,
}: {
  handle: string
  space: ByHandleSpace
}) {
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
          to="/$handle/$spaceSlug"
          params={{ handle, spaceSlug: space.slug }}
          className={[
            'min-w-0 font-[family-name:var(--font-sans)] text-[length:var(--text-lg)] font-semibold leading-[var(--leading-tight)]',
            'tracking-[-0.01em] text-[var(--text-primary)] no-underline transition-colors duration-[var(--duration-fast)]',
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
