import { useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate } from '@tanstack/react-router'
import {
  ChevronDown,
  ChevronRight,
  FilePlus,
  MoreHorizontal,
  RotateCw,
} from 'lucide-react'
import { ApiError } from '../../lib/api'
import {
  useCreatePage,
  useDeletePage,
  usePages,
  useUpdatePage,
} from '../../lib/queries/pages'
import { useSpaces } from '../../lib/queries/spaces'
import { useSpaceFreshness } from '../../lib/queries/freshness'
import type { PageTreeNode } from '../../lib/types'
import { useExpandedNodes } from '../../lib/useExpandedNodes'
import { StalenessDot } from './StalenessDot'
import { Button } from '../ui/button'
import { Card, CardBody, CardFooter } from '../ui/card'
import { emitOpenNewPage } from '../../lib/newPageEvent'
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '../ui/dialog'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '../ui/dropdown-menu'
import { Input } from '../ui/input'
import { VisibilityBadge } from '../ui/visibility-badge'
import { MovePageDialog } from './move-page-dialog'
import { cn } from '../../lib/utils'

const UNTITLED_TITLE = 'Untitled'

// findAncestors returns the chain of ancestor ids from outermost root down to
// the target's immediate parent. Returns [] when the target is itself a root,
// null when the target isn't in the tree at all. Used by the auto-reveal
// effect to expand every collapsed ancestor on a route change.
function findAncestors(nodes: PageTreeNode[], targetId: number): number[] | null {
  for (const node of nodes) {
    if (node.id === targetId) return []
    const childChain = findAncestors(node.children, targetId)
    if (childChain != null) return [node.id, ...childChain]
  }
  return null
}

interface PagesTreeProps {
  spaceId: number
  activePageId: number | null
}

export function PagesTree({ spaceId, activePageId }: PagesTreeProps) {
  const navigate = useNavigate()
  const tree = usePages({ spaceId, tree: true })
  const createPage = useCreatePage()
  const { expanded, toggle, expand, expandMany } = useExpandedNodes(spaceId)
  const spaces = useSpaces()
  const spaceName = spaces.data?.find((s) => s.id === spaceId)?.name

  const treeData = tree.data as PageTreeNode[] | undefined
  const nodes = treeData ?? []

  // Per-page index staleness for the dots. Only when the embedder is enabled
  // (otherwise every page reads as "unindexed", which is noise on a dark
  // instance). Map page id → 'stale' | 'unindexed'; absent = fresh/empty.
  const freshness = useSpaceFreshness(spaceId)
  const staleStatus = useMemo(() => {
    const m = new Map<number, 'stale' | 'unindexed'>()
    if (!freshness.data?.enabled) return m
    for (const p of freshness.data.pages) {
      if (p.status === 'stale' || p.status === 'unindexed') m.set(p.page_id, p.status)
    }
    return m
  }, [freshness.data])

  // Auto-reveal the active page when navigation lands on it from outside the
  // sidebar (backlink click, command-palette result, [[wikilink]], direct URL,
  // tela://page/N link). The highlight prop already flows through; this just
  // ensures every collapsed ancestor on the path gets opened so the row is
  // rendered. Negative ids (optimistic collab placeholders) won't match any
  // tree row → findAncestors returns null → no-op.
  useEffect(() => {
    if (activePageId == null || activePageId < 0 || !treeData) return
    const chain = findAncestors(treeData, activePageId)
    if (chain && chain.length > 0) expandMany(chain)
  }, [activePageId, treeData, expandMany])

  async function handleCreate(parentId: number | null) {
    if (parentId != null) expand(parentId)
    try {
      const created = await createPage.mutateAsync({
        space_id: spaceId,
        parent_id: parentId,
        title: UNTITLED_TITLE,
      })
      void navigate({
        to: '/spaces/$spaceId/pages/$pageId/{-$slug}',
        params: { spaceId, pageId: created.id, slug: undefined },
      })
    } catch {
      // Surface via tree refetch on next interaction; in v0 swallowing the toast.
    }
  }

  return (
    <section
      className="flex flex-col gap-[var(--space-1)] px-[var(--space-3)] pt-[var(--space-4)] pb-[var(--space-3)] mt-[var(--space-2)] flex-1 min-h-0 overflow-y-auto border-t border-[var(--border-subtle)]"
      aria-labelledby="sidebar-pages-heading"
    >
      <div className="flex items-center justify-between gap-[var(--space-2)] pl-[var(--space-2)] pr-[var(--space-1)]">
        {/* The tree always shows the active space's pages — name the space here
            so the section reads as "<Space> · its pages", not a stray list. */}
        <h2
          id="sidebar-pages-heading"
          className="m-0 min-w-0 truncate font-[family-name:var(--font-sans)] text-[length:var(--text-sm)] font-medium leading-[var(--leading-tight)] text-[var(--text-primary)]"
          title={spaceName ?? 'Pages'}
        >
          {spaceName ?? 'Pages'}
        </h2>
        <Button
          variant="ghost"
          size="sm"
          aria-label="New top-level page"
          className="h-[var(--space-6)] w-[var(--space-6)] p-0"
          onClick={() => void handleCreate(null)}
          disabled={createPage.isPending}
        >
          <FilePlus width={14} height={14} />
        </Button>
      </div>

      {tree.isLoading ? <PagesSkeleton /> : null}

      {tree.isError ? (
        <PagesError onRetry={() => void tree.refetch()} />
      ) : null}

      {tree.data && nodes.length === 0 ? (
        <Card className="bg-[var(--surface-1)]">
          <CardBody className="px-[var(--space-4)] py-[var(--space-3)] gap-[var(--space-1)]">
            <p className="m-0 text-[length:var(--text-sm)] font-medium text-[var(--text-primary)]">
              No pages yet
            </p>
            <p className="m-0 text-[length:var(--text-xs)] text-[var(--text-muted)] leading-[var(--leading-relaxed)]">
              Add a page to start writing.
            </p>
          </CardBody>
          <CardFooter className="px-[var(--space-4)] pt-0 pb-[var(--space-3)]">
            <Button
              variant="primary"
              size="sm"
              className="w-full"
              onClick={() => emitOpenNewPage()}
            >
              <FilePlus width={14} height={14} /> New page
            </Button>
          </CardFooter>
        </Card>
      ) : null}

      <ul className="m-0 p-0 list-none flex flex-col gap-[1px]">
        {nodes.map((node) => (
          <PageNode
            key={node.id}
            node={node}
            depth={0}
            spaceId={spaceId}
            activePageId={activePageId}
            expanded={expanded}
            onToggle={toggle}
            onExpand={expand}
            allNodes={nodes}
            staleStatus={staleStatus}
          />
        ))}
      </ul>
    </section>
  )
}

