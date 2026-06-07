import { useEffect, useState } from 'react'
import {
  createRootRoute,
  createRoute,
  createRouter,
  lazyRouteComponent,
  Link,
  Outlet,
  redirect,
  useLocation,
  useNavigate,
  useParams,
} from '@tanstack/react-router'
import { FilePlus, FileQuestion, Menu } from 'lucide-react'
import { AppCommandHost } from '../components/app/AppCommandHost'
import { NotificationBell } from '../components/app/NotificationBell'
import { EmptyState } from '../components/ui/empty-state'
import { PageView } from '../components/app/PageView'
import { Sidebar } from '../components/app/Sidebar'
import { ThemeSwitcher } from '../components/ThemeSwitcher'
import { Button } from '../components/ui/button'
import {
  Card,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from '../components/ui/card'
import { queryClient } from '../lib/queryClient'
import {
  authKeys,
  fetchMe,
  sanitizeNextPath,
  type AuthUser,
} from '../lib/queries/auth'
import { spaceKeys } from '../lib/queries/spaces'
import { api } from '../lib/api'
import { writeLastPage } from '../lib/lastPage'
import { useCreatePage, usePages } from '../lib/queries/pages'
import { LoginPage } from './login'
import { RegisterPage } from './register'
import { VerifyEmailPage } from './verify-email'
import { ForgotPasswordPage } from './forgot-password'
import { ResetPasswordPage } from './reset-password'
import { SettingsPage } from './settings'
import type { Page, PageTreeNode, Space } from '../lib/types'

// Resolve the session once and cache it; both AppLayout and LoginRoute reuse
// the same cached result so a quick login → redirect doesn't trigger two
// /api/auth/me round-trips.
function ensureMe(): Promise<AuthUser | null> {
  return queryClient.ensureQueryData({
    queryKey: authKeys.me(),
    queryFn: fetchMe,
    retry: false,
    staleTime: Infinity,
  })
}

// Bare root: just an Outlet. The login route renders WITHOUT the sidebar
// shell, so we can't bake the shell into the root component anymore.
const rootRoute = createRootRoute({
  component: function Root() {
    return <Outlet />
  },
  notFoundComponent: function NotFound() {
    return (
      <EmptyState
        fullScreen
        icon={FileQuestion}
        title="Page not found"
        description="That page doesn’t exist or you don’t have access to it."
        actions={
          <Button asChild variant="primary" size="lg">
            <Link to="/">Back to tela</Link>
          </Button>
        }
      />
    )
  },
})

// Pathless layout route ("_app") owning every authenticated page. Hosts the
// sidebar + header chrome and the app-level command palette host. Gates
// access via beforeLoad — unauthenticated visitors are bounced to /login.
const appLayoutRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: '_app',
  beforeLoad: async ({ location }) => {
    const user = await ensureMe()
    if (user) return
    // Path + search only. TanStack's `location.href` is already the routed
    // URL (no origin); we strip any accidental origin defensively in case
    // the router ever emits one — protocol-relative `//evil` slips past the
    // `startsWith('/')` check, but the LoginRoute sanitizer rejects those.
    const here = (location.href || '/').replace(window.location.origin, '')
    throw redirect({
      to: '/login',
      search: { next: here },
    })
  },
  component: function AppLayout() {
    const [sidebarOpen, setSidebarOpen] = useState(false)
    const { pathname } = useLocation()
    // Close the mobile drawer on navigation (e.g. tapping a page link).
    useEffect(() => {
      setSidebarOpen(false)
    }, [pathname])
    // Escape closes the drawer.
    useEffect(() => {
      if (!sidebarOpen) return
      const onKey = (e: KeyboardEvent) => {
        if (e.key === 'Escape') setSidebarOpen(false)
      }
      document.addEventListener('keydown', onKey)
      return () => document.removeEventListener('keydown', onKey)
    }, [sidebarOpen])
    return (
      <div className="h-dvh flex bg-[var(--surface-1)] text-[var(--text-primary)] overflow-hidden">
        <Sidebar open={sidebarOpen} />
        {sidebarOpen ? (
          <button
            type="button"
            aria-label="Close navigation"
            className="fixed inset-0 z-40 bg-black/40 md:hidden"
            onClick={() => setSidebarOpen(false)}
          />
        ) : null}
        <div className="flex-1 flex flex-col min-w-0">
          <header className="flex items-center justify-between px-[var(--space-6)] py-[var(--space-3)] border-b border-[var(--border-subtle)] shrink-0">
            <h1 className="m-0 flex items-center gap-[var(--space-2)] text-[length:var(--text-lg)] leading-[var(--leading-tight)] font-[family-name:var(--font-sans)]">
              <button
                type="button"
                aria-label="Open navigation"
                className="md:hidden inline-flex items-center justify-center rounded-[var(--radius-xs)] p-[var(--space-1)] text-[var(--text-muted)] hover:text-[var(--text-primary)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent)]"
                onClick={() => setSidebarOpen(true)}
              >
                <Menu size="1.1em" aria-hidden />
              </button>
              <Link
                to="/"
                aria-label="tela home"
                className="inline-block rounded-[var(--radius-xs)] text-[var(--text-primary)] no-underline transition-opacity duration-[var(--duration-fast)] hover:opacity-70 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent)]"
              >
                tela
              </Link>
            </h1>
            <div className="flex items-center gap-[var(--space-2)]">
              <NotificationBell />
              <ThemeSwitcher />
            </div>
          </header>
          <main
            data-page-scroll
            className="flex-1 flex flex-col overflow-y-auto overscroll-contain min-h-0"
          >
            <Outlet />
          </main>
        </div>
        {/* AppCommandHost is mounted INSIDE the authed layout so its
            useQuery hooks (spaces, search) don't fire on /login and trip
            the global 401-redirect. */}
        <AppCommandHost />
      </div>
    )
  },
})

