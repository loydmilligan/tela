import { useMemo } from 'react'
import { Link } from '@tanstack/react-router'
import { relativeTimeFromSqlite } from '../../lib/relativeTime'
import { pageSlug } from '../../lib/slug'
import {
  type PublicPageNode,
  type PublicSpacePayload,
} from '../../lib/queries/public'
import { Button } from '../ui/button'
import { ThemeSwitcher } from '../ThemeSwitcher'

interface PublicSpaceIndexProps {
  space: PublicSpacePayload
  pages: PublicPageNode[]
}

// The curated front page of a public space — a blog-style index. Top-level pages
// are the "posts" (nested pages are sub-sections, reachable by navigating in),
// ordered by the author's arrangement (position). Each entry links to the
// no-login reader. Chrome mirrors the reader's topbar so the surface reads as
// one site.
export function PublicSpaceIndex({ space, pages }: PublicSpaceIndexProps) {
  const posts = useMemo(
    () =>
      pages
        .filter((p) => p.parent_id == null)
        .sort((a, b) => a.position - b.position || a.id - b.id),
    [pages],
  )

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
            {space.name}
          </h1>

          {posts.length === 0 ? (
            <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)]">
              Nothing published here yet.
            </p>
          ) : (
            <ul className="m-0 p-0 list-none flex flex-col">
              {posts.map((p) => (
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
                    className="group flex flex-col gap-[var(--space-1)] py-[var(--space-4)] no-underline"
                  >
                    <span className="text-[length:var(--text-lg)] font-medium text-[var(--text-primary)] transition-colors duration-[var(--duration-fast)] group-hover:text-[var(--accent)]">
                      {p.title || 'Untitled'}
                    </span>
                    <span className="text-[length:var(--text-xs)] text-[var(--text-muted)]">
                      {relativeTimeFromSqlite(p.updated_at)}
                    </span>
                  </Link>
                </li>
              ))}
            </ul>
          )}
        </div>
      </main>
    </div>
  )
}
