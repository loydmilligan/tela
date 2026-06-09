import { useCallback, useMemo, useState } from 'react'
import { Link, useNavigate, useParams } from '@tanstack/react-router'
import { useQueryClient } from '@tanstack/react-query'
import { ChevronRight, History } from 'lucide-react'
import { ApiError, api } from '../../lib/api'
import { usePage } from '../../lib/queries/pages'
import { useSpace, useSpaceRole } from '../../lib/queries/spaces'
import {
  revisionKeys,
  useRevision,
  useRevisions,
  type PageRevision,
} from '../../lib/queries/page-revisions'
import { parseSqliteTs } from '../../lib/types'
import { relativeTimeFromSqlite } from '../../lib/relativeTime'
import { Badge } from '../ui/badge'
import { Button } from '../ui/button'
import {
  Card,
  CardBody,
  CardDescription,
  CardHeader,
  CardTitle,
} from '../ui/card'
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '../ui/dialog'
import { cn } from '../../lib/utils'
import { DiffViewer } from './DiffViewer'

const REVISIONS_PAGE_SIZE = 50

interface PageHistoryViewProps {
  spaceId: number
  pageId: number
}

// Thin wrapper exposed to TanStack Router's lazyRouteComponent so the entire
// history surface (this module + its DiffViewer dep) ships as its own chunk
// and the main bundle doesn't carry code only used on /history.
export function PageHistoryRoute() {
  const { spaceId, pageId } = useParams({
    from: '/_app/spaces/$spaceId/pages/$pageId/history',
  })
  return <PageHistoryView spaceId={spaceId} pageId={pageId} />
}

export function PageHistoryView({ spaceId, pageId }: PageHistoryViewProps) {
  const page = usePage(pageId)

  if (page.isError) {
    const status = page.error instanceof ApiError ? page.error.status : null
    if (status === 404) return <HistoryNotFound spaceId={spaceId} />
    return <HistoryError onRetry={() => void page.refetch()} />
  }
  if (page.isLoading || !page.data) return <HistoryLoading />
  if (page.data.space_id !== spaceId) return <HistoryNotFound spaceId={spaceId} />

  return (
    <PageHistoryBody
      pageId={page.data.id}
      pageTitle={page.data.title}
      spaceId={spaceId}
    />
  )
}

interface PageHistoryBodyProps {
  pageId: number
  pageTitle: string
  spaceId: number
}

function PageHistoryBody({ pageId, pageTitle, spaceId }: PageHistoryBodyProps) {
  const { resolved: roleResolved, isViewer, isOwner } = useSpaceRole(spaceId)

  if (!roleResolved) return <HistoryLoading />
  if (isViewer)
    return <HistoryViewerEmpty spaceId={spaceId} pageId={pageId} />

  return (
    <PageHistoryAuthed
      pageId={pageId}
      pageTitle={pageTitle}
      spaceId={spaceId}
      isOwner={isOwner}
    />
  )
}

interface PageHistoryAuthedProps {
  pageId: number
  pageTitle: string
  spaceId: number
  isOwner: boolean
}