const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/login',
  validateSearch: (
    search: Record<string, unknown>,
  ): { next?: string; sso_error?: string } => ({
    ...(typeof search.next === 'string' ? { next: search.next } : {}),
    // Surfaced when a federated sign-in bounces back here (SSOCallback failure).
    ...(typeof search.sso_error === 'string'
      ? { sso_error: search.sso_error }
      : {}),
  }),
  beforeLoad: async ({ search }) => {
    const user = await ensureMe()
    if (!user) return
    const next = sanitizeNextPath(search.next) ?? '/'
    // Cast: `next` is a validated in-app path string, not a typed route.
    throw redirect({ to: next as never })
  },
  component: LoginPage,
})

// Self-registration. Like /login, an already-authenticated visitor is bounced
// to the app rather than shown the signup form.
const registerRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/register',
  beforeLoad: async () => {
    const user = await ensureMe()
    if (user) throw redirect({ to: '/' })
  },
  component: RegisterPage,
})

// Email confirmation landing. Reads `?token=` and confirms on mount; no auth
// gate (the token is the authorization).
const verifyEmailRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/verify-email',
  validateSearch: (search: Record<string, unknown>): { token?: string } =>
    typeof search.token === 'string' ? { token: search.token } : {},
  component: VerifyEmailPage,
})

const forgotPasswordRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/forgot-password',
  beforeLoad: async () => {
    const user = await ensureMe()
    if (user) throw redirect({ to: '/' })
  },
  component: ForgotPasswordPage,
})

// Password-reset landing. Reads `?token=`; no auth gate (the token authorizes).
const resetPasswordRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/reset-password',
  validateSearch: (search: Record<string, unknown>): { token?: string } =>
    typeof search.token === 'string' ? { token: search.token } : {},
  component: ResetPasswordPage,
})

// Home dashboard at `/` — the app's landing surface (recent changes, your
// edits, favorites, recently-visited, your spaces). This is where login and the
// logo land. Replaces the old resume-last-page redirect; the dashboard's
// "Recently visited" widget covers the resume case, and it handles the
// no-spaces first-run state itself. Lazy so its feed queries stay off the main
// chunk.
const indexRoute = createRoute({
  getParentRoute: () => appLayoutRoute,
  path: '/',
  component: lazyRouteComponent(
    () => import('../components/app/HomeView'),
    'HomeRoute',
  ),
})

const settingsRoute = createRoute({
  getParentRoute: () => appLayoutRoute,
  path: '/settings',
  component: SettingsPage,
})

