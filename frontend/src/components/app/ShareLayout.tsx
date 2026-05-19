import { useMemo } from 'react'
import { Link, useParams } from '@tanstack/react-router'
import { FileText } from 'lucide-react'
import { useShareTree, type SharePublicMeta } from '../../lib/queries/share'
import { ThemeSwitcher } from '../ThemeSwitcher'
import { Button } from '../ui/button'
import { cn } from '../../lib/utils'

interface ShareLayoutProps {
  token: string
  share: SharePublicMeta
  children: React.ReactNode
}

// Public share shell. Top chrome: tela wordmark + theme switcher + sign-in
// button. Left sidebar: in-scope subtree (only when include_descendants AND
// the tree has more than the root page). NO command palette, NO comments,
// NO all-spaces switcher — those are deliberately absent in share-mode.
export function ShareLayout({ token, share, children }: ShareLayoutProps) {
  const treeEnabled = share.include_descendants
  const tree = useShareTree(token, treeEnabled)
  const showSidebar = treeEnabled && (tree.data?.pages.length ?? 0) > 1

  return (
    <div className="min-h-screen flex flex-col bg-[var(--surface-1)] text-[var(--text-primary)]">
      <header className="flex items-center justify-between px-[var(--space-6)] py-[var(--space-3)] border-b border-[var(--border-subtle)] shrink-0">
        <h1 className="m-0 text-[length:var(--text-lg)] leading-[var(--leading-tight)] font-[family-name:var(--font-sans)]">
          tela
        </h1>
        <div className="flex items-center gap-[var(--space-3)]">
          <ThemeSwitcher />
          {/* Plain <a> rather than the router Link: a full page reload on
              sign-in is intentional so the post-login app boots cleanly
              outside the share-mode shell. */}
          <Button asChild variant="ghost" size="sm">
            <a href="/login">Sign in</a>
          </Button>
        </div>
      </header>
      <div className="flex-1 flex min-h-0">
        {showSidebar ? (
          <ShareSidebar token={token} pages={tree.data?.pages ?? []} />
        ) : null}
        <main className="flex-1 flex flex-col overflow-y-auto min-h-0">
          {children}
        </main>
      </div>
    </div>
  )
}

interface ShareSidebarProps {
  token: string
  pages: { id: number; title: string; parent_id: number | null }[]
}

function ShareSidebar({ token, pages }: ShareSidebarProps) {
  // M15.1 — flat list keyed by id. The backend returns the in-scope pages
  // ordered by parent → position which is usable as a stable rendering
  // sequence; nested rendering / expand-collapse is M15.2 polish.
  const params = useParams({ strict: false }) as { pageId?: string }
  const activeId = params.pageId ? Number(params.pageId) : null

  // Resolve the share's root (no parent in the in-scope set) so we can mark
  // it as the root link to /share/{token}. Other pages route to /p/{id}.
  const rootId = useMemo(() => {
    const ids = new Set(pages.map((p) => p.id))
    const root = pages.find((p) => p.parent_id == null || !ids.has(p.parent_id))
    return root?.id ?? null
  }, [pages])

  // Compute depth per page for indentation. Limited to MAX_DEPTH visual
  // levels so deeply nested trees still fit in the narrow sidebar.
  const MAX_DEPTH = 6
  const depths = useMemo(() => {
    const idToParent = new Map<number, number | null>()
    for (const p of pages) idToParent.set(p.id, p.parent_id)
    const cache = new Map<number, number>()
    const resolve = (id: number): number => {
      if (cache.has(id)) return cache.get(id)!
      const parent = idToParent.get(id)
      if (parent == null || !idToParent.has(parent)) {
        cache.set(id, 0)
        return 0
      }
      const d = Math.min(MAX_DEPTH, resolve(parent) + 1)
      cache.set(id, d)
      return d
    }
    return new Map(pages.map((p) => [p.id, resolve(p.id)]))
  }, [pages])

  return (
    <nav
      aria-label="Shared pages"
      className="w-[var(--space-9)] max-w-[16rem] shrink-0 border-r border-[var(--border-subtle)] bg-[var(--surface-2)] overflow-y-auto p-[var(--space-3)]"
      style={{ width: 'clamp(13rem, 20vw, 16rem)' }}
    >
      <ul className="m-0 p-0 list-none flex flex-col gap-[1px]">
        {pages.map((p) => {
          const depth = depths.get(p.id) ?? 0
          const isRoot = p.id === rootId
          const isActive =
            (activeId == null && isRoot) || activeId === p.id
          const linkClass = cn(
            'group flex items-start gap-[var(--space-2)]',
            'px-[var(--space-2)] py-[var(--space-2)]',
            'rounded-[var(--radius-sm)]',
            'text-[length:var(--text-sm)] font-[family-name:var(--font-sans)]',
            'hover:bg-[var(--surface-1)]',
            'focus-visible:ring-2 focus-visible:ring-[var(--accent)]',
            isActive
              ? 'bg-[var(--surface-1)] text-[var(--text-primary)] font-medium'
              : 'text-[var(--text-muted)]',
          )
          const indentStyle = { paddingLeft: `calc(${depth} * var(--space-3) + var(--space-2))` }
          const inner = (
            <>
              <FileText
                aria-hidden
                width={14}
                height={14}
                className="mt-[2px] shrink-0"
              />
              <span className="truncate">{p.title || 'Untitled'}</span>
            </>
          )
          return (
            <li key={p.id} className="m-0 p-0 list-none">
              {isRoot ? (
                <Link
                  to="/share/$token"
                  params={{ token }}
                  className={linkClass}
                  style={indentStyle}
                >
                  {inner}
                </Link>
              ) : (
                <Link
                  to="/share/$token/p/$pageId"
                  params={{ token, pageId: p.id }}
                  className={linkClass}
                  style={indentStyle}
                >
                  {inner}
                </Link>
              )}
            </li>
          )
        })}
      </ul>
    </nav>
  )
}
