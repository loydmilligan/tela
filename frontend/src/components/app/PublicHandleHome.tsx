import { Link } from '@tanstack/react-router'
import {
  type ByHandleResponse,
  type ByHandleSpace,
} from '../../lib/queries/public'
import { useHeadMeta } from '../../lib/useHeadMeta'
import { PublicPageShell } from './blog/PublicPageShell'
import { PublicMasthead, MetaDot } from './blog/PublicMasthead'
import { SpaceCard } from './blog/SpaceCard'

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
    <PublicPageShell>
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
    </PublicPageShell>
  )
}

// One public space on the handle home → links to /{handle}/{space-slug} (the
// unified handle route); no owner byline since we're already under the handle.
function HandleSpaceCard({
  handle,
  space,
}: {
  handle: string
  space: ByHandleSpace
}) {
  return (
    <SpaceCard
      name={space.name}
      seed={space.slug || space.name}
      description={space.description}
      pageCount={space.page_count}
      updatedAt={space.updated_at}
      renderTitleLink={({ className, children }) => (
        <Link
          to="/$handle/$spaceSlug"
          params={{ handle, spaceSlug: space.slug }}
          className={className}
        >
          {children}
        </Link>
      )}
    />
  )
}