function PageHistoryAuthed({
  pageId,
  pageTitle,
  spaceId,
  isOwner,
}: PageHistoryAuthedProps) {
  const initial = useRevisions({ pageId })
  const qc = useQueryClient()
  // Accumulated revisions appended by successive 'Load more' clicks. Each
  // click imperatively fetches the next page via queryClient.fetchQuery
  // (cached under its own cursor-scoped key) and pushes the result onto
  // `extraPages` without going through a useQuery hook — keeps the
  // load-more flow effect-free.
  const [extraPages, setExtraPages] = useState<PageRevision[][]>([])
  const [loadingMore, setLoadingMore] = useState(false)

  const allRevisions: PageRevision[] = useMemo(() => {
    const base = initial.data ?? []
    if (extraPages.length === 0) return base
    return [...base, ...extraPages.flat()]
  }, [initial.data, extraPages])

  // The last fetched page determines whether 'Load more' is shown — if
  // fewer rows came back than the limit, there's nothing left to fetch.
  const lastFetchedLength =
    extraPages.length > 0
      ? extraPages[extraPages.length - 1].length
      : (initial.data?.length ?? 0)
  const canLoadMore =
    initial.data != null && lastFetchedLength === REVISIONS_PAGE_SIZE
  const lastId =
    allRevisions.length > 0 ? allRevisions[allRevisions.length - 1].id : null

  const handleLoadMore = useCallback(async () => {
    if (!canLoadMore || lastId == null || loadingMore) return
    setLoadingMore(true)
    try {
      const rows = await qc.fetchQuery({
        queryKey: [...revisionKeys.page(pageId), 'after', lastId] as const,
        queryFn: async () => {
          const { revisions } = await api<{ revisions: PageRevision[] }>(
            `/api/pages/${pageId}/revisions?cursor=${lastId}`,
          )
          return revisions
        },
      })
      setExtraPages((prev) => [...prev, rows])
    } catch {
      // Failure surfaces via the disabled-button reset; v0 has no toast layer.
    } finally {
      setLoadingMore(false)
    }
  }, [canLoadMore, lastId, loadingMore, pageId, qc])

  const [selectedId, setSelectedId] = useState<number | null>(null)
  const selectedRevision = useMemo(
    () => allRevisions.find((r) => r.id === selectedId) ?? null,
    [allRevisions, selectedId],
  )

  const handleRowClick = useCallback((id: number) => {
    setSelectedId((prev) => (prev === id ? null : id))
  }, [])

  return (
    <div className="flex-1 flex flex-col min-h-0">
      <header className="flex items-center justify-between gap-[var(--space-4)] px-[var(--space-6)] py-[var(--space-3)] border-b border-[var(--border-subtle)] shrink-0">
        <HistoryBreadcrumb
          spaceId={spaceId}
          pageId={pageId}
          pageTitle={pageTitle}
        />
        <Button asChild variant="ghost" size="sm">
          <Link
            to="/spaces/$spaceId/pages/$pageId/{-$slug}"
            params={{ spaceId, pageId, slug: undefined }}
          >
            Back to page
          </Link>
        </Button>
      </header>

      <div className="flex-1 grid grid-cols-1 lg:grid-cols-[20rem_1fr] gap-[var(--space-6)] p-[var(--space-7)] max-w-[72rem] w-full self-center min-h-0">
        <section
          aria-label="Revisions"
          className="flex flex-col gap-[var(--space-3)] min-h-0"
        >
          <h2 className="m-0 text-[length:var(--text-xs)] uppercase tracking-wider text-[var(--text-muted)] font-[family-name:var(--font-sans)]">
            Revisions
          </h2>
          {initial.isLoading ? (
            <RevisionListSkeleton />
          ) : initial.isError ? (
            <RevisionListError onRetry={() => void initial.refetch()} />
          ) : allRevisions.length === 0 ? (
            <RevisionListEmpty />
          ) : (
            <ul className="m-0 p-0 list-none flex flex-col gap-[var(--space-1)]">
              {allRevisions.map((rev) => (
                <RevisionRow
                  key={rev.id}
                  revision={rev}
                  selected={rev.id === selectedId}
                  onSelect={handleRowClick}
                />
              ))}
            </ul>
          )}
          {canLoadMore ? (
            <div className="pt-[var(--space-2)]">
              <Button
                type="button"
                variant="secondary"
                size="sm"
                onClick={() => void handleLoadMore()}
                disabled={loadingMore}
              >
                {loadingMore ? 'Loading…' : 'Load more'}
              </Button>
            </div>
          ) : null}
        </section>

        <RevisionPane
          revision={selectedRevision}
          spaceId={spaceId}
          pageId={pageId}
          isOwner={isOwner}
        />
      </div>
    </div>
  )
}

interface RevisionRowProps {
  revision: PageRevision
  selected: boolean
  onSelect: (id: number) => void
}

function RevisionRow({ revision, selected, onSelect }: RevisionRowProps) {
  const { id, author_username, source, byte_size, created_at } = revision
  const absoluteTs = useMemo(
    () => parseSqliteTs(created_at).toLocaleString(),
    [created_at],
  )
  const rel = relativeTimeFromSqlite(created_at)
  const author = author_username ?? 'Unknown'
  const sizeLabel = formatBytes(byte_size)

  return (
    <li className="m-0 p-0 list-none">
      <button
        type="button"
        onClick={() => onSelect(id)}
        aria-pressed={selected}
        data-revision-id={String(id)}
        className={cn(
          'w-full text-left',
          'flex flex-col gap-[var(--space-1)]',
          'px-[var(--space-3)] py-[var(--space-2)]',
          'rounded-[var(--radius-sm)]',
          'border border-transparent',
          'bg-transparent cursor-pointer outline-none',
          'transition-[background-color,border-color] duration-[var(--duration-fast)] ease-[var(--ease-out)]',
          'hover:bg-[var(--surface-2)]',
          'focus-visible:ring-2 focus-visible:ring-[var(--accent)] focus-visible:ring-offset-2 focus-visible:ring-offset-[var(--surface-1)]',
          selected &&
            'bg-[var(--surface-2)] border-[var(--accent)] ring-1 ring-[var(--accent)]',
        )}
      >
        <span
          title={absoluteTs}
          className="text-[length:var(--text-sm)] text-[var(--text-primary)] font-[family-name:var(--font-sans)]"
        >
          {rel}
        </span>
        <span className="flex items-center gap-[var(--space-2)] text-[length:var(--text-xs)] text-[var(--text-muted)] font-[family-name:var(--font-sans)]">
          <span className="truncate">{author}</span>
          <Badge variant="muted">{source}</Badge>
          <span className="truncate">{sizeLabel}</span>
        </span>
      </button>
    </li>
  )
}

