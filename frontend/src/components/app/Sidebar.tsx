import { useParams } from '@tanstack/react-router'
import { PagesTree } from './PagesTree'
import { SpacesList } from './SpacesList'

export function Sidebar() {
  // Read both params loosely — sidebar lives in the root layout, so we may be
  // anywhere in the tree (index, space, or page route).
  const params = useParams({ strict: false }) as {
    spaceId?: number
    pageId?: number
  }
  const activeSpaceId = typeof params.spaceId === 'number' ? params.spaceId : null
  const activePageId = typeof params.pageId === 'number' ? params.pageId : null

  return (
    <aside
      aria-label="Navigation"
      className="flex flex-col w-[var(--sidebar-width)] shrink-0 border-r border-[var(--border-subtle)] bg-[var(--surface-2)] overflow-hidden"
    >
      <SpacesList activeSpaceId={activeSpaceId} />
      {activeSpaceId != null ? (
        <PagesTree spaceId={activeSpaceId} activePageId={activePageId} />
      ) : (
        <div className="flex-1" />
      )}
    </aside>
  )
}
