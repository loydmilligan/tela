import { useMemo, useSyncExternalStore } from 'react'
import type { Awareness } from 'y-protocols/awareness'

// Shape of the awareness state a peer publishes via TelaProvider. The `user`
// field is set by PageView once `useMe` resolves (id + username) plus a
// deterministic colour token index. Any other field is opaque and reserved
// for future plugins (y-prosemirror cursors, etc.).
export interface AwarenessUserState {
  id: number
  username: string
  // 0..7 — index into --collab-cursor-{1..8}. Picked deterministically by the
  // emitter so the same human gets the same colour across reloads.
  colorIdx: number
}

export interface AwarenessLocalState {
  user?: AwarenessUserState
  [key: string]: unknown
}

// A single peer as surfaced to UI consumers. clientID is the Y.Doc-instance
// id, NOT the human user id — two tabs of the same human are two clientIDs
// with the same `user.id`. UI must dedupe by `user.id`; see useActivePeers.
export interface AwarenessPeer {
  clientID: number
  user: AwarenessUserState
}

// useAwareness returns the live list of peers with a `user` field. Resubscribes
// the React tree on every awareness 'change' (added / updated / removed).
//
// useSyncExternalStore guarantees that getSnapshot is stable across renders
// when the underlying data hasn't changed — we satisfy that by memoising the
// snapshot inside a closure and recomputing only when 'change' fires.
export function useAwareness(awareness: Awareness | null): AwarenessPeer[] {
  return useSyncExternalStore(
    (notify) => {
      if (!awareness) return () => {}
      awareness.on('change', notify)
      return () => {
        awareness.off('change', notify)
      }
    },
    () => snapshotPeers(awareness),
    // Server snapshot — no awareness on SSR, no peers.
    () => EMPTY_PEERS,
  )
}

// useActivePeers deduplicates by user.id — two tabs of the same human render
// as a single avatar. Ordering is stable (sorted by user.id) so the avatar
// stack doesn't reshuffle when peers come/go.
export function useActivePeers(awareness: Awareness | null): AwarenessPeer[] {
  const raw = useAwareness(awareness)
  return useMemo(() => dedupeByUserId(raw), [raw])
}

// useDiagramEditors returns, per diagram id, the usernames of OTHER clients
// currently editing it (from each peer's `editingDiagramId` awareness field —
// set by the editor while its Excalidraw sheet is open). Drives the in-page
// "editing" badge. Excludes this client (we don't badge a diagram we have open
// ourselves). Stable snapshot via the same content-hash cache as useAwareness.
export function useDiagramEditors(
  awareness: Awareness | null,
): Map<string, string[]> {
  return useSyncExternalStore(
    (notify) => {
      if (!awareness) return () => {}
      awareness.on('change', notify)
      return () => {
        awareness.off('change', notify)
      }
    },
    () => snapshotDiagramEditors(awareness),
    () => EMPTY_EDITORS,
  )
}

const EMPTY_EDITORS = new Map<string, string[]>()
const editorsCache = new WeakMap<
  Awareness,
  { hash: string; value: Map<string, string[]> }
>()

// Exported for unit tests. Pure over awareness.getStates() + awareness.clientID.
export function snapshotDiagramEditors(
  awareness: Awareness | null,
): Map<string, string[]> {
  if (!awareness) return EMPTY_EDITORS
  const selfId = awareness.clientID
  const byDiagram = new Map<string, string[]>()
  for (const [clientID, state] of awareness.getStates().entries()) {
    if (clientID === selfId) continue
    const s = state as AwarenessLocalState | undefined
    const diagramId = s?.editingDiagramId
    if (typeof diagramId !== 'string' || diagramId === '') continue
    const name =
      typeof s?.user?.username === 'string' ? s.user.username : 'Someone'
    const list = byDiagram.get(diagramId)
    if (list) list.push(name)
    else byDiagram.set(diagramId, [name])
  }
  // Content hash for reference stability (useSyncExternalStore contract).
  const hash = [...byDiagram.entries()]
    .map(([id, names]) => `${id}:${[...names].sort().join(',')}`)
    .sort()
    .join('|')
  const cached = editorsCache.get(awareness)
  if (cached && cached.hash === hash) return cached.value
  editorsCache.set(awareness, { hash, value: byDiagram })
  return byDiagram
}

const EMPTY_PEERS: AwarenessPeer[] = []
// Cache snapshots per awareness instance so identical state keeps reference
// equality between calls (useSyncExternalStore requires this). The key is
// the awareness instance plus a content hash; when content changes we mint
// a fresh array.
const snapshotCache = new WeakMap<
  Awareness,
  { hash: string; value: AwarenessPeer[] }
>()

function snapshotPeers(awareness: Awareness | null): AwarenessPeer[] {
  if (!awareness) return EMPTY_PEERS
  const states = awareness.getStates()
  const peers: AwarenessPeer[] = []
  for (const [clientID, state] of states.entries()) {
    const user = (state as AwarenessLocalState | undefined)?.user
    if (!user || typeof user.id !== 'number' || typeof user.username !== 'string') {
      continue
    }
    peers.push({
      clientID,
      user: {
        id: user.id,
        username: user.username,
        colorIdx:
          typeof user.colorIdx === 'number' ? user.colorIdx : user.id % 8,
      },
    })
  }
  peers.sort((a, b) => a.clientID - b.clientID)
  const hash = peers.map(hashPeer).join('|')
  const cached = snapshotCache.get(awareness)
  if (cached && cached.hash === hash) return cached.value
  snapshotCache.set(awareness, { hash, value: peers })
  return peers
}

function hashPeer(p: AwarenessPeer): string {
  return `${p.clientID}:${p.user.id}:${p.user.username}:${p.user.colorIdx}`
}

function dedupeByUserId(peers: AwarenessPeer[]): AwarenessPeer[] {
  const seen = new Set<number>()
  const out: AwarenessPeer[] = []
  // Sort by user.id first so the winning clientID for each human is stable
  // regardless of join order; within a user.id, prefer the lowest clientID.
  const sorted = [...peers].sort((a, b) => {
    if (a.user.id !== b.user.id) return a.user.id - b.user.id
    return a.clientID - b.clientID
  })
  for (const p of sorted) {
    if (seen.has(p.user.id)) continue
    seen.add(p.user.id)
    out.push(p)
  }
  return out
}