interface RevisionPaneProps {
  revision: PageRevision | null
  spaceId: number
  pageId: number
  isOwner: boolean
}

function RevisionPane({
  revision,
  spaceId,
  pageId,
  isOwner,
}: RevisionPaneProps) {
  if (!revision) {
    return (
      <Card className="self-start">
        <CardHeader>
          <CardTitle>Pick a revision</CardTitle>
          <CardDescription>
            Select a revision on the left to see what changed.
          </CardDescription>
        </CardHeader>
        <CardBody>
          <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)]">
            The diff will appear here.
          </p>
        </CardBody>
      </Card>
    )
  }
  return (
    <RevisionPaneSelected
      revision={revision}
      spaceId={spaceId}
      pageId={pageId}
      isOwner={isOwner}
    />
  )
}

interface RevisionPaneSelectedProps {
  revision: PageRevision
  spaceId: number
  pageId: number
  isOwner: boolean
}

function RevisionPaneSelected({
  revision,
  spaceId,
  pageId,
  isOwner,
}: RevisionPaneSelectedProps) {
  const fullRevision = useRevision({ pageId, revId: revision.id })
  const page = usePage(pageId)
  const navigate = useNavigate()
  const ts = parseSqliteTs(revision.created_at).toLocaleString()
  const bothSettled =
    fullRevision.data != null && page.data != null
  const anyError = fullRevision.isError || page.isError

  // M9.3 — Open-as-draft confirmation. State lives per selected revision so
  // switching revisions in the list discards any half-opened dialog.
  const [confirmOpen, setConfirmOpen] = useState(false)
  const handleOpenAsDraft = useCallback(() => {
    setConfirmOpen(false)
    void navigate({
      to: '/spaces/$spaceId/pages/$pageId/{-$slug}',
      params: { spaceId, pageId, slug: undefined },
      search: { draft: revision.id },
    })
  }, [navigate, spaceId, pageId, revision.id])

  return (
    <Card className="self-start">
      <CardHeader>
        <CardTitle>Revision #{revision.id}</CardTitle>
        <CardDescription>
          {revision.author_username ?? 'Unknown'} · {ts}
        </CardDescription>
      </CardHeader>
      <CardBody>
        {anyError ? (
          <p className="m-0 text-[length:var(--text-sm)] text-[var(--danger)]">
            Couldn't load this revision.
          </p>
        ) : !bothSettled ? (
          <DiffPaneSkeleton />
        ) : (
          <DiffViewer
            oldBody={fullRevision.data.body}
            newBody={page.data.body}
            oldLabel={`Revision #${revision.id}`}
            newLabel="Current"
          />
        )}
        {isOwner ? (
          <div className="pt-[var(--space-2)]">
            <Button
              type="button"
              variant="secondary"
              size="sm"
              onClick={() => setConfirmOpen(true)}
            >
              <History width={14} height={14} /> Open as draft
            </Button>
            <Dialog open={confirmOpen} onOpenChange={setConfirmOpen}>
              <DialogContent>
                <DialogHeader>
                  <DialogTitle>
                    Open Revision #{revision.id} as draft?
                  </DialogTitle>
                  <DialogDescription>
                    This will load Revision #{revision.id}'s body into your
                    editor. You can review and edit, then press Save to commit
                    (which creates a new revision) or Cancel to abandon.
                  </DialogDescription>
                </DialogHeader>
                <DialogFooter>
                  <DialogClose asChild>
                    <Button type="button" variant="ghost">
                      Cancel
                    </Button>
                  </DialogClose>
                  <Button
                    type="button"
                    variant="primary"
                    onClick={handleOpenAsDraft}
                  >
                    Open draft
                  </Button>
                </DialogFooter>
              </DialogContent>
            </Dialog>
          </div>
        ) : null}
      </CardBody>
    </Card>
  )
}

function DiffPaneSkeleton() {
  return (
    <div className="flex flex-col gap-[var(--space-2)]">
      <div className="h-[var(--space-6)] w-1/3 rounded-[var(--radius-sm)] bg-[var(--surface-3)]" />
      <div className="h-[var(--space-7)] rounded-[var(--radius-sm)] bg-[var(--surface-3)]" />
      <div className="h-[var(--space-7)] rounded-[var(--radius-sm)] bg-[var(--surface-3)]" />
      <div className="h-[var(--space-7)] rounded-[var(--radius-sm)] bg-[var(--surface-3)]" />
    </div>
  )
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  const kb = bytes / 1024
  if (kb < 1024) return `${kb < 10 ? kb.toFixed(1) : Math.round(kb)} KB`
  const mb = kb / 1024
  return `${mb < 10 ? mb.toFixed(1) : Math.round(mb)} MB`
}

