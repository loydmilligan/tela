import { useEffect, useMemo, useRef, useState } from 'react'
import { Check, FileText, Globe } from 'lucide-react'
import { ApiError } from '../../lib/api'
import { useCreatePage, usePages } from '../../lib/queries/pages'
import { useSpaces } from '../../lib/queries/spaces'
import { router } from '../../routes/router'
import type { PageTreeNode } from '../../lib/types'
import { Button } from '../ui/button'
import {
  CommandInlinePicker,
  type CommandItem,
} from '../ui/command'
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '../ui/dialog'
import { Input } from '../ui/input'
import { Select } from '../ui/select'

// Synthetic option id for the "(top of space)" row. Maps back to parent_id: null
// when POSTing. Chosen with double-underscores so it can't collide with a real
// page id stringified.
const ROOT_ID = '__root__'

interface FlatPage {
  id: number
  title: string
  breadcrumb: string
}

// Pre-order walk of the tree so siblings stay together and ancestors come
// before descendants. Breadcrumb is the slash-joined chain of ancestor titles.
function flattenPages(roots: PageTreeNode[]): FlatPage[] {
  const out: FlatPage[] = []
  function walk(nodes: PageTreeNode[], trail: string[]) {
    for (const n of nodes) {
      const title = n.title || 'Untitled'
      out.push({
        id: n.id,
        title,
        breadcrumb: trail.join(' / '),
      })
      if (n.children.length > 0) {
        walk(n.children, [...trail, title])
      }
    }
  }
  walk(roots, [])
  return out
}

function parentIdToPickerValue(parentId: number | null): string {
  return parentId == null ? ROOT_ID : String(parentId)
}

export interface NewPageDialogProps {
  open: boolean
  onOpenChange: (next: boolean) => void
  // Pre-fill defaults computed by the host based on current route state. They
  // are read once at the open transition; later route changes do not retro-
  // actively update an already-open dialog.
  defaultSpaceId?: number | null
  defaultParentId?: number | null
  // Pre-seed the title input. Used by the M5.2d broken-wikilink click flow to
  // carry the dead link's text into the create dialog. Same one-shot
  // semantics as the other defaults — re-opening re-seeds.
  defaultTitle?: string
}

