import * as Y from 'yjs'
import { Awareness } from 'y-protocols/awareness'
import {
  TAG_AWARENESS,
  TAG_SNAPSHOT_REQ,
  TAG_SNAPSHOT_RESP,
  TAG_SYNC_INIT,
  TAG_UPDATE,
  decodeSyncInit,
  encodeFrame,
} from './encode'

// Custom Yjs provider over Tela's `/ws/pages/{id}` wire protocol. The server
// is a dumb relay+persister (backend/internal/api/pages_ws.go) speaking a
// 5-tag binary scheme — NOT y-websocket. This shim wires Y.Doc ↔ that
// protocol and exposes a status stream + an Awareness instance for #65.
//
// Status transitions:
//   'connecting'   → ws not yet open OR open but pre-sync-init.
//   'connected'    → sync-init has been applied; editor can promote to
//                    editable. Status only flips after sync-init so the
//                    editor doesn't open editable on a stale empty doc.
//   'disconnected' → ws closed/erred; reconnect timer scheduled.
//
// Wire handling:
//   tag 0x01 update           — peer↔server raw Yjs update blob.
//   tag 0x02 snapshot-request — reply with tag 0x03 carrying full state.
//   tag 0x04 sync-init        — packs snapshot + tail updates; applied
//                                with origin=this so the doc.on('update')
//                                handler skips the echo.

export type TelaProviderStatus = 'connecting' | 'connected' | 'disconnected'

type StatusListener = (status: TelaProviderStatus) => void
type SyncListener = (info: { hadServerState: boolean }) => void

const RECONNECT_BASE_MS = 1000
const RECONNECT_MAX_MS = 30_000

export class TelaProvider {
  readonly doc: Y.Doc
  readonly awareness: Awareness
  readonly url: string

  private ws: WebSocket | null = null
  private status: TelaProviderStatus = 'connecting'
  private statusListeners = new Set<StatusListener>()
  // First-sync hook. Fired exactly once, on the first sync-init received
  // for this provider instance — used by the editor to gate empty-room
  // seeding from the canonical markdown body.
  private syncListeners = new Set<SyncListener>()
  private firstSyncFired = false
  private reconnectAttempts = 0
  private reconnectTimer: number | null = null
  private destroyed = false

  // Awareness wire-bridge is reserved for #65 (server tag 0x05). We
  // instantiate the Awareness instance so #65 has a hook and presence-aware
  // plugins (y-cursor) can be wired against it — but we DO NOT register the
  // awareness 'update' listener on the ws yet, because the server currently
  // has no broadcast path for awareness frames.
  constructor(url: string, doc: Y.Doc) {
    this.url = url
    this.doc = doc
    this.awareness = new Awareness(doc)
    this.doc.on('update', this.onDocUpdate)
    this.connect()
  }

  destroy(): void {
    this.destroyed = true
    if (this.reconnectTimer != null) {
      window.clearTimeout(this.reconnectTimer)
      this.reconnectTimer = null
    }
    this.doc.off('update', this.onDocUpdate)
    this.awareness.destroy()
    if (this.ws) {
      const ws = this.ws
      this.ws = null
      ws.onopen = null
      ws.onclose = null
      ws.onerror = null
      ws.onmessage = null
      try {
        ws.close()
      } catch {
        // best-effort
      }
    }
  }

  getStatus(): TelaProviderStatus {
    return this.status
  }

  onStatus(fn: StatusListener): () => void {
    this.statusListeners.add(fn)
    return () => {
      this.statusListeners.delete(fn)
    }
  }

  // Subscribe to the one-shot first-sync signal. If the first sync has
  // already fired by the time you subscribe, the listener is invoked
  // synchronously — keeps the seeder useEffect race-free.
  onFirstSync(fn: SyncListener, payload?: { hadServerState: boolean }): () => void {
    if (this.firstSyncFired) {
      fn(payload ?? { hadServerState: false })
      return () => {}
    }
    this.syncListeners.add(fn)
    return () => {
      this.syncListeners.delete(fn)
    }
  }