// `/n` — quick-capture shortcut. Find-or-creates the caller's "Quick Notes"
// page in their personal space, then redirects to it. Authenticated via the
// parent appLayoutRoute gate (which runs first and bounces to /login?next=/n
// when signed out). Pure redirect — the component never renders.
const quickNotesRoute = createRoute({
  getParentRoute: () => appLayoutRoute,
  path: '/n',
  beforeLoad: async () => {
    const { page } = await api<{ page: Page }>('/api/users/me/quick-notes', {
      method: 'POST',
    })
    throw redirect({
      to: '/spaces/$spaceId/pages/$pageId/{-$slug}',
      params: { spaceId: page.space_id, pageId: page.id, slug: undefined },
    })
  },
  component: () => null,
})

// Cross-space "Shared" audit view (docs/visibility-model.md). Lazy so its
// share-audit query + row chrome stay off the main chunk until visited.
const sharedRoute = createRoute({
  getParentRoute: () => appLayoutRoute,
  path: '/shared',
  component: lazyRouteComponent(
    () => import('../components/app/SharedView'),
    'SharedRoute',
  ),
})

// M10.2 — dedicated full-page body-fuzzy search. Hosts the same Orama indexes
// the palette tier-3 uses, but with filters (space, updated-since) and a
// top-50 result list. Lazy-loaded so the bundle (and its bodyExcerpt /
// SearchResult deps) stays off the main chunk until the user navigates here.
const searchRoute = createRoute({
  getParentRoute: () => appLayoutRoute,
  path: '/search',
  validateSearch: (
    search: Record<string, unknown>,
  ): { q?: string; spaces?: number[]; since?: string } => {
    const out: { q?: string; spaces?: number[]; since?: string } = {}
    if (typeof search.q === 'string') out.q = search.q
    const rawSpaces = search.spaces
    let ids: number[] | null = null
    if (Array.isArray(rawSpaces)) {
      ids = rawSpaces
        .map((s) => (typeof s === 'number' ? s : Number(s)))
        .filter((n): n is number => Number.isFinite(n))
    } else if (typeof rawSpaces === 'number' && Number.isFinite(rawSpaces)) {
      ids = [rawSpaces]
    } else if (typeof rawSpaces === 'string' && rawSpaces.length > 0) {
      const n = Number(rawSpaces)
      if (Number.isFinite(n)) ids = [n]
    }
    if (ids && ids.length > 0) out.spaces = ids
    if (typeof search.since === 'string' && search.since.length > 0) {
      out.since = search.since
    }
    return out
  },
  component: lazyRouteComponent(
    () => import('../components/app/SearchView'),
    'SearchRoute',
  ),
})

// Graph view — full-screen force-directed map of pages + their connections
// (wikilinks and the page hierarchy). `?focus=` scopes to a page's
// neighborhood; `?space=` scopes to one space. Lazy so d3-force + the canvas
// renderer stay off the main chunk.
const graphRoute = createRoute({
  getParentRoute: () => appLayoutRoute,
  path: '/graph',
  validateSearch: (
    search: Record<string, unknown>,
  ): { focus?: number; space?: number } => {
    const out: { focus?: number; space?: number } = {}
    const focus = Number(search.focus)
    if (Number.isFinite(focus) && focus > 0) out.focus = focus
    const space = Number(search.space)
    if (Number.isFinite(space) && space > 0) out.space = space
    return out
  },
  component: lazyRouteComponent(
    () => import('../components/app/GraphView'),
    'GraphRoute',
  ),
})

const spaceRoute = createRoute({
  getParentRoute: () => appLayoutRoute,
  path: '/spaces/$spaceId',
  parseParams: (raw) => ({ spaceId: Number(raw.spaceId) }),
  stringifyParams: (params) => ({ spaceId: String(params.spaceId) }),
  component: function SpaceLayout() {
    return <Outlet />
  },
})

