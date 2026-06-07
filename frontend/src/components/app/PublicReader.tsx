import { useCallback, useMemo, useState } from 'react'
import { Link, useNavigate } from '@tanstack/react-router'
import { applyPdfThemeParam } from '../../lib/theme'
import { buildWikilinkResolveIndex, pageSlug } from '../../lib/slug'
import {
  usePublicSpaceTree,
  type PublicPageNode,
  type PublicSpacePayload,
} from '../../lib/queries/public'
import { Button } from '../ui/button'
import { DownloadPdfButton } from './DownloadPdfButton'
import { ReaderShell } from './ReaderShell'

interface PublicReaderViewProps {
  space: PublicSpacePayload
  pageId: number
  pageTitle: string
  pageBody: string
  pageProps?: Record<string, unknown>
  updatedAt: string
}

// A no-login reader for a page in a PUBLIC space — the blog surface. Same
// chrome-free reading-mode shell as authenticated /read and the share reader,
// with public-space wiring: the whole space is in scope (every page links
// freely), wikilinks hop within the public route, and a multi-page space gets a
// slim nav rail. No editor, no comments, no collab — purely read-only.
export function PublicReaderView({
  space,
  pageId,
  pageTitle,
  pageBody,
  updatedAt,
}: PublicReaderViewProps) {
  const navigate = useNavigate()
  // Apply ?theme= once, pre-paint, for a themed PDF export; no-op for humans.
  useState(() => applyPdfThemeParam())

  const tree = usePublicSpaceTree(space.id)
  const pages = useMemo<PublicPageNode[]>(() => tree.data?.pages ?? [], [tree.data])

  // The whole public space is in scope — every page is freely linkable.
  const inScopePageIds = useMemo(() => {
    const set = new Set<number>()
    for (const p of pages) set.add(p.id)
    set.add(pageId)
    return set
  }, [pages, pageId])

  const wikilinkResolveIndex = useMemo<Map<string, number> | null>(
    () => (tree.data ? buildWikilinkResolveIndex(pages) : null),
    [tree.data, pages],
  )

  const onNavigateWikilink = useCallback(
    (targetPageId: number) => {
      if (!inScopePageIds.has(targetPageId)) return
      void navigate({
        to: '/public/spaces/$spaceId/pages/$pageId/{-$slug}',
        params: { spaceId: space.id, pageId: targetPageId, slug: undefined },
      })
    },
    [navigate, space.id, inScopePageIds],
  )

  const showSidebar = pages.length > 1

  return (
    <ReaderShell
      pageId={pageId}
      title={pageTitle}
      body={pageBody}
      updatedAt={updatedAt}
      wikilinkMode="share"
      aliveWikilinkIds={inScopePageIds}
      wikilinkResolveIndex={wikilinkResolveIndex}
      onNavigateWikilink={onNavigateWikilink}
      sidebar={
        showSidebar ? (
          <PublicSpaceNav
            spaceId={space.id}
            spaceName={space.name}
            pages={pages}
            activePageId={pageId}
          />
        ) : undefined
      }
      topbarLeading={
        <span className="flex items-center gap-[var(--space-2)] min-w-0">
          <a
            href="/"
            aria-label="tela home"
            className="inline-block rounded-[var(--radius-xs)] font-[family-name:var(--font-sans)] text-[length:var(--text-base)] font-medium text-[var(--text-primary)] no-underline transition-opacity duration-[var(--duration-fast)] hover:opacity-70 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent)]"
          >
            tela
          </a>
          <span aria-hidden className="text-[var(--text-muted)]">
            /
          </span>
          {/* Back to the space's front page (its blog index). */}
          <Link
            to="/public/spaces/$spaceId"
            params={{ spaceId: space.id }}
            className="truncate rounded-[var(--radius-xs)] font-[family-name:var(--font-sans)] text-[length:var(--text-sm)] text-[var(--text-muted)] no-underline transition-colors duration-[var(--duration-fast)] hover:text-[var(--text-primary)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent)]"
          >
            {space.name}
          </Link>
        </span>
      }
      sourceLabel={space.name}
      topbarTrailing={
        <>
          <DownloadPdfButton url={`${window.location.pathname}.pdf`} themed />
          <Button asChild variant="ghost" size="sm">
            <a href="/login">Sign in</a>
          </Button>
        </>
      }
    />
  )
}

// PublicSpaceNav — a slim left rail listing the public space's pages, ordered by
// position. Flat (no nesting) for v1; a curated space front page is a separate
// roadmap item.
function PublicSpaceNav({
  spaceId,
  spaceName,
  pages,
  activePageId,
}: {
  spaceId: number
  spaceName: string
  pages: PublicPageNode[]
  activePageId: number
}) {
  const ordered = useMemo(
    () => [...pages].sort((a, b) => a.position - b.position || a.id - b.id),
    [pages],
  )
  return (
    <nav
      aria-label={`${spaceName} pages`}
      className="flex flex-col gap-[var(--space-1)] p-[var(--space-4)]"
    >
      <p className="m-0 mb-[var(--space-2)] text-[length:var(--text-xs)] font-semibold uppercase tracking-[0.08em] text-[var(--text-muted)]">
        {spaceName}
      </p>
      {ordered.map((p) => (
        <Link
          key={p.id}
          to="/public/spaces/$spaceId/pages/$pageId/{-$slug}"
          params={{ spaceId, pageId: p.id, slug: pageSlug(p.title) || undefined }}
          className="block truncate rounded-[var(--radius-xs)] px-[var(--space-2)] py-[var(--space-1)] text-[length:var(--text-sm)] no-underline data-[active=true]:text-[var(--text-primary)] data-[active=true]:font-medium text-[var(--text-muted)] hover:text-[var(--text-primary)]"
          data-active={p.id === activePageId}
        >
          {p.title || 'Untitled'}
        </Link>
      ))}
    </nav>
  )
}
