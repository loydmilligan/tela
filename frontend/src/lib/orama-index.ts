// Tier-1 client-side title index for the command palette (M5.1 #26).
//
// Pure in-memory Orama BM25 search over page titles, hydrated from
// `/api/pages?space_id=…&tree=1` for every space on app boot and persisted to
// IndexedDB so cold loads start warm. Tier-2 server-side search lives in
// AppCommandHost and renders below tier-1 hits.
//
// Lifecycle:
//  1. ensureIndexHydrated() — kicks off the boot sequence: read cached pages
//     synchronously, build Orama from cache, then fire a server refresh.
//  2. refreshIndex() — fetches every space's tree, flattens, recomputes the
//     breadcrumb-by-parent chain, rebuilds Orama, persists, and notifies
//     subscribers. Triggered manually (post-mutation via the page-mutation
//     event) and once on boot.
//  3. searchTitles() — synchronous: returns [] when the index isn't ready yet
//     (cold boot before any data lands) so the palette never blocks on tier-1.
//
// The breadcrumb intentionally excludes the space name to match the tier-2
// search response shape (root → immediate parent, page-title excluded).

import { create, insertMultiple, search, type AnyOrama } from '@orama/orama'
import { api } from './api'
import { subscribeToPageMutation } from './pageMutationEvent'
import type { Space, PageTreeNode } from './types'

interface IndexedPage {
  id: string
  title: string
  spaceId: number
  parentId: number | null
  breadcrumb: string[]
}

interface CachedIndex {
  generation: string
  pages: IndexedPage[]
}

export interface TitleHit {
  pageId: number
  spaceId: number
  title: string
  breadcrumb: string[]
}

const SCHEMA = { title: 'string' } as const
const IDB_DB = 'tela-orama'
const IDB_STORE = 'index'
const IDB_KEY = 'index-v1'
const SEARCH_LIMIT = 20
const FUZZY_TOLERANCE_SHORT = 1
const FUZZY_TOLERANCE_LONG = 2
const FUZZY_LENGTH_BREAK = 8

let memoryIndex: AnyOrama | null = null
let initStarted = false
let listenersAttached = false
let lastGeneration = ''
const updateSubscribers = new Set<() => void>()

// ---------- Subscriptions -----------------------------------------------------

// AppCommandHost subscribes here to know when to re-run searchTitles() — e.g.,
// after a page mutation rebuild lands. Synchronous callback; subscriber is
// expected to do nothing heavier than bump a state nonce.
export function subscribeToIndexUpdate(cb: () => void): () => void {
  updateSubscribers.add(cb)
  return () => {
    updateSubscribers.delete(cb)
  }
}

function notifyUpdate() {
  for (const cb of updateSubscribers) {
    try {
      cb()
    } catch {
      // Subscriber errors should not block other subscribers or the index.
    }
  }
}

// ---------- IndexedDB cache --------------------------------------------------

function openDB(): Promise<IDBDatabase | null> {
  if (typeof indexedDB === 'undefined') return Promise.resolve(null)
  return new Promise((resolve) => {
    let req: IDBOpenDBRequest
    try {
      req = indexedDB.open(IDB_DB, 1)
    } catch {
      resolve(null)
      return
    }
    req.onerror = () => resolve(null)
    req.onsuccess = () => resolve(req.result)
    req.onupgradeneeded = () => {
      const db = req.result
      if (!db.objectStoreNames.contains(IDB_STORE)) {
        db.createObjectStore(IDB_STORE)
      }
    }
  })
}

async function readCache(): Promise<CachedIndex | null> {
  const db = await openDB()
  if (!db) return null
  return new Promise<CachedIndex | null>((resolve) => {
    try {
      const tx = db.transaction(IDB_STORE, 'readonly')
      const req = tx.objectStore(IDB_STORE).get(IDB_KEY)
      req.onsuccess = () => {
        const val = req.result as CachedIndex | undefined
        if (
          val &&
          typeof val.generation === 'string' &&
          Array.isArray(val.pages)
        ) {
          resolve(val)
        } else {
          resolve(null)
        }
      }
      req.onerror = () => resolve(null)
    } catch {
      resolve(null)
    }
  })
}

async function writeCache(cache: CachedIndex): Promise<void> {
  const db = await openDB()
  if (!db) return
  await new Promise<void>((resolve) => {
    try {
      const tx = db.transaction(IDB_STORE, 'readwrite')
      tx.objectStore(IDB_STORE).put(cache, IDB_KEY)
      tx.oncomplete = () => resolve()
      tx.onerror = () => resolve()
      tx.onabort = () => resolve()
    } catch {
      resolve()
    }
  })
}

// ---------- Orama build & search --------------------------------------------

