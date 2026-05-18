import {
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  redirect,
  useParams,
} from '@tanstack/react-router'
import { ThemeSwitcher } from '../components/ThemeSwitcher'
import { queryClient } from '../lib/queryClient'
import { spaceKeys } from '../lib/queries/spaces'
import { api } from '../lib/api'
import type { Space } from '../lib/types'

// Root layout — sidebar slot is filled in by #11, page view slot by #12.
// For now we mount the ThemeSwitcher in a top bar and an Outlet for the rest.
const rootRoute = createRootRoute({
  component: function RootLayout() {
    return (
      <div className="min-h-screen flex flex-col bg-[var(--surface-1)] text-[var(--text-primary)]">
        <header className="flex items-center justify-between px-[var(--space-6)] py-[var(--space-4)] border-b border-[var(--border-subtle)]">
          <h1 className="m-0 text-[length:var(--text-xl)] leading-[var(--leading-tight)] font-[family-name:var(--font-sans)]">
            tela
          </h1>
          <ThemeSwitcher />
        </header>
        <main className="flex-1 flex">
          <Outlet />
        </main>
      </div>
    )
  },
})

async function ensureSpaces(): Promise<Space[]> {
  return queryClient.ensureQueryData({
    queryKey: spaceKeys.list(),
    queryFn: async () => {
      const { spaces } = await api<{ spaces: Space[] }>('/api/spaces')
      return spaces
    },
  })
}

const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/',
  beforeLoad: async () => {
    const spaces = await ensureSpaces()
    if (spaces.length > 0) {
      throw redirect({ to: '/spaces/$spaceId', params: { spaceId: spaces[0].id } })
    }
  },
  component: function IndexEmpty() {
    return (
      <div className="flex-1 flex items-center justify-center p-[var(--space-7)]">
        <div className="flex flex-col items-center gap-[var(--space-2)] text-center">
          <p className="m-0 text-[length:var(--text-lg)] text-[var(--text-primary)]">
            No spaces yet
          </p>
          <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)]">
            Create your first space from the sidebar to get started.
          </p>
        </div>
      </div>
    )
  },
})

const spaceRoute = createRoute({
  getParentRoute: () => rootRoute,
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
    const { spaceId } = useParams({ from: '/spaces/$spaceId/' })
    return (
      <div className="flex-1 flex items-center justify-center p-[var(--space-7)]">
        <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)]">
          Space {spaceId} — select a page from the sidebar or create one.
        </p>
      </div>
    )
  },
})

const pageRoute = createRoute({
  getParentRoute: () => spaceRoute,
  path: 'pages/$pageId',
  parseParams: (raw) => ({ pageId: Number(raw.pageId) }),
  stringifyParams: (params) => ({ pageId: String(params.pageId) }),
  component: function PagePlaceholder() {
    const { spaceId, pageId } = useParams({ from: '/spaces/$spaceId/pages/$pageId' })
    return (
      <div className="flex-1 flex items-center justify-center p-[var(--space-7)]">
        <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)]">
          Space {spaceId} · Page {pageId} — view lands in task #12.
        </p>
      </div>
    )
  },
})

const routeTree = rootRoute.addChildren([
  indexRoute,
  spaceRoute.addChildren([spaceIndexRoute, pageRoute]),
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
