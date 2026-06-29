// Global "open the feedback widget" bus, mirroring newSpaceEvent. The widget is
// mounted once (in the app header); every entry point — the header icon's own
// trigger, the user-menu item, and the ⌘K "Send feedback" command — opens that
// single instance by emitting here rather than owning a copy.
const OPEN_FEEDBACK_EVENT = 'tela:open-feedback'

export function emitOpenFeedback(): void {
  if (typeof window === 'undefined') return
  window.dispatchEvent(new CustomEvent(OPEN_FEEDBACK_EVENT))
}

export function subscribeToOpenFeedback(cb: () => void): () => void {
  if (typeof window === 'undefined') return () => {}
  function handler() {
    cb()
  }
  window.addEventListener(OPEN_FEEDBACK_EVENT, handler)
  return () => window.removeEventListener(OPEN_FEEDBACK_EVENT, handler)
}