async function buildOrama(pages: IndexedPage[]): Promise<AnyOrama> {
  const db = create({ schema: SCHEMA })
  if (pages.length > 0) {
    // insertMultiple can be sync or async depending on internal batching; await
    // is safe in both cases.
    await insertMultiple(db, pages)
  }
  return db
}

export function searchTitles(query: string): TitleHit[] {
  const trimmed = query.trim()
  if (trimmed.length === 0 || memoryIndex === null) return []
  const tolerance =
    trimmed.length <= FUZZY_LENGTH_BREAK
      ? FUZZY_TOLERANCE_SHORT
      : FUZZY_TOLERANCE_LONG
  // search() returns Results | Promise<Results>. With no async plugins and our
  // tiny in-memory schema, the synchronous branch is taken — cast accordingly.
  // We never await it because the palette caller is synchronous and the result
  // is needed in the same frame.
  const res = search(memoryIndex, {
    term: trimmed,
    properties: ['title'],
    tolerance,
    limit: SEARCH_LIMIT,
  }) as { hits: Array<{ document: IndexedPage }> }
  return res.hits.map((h) => ({
    pageId: Number(h.document.id),
    spaceId: h.document.spaceId,
    title: h.document.title,
    breadcrumb: h.document.breadcrumb,
  }))
}

// ---------- Page-list aggregation -------------------------------------------

function flattenTree(
  nodes: PageTreeNode[],
  parentChain: string[],
  out: IndexedPage[],
) {
  for (const n of nodes) {
    const title = n.title || 'Untitled'
    out.push({
      id: String(n.id),
      title,
      spaceId: n.space_id,
      parentId: n.parent_id,
      breadcrumb: parentChain,
    })
    if (n.children.length > 0) {
      flattenTree(n.children, [...parentChain, title], out)
    }
  }
}

async function fetchAllPages(): Promise<{
  pages: IndexedPage[]
  generation: string
}> {
  const { spaces } = await api<{ spaces: Space[] }>('/api/spaces')
  const all: IndexedPage[] = []
  let generation = ''
  // Sequential fetches keep things simple and order-stable. For small numbers
  // of spaces (single-user POC) this is plenty fast; revisit if it bites.
  for (const space of spaces) {
    const params = new URLSearchParams({
      space_id: String(space.id),
      tree: '1',
    })
    const { pages } = await api<{ pages: PageTreeNode[] }>(
      `/api/pages?${params.toString()}`,
    )
    flattenTree(pages, [], all)
    // updated_at strings are SQLite-native "YYYY-MM-DD HH:MM:SS" which sort
    // correctly under simple string comparison.
    for (const p of pages) walkForGeneration(p, (ts) => {
      if (ts > generation) generation = ts
    })
  }
  return { pages: all, generation }
}

function walkForGeneration(node: PageTreeNode, cb: (ts: string) => void) {
  cb(node.updated_at)
  for (const c of node.children) walkForGeneration(c, cb)
}

// ---------- Hydration & refresh ---------------------------------------------

async function hydrateFromCacheAndRefresh(): Promise<void> {
  // Phase 1: read cache, build instant index if available.
  const cached = await readCache()
  if (cached && cached.pages.length > 0) {
    memoryIndex = await buildOrama(cached.pages)
    lastGeneration = cached.generation
    notifyUpdate()
  }

  // Phase 2: fetch fresh data and replace if newer (or if cache was empty).
  await refreshIndex()
}

export async function refreshIndex(): Promise<void> {
  try {
    const { pages, generation } = await fetchAllPages()
    if (memoryIndex && generation && generation === lastGeneration) {
      // No-op: cached generation already matches the server.
      return
    }
    memoryIndex = await buildOrama(pages)
    lastGeneration = generation
    await writeCache({ generation, pages })
    notifyUpdate()
  } catch {
    // A failed refresh leaves the previous index in place — tier-1 hits keep
    // working off the stale snapshot until the next refresh succeeds.
  }
}

export function ensureIndexHydrated(): void {
  if (initStarted) return
  initStarted = true
  attachMutationListener()
  void hydrateFromCacheAndRefresh()
}

function attachMutationListener() {
  if (listenersAttached) return
  listenersAttached = true
  subscribeToPageMutation(() => {
    void refreshIndex()
  })
}

// Reset module state on HMR so a fresh reload doesn't leak a stale Orama
// instance bound to an older schema. Vite's import.meta.hot is undefined in
// production builds.
if (import.meta.hot) {
  import.meta.hot.dispose(() => {
    memoryIndex = null
    initStarted = false
    listenersAttached = false
    lastGeneration = ''
    updateSubscribers.clear()
  })
}
