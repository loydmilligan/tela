import { useCallback, useState } from 'react'

// Per-space, localStorage-backed set of expanded page-tree node IDs.
// Storage shape: `tela.expanded.<spaceId>` -> JSON array of numeric ids.
const KEY = (spaceId: number) => `tela.expanded.${spaceId}`

function read(spaceId: number): Set<number> {
  if (typeof window === 'undefined') return new Set()
  try {
    const raw = window.localStorage.getItem(KEY(spaceId))
    if (!raw) return new Set()
    const parsed: unknown = JSON.parse(raw)
    if (!Array.isArray(parsed)) return new Set()
    return new Set(parsed.filter((v): v is number => typeof v === 'number'))
  } catch {
    return new Set()
  }
}

function write(spaceId: number, ids: Set<number>): void {
  if (typeof window === 'undefined') return
  try {
    window.localStorage.setItem(KEY(spaceId), JSON.stringify([...ids]))
  } catch {
    // Ignore quota / privacy-mode failures — collapsing on reload is acceptable.
  }
}

export function useExpandedNodes(spaceId: number | null | undefined) {
  // "Reset state when a prop changes" — React's preferred shape over a useEffect
  // that calls setState. https://react.dev/learn/you-might-not-need-an-effect
  const [prevSpaceId, setPrevSpaceId] = useState<number | null | undefined>(spaceId)
  const [expanded, setExpanded] = useState<Set<number>>(() =>
    spaceId != null ? read(spaceId) : new Set(),
  )
  if (prevSpaceId !== spaceId) {
    setPrevSpaceId(spaceId)
    setExpanded(spaceId != null ? read(spaceId) : new Set())
  }

  const toggle = useCallback(
    (id: number) => {
      if (spaceId == null) return
      setExpanded((prev) => {
        const next = new Set(prev)
        if (next.has(id)) next.delete(id)
        else next.add(id)
        write(spaceId, next)
        return next
      })
    },
    [spaceId],
  )

  const expand = useCallback(
    (id: number) => {
      if (spaceId == null) return
      setExpanded((prev) => {
        if (prev.has(id)) return prev
        const next = new Set(prev)
        next.add(id)
        write(spaceId, next)
        return next
      })
    },
    [spaceId],
  )

  return { expanded, toggle, expand }
}
