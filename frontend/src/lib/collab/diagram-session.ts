import type { TelaProvider } from './tela-provider'

// Live multiplayer Excalidraw transport for ONE open diagram.
//
// DiagramSession rides TelaProvider's tag-0x07 relay (shared page ws, never
// persisted, off the Y.Doc) and carries, between peers who currently have the
// SAME diagram open (matched by room `key`):
//
//   - scene deltas ('s'): elements changed since our last broadcast (diffed by
//     Excalidraw's per-element `version`) — the firehose of mid-draw edits
//     without re-sending the whole scene every tick.
//   - pointers ('p'): the local cursor, for live remote cursors.
//   - state request ('r'): a late joiner asks present peers for the current
//     scene, since deltas it missed before connecting won't replay.
//   - leave ('l'): a peer closing the diagram, so its cursor disappears at once.
//
// Convergence is Excalidraw's own `reconcileElements` (per-element
// last-writer-wins by version) — applied by the CONSUMER (the edit sheet, which
// already lazy-loads the Excalidraw module), NOT here. This module is
// transport-only: it knows nothing about Excalidraw's runtime, keeping the
// scene's canonical home in pages.body untouched.
//
// Design notes:
//   - Wire format is JSON-over-TextEncoder. Deliberately kept simple: the
//     throttled delta stream is small and the spike confirmed it feels live;
//     a binary format would add complexity for no measured win. Revisit only
//     if a payload-size problem shows up.
//   - Collaborators are keyed by awareness clientID (unique per tab/connection)
//     so two tabs of one human are distinct cursors. Pruned three ways: an
//     explicit 'l' on close, awareness removal on hard tab-close, and an idle
//     timeout backstop for crashes.

// Minimal structural view of an Excalidraw element — all we need to diff and
// relay. The full object is passed through opaquely.
export interface SceneElement {
  id: string
  version: number
  isDeleted?: boolean
  [k: string]: unknown
}

export interface DiagramSelf {
  // Human identity (for the cursor label + colour).
  id: number
  username: string
  colorIdx: number
  // Per-connection awareness clientID — the collaborator map key, so two tabs
  // of the same human render as distinct cursors.
  clientId: number
}

export interface DiagramCollaborator {
  id: number
  username: string
  colorIdx: number
  pointer?: { x: number; y: number }
}

type SceneListener = (elements: SceneElement[]) => void
type CollaboratorListener = (collaborators: Map<string, DiagramCollaborator>) => void

// Trailing-throttle window for outbound scene + pointer frames. ~50ms (≈20fps)
// reads as smooth without flooding (confirmed by the two-browser test).
const SCENE_THROTTLE_MS = 50
const POINTER_THROTTLE_MS = 50
// Backstop: drop a remote collaborator we haven't heard from in this long
// (covers a peer that crashed without sending 'l' or an awareness removal).
const COLLAB_IDLE_MS = 10_000
// Re-send the late-join state request once after this delay, in case the ws
// wasn't open yet at construction (opening a diagram during initial page load).
const REQUEST_RETRY_MS = 500

interface WireScene {
  k: string
  t: 's'
  e: SceneElement[]
  u: DiagramSelf
}
interface WirePointer {
  k: string
  t: 'p'
  x: number
  y: number
  u: DiagramSelf
}
interface WireRequest {
  k: string
  t: 'r'
  u: DiagramSelf
}
interface WireLeave {
  k: string
  t: 'l'
  u: DiagramSelf
}
type WireMsg = WireScene | WirePointer | WireRequest | WireLeave

export class DiagramSession {
  private readonly provider: TelaProvider
  private readonly key: string
  private readonly self: DiagramSelf
  private readonly enc = new TextEncoder()
  private readonly dec = new TextDecoder()

  private sceneListeners = new Set<SceneListener>()
  private collaboratorListeners = new Set<CollaboratorListener>()
  private unsubProvider: () => void
  private onAwarenessChange: (changes: { removed: number[] }) => void

