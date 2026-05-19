import type { DecorationAttrs } from 'prosemirror-view'

// y-prosemirror's yCursorPlugin renders one widget per remote peer (the caret)
// + one inline decoration per peer with a non-empty selection (the highlight).
// Both consult `user` from awareness state. We override the defaults so that:
//   1. All colour comes from the --collab-cursor-{1..8} tokens (no hardcoded
//      hex), routed through a `data-collab-color` attribute — matches the
//      Avatar primitive's `collab-N` tone enum so the caret and the avatar pill
//      for the same peer are guaranteed identical hues across all 3 themes.
//   2. Username sits in a hover-revealed label above the caret rather than
//      always-on, keeping the editor visually quiet during multi-peer editing.
//   3. Selection range overlays as `color-mix(token, alpha)` so the underlying
//      text remains legible in every theme.

// User shape provided by PageView's awareness seed via the `user` field on the
// local awareness state (see lib/collab/use-awareness.ts). y-prosemirror's
// createDecorations mutates `user.color`/`user.name` with defaults if missing
// — we never read those, but the keys are present at runtime.
interface CollabUser {
  username?: string
  colorIdx?: number
}

// 0..7 colorIdx → 1..8 token suffix. Defensive clamp protects against future
// schema drift (a peer publishing a stale or out-of-range index shouldn't
// blank-out the cursor; it falls back to the red hue).
function toTokenIndex(idx: unknown): number {
  if (typeof idx !== 'number' || !Number.isFinite(idx)) return 1
  return (((Math.trunc(idx) % 8) + 8) % 8) + 1
}

function labelFor(user: CollabUser, clientId: number): string {
  const name = (user.username ?? '').trim()
  if (name.length > 0) return name
  return `User ${clientId}`
}

export function cursorBuilder(user: CollabUser, clientId: number): HTMLElement {
  const idx = toTokenIndex(user.colorIdx)
  const caret = document.createElement('span')
  caret.className = 'tela-yjs-cursor'
  caret.setAttribute('data-collab-color', String(idx))
  const label = document.createElement('span')
  label.className = 'tela-yjs-cursor-label'
  label.textContent = labelFor(user, clientId)
  caret.appendChild(label)
  return caret
}

export function selectionBuilder(user: CollabUser): DecorationAttrs {
  const idx = toTokenIndex(user.colorIdx)
  return {
    class: 'tela-yjs-selection',
    'data-collab-color': String(idx),
  }
}
