import {
  Suspense,
  lazy,
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from 'react'
import { Link, useNavigate, useParams, useSearch } from '@tanstack/react-router'
import { useQueryClient } from '@tanstack/react-query'
import {
  BookOpen,
  ChevronRight,
  FileDown,
  FileQuestion,
  FileText,
  Hash,
  History,
  Link2,
  MessageSquare,
  MoreHorizontal,
  Plus,
  Share2,
  Trash2,
  TriangleAlert,
} from 'lucide-react'
import type { EditorView } from '@milkdown/kit/prose/view'
import { ApiError } from '../../lib/api'
import { pushRecentPage } from '../../lib/recentPages'
import { buildWikilinkResolveIndex, pageSlug } from '../../lib/slug'
import type { TelaProvider } from '../../lib/collab/tela-provider'
import { captureAnchor, type CommentAnchor } from '../../lib/comments/anchor'
import { scrollAndFlashPanelThread } from '../../lib/comments/coordination'
import { useComments } from '../../lib/comments/use-comments'
import { useMe } from '../../lib/queries/auth'
import { useSpaceMembers } from '../../lib/queries/members'
import { useRevision } from '../../lib/queries/page-revisions'
import { AttachmentStrip } from './AttachmentStrip'
import { CommentsPanel } from './CommentsPanel'
import { PageProperties } from './PageProperties'
import { LocalGraphCard } from './LocalGraphCard'
import { LocalGraphPanel } from './LocalGraphPanel'
import { PresenceAvatars } from './presence-avatars'
import { FavoriteStar } from './FavoriteStar'
import { FollowButton } from './FollowButton'
import { WikilinkHoverPreview } from './wikilink-hover-preview'

// ShareManagerSheet only opens when an editor+ clicks the header Share button;
// its Sheet + create-form + management hooks + Lucide icons are ~15 KB raw that
// shouldn't ride in the main entry chunk on every paint. Split into its own
// lazy chunk (loaded on first Share click).
const ShareManagerSheet = lazy(() =>
  import('./ShareManagerSheet').then((m) => ({ default: m.ShareManagerSheet })),
)
import {
  prefetchPage,
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
import { EmptyState } from '../ui/empty-state'
import {
  Card,
  CardDescription,
  CardFooter,
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
import { SaveIndicator, type SaveStatus } from '../ui/save-indicator'
import { VisibilityBadge } from '../ui/visibility-badge'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '../ui/dropdown-menu'
import { cn } from '../../lib/utils'
import { BacklinksSection } from './BacklinksSection'
import { useFileDownload } from './use-file-download'

// Milkdown is the largest dependency in the app (~700 KB raw). Lazy-load it so
// non-editor routes (sidebar, spaces list, command palette) don't pay for it
// on first paint. M13.3b — the editor also owns the Excalidraw Edit Sheet
// lazy chunk internally, so opening a diagram never lands a byte in main.
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

// Persisted preference: "show resolved comments" on/off. Stored as a single
// app-wide flag rather than per-page since reviewers usually want a stable
// global default ("I always want to see resolved threads" vs "I never do").
const SHOW_RESOLVED_STORAGE_KEY = 'tela:comments:show-resolved'

interface PageViewProps {
  spaceId: number
  pageId: number
}

export function PageView({ spaceId, pageId }: PageViewProps) {
  const page = usePage(pageId)
  const navigate = useNavigate()
  // M9.3 — soft-draft mode is opted into via `?draft=$revId`. The param is
  // typed by the route's validateSearch in router.tsx.
  const { draft: draftRevId } = useSearch({
    from: '/_app/spaces/$spaceId/pages/$pageId/{-$slug}',
  })
  const { slug: currentSlug } = useParams({
    from: '/_app/spaces/$spaceId/pages/$pageId/{-$slug}',
  })

  // Confluence-style canonical URL: keep the address bar at /pages/{id}/{slug}
  // (or bare when the title yields no slug), refreshed on rename. This is a
  // param change on the SAME route, so it never remounts the editor.
  const persistedTitle = page.data?.title ?? ''
  const inThisSpace = page.data?.space_id === spaceId
  useEffect(() => {
    if (!page.data || !inThisSpace) return
    const desired = pageSlug(persistedTitle)
    if (desired === (currentSlug ?? '')) return
    void navigate({
      to: '/spaces/$spaceId/pages/$pageId/{-$slug}',
      params: { spaceId, pageId, slug: desired || undefined },
      replace: true,
      search: (prev) => prev,
    })
  }, [page.data, inThisSpace, persistedTitle, currentSlug, spaceId, pageId, navigate])

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
      draftRevId={draftRevId ?? null}
      onDeleted={() =>
        void navigate({ to: '/spaces/$spaceId', params: { spaceId } })
      }
    />
  )
}

// PageActionsMenu — the header "•••" overflow. Keeps the bar to its frequent
// actions (Comments, Share) and tucks the rest here, Confluence-style. "Copy
// link" is the primary, obvious action (pretty /p/{id}/{slug}); "Copy short
// link" (bare /p/{id}) is present but deliberately demoted so people don't grab
// the opaque one by default. Links are built off window.location.origin and
// resolve via the canonical id, surviving rename.
function PageActionsMenu({
  spaceId,
  pageId,
  title,
  isViewer,
  onDelete,
}: {
  spaceId: number
  pageId: number
  title: string
  isViewer: boolean
  onDelete: () => void
}) {
  const navigate = useNavigate()
  const { download: downloadPdf } = useFileDownload(`/api/pages/${pageId}/pdf`, {
    themed: true,
  })
  const { download: downloadMd } = useFileDownload(`/api/pages/${pageId}/md`, {
    fallbackName: 'page.md',
  })
  const origin = window.location.origin
  const slug = pageSlug(title)
  const pretty = slug ? `${origin}/p/${pageId}/${slug}` : `${origin}/p/${pageId}`
  const short = `${origin}/p/${pageId}`
  const copy = (url: string) => {
    void navigator.clipboard?.writeText?.(url)
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          type="button"
          variant="ghost"
          size="sm"
          aria-label="More actions"
          className="h-[var(--space-8)] w-[var(--space-8)] p-0"
        >
          <MoreHorizontal width={16} height={16} />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="min-w-[12rem]">
        <DropdownMenuItem onSelect={() => copy(pretty)}>
          <Link2 width={14} height={14} /> Copy link
        </DropdownMenuItem>
        <DropdownMenuItem
          onSelect={() => copy(short)}
          className="text-[var(--text-muted)]"
        >
          <Hash width={14} height={14} /> Copy short link
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem
          onSelect={() =>
            void navigate({
              to: '/read/$spaceId/$pageId',
              params: { spaceId, pageId },
            })
          }
        >
          <BookOpen width={14} height={14} /> Read mode
        </DropdownMenuItem>
        <DropdownMenuItem
          onSelect={() =>
            void navigate({ to: '/graph', search: { focus: pageId } })
          }
        >
          <Share2 width={14} height={14} /> Open in graph
        </DropdownMenuItem>
        <DropdownMenuItem onSelect={() => void downloadPdf()}>
          <FileDown width={14} height={14} /> Export PDF
        </DropdownMenuItem>
        <DropdownMenuItem onSelect={() => void downloadMd()}>
          <FileDown width={14} height={14} /> Export Markdown
        </DropdownMenuItem>
        {!isViewer ? (
          <DropdownMenuItem
            onSelect={() =>
              void navigate({
                to: '/spaces/$spaceId/pages/$pageId/history',
                params: { spaceId, pageId },
              })
            }
          >
            <History width={14} height={14} /> Version history
          </DropdownMenuItem>
        ) : null}
        {!isViewer ? (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuItem destructive onSelect={onDelete}>
              <Trash2 width={14} height={14} /> Delete page
            </DropdownMenuItem>
          </>
        ) : null}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

interface PageEditorProps {
  page: Page
  spaceId: number
  draftRevId: number | null
  onDeleted: () => void
}

function PageEditor({ page, spaceId, draftRevId, onDeleted }: PageEditorProps) {
  const updatePage = useUpdatePage()
  const navigate = useNavigate()
  const [title, setTitle] = useState(page.title)
  const [body, setBody] = useState(page.body)
  // Title is a textarea (so long titles wrap instead of clipping); keep its
  // height pinned to its content so it reads like a heading, not a box.
  const titleRef = useRef<HTMLTextAreaElement>(null)
  const fitTitleHeight = useCallback(() => {
    const el = titleRef.current
    if (!el) return
    el.style.height = 'auto'
    el.style.height = `${el.scrollHeight}px`
  }, [])
  useLayoutEffect(fitTitleHeight, [fitTitleHeight, title])
  const [deleteOpen, setDeleteOpen] = useState(false)
  // Container for wikilink hover-preview delegation (covers the editor body).
  const contentRef = useRef<HTMLDivElement>(null)

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
  const isSpaceOwner = myMembership?.role === 'owner'

  // M9.3 — soft-draft mode. Owner-only (Q30): non-owners that paste a
  // ?draft=N URL silently drop back to normal mode. We can't compute
  // `isDraftMode` until role resolves; until then, treat as not-in-draft so
  // the regular editor / viewer-empty branches behave as before. A
  // useEffect emits the one-shot console.warn for non-owner URL paste so
  // the param doesn't disappear silently from the developer's POV.
  const isDraftMode = draftRevId != null && roleResolved && isSpaceOwner
  const draftRevisionQuery = useRevision({
    pageId: page.id,
    revId: draftRevId,
    enabled: isDraftMode,
  })
  useEffect(() => {
    if (draftRevId != null && roleResolved && !isSpaceOwner) {
      console.warn(
        `tela: ?draft=${draftRevId} ignored — only space owners can open a revision as draft.`,
      )
    }
  }, [draftRevId, roleResolved, isSpaceOwner])

  // Seed the editor with the revision body + title exactly once per draft
  // entry. A ref tracks which revision we've already seeded so React's
  // strict-mode double-invoke or any unrelated re-render can't clobber
  // in-flight edits. Cleared when draft mode exits.
  const seededForRevIdRef = useRef<number | null>(null)
  useEffect(() => {
    if (!isDraftMode) {
      seededForRevIdRef.current = null
      return
    }
    if (draftRevId == null) return
    if (seededForRevIdRef.current === draftRevId) return
    const rev = draftRevisionQuery.data
    if (!rev) return
    seededForRevIdRef.current = draftRevId
    // One-shot seed per draft entry (guarded above) — correct-by-design
    // effect-driven setState (memory.md "set-state-in-effect snapshot pattern").
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setTitle(rev.title)
    setBody(rev.body)
  }, [isDraftMode, draftRevId, draftRevisionQuery.data])

  const stripDraftParam = useCallback(() => {
    void navigate({
      to: '/spaces/$spaceId/pages/$pageId/{-$slug}',
      params: { spaceId, pageId: page.id, slug: undefined },
      search: {},
      replace: true,
    })
  }, [navigate, spaceId, page.id])

  // M8.3 — comments surface. The panel toggle, panel itself, and selection
  // bridge are all gated behind a non-viewer role; viewers don't see the
  // button at all and the editor's selection bridge is unused. The editor's
  // PM view ref is owned here so the composer's captureAnchor() reads the
  // live selection at submit time without prop-drilling the view down.
  const editorViewRef = useRef<EditorView | null>(null)
  const [commentsOpen, setCommentsOpen] = useState(false)
  const [graphOpen, setGraphOpen] = useState(false)
  const [shareOpen, setShareOpen] = useState(false)
  const [selectionEmpty, setSelectionEmpty] = useState(true)
  const [selectionPreview, setSelectionPreview] = useState('')
  const handleViewReady = useCallback((view: EditorView | null) => {
    editorViewRef.current = view
  }, [])
  const handleSelectionChange = useCallback(
    ({ isEmpty, text }: { isEmpty: boolean; text: string }) => {
      setSelectionEmpty(isEmpty)
      setSelectionPreview(text)
    },
    [],
  )
  const captureCurrentAnchor = useCallback((): CommentAnchor | null => {
    const view = editorViewRef.current
    if (!view) return null
    const { from, to } = view.state.selection
    if (from === to) return null
    try {
      return captureAnchor(view, from, to)
    } catch {
      return null
    }
  }, [])
  // Drives the "Comments (N)" badge on the header toggle, feeds the
  // CommentsPanel, and (M8.4) feeds the in-body anchor-decoration plugin.
  // Shared queryKey so all three consume the same cache; skipped entirely
  // for viewers (backend 403s them).
  const commentsForHeader = useComments({
    pageId: page.id,
    enabled: roleResolved && !isViewer,
  })
  const openThreadCount = useMemo(() => {
    const data = commentsForHeader.data
    if (!data) return null
    return data.reduce((n, t) => n + (t.root.resolved ? 0 : 1), 0)
  }, [commentsForHeader.data])

  // M8.4 — orphan-thread map. The editor's anchor-decoration plugin reports
  // each thread's resolution after every rebuild (debounced 250ms on
  // typing, immediate on thread-list change). null = no match in the
  // current text → render an "Orphaned" tag in the panel. Stored as a
  // sorted-ish array of ids in a Set for O(1) lookup per thread row.
  const [orphanIds, setOrphanIds] = useState<Set<number>>(() => new Set())
  const handleAnchorsResolved = useCallback(
    (resolutions: Map<number, { from: number; to: number } | null>) => {
      const next = new Set<number>()
      for (const [id, range] of resolutions) {
        if (range === null) next.add(id)
      }
      // Avoid setState if the new set is identical to the previous one —
      // a same-size, same-membership Set should not retrigger renders.
      setOrphanIds((prev) => {
        if (prev.size !== next.size) return next
        for (const id of next) if (!prev.has(id)) return next
        return prev
      })
    },
    [],
  )

  // M8.4 — body-click → panel-open + scroll. The decoration plugin fires
  // this when the user clicks any .tela-comment-anchor span. Opens the
  // Sheet if closed and scrolls/flashes the matching thread row. A short
  // setTimeout gives Radix one frame to mount the row's DOM before the
  // scroll attempt.
  const handleAnchorClick = useCallback((threadId: number) => {
    setCommentsOpen(true)
    window.setTimeout(() => {
      scrollAndFlashPanelThread(threadId)
    }, 50)
  }, [])

  // M8.5 — "Show resolved" filter. Lifted into PageView so the editor's
  // anchor decoration plugin and the panel's thread list stay in lockstep:
  // the body underline mutes for resolved threads only while this is true.
  // Persisted under a single localStorage key so the preference survives
  // reloads but doesn't bleed across pages (one rule for all comments).
  const [showResolvedComments, setShowResolvedComments] = useState<boolean>(
    () => {
      try {
        return localStorage.getItem(SHOW_RESOLVED_STORAGE_KEY) === '1'
      } catch {
        return false
      }
    },
  )
  const handleShowResolvedChange = useCallback((next: boolean) => {
    setShowResolvedComments(next)
    try {
      localStorage.setItem(SHOW_RESOLVED_STORAGE_KEY, next ? '1' : '0')
    } catch {
      // Private-mode / quota — fall through; UX still works for this session.
    }
  }, [])

  // Pass an empty array (not undefined) to the editor when comments are
  // enabled — see milkdown-editor.tsx commentsEnabled doc: the editor
  // builder closure captures the prop ONCE at build time, so undefined-now
  // would never upgrade to plugin-mounted when the query later returns.
  // Memoized so its identity is stable across unrelated re-renders (e.g. typing
  // the title) — otherwise a fresh [] every render defeats MilkdownEditor's
  // React.memo and the editor re-renders on each keystroke (triggering a
  // flushSync storm from the prosemirror-adapter that steals title focus).
  const commentThreadsForEditor = useMemo(
    () =>
      roleResolved && !isViewer ? (commentsForHeader.data ?? []) : undefined,
    [roleResolved, isViewer, commentsForHeader.data],
  )

  // M7.4 — collab provider, lifted into PageView so the header
  // <PresenceAvatars /> and the local-awareness user-state seeding share
  // the exact provider instance Milkdown is binding y-prosemirror against.
  // `provider` is set via the MilkdownEditor onCollabReady callback once
  // the editor mounts and the provider is constructed. It's nulled by the
  // PageEditor remount on page change (parent key=page.id) — no manual
  // teardown needed here.
  const [provider, setProvider] = useState<TelaProvider | null>(null)
  useEffect(() => {
    if (!provider) return
    if (!me.data) return
    const myId = me.data.id
    const myUsername = me.data.username
    // colorIdx = stable per-user hue index into --collab-cursor-{1..8}. id%8
    // matches the brief; deterministic across reloads so a peer's avatar /
    // cursor colour doesn't drift.
    const seedLocal = () => {
      provider.awareness.setLocalState({
        user: {
          id: myId,
          username: myUsername,
          colorIdx: myId % 8,
        },
      })
    }
    if (provider.getStatus() === 'connected') seedLocal()
    return provider.onStatus((status) => {
      if (status === 'connected') seedLocal()
    })
  }, [provider, me.data])

  // Always open a page scrolled to the top. Without this, the content scroller
  // can retain a previous scroll position on refresh / sidebar navigation, so
  // the page title isn't visible on open. Runs once per page (PageEditor
  // remounts on page.id via its key).
  useEffect(() => {
    document
      .querySelector('[data-page-scroll]')
      ?.scrollTo({ top: 0, left: 0 })
  }, [page.id])

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

  // Slug→id map for `[[Name]]` bracket-wikilink resolution, scoped to this
  // page's space to match the backend's space-scoped backlink resolution.
  const wikilinkResolveIndex = useMemo<Map<string, number> | null>(() => {
    if (!allPagesData) return null
    return buildWikilinkResolveIndex(
      allPagesData.filter((p) => p.space_id === page.space_id),
    )
  }, [allPagesData, page.space_id])

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
    async (patch: { title?: string; body?: string }): Promise<boolean> => {
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
        return true
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
          return false
        }
        setStatus('error')
        return false
      }
    },
    [page.id, updatePage],
  )

  const handleTitleBlur = useCallback(() => {
    // In draft mode the user commits explicitly via the Save button; blurring
    // the title input must not persist.
    if (isDraftMode) return
    const trimmed = title.trim()
    if (trimmed === lastSavedRef.current.title) return
    if (!trimmed) {
      // Empty title — revert to last saved value rather than persisting blank.
      setTitle(lastSavedRef.current.title)
      return
    }
    void save({ title: trimmed })
  }, [title, save, isDraftMode])

  // Debounced body auto-save: schedule a save 500ms after the last keystroke;
  // blur cancels the timer and fires immediately if there's a pending change.
  // In draft mode we still update local `body` state but suppress the
  // debounced save — user commits via the explicit Save button.
  const handleBodyChange = useCallback(
    (next: string) => {
      setBody(next)
      if (isDraftMode) return
      cancelPendingSave()
      debounceRef.current = window.setTimeout(() => {
        debounceRef.current = null
        if (next === lastSavedRef.current.body) return
        void save({ body: next })
      }, BODY_DEBOUNCE_MS)
    },
    [save, cancelPendingSave, isDraftMode],
  )

  // Read latest body via ref so onBlur, which is wired into Milkdown's
  // listener once at mount, always sees the most recent value.
  const bodyRef = useRef(body)
  bodyRef.current = body

  const handleBodyBlur = useCallback(() => {
    if (isDraftMode) return
    cancelPendingSave()
    const current = bodyRef.current
    if (current === lastSavedRef.current.body) return
    void save({ body: current })
  }, [save, cancelPendingSave, isDraftMode])

  // M9.3 — explicit draft commit. Force-flushes any in-flight debounce
  // (no-op in draft mode since we suppressed auto-save, but cheap and safe)
  // then PATCHes title + body via the existing save flow. On success the
  // ?draft= param is stripped — PageView re-renders and the editor remounts
  // (key flips from `draft-N` to `live`) with the now-canonical body.
  const handleDraftSave = useCallback(async () => {
    cancelPendingSave()
    const trimmedTitle = title.trim() || lastSavedRef.current.title
    const ok = await save({ title: trimmedTitle, body })
    if (!ok) return
    stripDraftParam()
  }, [cancelPendingSave, title, body, save, stripDraftParam])

  const handleDraftCancel = useCallback(() => {
    stripDraftParam()
  }, [stripDraftParam])

  // autoFocus rule: empty title → focus title; non-empty → focus body.
  const titleAutoFocus = page.title.length === 0
  const bodyAutoFocus = page.title.length > 0

  return (
    <div className="flex-1 flex flex-col min-h-0">
      <header className="flex items-center justify-between gap-[var(--space-4)] px-[var(--space-6)] py-[var(--space-3)] border-b border-[var(--border-subtle)] shrink-0">
        <Breadcrumb spaceId={spaceId} pageId={page.id} />
        <div className="flex items-center gap-[var(--space-3)]">
          {isDraftMode ? (
            <>
              <Button
                type="button"
                variant="ghost"
                size="sm"
                onClick={handleDraftCancel}
                className="h-[var(--space-8)] px-[var(--space-3)]"
              >
                Cancel
              </Button>
              <Button
                type="button"
                variant="primary"
                size="sm"
                onClick={() => void handleDraftSave()}
                disabled={status === 'saving' || status === 'retrying'}
                className="h-[var(--space-8)] px-[var(--space-3)]"
              >
                {status === 'saving' || status === 'retrying'
                  ? 'Saving…'
                  : 'Save'}
              </Button>
            </>
          ) : (
            <>
              <PresenceAvatars awareness={provider?.awareness ?? null} />
              <SaveIndicator status={status} />
              {roleResolved ? <FavoriteStar pageId={page.id} /> : null}
              {roleResolved ? <FollowButton pageId={page.id} /> : null}
              <PageProperties props={page.props} />
              {/* Frequent actions stay on the bar; the rest live in the "•••"
                  menu (PageActionsMenu) to keep the header uncluttered. */}
              {roleResolved && !isViewer ? (
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  aria-label="Comments"
                  onClick={() => {
                    setGraphOpen(false)
                    setCommentsOpen(true)
                  }}
                  className="h-[var(--space-8)] px-[var(--space-3)]"
                >
                  <MessageSquare width={16} height={16} />
                  {openThreadCount != null ? (
                    <span>Comments ({openThreadCount})</span>
                  ) : (
                    <span>Comments</span>
                  )}
                </Button>
              ) : null}
              {roleResolved ? (
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  aria-label="Graph"
                  title="Graph — this page's connections"
                  onClick={() => {
                    setCommentsOpen(false)
                    setGraphOpen(true)
                  }}
                  className="h-[var(--space-8)] w-[var(--space-8)] p-0"
                >
                  <Share2 width={16} height={16} />
                </Button>
              ) : null}
              {/* Visibility pill — ambient exposure indicator; editors click to
                  manage sharing, viewers see a static status chip. */}
              {roleResolved && !isViewer ? (
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  aria-label="Manage sharing"
                  onClick={() => setShareOpen(true)}
                  className="h-[var(--space-8)] px-[var(--space-2)]"
                >
                  <VisibilityBadge
                    state={page.exposure?.state ?? 'private'}
                    inherited={page.exposure?.inherited ?? false}
                  />
                </Button>
              ) : null}
              {roleResolved && isViewer ? (
                <VisibilityBadge
                  state={page.exposure?.state ?? 'private'}
                  inherited={page.exposure?.inherited ?? false}
                  className="ml-[var(--space-1)]"
                />
              ) : null}
              {roleResolved ? (
                <PageActionsMenu
                  spaceId={spaceId}
                  pageId={page.id}
                  title={page.title}
                  isViewer={isViewer}
                  onDelete={() => setDeleteOpen(true)}
                />
              ) : null}
            </>
          )}
        </div>
      </header>

      <div
        ref={contentRef}
        className="flex-1 flex flex-col gap-[var(--space-4)] p-[var(--space-7)] max-w-[48rem] w-full self-center min-h-0"
      >
        <WikilinkHoverPreview containerRef={contentRef} />
        {isDraftMode ? (
          <div
            role="status"
            className={cn(
              'flex items-center gap-[var(--space-2)]',
              'bg-[var(--surface-2)] border border-[var(--border-subtle)]',
              'rounded-[var(--radius-sm)]',
              'px-[var(--space-3)] py-[var(--space-2)]',
              'text-[length:var(--text-sm)] text-[var(--text-muted)]',
            )}
          >
            <History aria-hidden width={14} height={14} />
            <span>
              Restoring Revision #{draftRevId} · review and press Save to
              commit, or Cancel to abandon.
            </span>
          </div>
        ) : null}

        <textarea
          ref={titleRef}
          rows={1}
          autoFocus={titleAutoFocus && !isDraftMode}
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          onBlur={handleTitleBlur}
          onKeyDown={(e) => {
            // Titles are single-logical-line: Enter commits and moves on
            // rather than inserting a newline.
            if (e.key === 'Enter') {
              e.preventDefault()
              e.currentTarget.blur()
            }
          }}
          placeholder="Untitled page"
          aria-label="Page title"
          className={cn(
            'block w-full resize-none overflow-hidden bg-transparent',
            'rounded-[var(--radius-md)] border border-transparent outline-none',
            'px-[var(--space-2)] py-[var(--space-2)]',
            'text-[length:var(--text-3xl)] leading-[var(--leading-tight)] font-medium',
            'text-[var(--text-primary)] placeholder:text-[var(--text-muted)]',
            'focus-visible:border-[var(--border-subtle)]',
          )}
        />

        {!isDraftMode ? <AttachmentStrip pageId={page.id} editable /> : null}

        {isDraftMode ? (
          draftRevisionQuery.isError ? (
            <Card>
              <CardHeader>
                <CardTitle>Couldn't load revision</CardTitle>
                <CardDescription>
                  Revision #{draftRevId} couldn't be retrieved. Cancel to
                  return to the live page.
                </CardDescription>
              </CardHeader>
              <CardFooter>
                <Button
                  type="button"
                  variant="secondary"
                  onClick={handleDraftCancel}
                >
                  Cancel draft
                </Button>
              </CardFooter>
            </Card>
          ) : !draftRevisionQuery.data ? (
            <EditorFallback />
          ) : (
            <Suspense fallback={<EditorFallback />}>
              <MilkdownEditor
                key={`draft-${draftRevId}`}
                defaultValue={draftRevisionQuery.data.body}
                onChange={handleBodyChange}
                onBlur={handleBodyBlur}
                autoFocus={true}
                ariaLabel="Page body (draft)"
                className={EDITOR_MIN_H}
                aliveWikilinkIds={aliveWikilinkIds}
                wikilinkResolveIndex={wikilinkResolveIndex}
                collabPageId={null}
                readOnly={false}
                pageId={page.id}
                spaceId={spaceId}
              />
            </Suspense>
          )
        ) : roleResolved ? (
          <Suspense fallback={<EditorFallback />}>
            <MilkdownEditor
              key="live"
              defaultValue={page.body}
              onChange={handleBodyChange}
              onBlur={handleBodyBlur}
              autoFocus={bodyAutoFocus}
              ariaLabel="Page body"
              className={EDITOR_MIN_H}
              aliveWikilinkIds={aliveWikilinkIds}
              wikilinkResolveIndex={wikilinkResolveIndex}
              collabPageId={isViewer ? null : page.id}
              readOnly={isViewer}
              onCollabReady={setProvider}
              onViewReady={isViewer ? undefined : handleViewReady}
              onSelectionChange={isViewer ? undefined : handleSelectionChange}
              commentThreads={commentThreadsForEditor}
              onAnchorClick={isViewer ? undefined : handleAnchorClick}
              onAnchorsResolved={isViewer ? undefined : handleAnchorsResolved}
              showResolvedAnchors={showResolvedComments}
              pageId={page.id}
              spaceId={spaceId}
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

        <LocalGraphCard pageId={page.id} />
      </div>

      <DeletePageConfirmDialog
        page={page}
        spaceId={spaceId}
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        onDeleted={onDeleted}
      />

      {roleResolved ? (
        <LocalGraphPanel
          pageId={page.id}
          open={graphOpen}
          onOpenChange={setGraphOpen}
        />
      ) : null}

      {roleResolved && !isViewer && !isDraftMode && me.data ? (
        <CommentsPanel
          pageId={page.id}
          open={commentsOpen}
          onOpenChange={setCommentsOpen}
          hasSelection={!selectionEmpty}
          captureAnchor={captureCurrentAnchor}
          anchorPreview={selectionEmpty ? null : selectionPreview}
          me={{ id: me.data.id, username: me.data.username }}
          isSpaceOwner={isSpaceOwner}
          orphanIds={orphanIds}
          showResolved={showResolvedComments}
          onShowResolvedChange={handleShowResolvedChange}
        />
      ) : null}

      {roleResolved && !isViewer && !isDraftMode ? (
        <Suspense fallback={null}>
          <ShareManagerSheet
            pageId={page.id}
            open={shareOpen}
            onOpenChange={setShareOpen}
          />
        </Suspense>
      ) : null}
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
  const qc = useQueryClient()
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
        to: '/spaces/$spaceId/pages/$pageId/{-$slug}',
        params: { spaceId, pageId: created.id, slug: undefined },
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
                onMouseEnter={() => prefetchPage(qc, child.id)}
                onFocus={() => prefetchPage(qc, child.id)}
                onClick={() =>
                  void navigate({
                    to: '/spaces/$spaceId/pages/$pageId/{-$slug}',
                    params: { spaceId, pageId: child.id, slug: undefined },
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
                to="/spaces/$spaceId/pages/$pageId/{-$slug}"
                params={{ spaceId, pageId: node.id, slug: undefined }}
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
    <EmptyState
      icon={TriangleAlert}
      tone="danger"
      title="Couldn't load this page"
      description="Something went wrong reaching the server. Try again."
      actions={
        <Button variant="secondary" onClick={onRetry}>
          Retry
        </Button>
      }
    />
  )
}

function PageNotFound({ spaceId }: { spaceId: number }) {
  return (
    <EmptyState
      icon={FileQuestion}
      title="Page not found"
      description="The page may have been deleted or moved to another space."
      actions={
        <Button asChild variant="secondary">
          <Link to="/spaces/$spaceId" params={{ spaceId }}>
            Back to space
          </Link>
        </Button>
      }
    />
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