function PagesSkeleton() {
  return (
    <div
      className="flex flex-col gap-[var(--space-1)] px-[var(--space-2)]"
      aria-hidden="true"
    >
      {[0, 1, 2, 3].map((i) => (
        <div
          key={i}
          className="h-[var(--space-7)] rounded-[var(--radius-sm)] bg-[var(--surface-2)]"
        />
      ))}
    </div>
  )
}

function PagesError({ onRetry }: { onRetry: () => void }) {
  return (
    <div className="flex items-center justify-between gap-[var(--space-2)] px-[var(--space-2)] py-[var(--space-2)] rounded-[var(--radius-sm)] bg-[var(--surface-2)] text-[length:var(--text-sm)] text-[var(--danger)]">
      <span>Couldn't load pages.</span>
      <Button variant="ghost" size="sm" onClick={onRetry} aria-label="Retry">
        <RotateCw width={14} height={14} />
      </Button>
    </div>
  )
}

interface PageNodeProps {
  node: PageTreeNode
  depth: number
  spaceId: number
  activePageId: number | null
  expanded: Set<number>
  onToggle: (id: number) => void
  onExpand: (id: number) => void
  allNodes: PageTreeNode[]
  staleStatus: Map<number, 'stale' | 'unindexed'>
}

function PageNode({
  node,
  depth,
  spaceId,
  activePageId,
  expanded,
  onToggle,
  onExpand,
  allNodes,
  staleStatus,
}: PageNodeProps) {
  const navigate = useNavigate()
  const createPage = useCreatePage()
  const [renameOpen, setRenameOpen] = useState(false)
  const [moveOpen, setMoveOpen] = useState(false)
  const [deleteOpen, setDeleteOpen] = useState(false)

  const hasChildren = node.children.length > 0
  const isOpen = expanded.has(node.id)
  const active = activePageId === node.id
  const rowRef = useRef<HTMLDivElement | null>(null)

  // Scroll the row into view exactly once each time it transitions to active.
  // `block: 'nearest'` is a no-op when the row is already on-screen, so we
  // don't fight the user who has manually scrolled the sidebar. Fires on
  // mount-active too (which is the path that matters: the auto-expand effect
  // reveals a previously hidden ancestor → child node mounts already active).
  useEffect(() => {
    if (active && rowRef.current) {
      rowRef.current.scrollIntoView({ block: 'nearest' })
    }
  }, [active])

  async function handleNewChild() {
    onExpand(node.id)
    try {
      const created = await createPage.mutateAsync({
        space_id: spaceId,
        parent_id: node.id,
        title: UNTITLED_TITLE,
      })
      void navigate({
        to: '/spaces/$spaceId/pages/$pageId/{-$slug}',
        params: { spaceId, pageId: created.id, slug: undefined },
      })
    } catch {
      // Tree refetch surfaces failure on next interaction.
    }
  }

  return (
    <li className="m-0 p-0 list-none">
      <div
        ref={rowRef}
        className={cn(
          'group flex items-center gap-[var(--space-1)] pr-[var(--space-1)] rounded-[var(--radius-sm)]',
          'hover:bg-[var(--sidebar-item-hover)]',
          active &&
            'bg-[var(--sidebar-item-active)] shadow-[inset_2px_0_0_0_var(--sidebar-item-active-bar)]',
        )}
        style={{
          paddingLeft: `calc(var(--sidebar-indent) * ${depth} + var(--space-2))`,
        }}
      >
        {hasChildren ? (
          <button
            type="button"
            aria-label={isOpen ? 'Collapse' : 'Expand'}
            aria-expanded={isOpen}
            onClick={() => onToggle(node.id)}
            className="inline-flex items-center justify-center h-[var(--space-6)] w-[var(--space-4)] shrink-0 rounded-[var(--radius-xs)] bg-transparent border-0 cursor-pointer text-[var(--text-muted)] hover:text-[var(--text-primary)] outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent)]"
          >
            {isOpen ? (
              <ChevronDown width={13} height={13} />
            ) : (
              <ChevronRight width={13} height={13} />
            )}
          </button>
        ) : (
          <span
            aria-hidden="true"
            className="inline-block h-[var(--space-6)] w-[var(--space-4)] shrink-0"
          />
        )}

        <button
          type="button"
          aria-current={active ? 'page' : undefined}
          onClick={() =>
            void navigate({
              to: '/spaces/$spaceId/pages/$pageId/{-$slug}',
              params: { spaceId, pageId: node.id, slug: undefined },
            })
          }
          className={cn(
            'flex-1 min-w-0 text-left',
            'py-[var(--space-2)]',
            'font-[family-name:var(--font-sans)] text-[length:var(--text-sm)] leading-[var(--leading-tight)]',
            'text-[var(--text-primary)] bg-transparent border-0 cursor-pointer outline-none',
            'truncate',
            active && 'text-[var(--accent)] font-medium',
          )}
        >
          {node.title || (
            <span className="text-[var(--text-muted)]">{UNTITLED_TITLE}</span>
          )}
        </button>

        {/* Staleness marker — trailing, only when this page's index is out of
            date. Hides on row hover to make room for the ⋯ menu. */}
        {staleStatus.has(node.id) ? (
          <span className="shrink-0 inline-flex items-center group-hover:hidden">
            <StalenessDot
              label={
                staleStatus.get(node.id) === 'stale'
                  ? 'Edited since last indexed'
                  : 'Not indexed yet'
              }
            />
          </span>
        ) : null}

        {/* Exposure marker — trailing, only when the page is actually exposed.
            Reclaims the left gutter (titles align tight to the chevron). */}
        {node.exposure && node.exposure.state !== 'private' ? (
          <span className="shrink-0 inline-flex items-center justify-center group-hover:hidden">
            <VisibilityBadge
              state={node.exposure.state}
              inherited={node.exposure.inherited}
              compact
            />
          </span>
        ) : null}

        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button
              variant="ghost"
              size="sm"
              aria-label={`Actions for ${node.title || UNTITLED_TITLE}`}
              className="shrink-0 h-[var(--space-6)] w-[var(--space-6)] p-0 hidden group-hover:inline-flex data-[state=open]:inline-flex focus-visible:inline-flex"
            >
              <MoreHorizontal width={14} height={14} />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem onSelect={() => setRenameOpen(true)}>
              Rename
            </DropdownMenuItem>
            <DropdownMenuItem onSelect={() => void handleNewChild()}>
              New child page
            </DropdownMenuItem>
            <DropdownMenuItem onSelect={() => setMoveOpen(true)}>
              Move…
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem destructive onSelect={() => setDeleteOpen(true)}>
              Delete
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>

      {hasChildren && isOpen ? (
        <ul className="m-0 p-0 list-none flex flex-col gap-[1px]">
          {node.children.map((child) => (
            <PageNode
              key={child.id}
              node={child}
              depth={depth + 1}
              spaceId={spaceId}
              activePageId={activePageId}
              expanded={expanded}
              onToggle={onToggle}
              onExpand={onExpand}
              allNodes={allNodes}
              staleStatus={staleStatus}
            />
          ))}
        </ul>
      ) : null}

      <RenamePageDialog
        node={node}
        open={renameOpen}
        onOpenChange={setRenameOpen}
      />
      <MovePageDialog
        node={node}
        spaceId={spaceId}
        roots={allNodes}
        open={moveOpen}
        onOpenChange={setMoveOpen}
      />
      <DeletePageDialog
        node={node}
        spaceId={spaceId}
        active={active}
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
      />
    </li>
  )
}

