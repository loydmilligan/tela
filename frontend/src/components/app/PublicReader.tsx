import { useCallback, useMemo, useState } from 'react'
import { Link, useNavigate } from '@tanstack/react-router'
import { applyPdfThemeParam } from '../../lib/theme'
import { buildWikilinkResolveIndex, pageSlug } from '../../lib/slug'
import { bodyExcerpt } from '../../lib/search/body-excerpt'
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
  createdAt: string
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
  pageProps,
  createdAt,
  updatedAt,
}: PublicReaderViewProps) {
  const navigate = useNavigate()

  // SEO/social head for this public article: an author summary in frontmatter
  // wins, else the body lead. Canonical is the current (pretty) reader URL.
  const metaDescription = useMemo(() => {
    const fromProps = ['summary', 'excerpt', 'description']
      .map((k) => pageProps?.[k])
      .find((v): v is string => typeof v === 'string' && v.trim() !== '')
    return (fromProps ?? bodyExcerpt(pageBody, '', 90)).trim()
  }, [pageProps, pageBody])
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

  // Hero cover only when the post actually sets one (props.cover) — no gradient
  // fallback in the article, that's an index-card affordance.
  const coverImage =
    typeof pageProps?.cover === 'string' && pageProps.cover.trim()
      ? pageProps.cover.trim()
      : typeof pageProps?.image === 'string' && pageProps.image.trim()
        ? pageProps.image.trim()
        : undefined

  // Byline → the space owner's author home.
  const byline = space.owner_handle ? (
    <>
      by{' '}
      <Link
        to="/u/$username"
        params={{ username: space.owner_handle }}
        className="reader-meta-link"
      >
        @{space.owner_handle}
      </Link>
    </>
  ) : undefined

  // Previous/next among the space's top-level posts (the "posts"), in published
  // order. Only shown when the current page is itself a top-level post.
  const postNav = useMemo(() => {
    const posts = pages
      .filter((p) => p.parent_id == null)
      .sort((a, b) => b.created_at.localeCompare(a.created_at))
    const i = posts.findIndex((p) => p.id === pageId)
    if (i === -1) return null
    return { newer: posts[i - 1], older: posts[i + 1] } // newest-first order
  }, [pages, pageId])

  const articleFooter =
    postNav && (postNav.newer || postNav.older) ? (
      <nav className="reader-postnav" aria-label="More posts">
        {postNav.older ? (
          <PostNavLink spaceId={space.id} post={postNav.older} dir="older" />
        ) : (
          <span />
        )}
        {postNav.newer ? (
          <PostNavLink spaceId={space.id} post={postNav.newer} dir="newer" />
        ) : (
          <span />
        )}
      </nav>
    ) : undefined

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
      coverImage={coverImage}
      byline={byline}
      publishedAt={createdAt}
      articleFooter={articleFooter}
      headMeta={{
        description: metaDescription,
        canonicalPath: window.location.pathname,
        image: `/p/${pageId}/og.png`,
        feedHref: `/api/public/spaces/${space.id}/feed.xml`,
      }}
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

// One end of the previous/next post navigation under an article.
function PostNavLink({
  spaceId,
  post,
  dir,
}: {
  spaceId: number
  post: PublicPageNode
  dir: 'older' | 'newer'
}) {
  return (
    <Link
      to="/public/spaces/$spaceId/pages/$pageId/{-$slug}"
      params={{ spaceId, pageId: post.id, slug: pageSlug(post.title) || undefined }}
      className={`reader-postnav-link reader-postnav-${dir}`}
    >
      <span className="reader-postnav-dir">
        {dir === 'older' ? '← Older' : 'Newer →'}
      </span>
      <span className="reader-postnav-title">{post.title || 'Untitled'}</span>
    </Link>
  )
}

// PublicSpaceNav — the structural left rail in the public reader. Reflects the
// space's actual page hierarchy (nested children indented under their parent,
// siblings in author position order) so it's clear what belongs to what. Width
// is bounded in reader.css; long titles truncate rather than ballooning the rail.
interface NavNode extends PublicPageNode {
  children: NavNode[]
}

// Fold the flat page list into a tree. A child whose parent isn't in the public
// set (private/unpublished) surfaces as a root rather than vanishing.
function buildNavTree(pages: PublicPageNode[]): NavNode[] {
  const byId = new Map<number, NavNode>()
  for (const p of pages) byId.set(p.id, { ...p, children: [] })
  const roots: NavNode[] = []
  for (const node of byId.values()) {
    const parent = node.parent_id != null ? byId.get(node.parent_id) : undefined
    if (parent) parent.children.push(node)
    else roots.push(node)
  }
  const sortRec = (nodes: NavNode[]) => {
    nodes.sort((a, b) => a.position - b.position || a.id - b.id)
    for (const n of nodes) sortRec(n.children)
  }
  sortRec(roots)
  return roots
}

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
  const tree = useMemo(() => buildNavTree(pages), [pages])
  return (
    <nav aria-label={`${spaceName} pages`} className="reader-spacenav">
      <p className="reader-spacenav-title">{spaceName}</p>
      <PublicSpaceNavList
        spaceId={spaceId}
        nodes={tree}
        activePageId={activePageId}
      />
    </nav>
  )
}

function PublicSpaceNavList({
  spaceId,
  nodes,
  activePageId,
  sub = false,
}: {
  spaceId: number
  nodes: NavNode[]
  activePageId: number
  sub?: boolean
}) {
  return (
    <ul className={sub ? 'reader-spacenav-sublist' : 'reader-spacenav-list'}>
      {nodes.map((node) => (
        <li key={node.id}>
          <Link
            to="/public/spaces/$spaceId/pages/$pageId/{-$slug}"
            params={{
              spaceId,
              pageId: node.id,
              slug: pageSlug(node.title) || undefined,
            }}
            className="reader-spacenav-link"
            data-active={node.id === activePageId}
            aria-current={node.id === activePageId ? 'page' : undefined}
            title={node.title || 'Untitled'}
          >
            {node.title || 'Untitled'}
          </Link>
          {node.children.length > 0 ? (
            <PublicSpaceNavList
              spaceId={spaceId}
              nodes={node.children}
              activePageId={activePageId}
              sub
            />
          ) : null}
        </li>
      ))}
    </ul>
  )
}