const spaceIndexRoute = createRoute({
  getParentRoute: () => spaceRoute,
  path: '/',
  component: function SpaceLanding() {
    const { spaceId } = useParams({ from: '/_app/spaces/$spaceId/' })
    const navigate = useNavigate()
    const tree = usePages({ spaceId, tree: true })
    const createPage = useCreatePage()
    const spaces = queryClient.getQueryData<Space[]>(spaceKeys.list()) ?? []
    const spaceName = spaces.find((s) => s.id === spaceId)?.name ?? ''
    const nodes = (tree.data as PageTreeNode[] | undefined) ?? []

    async function handleCreate() {
      try {
        const created = await createPage.mutateAsync({
          space_id: spaceId,
          parent_id: null,
          title: 'Untitled',
        })
        void navigate({
          to: '/spaces/$spaceId/pages/$pageId/{-$slug}',
          params: { spaceId, pageId: created.id, slug: undefined },
        })
      } catch {
        // Failure surfaces via tree refetch / sidebar error state.
      }
    }

    if (tree.data && nodes.length === 0) {
      return (
        <div className="flex-1 flex items-center justify-center p-[var(--space-7)]">
          <Card className="w-full max-w-[28rem] text-center">
            <CardHeader className="items-center">
              <CardTitle className="text-[length:var(--text-2xl)]">
                {spaceName ? `No pages in ${spaceName}` : 'No pages yet'}
              </CardTitle>
              <CardDescription>
                Start with an Untitled page, then rename it once you know what
                it'll be about.
              </CardDescription>
            </CardHeader>
            <CardFooter className="justify-center">
              <Button
                variant="primary"
                size="lg"
                onClick={() => void handleCreate()}
                disabled={createPage.isPending}
              >
                <FilePlus width={16} height={16} /> Create your first page
              </Button>
            </CardFooter>
          </Card>
        </div>
      )
    }

    return (
      <div className="flex-1 flex items-center justify-center p-[var(--space-7)]">
        <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)]">
          Select a page from the sidebar or create one.
        </p>
      </div>
    )
  },
})

// M9.3 — soft-draft restore. `?draft=$revId` opens the page in draft mode,
// seeded from that revision's body. Shared by the bare and slugged page routes
// so `?draft=` survives canonicalisation between them.
function validatePageSearch(search: Record<string, unknown>): { draft?: number } {
  const raw = search.draft
  if (raw == null) return {}
  const n = typeof raw === 'number' ? raw : Number(raw)
  return Number.isFinite(n) && n > 0 ? { draft: n } : {}
}

const pageRoute = createRoute({
  getParentRoute: () => spaceRoute,
  // Optional cosmetic slug: one route matches both /pages/{id} and
  // /pages/{id}/{slug}, so canonicalising the slug is a param change, never a
  // route swap (no editor remount). The id stays canonical.
  path: 'pages/$pageId/{-$slug}',
  parseParams: (raw) => ({ pageId: Number(raw.pageId), slug: raw.slug }),
  stringifyParams: (params) => ({
    pageId: String(params.pageId),
    slug: params.slug,
  }),
  validateSearch: validatePageSearch,
  beforeLoad: ({ params }) => {
    // Remember the last-viewed page so a future cold mount can restore it. We
    // write eagerly here (rather than only on a successful detail fetch) so
    // even a quick visit gets recorded; the index route validates by fetching
    // the detail and clears the key on 404.
    writeLastPage({ spaceId: params.spaceId, pageId: params.pageId })
  },
  component: function PageRouteComponent() {
    const { spaceId, pageId } = useParams({
      from: '/_app/spaces/$spaceId/pages/$pageId/{-$slug}',
    })
    return <PageView spaceId={spaceId} pageId={pageId} />
  },
})

// Lazy-loaded so the page-history surface (PageHistoryView + DiffViewer +
// page-revisions queries) ships as its own chunk. First nav to /history
// triggers the fetch; `defaultPreload: 'intent'` below promotes the load to
// hover-time when the user shows intent on the nav link.
const pageHistoryRoute = createRoute({
  getParentRoute: () => spaceRoute,
  path: 'pages/$pageId/history',
  parseParams: (raw) => ({ pageId: Number(raw.pageId) }),
  stringifyParams: (params) => ({ pageId: String(params.pageId) }),
  component: lazyRouteComponent(
    () => import('../components/app/PageHistoryView'),
    'PageHistoryRoute',
  ),
})

// M15.1 — public share routes. Children of `rootRoute` (NOT appLayoutRoute)
// because share-mode is unauthenticated; no ensureMe gate, no sidebar / app
// shell. Both routes lazy-load the share view so the share bundle stays off
// the main chunk for logged-in users.
const shareRootRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/share/$token',
  component: lazyRouteComponent(
    () => import('./share'),
    'ShareRootRoute',
  ),
})