interface HistoryBreadcrumbProps {
  spaceId: number
  pageId: number
  pageTitle: string
}

function HistoryBreadcrumb({
  spaceId,
  pageId,
  pageTitle,
}: HistoryBreadcrumbProps) {
  const space = useSpace(spaceId)
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
      <ChevronRight
        aria-hidden
        width={14}
        height={14}
        className="mx-[var(--space-1)] shrink-0"
      />
      <Link
        to="/spaces/$spaceId/pages/$pageId/{-$slug}"
        params={{ spaceId, pageId, slug: undefined }}
        className="truncate hover:text-[var(--text-primary)] hover:underline underline-offset-2"
      >
        {pageTitle || 'Untitled'}
      </Link>
      <ChevronRight
        aria-hidden
        width={14}
        height={14}
        className="mx-[var(--space-1)] shrink-0"
      />
      <span className="truncate text-[var(--text-primary)]" aria-current="page">
        History
      </span>
    </nav>
  )
}

function HistoryViewerEmpty({
  spaceId,
  pageId,
}: {
  spaceId: number
  pageId: number
}) {
  return (
    <div className="flex-1 flex items-center justify-center p-[var(--space-7)]">
      <Card className="w-full max-w-[28rem] text-center">
        <CardHeader className="items-center">
          <CardTitle>History is editor-only</CardTitle>
          <CardDescription>
            Revisions and comments are visible to editors and owners of this
            space. Ask an owner to upgrade your role if you need access.
          </CardDescription>
        </CardHeader>
        <CardBody className="items-center">
          <Button asChild variant="secondary">
            <Link
              to="/spaces/$spaceId/pages/$pageId/{-$slug}"
              params={{ spaceId, pageId, slug: undefined }}
            >
              Back to page
            </Link>
          </Button>
        </CardBody>
      </Card>
    </div>
  )
}

function HistoryLoading() {
  return (
    <div className="flex-1 flex flex-col gap-[var(--space-4)] p-[var(--space-7)] max-w-[72rem] w-full self-center">
      <div className="h-[var(--space-7)] w-1/2 rounded-[var(--radius-sm)] bg-[var(--surface-2)]" />
      <div className="grid grid-cols-1 lg:grid-cols-[20rem_1fr] gap-[var(--space-6)]">
        <div className="flex flex-col gap-[var(--space-2)]">
          <div className="h-[var(--space-8)] rounded-[var(--radius-sm)] bg-[var(--surface-2)]" />
          <div className="h-[var(--space-8)] rounded-[var(--radius-sm)] bg-[var(--surface-2)]" />
          <div className="h-[var(--space-8)] rounded-[var(--radius-sm)] bg-[var(--surface-2)]" />
        </div>
        <div className="h-[calc(var(--space-8)*4)] rounded-[var(--radius-md)] bg-[var(--surface-2)]" />
      </div>
    </div>
  )
}

function HistoryError({ onRetry }: { onRetry: () => void }) {
  return (
    <div className="flex-1 flex items-center justify-center p-[var(--space-7)]">
      <div className="flex flex-col items-center gap-[var(--space-3)] text-center">
        <p className="m-0 text-[length:var(--text-base)] text-[var(--danger)]">
          Couldn't load this page's history.
        </p>
        <Button variant="secondary" onClick={onRetry}>
          Retry
        </Button>
      </div>
    </div>
  )
}

function HistoryNotFound({ spaceId }: { spaceId: number }) {
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

function RevisionListSkeleton() {
  return (
    <div className="flex flex-col gap-[var(--space-2)]">
      <div className="h-[var(--space-8)] rounded-[var(--radius-sm)] bg-[var(--surface-2)]" />
      <div className="h-[var(--space-8)] rounded-[var(--radius-sm)] bg-[var(--surface-2)]" />
      <div className="h-[var(--space-8)] rounded-[var(--radius-sm)] bg-[var(--surface-2)]" />
    </div>
  )
}

function RevisionListError({ onRetry }: { onRetry: () => void }) {
  return (
    <div className="flex flex-col items-start gap-[var(--space-2)] py-[var(--space-2)]">
      <p className="m-0 text-[length:var(--text-sm)] text-[var(--danger)]">
        Couldn't load revisions.
      </p>
      <Button variant="secondary" size="sm" onClick={onRetry}>
        Retry
      </Button>
    </div>
  )
}

function RevisionListEmpty() {
  return (
    <p className="m-0 py-[var(--space-3)] text-[length:var(--text-sm)] text-[var(--text-muted)]">
      No revisions yet. Save this page once to capture its first revision.
    </p>
  )
}
