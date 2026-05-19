// M10.1 — body-fuzzy Orama index per readable space.
//
// Data layer only — no UI. Palette tier-3 + dedicated /search route are M10.2.
//
// Strategy: per-space Orama instance with full bodies, hydrated from the
// `/api/pages/bodies?space_id=&since=&cursor=&limit=` cursor-paginated endpoint
// (M10.0, commit 9e9bde0). Persisted to IndexedDB via orama's native
// `save()`/`load()` (NOT `@orama/plugin-data-persistence` — that plugin's
// `import dpack` pulls Node `stream` via msgpack and throws in-browser; see
// memory.md "Known Pitfalls"). IDB layout: DB `tela`, store `bodyIndexes`,
// key `space-<id>:v1`, value = `JSON.stringify(oramaSave(orama))`. LS
// watermark per space tracks the max `updated_at` we've ingested so refresh()
// pulls only deltas. LS version per space mirrors the last
// `/api/spaces/{id}/index-version` value we've reconciled against — palette
// open compares and triggers a refresh on drift.
//
// PATCH-onSuccess: useUpdatePage fires `bodyIndexUpdateOneShim(updated)` — if
// the page's space has a loaded index, the row is upserted; otherwise no-op.
// DELETE-onSuccess: `bodyIndexRemoveShim(id)` removes the id from every loaded
// space's index (cheap — Orama treats unknown ids as a no-op).
//
// Yjs scope (Hard Rule #6): zero Yjs imports anywhere in this file.

import type { AnyOrama, RawData, Results } from '@orama/orama'
import {
  create,
  load as oramaLoad,
  remove as oramaRemove,
  save as oramaSave,
  search as oramaSearch,
  upsert as oramaUpsert,
} from '@orama/orama'
import { api } from '../api'
import type { Page, Space } from '../types'

const SCHEMA = {
  space_id: 'number',
  title: 'string',
  body: 'string',
  updated_at: 'string',
} as const

const IDB_DB = 'tela'
const IDB_STORE = 'bodyIndexes'
const IDB_DB_VERSION = 1
const REFRESH_PAGE_LIMIT = 200
const SEARCH_DEFAULT_LIMIT = 50

function idbKey(spaceId: number): string {
  return `space-${spaceId}:v1`
}

function lsSinceKey(spaceId: number): string {
  return `tela:body-index:space-${spaceId}:since`
}

function lsVersionKey(spaceId: number): string {
  return `tela:body-index:space-${spaceId}:version`
}

// ---------- IndexedDB --------------------------------------------------------

