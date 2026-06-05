import { useEffect, useMemo } from 'react'
import { Link, useParams } from '@tanstack/react-router'
import { ChevronDown, ChevronRight, FileText } from 'lucide-react'
import { useShareTree, type SharePublicMeta } from '../../lib/queries/share'
import { useShareExpanded } from '../../lib/useShareExpanded'
import { ThemeSwitcher } from '../ThemeSwitcher'
import { BrandMark } from '../BrandMark'
import { Button } from '../ui/button'
import { cn } from '../../lib/utils'

interface SharePageNode {
  id: number
  title: string
  parent_id: number | null
}

// Visual indent cap. Pages nested deeper than this still render — they just
// stop gaining additional left-padding so the narrow sidebar stays readable.
const MAX_DEPTH = 6

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
  const pages = tree.data?.pages ?? []
  const showSidebar = treeEnabled && pages.length > 1

  return (
    <div className="min-h-dvh flex flex-col bg-[var(--surface-1)] text-[var(--text-primary)]">
      <header className="flex items-center justify-between px-[var(--space-6)] py-[var(--space-3)] border-b border-[var(--border-subtle)] shrink-0">
        <h1 className="m-0 text-[length:var(--text-lg)] leading-[var(--leading-tight)] font-[family-name:var(--font-sans)]">
          {/* Plain <a> (full nav) to the apex marketing landing — like the
              "Sign in" link, the share-mode wordmark escapes the SPA rather
              than client-routing into an authed/app surface. */}
          <a
            href="/"
            aria-label="tela home"
            className="inline-flex items-center gap-[var(--space-2)] rounded-[var(--radius-xs)] text-[var(--text-primary)] no-underline transition-opacity duration-[var(--duration-fast)] hover:opacity-70 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent)]"
          >
            <BrandMark size={20} />
            tela
          </a>
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
        {showSidebar ? <ShareSidebar token={token} pages={pages} /> : null}
        <main className="flex-1 flex flex-col overflow-y-auto min-h-0">
          {children}
        </main>
      </div>
    </div>
  )
}

interface ShareSidebarProps {
  token: string
  pages: SharePageNode[]
}

// Exported so Storybook stories can render the sidebar in isolation without
// going through useShareTree. The route component never imports it directly
// (it goes through <ShareLayout>).
export function ShareSidebar({ token, pages }: ShareSidebarProps) {
  const params = useParams({ strict: false }) as { pageId?: string }
  const activeId = params.pageId ? Number(params.pageId) : null

  // Resolve the share's root (no parent in the in-scope set) so we can mark
  // it as the root link to /share/{token}. The root's parent_id may NOT be
  // null — if a subtree was shared, the root's parent points OUTSIDE the
  // scope. Detect via "no in-scope parent", not "parent_id == null".
  const { rootId, parentToChildren, idToParent } = useMemo(() => {
    const ids = new Set(pages.map((p) => p.id))
    const root = pages.find((p) => p.parent_id == null || !ids.has(p.parent_id))
    const p2c = new Map<number | null, SharePageNode[]>()
    const c2p = new Map<number, number | null>()
    for (const page of pages) {
      const key = page.parent_id != null && ids.has(page.parent_id) ? page.parent_id : null
      const bucket = p2c.get(key) ?? []
      bucket.push(page)
      p2c.set(key, bucket)
      c2p.set(page.id, key)
    }
    return { rootId: root?.id ?? null, parentToChildren: p2c, idToParent: c2p }
  }, [pages])

  const { expanded, toggle, expand } = useShareExpanded(token, rootId)

  // Auto-expand the ancestor chain of the active page. Runs on mount AND on
  // every activeId / parent-map change so deep navigation unfolds the tree
  // even if the user manually collapsed nodes earlier. This is a legitimate
  // useEffect: "synchronize state with an external value (the URL param)"
  // rather than the "compute from props" anti-pattern.
  useEffect(() => {
    if (activeId == null) return
    const visited = new Set<number>()
    let current = idToParent.get(activeId)
    while (current != null && !visited.has(current)) {
      visited.add(current)
      expand(current)
      current = idToParent.get(current)
    }
  }, [activeId, idToParent, expand])

  return (
    <nav
      aria-label="Shared pages"
      className="shrink-0 border-r border-[var(--border-subtle)] bg-[var(--surface-2)] overflow-y-auto p-[var(--space-3)]"
      style={{ width: 'clamp(13rem, 20vw, 16rem)' }}
    >
      <ShareTreeList
        token={token}
        parentId={null}
        depth={0}
        activeId={activeId}
        rootId={rootId}
        parentToChildren={parentToChildren}
        expanded={expanded}
        onToggle={toggle}
      />
    </nav>
  )
}

