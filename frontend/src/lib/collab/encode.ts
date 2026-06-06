// Tag bytes mirror backend/internal/api/pages_ws.go. Custom 1-byte tag +
// payload wire protocol. Multi-byte ints are big-endian. Awareness (0x05) is
// reserved for #65 and currently unused on this side.
export const TAG_UPDATE = 0x01
export const TAG_SNAPSHOT_REQ = 0x02
export const TAG_SNAPSHOT_RESP = 0x03
export const TAG_SYNC_INIT = 0x04
export const TAG_AWARENESS = 0x05
// 0x06 reset: server→peer. The page body was rewritten out-of-band (an agent
// MCP write) and the Yjs overlay dropped; this Y.Doc is now stale and must be
// reloaded from pages.body.
export const TAG_RESET = 0x06

// Prefix `payload` with `tag` into a fresh ArrayBuffer suitable for ws.send.
export function encodeFrame(tag: number, payload: Uint8Array): ArrayBuffer {
  const out = new Uint8Array(1 + payload.byteLength)
  out[0] = tag & 0xff
  out.set(payload, 1)
  // Return the underlying ArrayBuffer so WebSocket.send treats it as binary.
  return out.buffer
}

export interface SyncInitFrame {
  snapshot: Uint8Array | null
  updates: Uint8Array[]
}

// Decode a sync-init payload (tag byte already stripped). Layout:
//   u32 snapLen | snap bytes | u32 nUpd | (u32 len + bytes) * nUpd
// snapLen may be 0 (no snapshot yet); nUpd may be 0. Big-endian throughout.
export function decodeSyncInit(payload: Uint8Array): SyncInitFrame {
  const view = new DataView(
    payload.buffer,
    payload.byteOffset,
    payload.byteLength,
  )
  let off = 0
  const snapLen = view.getUint32(off, false)
  off += 4
  if (off + snapLen > payload.byteLength) {
    throw new Error('sync-init: snapshot overruns payload')
  }
  const snapshot =
    snapLen > 0
      ? payload.subarray(off, off + snapLen).slice()
      : null
  off += snapLen
  const nUpd = view.getUint32(off, false)
  off += 4
  const updates: Uint8Array[] = []
  for (let i = 0; i < nUpd; i++) {
    if (off + 4 > payload.byteLength) {
      throw new Error('sync-init: update header overruns payload')
    }
    const len = view.getUint32(off, false)
    off += 4
    if (off + len > payload.byteLength) {
      throw new Error('sync-init: update body overruns payload')
    }
    updates.push(payload.subarray(off, off + len).slice())
    off += len
  }
  return { snapshot, updates }
}