function openDB(): Promise<IDBDatabase | null> {
  if (typeof indexedDB === 'undefined') return Promise.resolve(null)
  return new Promise((resolve) => {
    let req: IDBOpenDBRequest
    try {
      req = indexedDB.open(IDB_DB, IDB_DB_VERSION)
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

async function readIdb(spaceId: number): Promise<string | null> {
  const db = await openDB()
  if (!db) return null
  return new Promise((resolve) => {
    try {
      const tx = db.transaction(IDB_STORE, 'readonly')
      const req = tx.objectStore(IDB_STORE).get(idbKey(spaceId))
      req.onsuccess = () => {
        const v = req.result
        resolve(typeof v === 'string' ? v : null)
      }
      req.onerror = () => resolve(null)
    } catch {
      resolve(null)
    }
  })
}

async function writeIdb(spaceId: number, payload: string): Promise<void> {
  const db = await openDB()
  if (!db) return
  await new Promise<void>((resolve) => {
    try {
      const tx = db.transaction(IDB_STORE, 'readwrite')
      tx.objectStore(IDB_STORE).put(payload, idbKey(spaceId))
      tx.oncomplete = () => resolve()
      tx.onerror = () => resolve()
      tx.onabort = () => resolve()
    } catch {
      resolve()
    }
  })
}

// ---------- LocalStorage (watermark + version) ------------------------------

function readSince(spaceId: number): string {
  try {
    return localStorage.getItem(lsSinceKey(spaceId)) ?? ''
  } catch {
    return ''
  }
}

function writeSince(spaceId: number, value: string): void {
  if (!value) return
  try {
    localStorage.setItem(lsSinceKey(spaceId), value)
  } catch {
    // ignored — IDB miss is non-fatal
  }
}

function readVersion(spaceId: number): string {
  try {
    return localStorage.getItem(lsVersionKey(spaceId)) ?? ''
  } catch {
    return ''
  }
}

function writeVersion(spaceId: number, value: string): void {
  try {
    localStorage.setItem(lsVersionKey(spaceId), value)
  } catch {
    // ignored
  }
}

// ---------- Types ------------------------------------------------------------

export interface PageBody {
  id: number
  space_id: number
  title: string
  body: string
  updated_at: string
}

interface BodiesResponse {
  pages: PageBody[]
  next_cursor: number | null
  has_more: boolean
}

export interface BodySearchHit {
  id: number
  title: string
  body: string
  updated_at: string
  score: number
}

// ---------- Registry ---------------------------------------------------------

const loaded = new Map<number, BodyIndex>()
const loadInFlight = new Map<number, Promise<BodyIndex>>()

export function getLoadedBodyIndex(spaceId: number): BodyIndex | undefined {
  return loaded.get(spaceId)
}

export function loadedSpaceIds(): number[] {
  return Array.from(loaded.keys())
}

// ---------- BodyIndex --------------------------------------------------------

export class BodyIndex {
  readonly spaceId: number
  private orama: AnyOrama

  private constructor(spaceId: number, orama: AnyOrama) {
    this.spaceId = spaceId
    this.orama = orama
  }

  static async load(spaceId: number): Promise<BodyIndex> {
    const existing = loaded.get(spaceId)
    if (existing) return existing
    const inFlight = loadInFlight.get(spaceId)
    if (inFlight) return inFlight
    const p = (async () => {
      const orama = create({ schema: SCHEMA })
      const cached = await readIdb(spaceId)
      if (cached) {
        try {
          const raw = JSON.parse(cached) as RawData
          oramaLoad(orama, raw)
        } catch {
          // Corrupt cache — fall back to empty index. refresh() will rebuild.
        }
      }
      const idx = new BodyIndex(spaceId, orama)
      loaded.set(spaceId, idx)
      return idx
    })()
    loadInFlight.set(spaceId, p)
    try {
      return await p
    } finally {
      loadInFlight.delete(spaceId)
    }
  }

  async refresh(): Promise<{ added: number; updated: number }> {
    const since = readSince(this.spaceId)
    let cursor: number | null = null
    let maxUpdatedAt = since
    let added = 0
    let updated = 0
    let hasMore = true
    while (hasMore) {
      const params = new URLSearchParams()
      params.set('space_id', String(this.spaceId))
      if (since) params.set('since', since)
      if (cursor != null) params.set('cursor', String(cursor))
      params.set('limit', String(REFRESH_PAGE_LIMIT))
      const data = await api<BodiesResponse>(
        `/api/pages/bodies?${params.toString()}`,
      )
      for (const p of data.pages) {
        const existed = await this.upsertRow(p)
        if (existed) updated++
        else added++
        if (p.updated_at > maxUpdatedAt) maxUpdatedAt = p.updated_at
      }
      hasMore = data.has_more && data.next_cursor != null
      cursor = data.next_cursor
    }
    if (added > 0 || updated > 0) {
      await this.persistToIdb()
    }
    if (maxUpdatedAt && maxUpdatedAt !== since) {
      writeSince(this.spaceId, maxUpdatedAt)
    }
    return { added, updated }
  }

  async updateOne(page: PageBody): Promise<void> {
    await this.upsertRow(page)
  }

  async remove(id: number): Promise<void> {
    try {
      await oramaRemove(this.orama, String(id))
    } catch {
      // unknown id — orama may throw; treat as no-op
    }
  }

  async search(
    q: string,
    opts?: { limit?: number },
  ): Promise<BodySearchHit[]> {
    const term = q.trim()
    if (term.length === 0) return []
    const limit = opts?.limit ?? SEARCH_DEFAULT_LIMIT
    const res = (await oramaSearch(this.orama, {
      term,
      properties: ['title', 'body'],
      tolerance: term.length <= 8 ? 1 : 2,
      limit,
    })) as Results<PageBody & { id: string }>
    return res.hits.map((h) => ({
      id: Number(h.document.id),
      title: h.document.title,
      body: h.document.body,
      updated_at: h.document.updated_at,
      score: h.score,
    }))
  }

  // ---------- internals ------------------------------------------------------

  private async upsertRow(p: PageBody): Promise<boolean> {
    // Remove-then-upsert keeps Orama state internally consistent if an older
    // row is present. remove() returns `true` when the row existed beforehand,
    // which we surface in refresh() stats.
    let existed: boolean
    try {
      existed = (await oramaRemove(this.orama, String(p.id))) === true
    } catch {
      existed = false
    }
    await oramaUpsert(this.orama, {
      id: String(p.id),
      space_id: p.space_id,
      title: p.title,
      body: p.body,
      updated_at: p.updated_at,
    })
    return existed
  }

  private async persistToIdb(): Promise<void> {
    try {
      const raw = oramaSave(this.orama)
      await writeIdb(this.spaceId, JSON.stringify(raw))
    } catch {
      // persistence is best-effort; current session still works in-memory
    }
  }
}

// ---------- Shims (called from useUpdatePage / useDeletePage onSuccess) ------

export function bodyIndexUpdateOneShim(page: Page): void {
  const idx = loaded.get(page.space_id)
  if (!idx) return
  void idx
    .updateOne({
      id: page.id,
      space_id: page.space_id,
      title: page.title,
      body: page.body,
      updated_at: page.updated_at,
    })
    .catch((e) => {
      console.warn('body-index updateOne failed', e)
    })
}

export function bodyIndexRemoveShim(pageId: number): void {
  for (const idx of loaded.values()) {
    void idx.remove(pageId).catch(() => {
      // ignored — unknown id is expected in most spaces
    })
  }
}

// ---------- Palette-open version check --------------------------------------

let didCheckThisSession = false

// Called from AppCommandHost on first palette-open per session. Fires
// `/api/spaces/{id}/index-version` per space in parallel, compares to the
// stored LS version, and refreshes the index on drift. Returns immediately —
// all real work runs in the background.
export function kickoffPaletteVersionCheck(spaces: Space[]): void {
  if (didCheckThisSession) return
  if (spaces.length === 0) return
  didCheckThisSession = true
  for (const space of spaces) {
    void checkOneSpace(space.id)
  }
}

async function checkOneSpace(spaceId: number): Promise<void> {
  try {
    const { version } = await api<{ version: string }>(
      `/api/spaces/${spaceId}/index-version`,
    )
    const stored = readVersion(spaceId)
    if (stored === version && stored !== '') return
    const idx = await BodyIndex.load(spaceId)
    await idx.refresh()
    writeVersion(spaceId, version)
  } catch {
    // Non-member spaces 403, network errors, etc. — palette must remain usable.
  }
}
