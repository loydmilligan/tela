import { useCallback, useEffect, useRef, useState } from 'react'
import { Link, useNavigate } from '@tanstack/react-router'
import { ChevronRight, Trash2 } from 'lucide-react'
import { ApiError } from '../../lib/api'
import {
  useDeletePage,
  usePage,
  usePages,
  useUpdatePage,
} from '../../lib/queries/pages'
import { useSpace } from '../../lib/queries/spaces'
import type { Page, PageTreeNode } from '../../lib/types'
import { Button } from '../ui/button'
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
import { SaveIndicator, type SaveStatus } from '../ui/save-indicator'
import { MilkdownEditor } from './milkdown-editor'
import { cn } from '../../lib/utils'

const SAVED_FLASH_MS = 1500
const BODY_DEBOUNCE_MS = 500
const RETRY_DELAY_MS = 1500

interface PageViewProps {
  spaceId: number
  pageId: number
}

export function PageView({ spaceId, pageId }: PageViewProps) {
  const page = usePage(pageId)
  const navigate = useNavigate()

  // 404 / wrong-space handling
  if (page.isError) {
    const status = page.error instanceof ApiError ? page.error.status : null
    if (status === 404) return <PageNotFound spaceId={spaceId} />
    return <PageError onRetry={() => void page.refetch()} />
  }

  if (page.isLoading || !page.data) return <PageLoading />

  if (page.data.space_id !== spaceId) {
    // Page exists but in a different space — treat as not-found for this URL.
    return <PageNotFound spaceId={spaceId} />
  }

  return (
    <PageEditor
      key={page.data.id}
      page={page.data}
      spaceId={spaceId}
      onDeleted={() =>
        void navigate({ to: '/spaces/$spaceId', params: { spaceId } })
      }
    />
  )
}

interface PageEditorProps {
  page: Page
  spaceId: number
  onDeleted: () => void
}

