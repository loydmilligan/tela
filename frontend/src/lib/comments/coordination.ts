// Body ↔ panel scroll/flash coordination for commented passages.
//
// Two-direction wiring:
//   - body-click → panel-open: anchor-decoration plugin invokes the
//     onAnchorClick callback (wired in PageView), which opens the Sheet and
//     calls scrollAndFlashPanelThread.
//   - panel-click → body-scroll: CommentThread fires
//     scrollAndFlashBodyAnchor on row click.
//
// Both helpers are DOM-only — they take a thread id, locate the right
// element by data attribute, scroll it into view, and re-add the is-flash
// class via the force-reflow trick so the animation restarts on repeated
// clicks. No React, no Yjs, no PM.

const FLASH_CLASS = 'is-flash'
const FLASH_MS = 600

// Re-add an animation class via a forced reflow so the animation restarts
// on repeated triggers (without this, the second click finds the class
// already present and the keyframe doesn't re-fire).
function reflashClass(el: HTMLElement, className: string, durationMs: number) {
  el.classList.remove(className)
  // Force reflow — read a layout property to flush the class removal before
  // re-adding so the keyframe sees a transition into "with class".
  void el.offsetWidth
  el.classList.add(className)
  window.setTimeout(() => el.classList.remove(className), durationMs)
}

// Scroll the editor to the commented passage matching `threadId` and flash
// its underline. Returns false if no decoration is currently mounted (e.g.
// thread is orphaned, the decoration plugin hasn't run yet, or the editor
// isn't mounted).
export function scrollAndFlashBodyAnchor(threadId: number): boolean {
  // The data-attribute is also present on panel rows (CommentThread <li>),
  // so we MUST scope the selector — `.tela-comment-anchor[data-…]` is the
  // body decoration. A bare `[data-comment-thread-id="N"]` would match the
  // panel row first when both are in the DOM.
  const el = document.querySelector<HTMLElement>(
    `.tela-comment-anchor[data-comment-thread-id="${cssEscape(threadId)}"]`,
  )
  if (!el) return false
  el.scrollIntoView({ behavior: 'smooth', block: 'center' })
  reflashClass(el, FLASH_CLASS, FLASH_MS)
  return true
}

// Scroll the panel to the thread row matching `threadId` and flash it. Used
// after body-click → setCommentsOpen(true). The Sheet's content is mounted
// as soon as `open` flips, but Radix's animation/transition + portal layout
// means the row's bounding rect may not be settled in the first frame;
// callers typically wrap with a short setTimeout/requestAnimationFrame.
export function scrollAndFlashPanelThread(threadId: number): boolean {
  // Panel rows are <li data-comment-thread-id="N"> (CommentThread.tsx).
  // Scoping by tag distinguishes from body decoration spans.
  const el = document.querySelector<HTMLElement>(
    `li[data-comment-thread-id="${cssEscape(threadId)}"]`,
  )
  if (!el) return false
  el.scrollIntoView({ behavior: 'smooth', block: 'center' })
  reflashClass(el, FLASH_CLASS, FLASH_MS)
  return true
}

// CSS.escape isn't worth a polyfill for the bounded integer-string we'd be
// escaping (thread ids are positive ints from the backend / negative ints
// for optimistic rows). Strip anything that's not a digit or minus sign so
// the selector is safe even if a caller passes an unexpected value.
function cssEscape(threadId: number): string {
  return String(threadId).replace(/[^0-9-]/g, '')
}