interface ShareTreeListProps {
  token: string
  parentId: number | null
  depth: number
  activeId: number | null
  rootId: number | null
  parentToChildren: Map<number | null, SharePageNode[]>
  expanded: Set<number>
  onToggle: (id: number) => void
}

function ShareTreeList({
  token,
  parentId,
  depth,
  activeId,
  rootId,
  parentToChildren,
  expanded,
  onToggle,
}: ShareTreeListProps) {
  const children = parentToChildren.get(parentId) ?? []
  if (children.length === 0) return null
  return (
    <ul className="m-0 p-0 list-none flex flex-col gap-[1px]">
      {children.map((p) => (
        <ShareTreeRow
          key={p.id}
          token={token}
          page={p}
          depth={depth}
          activeId={activeId}
          rootId={rootId}
          parentToChildren={parentToChildren}
          expanded={expanded}
          onToggle={onToggle}
        />
      ))}
    </ul>
  )
}

interface ShareTreeRowProps {
  token: string
  page: SharePageNode
  depth: number
  activeId: number | null
  rootId: number | null
  parentToChildren: Map<number | null, SharePageNode[]>
  expanded: Set<number>
  onToggle: (id: number) => void
}

function ShareTreeRow({
  token,
  page,
  depth,
  activeId,
  rootId,
  parentToChildren,
  expanded,
  onToggle,
}: ShareTreeRowProps) {
  const isRoot = page.id === rootId
  const isActive = (activeId == null && isRoot) || activeId === page.id
  const hasChildren = (parentToChildren.get(page.id) ?? []).length > 0
  const isOpen = expanded.has(page.id)
  const cappedDepth = Math.min(depth, MAX_DEPTH)
  const indentStyle = { paddingLeft: `calc(${cappedDepth} * var(--space-3))` }

  const rowClass = cn(
    'group flex items-center gap-[var(--space-1)] pr-[var(--space-1)]',
    'rounded-[var(--radius-sm)]',
    'hover:bg-[var(--surface-1)]',
    isActive && 'bg-[var(--surface-1)]',
  )

  const linkClass = cn(
    'flex-1 min-w-0 flex items-center gap-[var(--space-2)]',
    'py-[var(--space-2)] pr-[var(--space-2)]',
    'text-[length:var(--text-sm)] font-[family-name:var(--font-sans)]',
    'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent)] rounded-[var(--radius-sm)]',
    isActive
      ? 'text-[var(--text-primary)] font-medium'
      : 'text-[var(--text-muted)]',
  )

  const inner = (
    <>
      <FileText
        aria-hidden
        width={14}
        height={14}
        className="shrink-0"
      />
      <span className="truncate">{page.title || 'Untitled'}</span>
    </>
  )

  return (
    <li className="m-0 p-0 list-none">
      <div className={rowClass} style={indentStyle}>
        {hasChildren ? (
          <button
            type="button"
            aria-label={isOpen ? 'Collapse' : 'Expand'}
            aria-expanded={isOpen}
            onClick={() => onToggle(page.id)}
            className="inline-flex items-center justify-center h-[var(--space-7)] w-[var(--space-7)] rounded-[var(--radius-sm)] bg-transparent border-0 cursor-pointer text-[var(--text-muted)] hover:text-[var(--text-primary)] outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent)]"
          >
            {isOpen ? (
              <ChevronDown width={14} height={14} />
            ) : (
              <ChevronRight width={14} height={14} />
            )}
          </button>
        ) : (
          <span
            aria-hidden="true"
            className="inline-block h-[var(--space-7)] w-[var(--space-7)]"
          />
        )}

        {isRoot ? (
          <Link to="/share/$token" params={{ token }} className={linkClass}>
            {inner}
          </Link>
        ) : (
          <Link
            to="/share/$token/p/$pageId"
            params={{ token, pageId: page.id }}
            className={linkClass}
          >
            {inner}
          </Link>
        )}
      </div>

      {hasChildren && isOpen ? (
        <ShareTreeList
          token={token}
          parentId={page.id}
          depth={depth + 1}
          activeId={activeId}
          rootId={rootId}
          parentToChildren={parentToChildren}
          expanded={expanded}
          onToggle={onToggle}
        />
      ) : null}
    </li>
  )
}