function PageEditor({ page, spaceId, onDeleted }: PageEditorProps) {
  const updatePage = useUpdatePage()
  const [title, setTitle] = useState(page.title)
  const [body, setBody] = useState(page.body)
  const [deleteOpen, setDeleteOpen] = useState(false)

  // Track last server-confirmed values so we can skip no-op saves and detect
  // dirtiness on blur.
  const lastSavedRef = useRef({ title: page.title, body: page.body })

  const [status, setStatus] = useState<SaveStatus>('idle')

  // Reset "Saved" flash back to idle after a short delay so the badge doesn't
  // linger.
  useEffect(() => {
    if (status !== 'saved') return
    const t = window.setTimeout(() => setStatus('idle'), SAVED_FLASH_MS)
    return () => window.clearTimeout(t)
  }, [status])

  // Debounce timer for body auto-save and retry timer for the one-shot 5xx
  // retry. Both are cancelled together: any fresh save attempt (or any new
  // keystroke that schedules one) supersedes a queued retry — last-write-wins.
  const debounceRef = useRef<number | null>(null)
  const retryTimerRef = useRef<number | null>(null)

  const cancelPendingSave = useCallback(() => {
    if (debounceRef.current != null) {
      window.clearTimeout(debounceRef.current)
      debounceRef.current = null
    }
    if (retryTimerRef.current != null) {
      window.clearTimeout(retryTimerRef.current)
      retryTimerRef.current = null
      // The displayed 'retrying' state is no longer accurate; reset so the
      // user doesn't see a stale label until the next save call sets 'saving'.
      setStatus((s) => (s === 'retrying' ? 'idle' : s))
    }
  }, [])

  useEffect(
    () => () => {
      if (debounceRef.current != null) window.clearTimeout(debounceRef.current)
      if (retryTimerRef.current != null)
        window.clearTimeout(retryTimerRef.current)
    },
    [],
  )

  const save = useCallback(
    async (patch: { title?: string; body?: string }) => {
      // Any new save attempt supersedes a pending retry of an earlier payload.
      if (retryTimerRef.current != null) {
        window.clearTimeout(retryTimerRef.current)
        retryTimerRef.current = null
      }
      setStatus('saving')
      try {
        const updated = await updatePage.mutateAsync({ id: page.id, ...patch })
        lastSavedRef.current = { title: updated.title, body: updated.body }
        setStatus('saved')
        return
      } catch (err) {
        // 5xx → one automatic retry after a short delay. 4xx and network
        // errors (status === 0) go straight to 'error' — those won't
        // self-heal within the retry window.
        if (err instanceof ApiError && err.status >= 500) {
          setStatus('retrying')
          retryTimerRef.current = window.setTimeout(async () => {
            retryTimerRef.current = null
            try {
              const updated = await updatePage.mutateAsync({
                id: page.id,
                ...patch,
              })
              lastSavedRef.current = {
                title: updated.title,
                body: updated.body,
              }
              setStatus('saved')
            } catch {
              setStatus('error')
            }
          }, RETRY_DELAY_MS)
          return
        }
        setStatus('error')
      }
    },
    [page.id, updatePage],
  )

  const handleTitleBlur = useCallback(() => {
    const trimmed = title.trim()
    if (trimmed === lastSavedRef.current.title) return
    if (!trimmed) {
      // Empty title — revert to last saved value rather than persisting blank.
      setTitle(lastSavedRef.current.title)
      return
    }
    void save({ title: trimmed })
  }, [title, save])

  // Debounced body auto-save: schedule a save 500ms after the last keystroke;
  // blur cancels the timer and fires immediately if there's a pending change.
  const handleBodyChange = useCallback(
    (next: string) => {
      setBody(next)
      cancelPendingSave()
      debounceRef.current = window.setTimeout(() => {
        debounceRef.current = null
        if (next === lastSavedRef.current.body) return
        void save({ body: next })
      }, BODY_DEBOUNCE_MS)
    },
    [save, cancelPendingSave],
  )

  // Read latest body via ref so onBlur, which is wired into Milkdown's
  // listener once at mount, always sees the most recent value.
  const bodyRef = useRef(body)
  bodyRef.current = body

  const handleBodyBlur = useCallback(() => {
    cancelPendingSave()
    const current = bodyRef.current
    if (current === lastSavedRef.current.body) return
    void save({ body: current })
  }, [save, cancelPendingSave])

  // autoFocus rule: empty title → focus title; non-empty → focus body.
  const titleAutoFocus = page.title.length === 0
  const bodyAutoFocus = page.title.length > 0

  return (
    <div className="flex-1 flex flex-col min-h-0">
      <header className="flex items-center justify-between gap-[var(--space-4)] px-[var(--space-6)] py-[var(--space-3)] border-b border-[var(--border-subtle)] shrink-0">
        <Breadcrumb spaceId={spaceId} pageId={page.id} />
        <div className="flex items-center gap-[var(--space-3)]">
          <SaveIndicator status={status} />
          <Button
            type="button"
            variant="ghost"
            size="sm"
            aria-label="Delete page"
            onClick={() => setDeleteOpen(true)}
            className="h-[var(--space-8)] w-[var(--space-8)] p-0 hover:text-[var(--danger)]"
          >
            <Trash2 width={16} height={16} />
          </Button>
        </div>
      </header>

      <div className="flex-1 flex flex-col gap-[var(--space-4)] p-[var(--space-7)] max-w-[48rem] w-full self-center min-h-0">
        <Input
          size="lg"
          autoFocus={titleAutoFocus}
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          onBlur={handleTitleBlur}
          placeholder="Untitled page"
          aria-label="Page title"
          className={cn(
            'border-transparent bg-transparent shadow-none',
            'h-auto px-[var(--space-2)] py-[var(--space-2)]',
            'text-[length:var(--text-3xl)] leading-[var(--leading-tight)] font-medium',
            'focus-visible:border-[var(--border-subtle)]',
          )}
        />

        <MilkdownEditor
          defaultValue={page.body}
          onChange={handleBodyChange}
          onBlur={handleBodyBlur}
          autoFocus={bodyAutoFocus}
          ariaLabel="Page body"
          className="flex-1 min-h-[calc(var(--space-8)*8)]"
        />
      </div>

      <DeletePageConfirmDialog
        page={page}
        spaceId={spaceId}
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        onDeleted={onDeleted}
      />
    </div>
  )
}

function findAncestorChain(
  tree: PageTreeNode[],
  pageId: number,
): PageTreeNode[] | null {
  for (const node of tree) {
    if (node.id === pageId) return [node]
    const sub = findAncestorChain(node.children, pageId)
    if (sub) return [node, ...sub]
  }
  return null
}