  private setStatus(next: TelaProviderStatus): void {
    if (this.status === next) return
    this.status = next
    for (const fn of this.statusListeners) fn(next)
  }

  private connect(): void {
    if (this.destroyed) return
    this.setStatus('connecting')
    let ws: WebSocket
    try {
      ws = new WebSocket(this.url)
    } catch {
      this.scheduleReconnect()
      return
    }
    ws.binaryType = 'arraybuffer'
    this.ws = ws

    ws.onopen = () => {
      if (this.destroyed) {
        try {
          ws.close()
        } catch {
          // best-effort
        }
        return
      }
      this.reconnectAttempts = 0
      // Status stays 'connecting' until sync-init lands.
    }
    ws.onmessage = (ev) => this.onMessage(ev)
    ws.onerror = () => {
      // onclose fires after onerror; do reconnect bookkeeping there only.
    }
    ws.onclose = () => {
      if (this.ws === ws) this.ws = null
      this.setStatus('disconnected')
      this.scheduleReconnect()
    }
  }

  private scheduleReconnect(): void {
    if (this.destroyed) return
    if (this.reconnectTimer != null) return
    const delay = Math.min(
      RECONNECT_BASE_MS * 2 ** this.reconnectAttempts,
      RECONNECT_MAX_MS,
    )
    this.reconnectAttempts += 1
    this.reconnectTimer = window.setTimeout(() => {
      this.reconnectTimer = null
      this.connect()
    }, delay)
  }

  // Y.Doc → wire. Skip the echo on inbound 0x01 frames: applyUpdate from the
  // ws-receive path sets origin=this, and we filter that out so we don't
  // bounce the same bytes back to the server. Local typing fires
  // origin=undefined (or some non-this origin) and goes out.
  private onDocUpdate = (update: Uint8Array, origin: unknown): void => {
    if (origin === this) return
    const ws = this.ws
    if (!ws || ws.readyState !== WebSocket.OPEN) return
    try {
      ws.send(encodeFrame(TAG_UPDATE, update))
    } catch {
      // Connection likely dying; onclose will handle reconnect.
    }
  }

  private onMessage(ev: MessageEvent): void {
    if (!(ev.data instanceof ArrayBuffer)) return
    const frame = new Uint8Array(ev.data)
    if (frame.byteLength < 1) return
    const tag = frame[0]
    const payload = frame.subarray(1)
    switch (tag) {
      case TAG_UPDATE:
        Y.applyUpdate(this.doc, payload, this)
        return
      case TAG_SNAPSHOT_REQ: {
        const ws = this.ws
        if (!ws || ws.readyState !== WebSocket.OPEN) return
        const state = Y.encodeStateAsUpdate(this.doc)
        try {
          ws.send(encodeFrame(TAG_SNAPSHOT_RESP, state))
        } catch {
          // Drop — server will re-request on the next threshold.
        }
        return
      }
      case TAG_SYNC_INIT: {
        let unpacked
        try {
          unpacked = decodeSyncInit(payload)
        } catch {
          return
        }
        const hadServerState =
          (unpacked.snapshot != null && unpacked.snapshot.byteLength > 0) ||
          unpacked.updates.length > 0
        if (unpacked.snapshot && unpacked.snapshot.byteLength > 0) {
          Y.applyUpdate(this.doc, unpacked.snapshot, this)
        }
        for (const upd of unpacked.updates) {
          Y.applyUpdate(this.doc, upd, this)
        }
        // First sync-init signals the editor it's safe to promote to
        // editable + potentially seed-from-markdown when the room is fresh.
        if (!this.firstSyncFired) {
          this.firstSyncFired = true
          for (const fn of this.syncListeners) fn({ hadServerState })
          this.syncListeners.clear()
        }
        this.setStatus('connected')
        return
      }
      case TAG_AWARENESS:
        // Reserved for #65. Until server broadcasts tag 0x05, this branch
        // shouldn't fire — but we tolerate it for forward-compatibility so a
        // future server roll-forward doesn't trip the unknown-tag log.
        return
      default:
        // Unknown / future tag — ignore.
        return
    }
  }
}
