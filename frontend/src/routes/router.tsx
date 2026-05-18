import { useState } from 'react'
import {
  createRootRoute,
  createRoute,
  createRouter,
  isRedirect,
  Outlet,
  redirect,
  useNavigate,
  useParams,
} from '@tanstack/react-router'
import { FilePlus, Plus } from 'lucide-react'
import { NewSpaceDialog } from '../components/app/NewSpaceDialog'
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
import { spaceKeys } from '../lib/queries/spaces'
import { pageKeys } from '../lib/queries/pages'
import { api, ApiError } from '../lib/api'
import { clearLastPage, readLastPage, writeLastPage } from '../lib/lastPage'
import { useCreatePage, usePages } from '../lib/queries/pages'
import type { Page, PageTreeNode, Space } from '../lib/types'

// Two-pane app shell: fixed sidebar on the left, flexible main pane on the right.
// The main pane has its own top bar (with the theme switcher and a slot for
// per-page actions later), and the routed Outlet underneath.
const rootRoute = createRootRoute({
  component: function RootLayout() {
    return (
      <div className="h-screen flex bg-[var(--surface-1)] text-[var(--text-primary)] overflow-hidden">
        <Sidebar />
        <div className="flex-1 flex flex-col min-w-0">
          <header className="flex items-center justify-between px-[var(--space-6)] py-[var(--space-3)] border-b border-[var(--border-subtle)] shrink-0">
            <h1 className="m-0 text-[length:var(--text-lg)] leading-[var(--leading-tight)] font-[family-name:var(--font-sans)]">
              tela
            </h1>
            <ThemeSwitcher />
          </header>
          <main className="flex-1 flex flex-col overflow-y-auto min-h-0">
            <Outlet />
          </main>
        </div>
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
    if (spaces.length === 0) return

    // Prefer the last-viewed page, but only if it still exists and is in a
    // space we know about. Falls back to the first space's landing when the
    // saved id has been deleted or otherwise can't be resolved.
    const last = readLastPage()
    if (last && spaces.some((s) => s.id === last.spaceId)) {
      try {
        const page = await queryClient.ensureQueryData({
          queryKey: pageKeys.detail(last.pageId),
          queryFn: async () => {
            const { page } = await api<{ page: Page }>(
              `/api/pages/${last.pageId}`,
            )
            return page
          },
        })
        if (page.space_id === last.spaceId) {
          throw redirect({
            to: '/spaces/$spaceId/pages/$pageId',
            params: { spaceId: page.space_id, pageId: page.id },
          })
        }
        // Page lives in a different space now — drop the stale pointer.
        clearLastPage()
      } catch (err) {
        // Re-throw the redirect so TanStack Router can act on it; only treat
        // genuine fetch failures as "saved id is dead".
        if (isRedirect(err)) throw err
        if (err instanceof ApiError) clearLastPage()
        // Other errors fall through to the default first-space redirect.
      }
    }

    throw redirect({
      to: '/spaces/$spaceId',
      params: { spaceId: spaces[0].id },
    })
  },
  component: function IndexEmpty() {
    const [open, setOpen] = useState(false)
    return (
      <div className="flex-1 flex items-center justify-center p-[var(--space-7)]">
        <Card className="w-full max-w-[28rem] text-center">
          <CardHeader className="items-center">
            <CardTitle className="text-[length:var(--text-2xl)]">
              Welcome to tela
            </CardTitle>
            <CardDescription>
              Spaces hold trees of pages. Create your first space to start
              writing.
            </CardDescription>
          </CardHeader>
          <CardFooter className="justify-center">
            <Button variant="primary" size="lg" onClick={() => setOpen(true)}>
              <Plus width={16} height={16} /> Create your first space
            </Button>
          </CardFooter>
          <NewSpaceDialog open={open} onOpenChange={setOpen} />
        </Card>
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
          to: '/spaces/$spaceId/pages/$pageId',
          params: { spaceId, pageId: created.id },
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

const pageRoute = createRoute({
  getParentRoute: () => spaceRoute,
  path: 'pages/$pageId',
  parseParams: (raw) => ({ pageId: Number(raw.pageId) }),
  stringifyParams: (params) => ({ pageId: String(params.pageId) }),
  beforeLoad: ({ params }) => {
    // Remember the last-viewed page so a future cold mount can restore it. We
    // write eagerly here (rather than only on a successful detail fetch) so
    // even a quick visit gets recorded; the index route validates by fetching
    // the detail and clears the key on 404.
    writeLastPage({ spaceId: params.spaceId, pageId: params.pageId })
  },
  component: function PageRouteComponent() {
    const { spaceId, pageId } = useParams({ from: '/spaces/$spaceId/pages/$pageId' })
    return <PageView spaceId={spaceId} pageId={pageId} />
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