function Breadcrumb({ spaceId, pageId }: { spaceId: number; pageId: number }) {
  const space = useSpace(spaceId)
  const tree = usePages({ spaceId, tree: true })
  const nodes = (tree.data as PageTreeNode[] | undefined) ?? []
  const chain = findAncestorChain(nodes, pageId) ?? []

  return (
    <nav
      aria-label="Breadcrumb"
      className="flex items-center min-w-0 text-[length:var(--text-sm)] text-[var(--text-muted)] font-[family-name:var(--font-sans)]"
    >
      <Link
        to="/spaces/$spaceId"
        params={{ spaceId }}
        className="truncate hover:text-[var(--text-primary)] hover:underline underline-offset-2"
      >
        {space.data?.name ?? 'Space'}
      </Link>
      {chain.map((node, idx) => {
        const isLast = idx === chain.length - 1
        return (
          <span
            key={node.id}
            className="flex items-center min-w-0"
            aria-current={isLast ? 'page' : undefined}
          >
            <ChevronRight
              aria-hidden
              width={14}
              height={14}
              className="mx-[var(--space-1)] shrink-0"
            />
            {isLast ? (
              <span className="truncate text-[var(--text-primary)]">
                {node.title || 'Untitled'}
              </span>
            ) : (
              <Link
                to="/spaces/$spaceId/pages/$pageId"
                params={{ spaceId, pageId: node.id }}
                className="truncate hover:text-[var(--text-primary)] hover:underline underline-offset-2"
              >
                {node.title || 'Untitled'}
              </Link>
            )}
          </span>
        )
      })}
    </nav>
  )
}

function PageLoading() {
  return (
    <div className="flex-1 flex flex-col gap-[var(--space-4)] p-[var(--space-7)] max-w-[48rem] w-full self-center">
      <div className="h-[calc(var(--space-8)+var(--space-3))] w-2/3 rounded-[var(--radius-sm)] bg-[var(--surface-2)]" />
      <div className="flex-1 min-h-[calc(var(--space-8)*4)] rounded-[var(--radius-md)] bg-[var(--surface-2)]" />
    </div>
  )
}

function PageError({ onRetry }: { onRetry: () => void }) {
  return (
    <div className="flex-1 flex items-center justify-center p-[var(--space-7)]">
      <div className="flex flex-col items-center gap-[var(--space-3)] text-center">
        <p className="m-0 text-[length:var(--text-base)] text-[var(--danger)]">
          Couldn't load this page.
        </p>
        <Button variant="secondary" onClick={onRetry}>
          Retry
        </Button>
      </div>
    </div>
  )
}

function PageNotFound({ spaceId }: { spaceId: number }) {
  return (
    <div className="flex-1 flex items-center justify-center p-[var(--space-7)]">
      <div className="flex flex-col items-center gap-[var(--space-3)] text-center max-w-[28rem]">
        <h2 className="m-0 text-[length:var(--text-xl)] leading-[var(--leading-tight)] font-[family-name:var(--font-sans)] text-[var(--text-primary)]">
          Page not found
        </h2>
        <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)]">
          The page may have been deleted or moved to another space.
        </p>
        <Button asChild variant="secondary">
          <Link to="/spaces/$spaceId" params={{ spaceId }}>
            Back to space
          </Link>
        </Button>
      </div>
    </div>
  )
}

interface DeletePageConfirmDialogProps {
  page: Page
  spaceId: number
  open: boolean
  onOpenChange: (next: boolean) => void
  onDeleted: () => void
}

function DeletePageConfirmDialog({
  page,
  spaceId,
  open,
  onOpenChange,
  onDeleted,
}: DeletePageConfirmDialogProps) {
  const [error, setError] = useState<string | null>(null)
  const deletePage = useDeletePage()

  function handleClose(next: boolean) {
    if (!next) setError(null)
    onOpenChange(next)
  }

  async function handleDelete() {
    setError(null)
    try {
      await deletePage.mutateAsync({ id: page.id, spaceId })
      handleClose(false)
      onDeleted()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to delete page.')
    }
  }

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Delete this page?</DialogTitle>
          <DialogDescription>
            "{page.title || 'Untitled'}" and any child pages will be permanently
            removed. This action cannot be undone.
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
