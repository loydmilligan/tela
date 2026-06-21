// Global "open the New space dialog" bus, mirroring newPageEvent. The dialog is
// mounted once in AppCommandHost; any surface (command palette, home dashboard,
// empty states) opens it by emitting here rather than owning its own copy.
const OPEN_NEW_SPACE_EVENT = 'tela:open-new-space'

export function emitOpenNewSpace(): void {
  if (typeof window === 'undefined') return
  window.dispatchEvent(new CustomEvent(OPEN_NEW_SPACE_EVENT))
}

export function subscribeToOpenNewSpace(cb: () => void): () => void {
  if (typeof window === 'undefined') return () => {}
  function handler() {
    cb()
  }
  window.addEventListener(OPEN_NEW_SPACE_EVENT, handler)
  return () => window.removeEventListener(OPEN_NEW_SPACE_EVENT, handler)
}