interface RenamePageDialogProps {
  node: PageTreeNode
  open: boolean
  onOpenChange: (next: boolean) => void
}

function RenamePageDialog({ node, open, onOpenChange }: RenamePageDialogProps) {
  const [title, setTitle] = useState(node.title)
  const [error, setError] = useState<string | null>(null)
  const updatePage = useUpdatePage()

  function handleClose(next: boolean) {
    if (!next) {
      setTitle(node.title)
      setError(null)
    }
    onOpenChange(next)
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    const trimmed = title.trim()
    if (!trimmed) {
      setError('Title is required.')
      return
    }
    if (trimmed === node.title) {
      handleClose(false)
      return
    }
    setError(null)
    try {
      await updatePage.mutateAsync({ id: node.id, title: trimmed })
      handleClose(false)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to rename page.')
    }
  }

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Rename page</DialogTitle>
        </DialogHeader>
        <form onSubmit={handleSubmit} className="flex flex-col gap-[var(--space-3)]">
          <div className="flex flex-col gap-[var(--space-2)]">
            <label
              htmlFor={`rename-page-${node.id}`}
              className="text-[length:var(--text-sm)] text-[var(--text-muted)]"
            >
              Title
            </label>
            <Input
              id={`rename-page-${node.id}`}
              autoFocus
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              aria-invalid={error != null}
            />
            {error ? (
              <p className="m-0 text-[length:var(--text-xs)] text-[var(--danger)]">
                {error}
              </p>
            ) : null}
          </div>
          <DialogFooter>
            <DialogClose asChild>
              <Button type="button" variant="ghost">
                Cancel
              </Button>
            </DialogClose>
            <Button type="submit" disabled={updatePage.isPending}>
              {updatePage.isPending ? 'Saving…' : 'Save'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

interface DeletePageDialogProps {
  node: PageTreeNode
  spaceId: number
  active: boolean
  open: boolean
  onOpenChange: (next: boolean) => void
}

function DeletePageDialog({
  node,
  spaceId,
  active,
  open,
  onOpenChange,
}: DeletePageDialogProps) {
  const [error, setError] = useState<string | null>(null)
  const navigate = useNavigate()
  const deletePage = useDeletePage()

  function handleClose(next: boolean) {
    if (!next) setError(null)
    onOpenChange(next)
  }

  async function handleDelete() {
    setError(null)
    try {
      await deletePage.mutateAsync({ id: node.id, spaceId })
      handleClose(false)
      if (active) {
        void navigate({ to: '/spaces/$spaceId', params: { spaceId } })
      }
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to delete page.')
    }
  }

  const childCount = node.children.length

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Delete this page?</DialogTitle>
          <DialogDescription>
            "{node.title || UNTITLED_TITLE}"
            {childCount > 0
              ? ` and its ${childCount} ${
                  childCount === 1 ? 'child page' : 'child pages'
                }`
              : ''}{' '}
            will be permanently removed. This action cannot be undone.
          </DialogDescription>
        </DialogHeader>
        {error ? (
          <p className="m-0 text-[length:var(--text-xs)] text-[var(--danger)]">
            {error}
          </p>
        ) : null}
        <DialogFooter>
          <DialogClose asChild>
            <Button type="button" variant="ghost">
              Cancel
            </Button>
          </DialogClose>
          <Button
            type="button"
            variant="danger"
            onClick={handleDelete}
            disabled={deletePage.isPending}
          >
            {deletePage.isPending ? 'Deleting…' : 'Delete'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
