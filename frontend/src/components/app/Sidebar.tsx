import { Link, useParams } from '@tanstack/react-router'
import { FilePlus, Search, Share2 } from 'lucide-react'
import { PagesTree } from './PagesTree'
import { SpacesList } from './SpacesList'
import { UserMenu } from './UserMenu'
import { BrandMark } from '../BrandMark'
import { Button } from '../ui/button'
import { emitOpenNewPage } from '../../lib/newPageEvent'
import { emitOpenPalette } from '../../lib/paletteEvent'
import { IS_MAC } from '../../lib/useGlobalShortcut'
import { cn } from '../../lib/utils'

export function Sidebar({ open = false }: { open?: boolean }) {
  // Read both params loosely — sidebar lives in the root layout, so we may be
  // anywhere in the tree (index, space, or page route).
  const params = useParams({ strict: false }) as {
    spaceId?: number
    pageId?: number
  }
  const activeSpaceId = typeof params.spaceId === 'number' ? params.spaceId : null
  const activePageId = typeof params.pageId === 'number' ? params.pageId : null

  const newPageShortcut = IS_MAC ? '⌘N' : 'Ctrl+N'
  const searchShortcut = IS_MAC ? '⌘K' : 'Ctrl+K'

  return (
    <aside
      aria-label="Navigation"
      className={cn(
        'flex flex-col w-[var(--sidebar-width)] shrink-0 border-r border-[var(--border-subtle)] bg-[var(--surface-2)] overflow-hidden',
        // Mobile: a fixed slide-in drawer toggled by the header hamburger.
        // Desktop (md+): static, in-flow, always visible — unchanged.
        'fixed inset-y-0 left-0 z-50 transition-transform duration-200 ease-out md:static md:z-auto md:translate-x-0',
        open ? 'translate-x-0' : '-translate-x-full',
      )}
    >
      <div className="px-[var(--space-3)] pt-[var(--space-4)] flex flex-col gap-[var(--space-1)]">
        <Link
          to="/"
          aria-label="tela — home"
          className="mb-[var(--space-2)] inline-flex items-center gap-[var(--space-2)] px-[var(--space-1)] text-[length:var(--text-base)] font-medium leading-none tracking-[-0.01em] text-[var(--text-primary)] no-underline transition-opacity duration-[var(--duration-fast)] hover:opacity-80"
        >
          <BrandMark size={20} />
          tela
        </Link>
        <Button
          variant="secondary"
          size="sm"
          className="w-full justify-start"
          onClick={() => emitOpenPalette('pages')}
          aria-label={`Search (${searchShortcut})`}
          title={`Search (${searchShortcut})`}
        >
          <Search width={14} height={14} />
          <span className="flex-1 text-left text-[var(--text-muted)]">Search…</span>
          <kbd
            aria-hidden
            className="text-[length:var(--text-xs)] text-[var(--text-muted)] font-[family-name:var(--font-sans)]"
          >
            {searchShortcut}
          </kbd>
        </Button>
        <Button
          variant="ghost"
          size="sm"
          className="w-full justify-start"
          onClick={() => emitOpenNewPage()}
          aria-label={`New page (${newPageShortcut})`}
          title={`New page (${newPageShortcut})`}
        >
          <FilePlus width={14} height={14} />
          <span className="flex-1 text-left">New page</span>
          <kbd
            aria-hidden
            className="text-[length:var(--text-xs)] text-[var(--text-muted)] font-[family-name:var(--font-sans)]"
          >
            {newPageShortcut}
          </kbd>
        </Button>
        <Button
          asChild
          variant="ghost"
          size="sm"
          className="w-full justify-start"
        >
          <Link to="/graph" aria-label="Graph view" title="Graph view">
            <Share2 width={14} height={14} />
            <span className="flex-1 text-left">Graph</span>
          </Link>
        </Button>
      </div>
      <SpacesList activeSpaceId={activeSpaceId} />
      {activeSpaceId != null ? (
        <PagesTree spaceId={activeSpaceId} activePageId={activePageId} />
      ) : (
        <div className="flex-1" />
      )}
      <UserMenu />
    </aside>
  )
}