  // Last version we broadcast per element id — the diff baseline.
  private sentVersions = new Map<string, number>()
  // Latest full local element list (cached on every push) — used to answer a
  // late joiner's state request without re-querying the canvas.
  private lastLocalElements: SceneElement[] = []
  private pendingScene: SceneElement[] | null = null
  private sceneTimer: ReturnType<typeof setTimeout> | null = null
  private pendingPointer: { x: number; y: number } | null = null
  private pointerTimer: ReturnType<typeof setTimeout> | null = null
  private requestRetryTimer: ReturnType<typeof setTimeout> | null = null

  // Remote peers, keyed by awareness clientID (string). Tracks last-seen for
  // the idle-prune backstop.
  private collaborators = new Map<string, DiagramCollaborator>()
  private lastSeen = new Map<string, number>()
  private pruneTimer: ReturnType<typeof setInterval> | null = null

  constructor(provider: TelaProvider, key: string, self: DiagramSelf) {
    this.provider = provider
    this.key = key
    this.self = self
    this.unsubProvider = provider.onEphemeral(this.onFrame)
    this.pruneTimer = setInterval(this.pruneIdle, COLLAB_IDLE_MS)

    // Hard tab-close removes the peer's awareness entry; drop its cursor at
    // once instead of waiting for the idle backstop.
    this.onAwarenessChange = ({ removed }) => {
      let changed = false
      for (const clientId of removed) {
        const k = String(clientId)
        this.lastSeen.delete(k)
        if (this.collaborators.delete(k)) changed = true
      }
      if (changed) this.emitCollaborators()
    }
    provider.awareness.on('change', this.onAwarenessChange)

    // Late-join catchup: ask present peers for the current scene (deltas we
    // missed before connecting won't replay). Retry once in case the ws wasn't
    // open yet.
    this.requestState()
    this.requestRetryTimer = setTimeout(() => {
      this.requestRetryTimer = null
      this.requestState()
    }, REQUEST_RETRY_MS)
  }

  // local → wire ----------------------------------------------------------

  // Feed the latest full element list (from Excalidraw onChange). We cache it
  // (to answer late-join requests) and broadcast only changed/new elements on
  // the next throttle tick.
  pushScene(elements: readonly SceneElement[]): void {
    this.lastLocalElements = elements as SceneElement[]
    this.pendingScene = elements as SceneElement[]
    if (this.sceneTimer != null) return
    this.sceneTimer = setTimeout(this.flushScene, SCENE_THROTTLE_MS)
  }

  pushPointer(x: number, y: number): void {
    this.pendingPointer = { x, y }
    if (this.pointerTimer != null) return
    this.pointerTimer = setTimeout(this.flushPointer, POINTER_THROTTLE_MS)
  }

  private flushScene = (): void => {
    this.sceneTimer = null
    const elements = this.pendingScene
    this.pendingScene = null
    if (!elements) return
    const changed: SceneElement[] = []
    for (const el of elements) {
      if (!el || typeof el.id !== 'string') continue
      const prev = this.sentVersions.get(el.id)
      if (prev === undefined || el.version > prev) {
        changed.push(el)
        this.sentVersions.set(el.id, el.version)
      }
    }
    if (changed.length === 0) return
    this.send({ k: this.key, t: 's', e: changed, u: this.self })
  }

  private flushPointer = (): void => {
    this.pointerTimer = null
    const p = this.pendingPointer
    this.pendingPointer = null
    if (!p) return
    this.send({ k: this.key, t: 'p', x: p.x, y: p.y, u: this.self })
  }

  private requestState(): void {
    this.send({ k: this.key, t: 'r', u: this.self })
  }

