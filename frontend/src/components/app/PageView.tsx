import {
  Suspense,
  lazy,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react'
import { Link, useNavigate } from '@tanstack/react-router'
import { ChevronRight, FileText, Plus, Trash2 } from 'lucide-react'
import { ApiError } from '../../lib/api'
import { pushRecentPage } from '../../lib/recentPages'
import { useMe } from '../../lib/queries/auth'
import { useSpaceMembers } from '../../lib/queries/members'
import {
  useAllPages,
  useCreatePage,
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
import { cn } from '../../lib/utils'
import { BacklinksSection } from './BacklinksSection'

// Milkdown is the largest dependency in the app (~700 KB raw). Lazy-load it so
// non-editor routes (sidebar, spaces list, command palette) don't pay for it
// on first paint.
const MilkdownEditor = lazy(() =>
  import('./milkdown-editor').then((m) => ({ default: m.MilkdownEditor })),
)

const EDITOR_MIN_H = 'min-h-[calc(var(--space-8)*8)]'

function EditorFallback() {
  return (
    <div
      role="status"
      aria-busy="true"
      aria-label="Loading editor"
      className={cn(
        EDITOR_MIN_H,
        'p-[var(--space-2)]',
        'rounded-[var(--radius-sm)]',
        'bg-[var(--surface-2)]',
      )}
    />
  )
}

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

  // M7.2 — viewer role gating. The backend rejects ws upgrade for viewers
  // (HTTP 403 + code `viewer_no_write`). We prefer Option A from the task
  // brief: check role up-front via the already-needed space members query
  // and skip opening a ws entirely for viewers — they render the page from
  // pages.body as a non-editable Milkdown. Editors and owners get a live
  // Yjs session via MilkdownEditor's `collabPageId` path.
  const me = useMe()
  const members = useSpaceMembers(spaceId)
  const myMembership = useMemo(
    () =>
      me.data && members.data
        ? members.data.find((m) => m.user_id === me.data!.id) ?? null
        : null,
    [me.data, members.data],
  )
  // Both queries must resolve before we mount the editor — otherwise the
  // role-pending window would mount Milkdown in non-collab mode, then
  // re-mount it in collab mode once role resolves. The re-mount would
  // throw away local PM state and (worse) double-seed the Y.Doc fragment.
  // `useMe` is typically cache-hot from the auth gate; `useSpaceMembers`
  // has 30s staleTime so within-space navigation is also instant. The
  // first page open per session has a brief loading-skeleton window.
  const roleResolved = me.data != null && members.data != null
  const isViewer = roleResolved && myMembership?.role === 'viewer'

  // Alive-page-ids snapshot powers M5.2d broken-wikilink rendering. `null` is
  // the "don't know yet" state — the editor's decoration plugin keeps every
  // wikilink in the alive style until the query lands, so first-paint never
  // flashes everything as broken. Set reference is memoized so unchanged data
  // doesn't keep retriggering the decoration plugin's rebuild path.
  const allPagesQuery = useAllPages()
  const allPagesData = allPagesQuery.data
  const aliveWikilinkIds = useMemo<Set<number> | null>(() => {
    if (!allPagesData) return null
    return new Set(allPagesData.map((p) => p.id))
  }, [allPagesData])

  // Record this visit in the recently-viewed list (consumed by the M5.1
  // palette empty state). Re-fires only when the page id changes — renaming
  // the open page shouldn't bump its position in the recents list, and the
  // cached title goes stale until the user navigates to the page from
  // elsewhere. Acceptable trade-off vs. either pushing on every keystroke or
  // missing rename-after-visit updates.
  useEffect(() => {
    pushRecentPage({
      pageId: page.id,
      spaceId: page.space_id,
      title: page.title,
      viewedAt: Date.now(),
    })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [page.id])

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

        {roleResolved ? (
          <Suspense fallback={<EditorFallback />}>
            <MilkdownEditor
              defaultValue={page.body}
              onChange={handleBodyChange}
              onBlur={handleBodyBlur}
              autoFocus={bodyAutoFocus}
              ariaLabel="Page body"
              className={EDITOR_MIN_H}
              aliveWikilinkIds={aliveWikilinkIds}
              collabPageId={isViewer ? null : page.id}
              readOnly={isViewer}
            />
          </Suspense>
        ) : (
          <EditorFallback />
        )}

        <ChildPagesSection
          spaceId={spaceId}
          pageId={page.id}
          bodyIsEmpty={body.trim().length === 0}
        />

        <BacklinksSection pageId={page.id} />
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

const EXCERPT_MAX = 80

function bodyExcerpt(body: string): string {
  // Strip the most common markdown noise so the snippet reads as plain prose
  // rather than syntax. Good enough for an at-a-glance preview; not a parser.
  const stripped = body
    .replace(/^#{1,6}\s+/gm, '')
    .replace(/`{1,3}[^`]*`{1,3}/g, ' ')
    .replace(/!\[[^\]]*\]\([^)]*\)/g, ' ')
    .replace(/\[([^\]]+)\]\([^)]*\)/g, '$1')
    .replace(/[*_~>]/g, '')
    .replace(/\s+/g, ' ')
    .trim()
  if (stripped.length <= EXCERPT_MAX) return stripped
  return stripped.slice(0, EXCERPT_MAX).trimEnd() + '…'
}

interface ChildPagesSectionProps {
  spaceId: number
  pageId: number
  bodyIsEmpty: boolean
}

function ChildPagesSection({
  spaceId,
  pageId,
  bodyIsEmpty,
}: ChildPagesSectionProps) {
  const navigate = useNavigate()
  const tree = usePages({ spaceId, tree: true })
  const createPage = useCreatePage()
  const nodes = (tree.data as PageTreeNode[] | undefined) ?? []
  const chain = findAncestorChain(nodes, pageId)
  const current = chain ? chain[chain.length - 1] : null
  const children = current?.children ?? []

  async function handleAddChild() {
    try {
      const created = await createPage.mutateAsync({
        space_id: spaceId,
        parent_id: pageId,
        title: 'Untitled',
      })
      void navigate({
        to: '/spaces/$spaceId/pages/$pageId',
        params: { spaceId, pageId: created.id },
      })
    } catch {
      // Tree refetch surfaces failure; v0 has no toast layer.
    }
  }

  if (children.length === 0) {
    if (!bodyIsEmpty) return null
    return (
      <div className="pt-[var(--space-3)] border-t border-[var(--border-subtle)]">
        <Button
          type="button"
          variant="ghost"
          size="sm"
          onClick={() => void handleAddChild()}
          disabled={createPage.isPending}
          className="text-[var(--text-muted)] hover:text-[var(--text-primary)] px-[var(--space-2)]"
        >
          <Plus width={14} height={14} /> Add child page
        </Button>
      </div>
    )
  }

  return (
    <section
      aria-labelledby={`child-pages-${pageId}`}
      className="flex flex-col gap-[var(--space-2)] pt-[var(--space-4)] border-t border-[var(--border-subtle)]"
    >
      <h2
        id={`child-pages-${pageId}`}
        className="m-0 text-[length:var(--text-xs)] uppercase tracking-wider text-[var(--text-muted)] font-[family-name:var(--font-sans)]"
      >
        Child pages
      </h2>
      <ul className="m-0 p-0 list-none flex flex-col gap-[1px]">
        {children.map((child) => {
          const excerpt = bodyExcerpt(child.body)
          return (
            <li key={child.id} className="m-0 p-0 list-none">
              <button
                type="button"
                onClick={() =>
                  void navigate({
                    to: '/spaces/$spaceId/pages/$pageId',
                    params: { spaceId, pageId: child.id },
                  })
                }
                className={cn(
                  'group w-full text-left',
                  'flex items-start gap-[var(--space-3)]',
                  'px-[var(--space-3)] py-[var(--space-2)]',
                  'rounded-[var(--radius-sm)]',
                  'bg-transparent border-0 cursor-pointer outline-none',
                  'hover:bg-[var(--surface-2)]',
                  'focus-visible:ring-2 focus-visible:ring-[var(--accent)]',
                )}
              >
                <FileText
                  aria-hidden
                  width={14}
                  height={14}
                  className="mt-[2px] shrink-0 text-[var(--text-muted)] group-hover:text-[var(--text-primary)]"
                />
                <span className="flex-1 min-w-0 flex flex-col gap-[2px]">
                  <span className="truncate text-[length:var(--text-sm)] text-[var(--text-primary)] font-medium font-[family-name:var(--font-sans)]">
                    {child.title || 'Untitled'}
                  </span>
                  {excerpt ? (
                    <span className="truncate text-[length:var(--text-xs)] text-[var(--text-muted)] font-[family-name:var(--font-sans)]">
                      {excerpt}
                    </span>
                  ) : null}
                </span>
              </button>
            </li>
          )
        })}
      </ul>
    </section>
  )
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
