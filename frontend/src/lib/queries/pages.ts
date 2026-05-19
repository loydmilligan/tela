import { useEffect } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../api'
import { emitPageMutation, subscribeToPageMutation } from '../pageMutationEvent'
import type {
  Backlink,
  CreatePageInput,
  MovePageInput,
  Page,
  PageListItem,
  PageTreeNode,
  UpdatePageInput,
} from '../types'

// Hash-friendly stable key for parent_id queries:
//   undefined -> 'any' (all pages in space, no parent filter)
//   null      -> 'root'
//   number    -> the number itself
function parentKey(parentId: number | null | undefined): 'any' | 'root' | number {
  if (parentId === undefined) return 'any'
  if (parentId === null) return 'root'
  return parentId
}

export const pageKeys = {
  all: ['pages'] as const,
  // Everything under a space — easy invalidation point after moves/edits.
  space: (spaceId: number) => [...pageKeys.all, 'space', spaceId] as const,
  lists: (spaceId: number) => [...pageKeys.space(spaceId), 'list'] as const,
  list: (spaceId: number, parentId: number | null | undefined) =>
    [...pageKeys.lists(spaceId), 'flat', parentKey(parentId)] as const,
  tree: (spaceId: number) => [...pageKeys.lists(spaceId), 'tree'] as const,
  details: () => [...pageKeys.all, 'detail'] as const,
  detail: (id: number) => [...pageKeys.details(), id] as const,
  // Flat cross-space listing for the wikilink picker. Sub-key under `all`
  // so a future `qc.invalidateQueries({ queryKey: pageKeys.all })` still
  // sweeps it.
  allFlat: () => [...pageKeys.all, 'all-flat'] as const,
  backlinks: (pageId: number) =>
    [...pageKeys.detail(pageId), 'backlinks'] as const,
}

interface UsePagesArgs {
  spaceId: number | null | undefined
  parentId?: number | null
  tree?: boolean
}

export function usePages(args: UsePagesArgs) {
  const { spaceId, parentId, tree } = args
  return useQuery({
    queryKey:
      spaceId != null
        ? tree
          ? pageKeys.tree(spaceId)
          : pageKeys.list(spaceId, parentId)
        : pageKeys.tree(-1),
    queryFn: async () => {
      const params = new URLSearchParams()
      params.set('space_id', String(spaceId))
      if (tree) {
        params.set('tree', '1')
      } else if (parentId === null) {
        params.set('parent_id', 'null')
      } else if (typeof parentId === 'number') {
        params.set('parent_id', String(parentId))
      }
      const { pages } = await api<{ pages: Page[] | PageTreeNode[] }>(
        `/api/pages?${params.toString()}`,
      )
      return pages
    },
    enabled: spaceId != null,
  }) as ReturnType<typeof useQuery> & { data: Page[] | PageTreeNode[] | undefined }
}

// Cross-space flat page list for the M5.2c `[[Page]]` autocomplete picker.
// Subscribes to the page-mutation bus to invalidate after any create / update
// / move / delete, so newly created or renamed pages surface without a manual
// reload. staleTime keeps tier-1-style snappiness while the picker is open
// across short-interval pop-ups.
export function useAllPages() {
  const qc = useQueryClient()
  useEffect(() => {
    return subscribeToPageMutation(() => {
      void qc.invalidateQueries({ queryKey: pageKeys.allFlat() })
    })
  }, [qc])
  return useQuery({
    queryKey: pageKeys.allFlat(),
    queryFn: async () => {
      const { pages } = await api<{ pages: PageListItem[] }>('/api/pages/all')
      return pages
    },
    staleTime: 60_000,
  })
}

// Incoming-link rows for the M5.2e backlinks panel. Subscribes to the
// `tela:page-mutation` bus so a save elsewhere (which may have added or
// removed an outgoing wikilink targeting this page) refreshes the panel
// without manual reload. staleTime mirrors `useAllPages` — both back
// link-graph UI and tolerate the same minute of staleness.
export function useBacklinks(pageId: number | null | undefined) {
  const qc = useQueryClient()
  useEffect(() => {
    if (pageId == null) return
    return subscribeToPageMutation(() => {
      void qc.invalidateQueries({ queryKey: pageKeys.backlinks(pageId) })
    })
  }, [qc, pageId])
  return useQuery({
    queryKey: pageId != null ? pageKeys.backlinks(pageId) : pageKeys.backlinks(-1),
    queryFn: async () => {
      const { backlinks } = await api<{ backlinks: Backlink[] }>(
        `/api/pages/${pageId}/backlinks`,
      )
      return backlinks
    },
    enabled: pageId != null,
    staleTime: 60_000,
  })
}

export function usePage(id: number | null | undefined) {
  return useQuery({
    queryKey: id != null ? pageKeys.detail(id) : pageKeys.detail(-1),
    queryFn: async () => {
      const { page } = await api<{ page: Page }>(`/api/pages/${id}`)
      return page
    },
    enabled: id != null,
  })
}

