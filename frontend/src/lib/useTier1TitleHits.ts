import { useEffect, useMemo, useState } from 'react'
import {
  ensureIndexHydrated,
  searchTitles,
  subscribeToIndexUpdate,
  type TitleHit,
} from './orama-index'

// Boots the tier-1 client-side Orama title index on mount and re-runs the
// search whenever the index rebuilds (post-mutation, hydration completion).
// `searchTitles` is sync against a module-level singleton — the indexEpoch
// state lives here purely to force recompute when that singleton is swapped.
export function useTier1TitleHits(trimmedQuery: string): TitleHit[] {
  const [indexEpoch, setIndexEpoch] = useState(0)
  useEffect(() => {
    ensureIndexHydrated()
    return subscribeToIndexUpdate(() => {
      setIndexEpoch((e) => e + 1)
    })
  }, [])
  return useMemo<TitleHit[]>(
    () => (trimmedQuery.length === 0 ? [] : searchTitles(trimmedQuery)),
    // indexEpoch deliberately participates: forces recompute when the
    // in-memory Orama instance is swapped under the singleton.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [trimmedQuery, indexEpoch],
  )
}
