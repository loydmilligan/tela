import { useEffect, useId, useMemo, useRef, useState } from 'react'
import { Check, FileText, Globe } from 'lucide-react'
import { ApiError } from '../../lib/api'
import { useMovePage, usePages } from '../../lib/queries/pages'
import { useSpaces } from '../../lib/queries/spaces'
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
import { Field } from '../ui/field'
import { Select } from '../ui/select'

const UNTITLED_TITLE = 'Untitled'

// Synthetic option id for the "(top of space)" row. Maps back to parent_id: null
// when calling /move. Mirrors NEW_PAGE_ROOT_ID in NewPageDialog so both pickers
// share the same sentinel shape.
const ROOT_ID = '__root__'

interface FlatPage {
  id: number
  title: string
  breadcrumb: string
}

// Pre-order walk that excludes the moved node AND everything beneath it. The
// backend rejects cycles with code: "cycle", but we filter client-side too so
// invalid rows never appear in the picker. Breadcrumb is the slash-joined chain
// of ancestor titles (omitting the page itself). When the target space differs
// from the moved page's space, movedId simply isn't present in the tree, so
// nothing is excluded — which is exactly right.
function flattenValidTargets(
  roots: PageTreeNode[],
  movedId: number,
): FlatPage[] {
  const out: FlatPage[] = []
  function walk(nodes: PageTreeNode[], trail: string[], underMoved: boolean) {
    for (const n of nodes) {
      const isMoved = n.id === movedId
      const title = n.title || UNTITLED_TITLE
      if (!underMoved && !isMoved) {
        out.push({ id: n.id, title, breadcrumb: trail.join(' / ') })
      }
      walk(n.children, [...trail, title], underMoved || isMoved)
    }
  }
  walk(roots, [], false)
  return out
}

function parentIdToPickerValue(parentId: number | null): string {
  return parentId == null ? ROOT_ID : String(parentId)
}

export interface MovePageDialogProps {
  node: PageTreeNode
  // Full root list for the moved page's space. Used to filter descendants and
  // to populate the parent picker without a refetch for the (common) same-space
  // case — it's the same data the sidebar already has cached.
  roots: PageTreeNode[]
  // Source space, threaded into useMovePage so we can invalidate it correctly
  // (both source and target spaces are invalidated on a cross-space move).
  spaceId: number
  open: boolean
  onOpenChange: (next: boolean) => void
}

export function MovePageDialog({
  node,
  roots,
  spaceId,
  open,
  onOpenChange,
}: MovePageDialogProps) {
  const movePage = useMovePage()
  const spaces = useSpaces()
  const spaceSelectId = useId()

  const [targetSpaceId, setTargetSpaceId] = useState<number>(spaceId)
  const [parentId, setParentId] = useState<number | null>(node.parent_id)
  const [pickerValue, setPickerValue] = useState<string>(() =>
    parentIdToPickerValue(node.parent_id),
  )
  const [error, setError] = useState<string | null>(null)

  const crossSpace = targetSpaceId !== spaceId

  // Fetch the target space's tree only when the dialog is open AND the chosen
  // destination differs from the source. Closed dialogs (one is mounted per row)
  // pass spaceId: null so the query stays disabled — no fetch storm. Same-space
  // moves reuse the `roots` prop, so no loading flash for the common case.
  const targetTree = usePages({
    spaceId: open && crossSpace ? targetSpaceId : null,
    tree: true,
  })
  const targetData = targetTree.data as PageTreeNode[] | undefined
  const targetRoots = useMemo(
    () => (crossSpace ? (targetData ?? []) : roots),
    [crossSpace, targetData, roots],
  )
  const loadingTargets = crossSpace && targetTree.isLoading

  // Re-seed on each fresh open so a closed-and-reopened dialog never leaks the
  // previous attempt's state. Mirrors NewPageDialog's openRef pattern.
  const openRef = useRef(false)
  useEffect(() => {
    if (open && !openRef.current) {
      openRef.current = true
      setTargetSpaceId(spaceId)
      setParentId(node.parent_id)
      setPickerValue(parentIdToPickerValue(node.parent_id))
      setError(null)
    }
    if (!open && openRef.current) {
      openRef.current = false
    }
  }, [open, node.parent_id, spaceId])

  function handleSpaceChange(nextSpaceId: number) {
    setTargetSpaceId(nextSpaceId)
    // The old parent can't exist in a different space; default to the space
    // root. When switching back to the source space, restore the original
    // parent so an accidental detour is a no-op.
    const nextParent = nextSpaceId === spaceId ? node.parent_id : null
    setParentId(nextParent)
    setPickerValue(parentIdToPickerValue(nextParent))
  }

  const validTargets = useMemo(
    () => flattenValidTargets(targetRoots, node.id),
    [targetRoots, node.id],
  )

  // Build picker items: synthetic "(top of space)" row first, then every
  // remaining page in the target space. The currently-selected row shows a
  // check icon so the user can see their pick even after typing to filter.
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
    for (const p of validTargets) {
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
  }, [validTargets, parentId])

  function handleClose(next: boolean) {
    if (!next) {
      setError(null)
    }
    onOpenChange(next)
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    if (!crossSpace && parentId === node.parent_id) {
      handleClose(false)
      return
    }
    setError(null)
    try {
      await movePage.mutateAsync({
        id: node.id,
        fromSpaceId: spaceId,
        ...(crossSpace ? { space_id: targetSpaceId } : {}),
        parent_id: parentId,
      })
      handleClose(false)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to move page.')
    }
  }

  const title = node.title || UNTITLED_TITLE
  const submitDisabled =
    movePage.isPending ||
    loadingTargets ||
    (!crossSpace && parentId === node.parent_id)

  const spaceList = spaces.data ?? []

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Move "{title}" to:</DialogTitle>
          <DialogDescription>
            Pick a destination space, then a new parent — or "(top of space)" to
            make it a root page. Moving across spaces takes the whole subtree
            along.
          </DialogDescription>
        </DialogHeader>
        <form
          onSubmit={handleSubmit}
          className="flex flex-col gap-[var(--space-4)]"
        >
          <Field label="Destination space" htmlFor={spaceSelectId}>
            <Select
              id={spaceSelectId}
              value={String(targetSpaceId)}
              onChange={(e) => handleSpaceChange(Number(e.target.value))}
            >
              {spaceList.map((s) => (
                <option key={s.id} value={String(s.id)}>
                  {s.name}
                  {s.id === spaceId ? ' (current)' : ''}
                </option>
              ))}
            </Select>
          </Field>

          <CommandInlinePicker
            items={pickerItems}
            placeholder={
              loadingTargets
                ? 'Loading pages…'
                : crossSpace
                  ? 'Search pages in the destination space…'
                  : 'Search pages in this space…'
            }
            emptyMessage={loadingTargets ? 'Loading…' : 'No matches.'}
            label="Parent page"
            value={pickerValue}
            onValueChange={setPickerValue}
          />

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
              {movePage.isPending ? 'Moving…' : 'Move'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