function makeOptimisticTreeNode(input: CreatePageInput, tempId: number): PageTreeNode {
  return {
    id: tempId,
    space_id: input.space_id,
    parent_id: input.parent_id ?? null,
    title: input.title,
    body: input.body ?? '',
    position: Number.MAX_SAFE_INTEGER,
    created_at: '',
    updated_at: '',
    children: [],
  }
}

function insertIntoTree(
  tree: PageTreeNode[],
  parentId: number | null,
  node: PageTreeNode,
): PageTreeNode[] {
  if (parentId === null) return [...tree, node]
  return tree.map((n) => {
    if (n.id === parentId) {
      return { ...n, children: [...n.children, node] }
    }
    if (n.children.length === 0) return n
    return { ...n, children: insertIntoTree(n.children, parentId, node) }
  })
}

export function useCreatePage() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (input: CreatePageInput) => {
      const { page } = await api<{ page: Page }>('/api/pages', {
        method: 'POST',
        body: JSON.stringify(input),
      })
      return page
    },
    onMutate: async (input) => {
      await qc.cancelQueries({ queryKey: pageKeys.tree(input.space_id) })
      const treeKey = pageKeys.tree(input.space_id)
      const previousTree = qc.getQueryData<PageTreeNode[]>(treeKey)
      const tempId = -Date.now()
      if (previousTree) {
        const optimistic = makeOptimisticTreeNode(input, tempId)
        qc.setQueryData<PageTreeNode[]>(
          treeKey,
          insertIntoTree(previousTree, input.parent_id ?? null, optimistic),
        )
      }
      return { previousTree, tempId, spaceId: input.space_id }
    },
    onError: (_err, _input, ctx) => {
      if (ctx?.previousTree) {
        qc.setQueryData(pageKeys.tree(ctx.spaceId), ctx.previousTree)
      }
    },
    onSuccess: (created) => {
      qc.setQueryData(pageKeys.detail(created.id), created)
      emitPageMutation()
    },
    onSettled: (_data, _err, input) => {
      void qc.invalidateQueries({ queryKey: pageKeys.space(input.space_id) })
    },
  })
}

// M10.1 — fire-and-forget body-index updates from PATCH/DELETE onSuccess.
// Dynamic-imports the body-index module so it stays out of the main chunk
// for users who never PATCH a page or open the palette. Vite caches the
// module after first load, so subsequent calls are O(1).
function notifyBodyIndexUpdate(page: Page): void {
  void import('../search/body-index').then((m) => {
    m.bodyIndexUpdateOneShim(page)
  })
}

function notifyBodyIndexRemove(pageId: number): void {
  void import('../search/body-index').then((m) => {
    m.bodyIndexRemoveShim(pageId)
  })
}

export function useUpdatePage() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async ({ id, ...patch }: UpdatePageInput & { id: number }) => {
      const { page } = await api<{ page: Page }>(`/api/pages/${id}`, {
        method: 'PATCH',
        body: JSON.stringify(patch),
      })
      return page
    },
    onSuccess: (updated) => {
      qc.setQueryData(pageKeys.detail(updated.id), updated)
      void qc.invalidateQueries({ queryKey: pageKeys.space(updated.space_id) })
      emitPageMutation()
      notifyBodyIndexUpdate(updated)
    },
  })
}

export function useDeletePage() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async ({ id }: { id: number; spaceId: number }) => {
      await api<void>(`/api/pages/${id}`, { method: 'DELETE' })
      return id
    },
    onSuccess: (id, vars) => {
      qc.removeQueries({ queryKey: pageKeys.detail(id) })
      void qc.invalidateQueries({ queryKey: pageKeys.space(vars.spaceId) })
      emitPageMutation()
      notifyBodyIndexRemove(id)
    },
  })
}

interface UseMovePageVars extends MovePageInput {
  id: number
  // Source space, needed so we can invalidate it when the move crosses spaces.
  fromSpaceId: number
}

export function useMovePage() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (vars: UseMovePageVars) => {
      const { id, fromSpaceId: _unused, ...move } = vars
      // Encode the parent_id distinction faithfully:
      //   omitted -> keep current (do NOT serialize the key)
      //   null    -> make root (must appear in JSON as null)
      //   number  -> reparent to that page
      const body: Record<string, unknown> = {}
      if (move.space_id !== undefined) body.space_id = move.space_id
      if ('parent_id' in move) body.parent_id = move.parent_id
      if (move.position !== undefined) body.position = move.position
      const { page } = await api<{ page: Page }>(`/api/pages/${id}/move`, {
        method: 'POST',
        body: JSON.stringify(body),
      })
      return page
    },
    onSuccess: (moved, vars) => {
      qc.setQueryData(pageKeys.detail(moved.id), moved)
      void qc.invalidateQueries({ queryKey: pageKeys.space(moved.space_id) })
      if (vars.fromSpaceId !== moved.space_id) {
        void qc.invalidateQueries({ queryKey: pageKeys.space(vars.fromSpaceId) })
      }
      emitPageMutation()
    },
  })
}