  // Answer a late joiner's request with the full current scene, bypassing the
  // version diff. Multiple peers answering is harmless — reconcile is LWW so
  // the joiner converges regardless.
  private broadcastFullScene(): void {
    if (this.lastLocalElements.length === 0) return
    for (const el of this.lastLocalElements) {
      if (el && typeof el.id === 'string') this.sentVersions.set(el.id, el.version)
    }
    this.send({ k: this.key, t: 's', e: this.lastLocalElements, u: this.self })
  }

  private send(msg: WireMsg): void {
    this.provider.sendEphemeral(this.enc.encode(JSON.stringify(msg)))
  }

  // wire → local ----------------------------------------------------------

  private onFrame = (payload: Uint8Array): void => {
    let msg: WireMsg
    try {
      msg = JSON.parse(this.dec.decode(payload)) as WireMsg
    } catch {
      return
    }
    if (!msg || msg.k !== this.key || !msg.u) return // not our diagram
    // No self-echo filter needed: the server only fans out to OTHER peers
    // (broadcastToOthers), so we never receive our own frames.
    const peerKey = String(msg.u.clientId)

    if (msg.t === 'l') {
      this.lastSeen.delete(peerKey)
      if (this.collaborators.delete(peerKey)) this.emitCollaborators()
      return
    }

    this.lastSeen.set(peerKey, performance.now())

    if (msg.t === 'r') {
      this.broadcastFullScene()
      return
    }
    if (msg.t === 's') {
      for (const fn of this.sceneListeners) fn(msg.e)
      return
    }
    if (msg.t === 'p') {
      this.collaborators.set(peerKey, {
        id: msg.u.id,
        username: msg.u.username,
        colorIdx: msg.u.colorIdx,
        pointer: { x: msg.x, y: msg.y },
      })
      this.emitCollaborators()
    }
  }

  private pruneIdle = (): void => {
    const now = performance.now()
    let changed = false
    for (const [k, t] of this.lastSeen) {
      if (now - t > COLLAB_IDLE_MS) {
        this.lastSeen.delete(k)
        if (this.collaborators.delete(k)) changed = true
      }
    }
    if (changed) this.emitCollaborators()
  }

  private emitCollaborators(): void {
    const snapshot = new Map(this.collaborators)
    for (const fn of this.collaboratorListeners) fn(snapshot)
  }

  // True when this peer should be the one to persist checkpoints: the lowest
  // clientID among the active set (self + everyone we've recently heard from).
  // Keeps N open peers from all uploading identical PNGs + writing identical
  // atom updates every idle tick. Alone ⇒ true. Checkpoints are idempotent
  // anyway (content-addressed PNG, converged scene), so a brief leadership
  // overlap during joins/leaves is harmless — this just trims the common case.
  isCheckpointLeader(): boolean {
    for (const k of this.lastSeen.keys()) {
      if (Number(k) < this.self.clientId) return false
    }
    return true
  }

  onRemoteScene(fn: SceneListener): () => void {
    this.sceneListeners.add(fn)
    return () => this.sceneListeners.delete(fn)
  }

  onCollaborators(fn: CollaboratorListener): () => void {
    this.collaboratorListeners.add(fn)
    return () => this.collaboratorListeners.delete(fn)
  }

  destroy(): void {
    // Tell peers we're gone so our cursor disappears immediately.
    this.send({ k: this.key, t: 'l', u: this.self })
    this.unsubProvider()
    this.provider.awareness.off('change', this.onAwarenessChange)
    if (this.sceneTimer != null) clearTimeout(this.sceneTimer)
    if (this.pointerTimer != null) clearTimeout(this.pointerTimer)
    if (this.requestRetryTimer != null) clearTimeout(this.requestRetryTimer)
    if (this.pruneTimer != null) clearInterval(this.pruneTimer)
    this.sceneListeners.clear()
    this.collaboratorListeners.clear()
    this.collaborators.clear()
    this.lastSeen.clear()
    this.sentVersions.clear()
    this.lastLocalElements = []
  }
}
