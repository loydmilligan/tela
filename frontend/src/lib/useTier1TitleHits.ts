import { useEffect, useMemo, useState } from 'react'
import type { TitleHit } from './orama-index'

// Module-scoped lazy loader for the Orama tier-1 index. The static import was
// pulling ~24 KB gzip (Orama + IndexedDB cache + mutation-listener wiring) into
// the initial bundle even though tier-1 is only consumed by the command
// palette. We now defer the import until the palette is opened for the first
// time, then memoize the in-flight promise so concurrent / subsequent opens
// dedupe to a single fetch.
type OramaModule = typeof import('./orama-index')
let oramaModulePromise: Promise<OramaModule> | null = null

function loadOrama(): Promise<OramaModule> {
  if (oramaModulePromise === null) {
    oramaModulePromise = import('./orama-index').then((m) => {
      m.ensureIndexHydrated()
      return m
    })
  }
  return oramaModulePromise
}

// Boots the tier-1 client-side Orama title index on first palette open and
// re-runs the search whenever the index rebuilds (post-mutation, hydration
// completion). Returns [] until the module has loaded — palette callers expect
// a sync result and tier-2 server search renders alongside, so the empty tier-1
// while loading is the desired graceful-degradation path.
//
// `enabled` should be passed the palette `open` flag (or any other "this is
// the first time the user has asked for search" signal). loadOrama() dedupes
// so flipping enabled false→true repeatedly is safe and only pays the import
// cost once.
export function useTier1TitleHits(
  trimmedQuery: string,
  enabled: boolean,
): TitleHit[] {
  const [indexEpoch, setIndexEpoch] = useState(0)
  const [oramaModule, setOramaModule] = useState<OramaModule | null>(null)

  useEffect(() => {
    if (!enabled) return
    let cancelled = false
    let unsubscribe: (() => void) | undefined
    void loadOrama().then((m) => {
      if (cancelled) return
      setOramaModule(m)
      unsubscribe = m.subscribeToIndexUpdate(() => {
        setIndexEpoch((e) => e + 1)
      })
    })
    return () => {
      cancelled = true
      unsubscribe?.()
    }
  }, [enabled])

  return useMemo<TitleHit[]>(
    () => {
      if (oramaModule === null || trimmedQuery.length === 0) return []
      return oramaModule.searchTitles(trimmedQuery)
    },
    // indexEpoch deliberately participates: forces recompute when the
    // in-memory Orama instance is swapped under the singleton.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [trimmedQuery, indexEpoch, oramaModule],
  )
}
