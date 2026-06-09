import { useMemo } from 'react'
import { Link } from '@tanstack/react-router'
import {
  type PublicUserResponse,
  type PublicUserSpace,
} from '../../lib/queries/public'
import { useHeadMeta } from '../../lib/useHeadMeta'
import { sortByNewest } from '../../lib/blog'
import { PublicPageShell } from './blog/PublicPageShell'
import { PublicMasthead, MetaDot } from './blog/PublicMasthead'
import { PostCard } from './blog/PostCard'

// The /u/{handle} home page — a front door for one author. A masthead with their
// bio, then a section per public space: the space header links to its front
// page, with its newest posts as cards beneath. No-login; only public content is
// ever returned by the API.
export function PublicUserHome({ data }: { data: PublicUserResponse }) {
  const { user, spaces } = data
  const totalPosts = useMemo(
    () => spaces.reduce((n, s) => n + s.pages.length, 0),
    [spaces],
  )

  useHeadMeta({
    title: `${user.username} — tela`,
    description: user.bio || `${user.username} on tela.`,
    canonicalPath: `/u/${user.username}`,
    image: `/api/public/users/${encodeURIComponent(user.username)}/og.png`,
    ogType: 'profile',
  })

  return (
    <PublicPageShell>
      <PublicMasthead
        title={user.username}
        avatarSeed={user.username}
        standfirst={user.bio || undefined}
        meta={
          <>
            <span>
              {totalPosts} {totalPosts === 1 ? 'post' : 'posts'}
            </span>
            <MetaDot />
            <span>
              {spaces.length} {spaces.length === 1 ? 'space' : 'spaces'}
            </span>
          </>
        }
      />

      <div className="mt-[var(--space-8)] flex flex-col gap-[var(--space-8)]">
        {spaces.map((space) => (
          <SpaceSection key={space.id} space={space} />
        ))}
      </div>
    </PublicPageShell>
  )
}

function SpaceSection({ space }: { space: PublicUserSpace }) {
  const posts = useMemo(() => sortByNewest(space.pages), [space.pages])

  return (
    <section className="flex flex-col gap-[var(--space-4)]">
      <div className="flex items-baseline justify-between gap-[var(--space-3)] border-b border-[var(--border-subtle)] pb-[var(--space-2)]">
        <Link
          to="/public/spaces/$spaceId"
          params={{ spaceId: space.id }}
          className="group inline-flex items-baseline gap-[var(--space-2)] no-underline"
        >
          <h2 className="m-0 text-[length:var(--text-xl)] font-semibold tracking-[-0.01em] text-[var(--text-primary)] transition-colors duration-[var(--duration-fast)] group-hover:text-[var(--accent)]">
            {space.name}
          </h2>
          <span className="text-[length:var(--text-sm)] text-[var(--text-muted)] transition-colors group-hover:text-[var(--accent)]">
            View all →
          </span>
        </Link>
        <span className="shrink-0 text-[length:var(--text-xs)] text-[var(--text-muted)]">
          {space.pages.length} {space.pages.length === 1 ? 'post' : 'posts'}
        </span>
      </div>

      {space.description ? (
        <p className="m-0 -mt-[var(--space-2)] max-w-[42rem] text-[length:var(--text-sm)] text-[var(--text-muted)]">
          {space.description}
        </p>
      ) : null}

      {posts.length === 0 ? (
        <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)]">
          No posts yet.
        </p>
      ) : (
        <div className="grid grid-cols-1 gap-[var(--space-5)] sm:grid-cols-2">
          {posts.map((p) => (
            <PostCard key={p.id} spaceId={space.id} post={p} />
          ))}
        </div>
      )}
    </section>
  )
}
