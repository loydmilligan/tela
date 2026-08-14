import { useCallback, useSyncExternalStore } from 'react'
import type { Awareness } from 'y-protocols/awareness'

// M7.3 leader election.
//
// Returns whether this peer is the elected save-leader for the room. The
// leader is the peer with the LOWEST awareness `clientID` currently in the
// awareness map. Recomputed on every awareness 'change' event.
//
// Why elect a leader: Yjs has already converged the doc across all peers,
// so every peer in the room would serialize the same markdown body. If
// every peer PATCHed `/api/pages/{id}` on debounce, we'd get N×writes for
// no benefit (last-writer-wins is correct but wasteful). One designated
// leader saves; everyone else skips.
//
// Fall-back rules:
//   - awareness is null (no collab session) → false. Callers gate the
//     non-collab path differently (legacy single-author path is unconditional).
//   - the local peer ALWAYS counts as a candidate, even when its own entry is
//     missing from the map (empty map included). The map can lose our entry
//     without the session ending — pagehide sends an awareness removal and a
//     BFCache/tab-switch restore doesn't always bounce the ws, so nothing
//     re-seeds it. Falling back to NOT-leader there silently disables every
//     save while the editor keeps accepting edits via Yjs (page 1257 lost a
//     whole editing session to this). Worst case of leaning leader is a
//     duplicate PATCH of an already-converged body — cheap and idempotent;
//     a save-dead editor is data loss.
//   - a REMOTE peer competes only once it has announced a `user` awareness
//     field (PageView seeds it on connect). Live-verified on prod: a provider
//     leaked by an interrupted PageView mount kept its ws + awareness renewals
//     running with a bare `{}` state and a lower clientID — it held the
//     leadership indefinitely and, having no editor, never saved a byte.
//     A peer that never finished initializing can't be trusted to save, so it
//     doesn't get to win. The brief pre-seed window where two healthy peers
//     both claim leadership costs one duplicate PATCH; a ghost-owned room
//     costs the whole session.
//
// Multi-tab note: until the awareness wire-bridge ships in #65, only this
// peer's local state is in the map (the editor seeds it via
// `setLocalState({})` on collab init). So one-tab-per-page trivially sees
// itself as leader, preserving single-tab save behaviour. Multi-tab still
// produces duplicate saves until #65 lands and peers see each other.
//
// Uses useSyncExternalStore — the idiomatic React API for syncing to an
// external observable, which handles the render/subscribe race React's
// useEffect can't (an awareness 'change' that fires between render and
// effect-mount would otherwise be lost).
export function computeIsLeader(awareness: Awareness | null): boolean {
  if (!awareness) return false
  // Seed the min with our own clientID: self is a candidate whether or not
  // our entry is currently in the map (see fall-back rules above). Remote
  // peers compete only when fully initialized (they carry a `user` field).
  let minId = awareness.clientID
  for (const [id, state] of awareness.getStates()) {
    if (id === awareness.clientID) continue
    if (!state || !(state as Record<string, unknown>).user) continue
    if (id < minId) minId = id
  }
  return minId === awareness.clientID
}

export function useLeaderElection(awareness: Awareness | null): boolean {
  const subscribe = useCallback(
    (onStoreChange: () => void) => {
      if (!awareness) return () => {}
      awareness.on('change', onStoreChange)
      return () => {
        awareness.off('change', onStoreChange)
      }
    },
    [awareness],
  )
  const getSnapshot = useCallback(
    () => computeIsLeader(awareness),
    [awareness],
  )
  return useSyncExternalStore(subscribe, getSnapshot)
}