export function NewPageDialog({
  open,
  onOpenChange,
  defaultSpaceId,
  defaultParentId,
  defaultTitle,
}: NewPageDialogProps) {
  const spacesQuery = useSpaces()
  const spaces = spacesQuery.data ?? []
  const createPage = useCreatePage()

  // Pick a sensible initial space: caller-provided default if it's in the list,
  // otherwise the first available. `null` only while spaces are still loading.
  const initialSpaceId = useMemo<number | null>(() => {
    if (defaultSpaceId != null && spaces.some((s) => s.id === defaultSpaceId)) {
      return defaultSpaceId
    }
    return spaces[0]?.id ?? null
  }, [defaultSpaceId, spaces])

  const [title, setTitle] = useState('')
  const [spaceId, setSpaceId] = useState<number | null>(initialSpaceId)
  const [parentId, setParentId] = useState<number | null>(
    defaultParentId ?? null,
  )
  const [pickerValue, setPickerValue] = useState<string>(() =>
    parentIdToPickerValue(defaultParentId ?? null),
  )
  const [error, setError] = useState<string | null>(null)

  // Reset all state on each fresh open so a closed-and-reopened dialog never
  // leaks the previous attempt's input. Mirrors the pattern in NewSpaceDialog.
  const openRef = useRef(false)
  useEffect(() => {
    if (open && !openRef.current) {
      openRef.current = true
      setTitle(defaultTitle ?? '')
      setError(null)
      // Defaults are honored only at the open transition (see prop doc).
      const seedSpace =
        defaultSpaceId != null && spaces.some((s) => s.id === defaultSpaceId)
          ? defaultSpaceId
          : spaces[0]?.id ?? null
      setSpaceId(seedSpace)
      // Parent default only applies when it's in the seed space; otherwise root.
      const parentInSeedSpace =
        defaultParentId != null &&
        seedSpace != null &&
        // We don't have the page detail loaded here, so trust the caller: the
        // host only passes defaultParentId when the user is currently on that
        // page within seedSpace.
        defaultSpaceId === seedSpace
          ? defaultParentId
          : null
      setParentId(parentInSeedSpace)
      setPickerValue(parentIdToPickerValue(parentInSeedSpace))
    }
    if (!open && openRef.current) {
      openRef.current = false
    }
  }, [open, defaultSpaceId, defaultParentId, defaultTitle, spaces])

  // Pages for the currently selected space — drives the parent picker rows.
  const pagesQuery = usePages({ spaceId, tree: true })
  const pageNodes = (pagesQuery.data as PageTreeNode[] | undefined) ?? []
  const flatPages = useMemo(() => flattenPages(pageNodes), [pageNodes])

  // Build picker items: synthetic "(top of space)" row first, then every page
  // in the space. The currently-selected row shows a check icon — the user
  // sees their pick even after they type to filter.
  const pickerItems: CommandItem[] = useMemo(() => {
    const items: CommandItem[] = []
    const rootChosen = parentId == null
    items.push({
      id: ROOT_ID,
      title: '(top of space)',
      subtitle: 'No parent — sits at the space root',
      icon: rootChosen ? (
        <Check width={14} height={14} />
      ) : (
        <Globe width={14} height={14} />
      ),
      keywords: ['root', 'top', 'space', 'none'],
      onSelect: () => {
        setParentId(null)
        setPickerValue(ROOT_ID)
      },
    })
    for (const p of flatPages) {
      const chosen = parentId === p.id
      items.push({
        id: String(p.id),
        title: p.title,
        breadcrumb: p.breadcrumb || undefined,
        icon: chosen ? (
          <Check width={14} height={14} />
        ) : (
          <FileText width={14} height={14} />
        ),
        onSelect: () => {
          setParentId(p.id)
          setPickerValue(String(p.id))
        },
      })
    }
    return items
  }, [flatPages, parentId])

  function handleSpaceChange(nextId: number) {
    setSpaceId(nextId)
    // Parent is space-scoped — selecting a different space invalidates any
    // previously-chosen parent. Reset to root for the new space.
    setParentId(null)
    setPickerValue(ROOT_ID)
  }

  function handleClose(next: boolean) {
    if (!next) {
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
    if (spaceId == null) {
      setError('No space available. Create a space first.')
      return
    }
    setError(null)
    try {
      const created = await createPage.mutateAsync({
        space_id: spaceId,
        parent_id: parentId,
        title: trimmed,
      })
      handleClose(false)
      // Use the imported router directly: NewPageDialog is rendered by
      // AppCommandHost, which is a sibling of RouterProvider — useNavigate()
      // wouldn't see the route definitions from this context.
      void router.navigate({
        to: '/spaces/$spaceId/pages/$pageId',
        params: { spaceId: created.space_id, pageId: created.id },
      })
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to create page.')
    }
  }

  const noSpaces = spacesQuery.isSuccess && spaces.length === 0
  const submitDisabled =
    title.trim().length === 0 || spaceId == null || createPage.isPending

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Create a new page</DialogTitle>
          <DialogDescription>
            Pick a parent page and a space. The new page opens in the editor
            once it's created.
          </DialogDescription>
        </DialogHeader>

        {noSpaces ? (
          <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)]">
            You don't have any spaces yet. Create a space before adding pages.
          </p>
        ) : (
          <form
            onSubmit={handleSubmit}
            className="flex flex-col gap-[var(--space-4)]"
          >
            <div className="flex flex-col gap-[var(--space-2)]">
              <label
                htmlFor="new-page-title"
                className="text-[length:var(--text-sm)] text-[var(--text-muted)]"
              >
                Title
              </label>
              <Input
                id="new-page-title"
                autoFocus
                value={title}
                onChange={(e) => setTitle(e.target.value)}
                placeholder="e.g. Architecture overview"
                aria-invalid={error != null && title.trim().length === 0}
              />
            </div>

            <div className="flex flex-col gap-[var(--space-2)]">
              <label
                htmlFor="new-page-parent"
                className="text-[length:var(--text-sm)] text-[var(--text-muted)]"
              >
                Parent page
              </label>
              <CommandInlinePicker
                items={pickerItems}
                placeholder="Search pages in this space…"
                emptyMessage={
                  pagesQuery.isLoading ? 'Loading pages…' : 'No matches.'
                }
                label="Parent page"
                autoFocus={false}
                value={pickerValue}
                onValueChange={setPickerValue}
              />
            </div>

            <div className="flex flex-col gap-[var(--space-2)]">
              <label
                htmlFor="new-page-space"
                className="text-[length:var(--text-sm)] text-[var(--text-muted)]"
              >
                Space
              </label>
              <Select
                id="new-page-space"
                value={spaceId == null ? '' : String(spaceId)}
                onChange={(e) => handleSpaceChange(Number(e.target.value))}
                disabled={spaces.length <= 1}
              >
                {spaces.map((space) => (
                  <option key={space.id} value={String(space.id)}>
                    {space.name || 'Untitled space'}
                  </option>
                ))}
              </Select>
            </div>

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
              <Button type="submit" disabled={submitDisabled}>
                {createPage.isPending ? 'Creating…' : 'Create page'}
              </Button>
            </DialogFooter>
          </form>
        )}
      </DialogContent>
    </Dialog>
  )
}

// Convenience re-export of the picker root id so other consumers (e.g., the
// move dialog in #20) can share the "(top of space)" sentinel value.
export { ROOT_ID as NEW_PAGE_ROOT_ID }
