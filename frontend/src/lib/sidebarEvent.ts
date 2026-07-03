// Tiny event bus for "toggle the left sidebar" requests. The collapsed state
// lives in the app-shell layout (routes/router.tsx), but the keyboard layer
// (KeymapHost) sits at the router root as a sibling — a window CustomEvent is
// the smallest bridge, matching the pattern used for new-page + theme events.

const TOGGLE_SIDEBAR_EVENT = 'tela:toggle-sidebar'

export function emitToggleSidebar(): void {
  if (typeof window === 'undefined') return
  window.dispatchEvent(new CustomEvent(TOGGLE_SIDEBAR_EVENT))
}

export function subscribeToToggleSidebar(cb: () => void): () => void {
  if (typeof window === 'undefined') return () => {}
  function handler() {
    cb()
  }
  window.addEventListener(TOGGLE_SIDEBAR_EVENT, handler)
  return () => window.removeEventListener(TOGGLE_SIDEBAR_EVENT, handler)
}
