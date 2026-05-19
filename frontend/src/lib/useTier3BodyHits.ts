import { useEffect, useState } from 'react'
import type { BodySearchHit } from './search/body-index'

// Tier-3 result: a body-fuzzy hit with the originating space surfaced. The
// palette needs `space_id` to render the breadcrumb (looked up against the
// spaces cache) and to navigate on select. Mirrors `BodySearchHit` from
// body-index but adds `space_id`, which isn't on the index row itself.
export interface Tier3BodyHit extends BodySearchHit {
  space_id: number
}

const SESSION_LIMIT = 5

// Computes the top-N body-fuzzy hits across every space whose BodyIndex is
// currently loaded in the registry. Designed for the command palette tier-3:
//
//  - Sub-ms in-memory search via Orama, so we sidestep react-query entirely.
//  - Dynamic-imports body-index inside the effect so the orama bundle stays
//    out of the main chunk. The palette host already kicks off load() via
//    `kickoffPaletteVersionCheck` on open, so by the time the user types
//    something the registry is populated for spaces already cached on disk.
//  - When `!open || trimmedQuery.length === 0`, returns []. The host clears
//    pagesItems on close anyway, but emitting [] here keeps the dep array
//    clean and avoids stale results on re-open.
//
// Spaces that aren't loaded yet are silently skipped: they'll start surfacing
// hits as soon as their index resolves, since the palette-open kickoff drives
// loads in parallel.
export function useTier3BodyHits(
  trimmedQuery: string,
  open: boolean,
  limit: number = SESSION_LIMIT,
): Tier3BodyHit[] {
  const [hits, setHits] = useState<Tier3BodyHit[]>([])

  useEffect(() => {
    if (!open || trimmedQuery.length === 0) {
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setHits([])
      return
    }
    let cancelled = false
    void import('./search/body-index')
      .then(async (m) => {
        const ids = m.loadedSpaceIds()
        if (ids.length === 0) {
          if (!cancelled) setHits([])
          return
        }
        const perSpace = await Promise.all(
          ids.map(async (id) => {
            const idx = m.getLoadedBodyIndex(id)
            if (!idx) return [] as Tier3BodyHit[]
            const rows = await idx.search(trimmedQuery, { limit })
            return rows.map<Tier3BodyHit>((h) => ({ ...h, space_id: id }))
          }),
        )
        if (cancelled) return
        const merged = perSpace
          .flat()
          .sort((a, b) => b.score - a.score)
          .slice(0, limit)
        setHits(merged)
      })
      .catch(() => {
        if (!cancelled) setHits([])
      })
    return () => {
      cancelled = true
    }
  }, [trimmedQuery, open, limit])

  return hits
}