// Cosmetic-slug variant of the share root: /share/{token}/{slug}. The token is
// canonical; the slug is ignored (ShareRootRoute reads it loosely and
// canonicalises). The static `/p/$pageId` descendant route out-ranks this.
const shareSlugRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/share/$token/$slug',
  component: lazyRouteComponent(() => import('./share'), 'ShareRootRoute'),
})

const shareDescendantRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/share/$token/p/$pageId',
  parseParams: (raw) => ({
    token: String(raw.token),
    pageId: Number(raw.pageId),
  }),
  stringifyParams: (params) => ({
    token: String(params.token),
    pageId: String(params.pageId),
  }),
  component: lazyRouteComponent(
    () => import('./share'),
    'ShareDescendantRoute',
  ),
})

// Reading mode (#3). Authenticated, but a child of `rootRoute` (NOT
// appLayoutRoute) so the reader renders full-bleed without the sidebar/header
// shell. Reuses the same ensureMe gate as the app layout — an unauthenticated
// visitor is bounced to /login with a `next` back to the reader URL.
const readRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/read/$spaceId/$pageId',
  parseParams: (raw) => ({
    spaceId: Number(raw.spaceId),
    pageId: Number(raw.pageId),
  }),
  stringifyParams: (params) => ({
    spaceId: String(params.spaceId),
    pageId: String(params.pageId),
  }),
  beforeLoad: async ({ location }) => {
    const user = await ensureMe()
    if (user) return
    const here = (location.href || '/').replace(window.location.origin, '')
    throw redirect({ to: '/login', search: { next: here } })
  },
  component: lazyRouteComponent(() => import('./read'), 'ReadRoute'),
})

// A user's public home page (/u/{handle}). Child of rootRoute (NO ensureMe gate).
const publicUserRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/u/$username',
  component: lazyRouteComponent(() => import('./public'), 'PublicUserRoute'),
})

// Public-space front page (curated index). Child of rootRoute (NO ensureMe gate).
const publicSpaceIndexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/public/spaces/$spaceId',
  parseParams: (raw) => ({ spaceId: Number(raw.spaceId) }),
  stringifyParams: (params) => ({ spaceId: String(params.spaceId) }),
  // ?tag= filters the index to one tag (shareable, back-button friendly).
  validateSearch: (search: Record<string, unknown>): { tag?: string } => ({
    tag:
      typeof search.tag === 'string' && search.tag.trim()
        ? search.tag.trim()
        : undefined,
  }),
  component: lazyRouteComponent(() => import('./public'), 'PublicSpaceIndexRoute'),
})

// Public-space reader. Child of rootRoute (NO ensureMe gate — a public space is
// readable logged-out). One route matches both /pages/{id} and /pages/{id}/{slug}
// via the optional slug, so slug canonicalisation never swaps routes. Lazy so
// the reader bundle stays off the main chunk.
const publicReaderRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/public/spaces/$spaceId/pages/$pageId/{-$slug}',
  parseParams: (raw) => ({
    spaceId: Number(raw.spaceId),
    pageId: Number(raw.pageId),
    slug: raw.slug,
  }),
  stringifyParams: (params) => ({
    spaceId: String(params.spaceId),
    pageId: String(params.pageId),
    slug: params.slug,
  }),
  component: lazyRouteComponent(() => import('./public'), 'PublicReaderRoute'),
})

// #3 PDF print surface. Public child of rootRoute (NO ensureMe gate — the
// signed print token is the authorization), loaded only by gotenberg's headless
// Chromium during PDF export. Lazy so it never lands in the main chunk.
const printRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/print/$token',
  component: lazyRouteComponent(() => import('./print'), 'PrintRoute'),
})

const routeTree = rootRoute.addChildren([
  loginRoute,
  registerRoute,
  verifyEmailRoute,
  forgotPasswordRoute,
  resetPasswordRoute,
  shareRootRoute,
  shareSlugRoute,
  shareDescendantRoute,
  publicUserRoute,
  publicSpaceIndexRoute,
  publicReaderRoute,
  readRoute,
  printRoute,
  appLayoutRoute.addChildren([
    indexRoute,
    quickNotesRoute,
    settingsRoute,
    sharedRoute,
    searchRoute,
    graphRoute,
    spaceRoute.addChildren([spaceIndexRoute, pageRoute, pageHistoryRoute]),
  ]),
])

export const router = createRouter({
  routeTree,
  defaultPreload: 'intent',
  context: { queryClient },
})

declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router
  }
}
