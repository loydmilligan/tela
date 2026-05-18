import { useState } from 'react'
import {
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  redirect,
  useNavigate,
  useParams,
} from '@tanstack/react-router'
import { FilePlus, Plus } from 'lucide-react'
import { NewSpaceDialog } from '../components/app/NewSpaceDialog'
import { Sidebar } from '../components/app/Sidebar'
import { ThemeSwitcher } from '../components/ThemeSwitcher'
import { Button } from '../components/ui/button'
import { queryClient } from '../lib/queryClient'
import { spaceKeys } from '../lib/queries/spaces'
import { api } from '../lib/api'
import { useCreatePage, usePages } from '../lib/queries/pages'
import type { PageTreeNode, Space } from '../lib/types'

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
    if (spaces.length > 0) {
      throw redirect({ to: '/spaces/$spaceId', params: { spaceId: spaces[0].id } })
    }
  },
  component: function IndexEmpty() {
    const [open, setOpen] = useState(false)
    return (
      <div className="flex-1 flex items-center justify-center p-[var(--space-7)]">
        <div className="flex flex-col items-center gap-[var(--space-4)] text-center max-w-[28rem]">
          <h2 className="m-0 text-[length:var(--text-2xl)] leading-[var(--leading-tight)] font-[family-name:var(--font-sans)] text-[var(--text-primary)]">
            Welcome to tela
          </h2>
          <p className="m-0 text-[length:var(--text-base)] leading-[var(--leading-relaxed)] text-[var(--text-muted)]">
            Spaces hold trees of pages. Create your first space to start writing.
          </p>
          <Button variant="primary" size="lg" onClick={() => setOpen(true)}>
            <Plus width={16} height={16} /> Create your first space
          </Button>
          <NewSpaceDialog open={open} onOpenChange={setOpen} />
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
    const navigate = useNavigate()
    const tree = usePages({ spaceId, tree: true })
    const createPage = useCreatePage()
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
          <div className="flex flex-col items-center gap-[var(--space-4)] text-center max-w-[28rem]">
            <h2 className="m-0 text-[length:var(--text-2xl)] leading-[var(--leading-tight)] font-[family-name:var(--font-sans)] text-[var(--text-primary)]">
              No pages yet
            </h2>
            <p className="m-0 text-[length:var(--text-base)] leading-[var(--leading-relaxed)] text-[var(--text-muted)]">
              Start with an Untitled page, then rename it once you know what
              it'll be about.
            </p>
            <Button
              variant="primary"
              size="lg"
              onClick={() => void handleCreate()}
              disabled={createPage.isPending}
            >
              <FilePlus width={16} height={16} /> Create your first page
            </Button>
          </div>
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
