// Track the last-viewed page so the app can re-open it on next mount.
// Storage shape: `tela:last-page` -> `{spaceId}/{pageId}` or absent. We persist
// both ids so we can route without a lookup; the index route still validates by
// fetching the page detail before redirecting, and silently drops the key on
// 404 / other failure.

const KEY = 'tela:last-page'

export interface LastPage {
  spaceId: number
  pageId: number
}

export function readLastPage(): LastPage | null {
  if (typeof window === 'undefined') return null
  try {
    const raw = window.localStorage.getItem(KEY)
    if (!raw) return null
    const [spaceRaw, pageRaw] = raw.split('/')
    const spaceId = Number(spaceRaw)
    const pageId = Number(pageRaw)
    if (!Number.isFinite(spaceId) || !Number.isFinite(pageId)) return null
    return { spaceId, pageId }
  } catch {
    return null
  }
}

export function writeLastPage(p: LastPage): void {
  if (typeof window === 'undefined') return
  try {
    window.localStorage.setItem(KEY, `${p.spaceId}/${p.pageId}`)
  } catch {
    // Private mode / quota — accept that next mount won't restore.
  }
}

export function clearLastPage(): void {
  if (typeof window === 'undefined') return
  try {
    window.localStorage.removeItem(KEY)
  } catch {
    // Ignore.
  }
}
