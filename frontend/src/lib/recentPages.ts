// Recently-viewed pages, used by the command palette's empty-state list.
// Storage shape: `tela:recent-pages` -> JSON array of RecentPage, most-recent
// first. Capped at 8 entries; dedup by pageId on push (entry moves to front).
// `viewedAt` is a client-side Date.now() so the SQLite datetime pitfall
// (YYYY-MM-DD HH:MM:SS treated as local time) doesn't bite here.

const KEY = 'tela:recent-pages'
const CAP = 8

export interface RecentPage {
  pageId: number
  spaceId: number
  title: string
  viewedAt: number
}

function isRecentPage(v: unknown): v is RecentPage {
  if (typeof v !== 'object' || v === null) return false
  const r = v as Partial<RecentPage>
  return (
    typeof r.pageId === 'number' &&
    typeof r.spaceId === 'number' &&
    typeof r.title === 'string' &&
    typeof r.viewedAt === 'number'
  )
}

export function readRecentPages(): RecentPage[] {
  if (typeof window === 'undefined') return []
  try {
    const raw = window.localStorage.getItem(KEY)
    if (!raw) return []
    const parsed: unknown = JSON.parse(raw)
    if (!Array.isArray(parsed)) return []
    return parsed.filter(isRecentPage).slice(0, CAP)
  } catch {
    return []
  }
}

export function pushRecentPage(p: RecentPage): void {
  if (typeof window === 'undefined') return
  try {
    const existing = readRecentPages().filter((e) => e.pageId !== p.pageId)
    const next = [p, ...existing].slice(0, CAP)
    window.localStorage.setItem(KEY, JSON.stringify(next))
  } catch {
    // Private mode / quota — accept that next open won't show this entry.
  }
}
