import { Link } from '@tanstack/react-router'
import { relativeTimeFromSqlite } from '../../lib/relativeTime'
import { pageSlug } from '../../lib/slug'
import { type PublicUserResponse } from '../../lib/queries/public'
import { Button } from '../ui/button'
import { ThemeSwitcher } from '../ThemeSwitcher'

// The /u/{handle} home page — a front door for one person. Lists their public
// spaces, each linking to its front page, with the space's top-level posts
// beneath. Chrome mirrors the public reader/index so the surface reads as one
// site. No-login; only public content is ever returned by the API.
export function PublicUserHome({ data }: { data: PublicUserResponse }) {
  const { user, spaces } = data
  return (
    <div className="min-h-dvh flex flex-col bg-[var(--surface-1)] text-[var(--text-primary)]">
      <header className="flex items-center justify-between px-[var(--space-5)] py-[var(--space-3)] border-b border-[var(--border-subtle)] shrink-0">
        <a
          href="/"
          aria-label="tela home"
          className="inline-block rounded-[var(--radius-xs)] font-[family-name:var(--font-sans)] text-[length:var(--text-base)] font-medium text-[var(--text-primary)] no-underline transition-opacity duration-[var(--duration-fast)] hover:opacity-70 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent)]"
        >
          tela
        </a>
        <div className="flex items-center gap-[var(--space-2)]">
          <ThemeSwitcher />
          <Button asChild variant="ghost" size="sm">
            <a href="/login">Sign in</a>
          </Button>
        </div>
      </header>

      <main className="flex-1 overflow-y-auto">
        <div className="mx-auto w-full max-w-[42rem] px-[var(--space-6)] py-[var(--space-8)]">
          <h1 className="m-0 mb-[var(--space-7)] font-[family-name:var(--font-sans)] text-[length:var(--text-3xl)] font-semibold leading-[var(--leading-tight)] tracking-[-0.015em]">
            {user.username}
          </h1>

          <div className="flex flex-col gap-[var(--space-8)]">
            {spaces.map((space) => (
              <section key={space.id} className="flex flex-col">
                <Link
                  to="/public/spaces/$spaceId"
                  params={{ spaceId: space.id }}
                  className="group mb-[var(--space-3)] inline-flex items-baseline gap-[var(--space-2)] no-underline"
                >
                  <span className="text-[length:var(--text-xl)] font-semibold text-[var(--text-primary)] transition-colors duration-[var(--duration-fast)] group-hover:text-[var(--accent)]">
                    {space.name}
                  </span>
                  <span className="text-[length:var(--text-xs)] text-[var(--text-muted)] opacity-0 transition-opacity duration-[var(--duration-fast)] group-hover:opacity-100">
                    View all →
                  </span>
                </Link>

                {space.pages.length === 0 ? (
                  <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)]">
                    No posts yet.
                  </p>
                ) : (
                  <ul className="m-0 p-0 list-none flex flex-col">
                    {space.pages.map((p) => (
                      <li
                        key={p.id}
                        className="border-b border-[var(--border-subtle)] last:border-b-0"
                      >
                        <Link
                          to="/public/spaces/$spaceId/pages/$pageId/{-$slug}"
                          params={{
                            spaceId: space.id,
                            pageId: p.id,
                            slug: pageSlug(p.title) || undefined,
                          }}
                          className="group flex items-baseline justify-between gap-[var(--space-4)] py-[var(--space-3)] no-underline"
                        >
                          <span className="min-w-0 truncate text-[length:var(--text-base)] text-[var(--text-primary)] transition-colors duration-[var(--duration-fast)] group-hover:text-[var(--accent)]">
                            {p.title || 'Untitled'}
                          </span>
                          <span className="shrink-0 text-[length:var(--text-xs)] text-[var(--text-muted)]">
                            {relativeTimeFromSqlite(p.updated_at)}
                          </span>
                        </Link>
                      </li>
                    ))}
                  </ul>
                )}
              </section>
            ))}
          </div>
        </div>
      </main>
    </div>
  )
}
