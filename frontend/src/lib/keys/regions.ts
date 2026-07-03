// DOM-side of the keymap: deciding when bare keys are live (normal vs insert
// "mode"), what surface we're on, and which list/scroller `j`/`k` drives.
//
// Roving is DOM-based on purpose: a list opts in by marking its container
// `data-keynav-region="<kind>"` and each navigable row `data-keynav-item`.
// The engine walks those nodes instead of threading focus state through every
// (often recursive) list component. Rows stay plain <button>/<a> elements.

import { router } from '../../routes/router'

export type RegionKind = 'reader' | 'list' | 'nav'

// Higher wins when several regions are on screen at once (e.g. the sidebar
// 'nav' and a results 'list' both exist on /search → the list drives j/k; a
// reader overlay outranks both).
const PRIORITY: Record<RegionKind, number> = { reader: 3, list: 2, nav: 1 }

// True when the keydown should be treated as text input (insert mode), so bare
// keys/sequences are left alone. Unlike useGlobalShortcut's modifier-combo gate
// (which deliberately fires inside Milkdown), the bare-key layer MUST stay out
// of any editable surface — including contenteditable (the editor) — or typing
// `j` would navigate instead of inserting a letter.
export function isEditableTarget(node: EventTarget | null): boolean {
  if (!(node instanceof Element)) return false
  const tag = node.tagName
  if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return true
  // The Defter spreadsheet grid is a focusable, key-driven surface (arrows move
  // cells, typing edits them) — treat it like an input so bare keys (`t`, `/`,
  // `\`, j/k…) go to the grid, not tela's command layer.
  if (node.closest('.defter-shell')) return true
  // Closest contenteditable host (ProseMirror sets it on the editor root).
  return node.closest('[contenteditable=""],[contenteditable="true"]') != null
}

// An open dialog / menu / listbox (command palette, the cheatsheet itself,
// dropdowns, comboboxes) owns the keyboard — the bare-key layer steps back so
// it doesn't hijack their own j/k/Esc/Enter handling.
function overlayOpen(): boolean {
  return document.querySelector(
    '[role="dialog"],[role="menu"],[role="listbox"]',
  ) != null
}

// Normal mode = a bare key should act as a command. False inside inputs / the
// editor, or while an overlay owns the keyboard.
export function isNormalMode(e: KeyboardEvent): boolean {
  if (isEditableTarget(e.target) || isEditableTarget(document.activeElement)) {
    return false
  }
  return !overlayOpen()
}

// 'app' for anything under the authenticated `_app` layout (including the
// ?view=read reader overlay, which is a search param on a real app route);
// 'public' otherwise (logged-out public/share readers, login). Read live from
// the router so the host can mount once at the root and still gate correctly.
export function currentSurface(): 'app' | 'public' {
  for (const m of router.state.matches) {
    if (String(m.routeId).includes('_app')) return 'app'
  }
  return 'public'
}

function visible(el: Element): boolean {
  return el instanceof HTMLElement && el.offsetParent != null
}

export interface ActiveRegion {
  el: HTMLElement
  kind: RegionKind
}

// The on-screen region that should receive motion keys, by priority.
export function activeRegion(): ActiveRegion | null {
  const els = Array.from(
    document.querySelectorAll<HTMLElement>('[data-keynav-region]'),
  ).filter(visible)
  let best: ActiveRegion | null = null
  for (const el of els) {
    const kind = el.dataset.keynavRegion as RegionKind
    if (!(kind in PRIORITY)) continue
    if (!best || PRIORITY[kind] > PRIORITY[best.kind]) best = { el, kind }
  }
  return best
}

// --- List roving --------------------------------------------------------

function items(region: HTMLElement): HTMLElement[] {
  return Array.from(
    region.querySelectorAll<HTMLElement>('[data-keynav-item]'),
  ).filter(visible)
}

// Current cursor index: the focused row if any, else the route's active row
// (aria-current="page"), else -1.
function cursorIndex(list: HTMLElement[]): number {
  const focused = list.findIndex((el) => el === document.activeElement)
  if (focused !== -1) return focused
  return list.findIndex((el) => el.getAttribute('aria-current') === 'page')
}

function focusItem(list: HTMLElement[], i: number): void {
  const el = list[i]
  if (!el) return
  for (const node of list) delete node.dataset.keynavActive
  el.dataset.keynavActive = 'true'
  el.focus()
  el.scrollIntoView({ block: 'nearest' })
}

export function listMove(region: HTMLElement, delta: 1 | -1): void {
  const list = items(region)
  if (list.length === 0) return
  const cur = cursorIndex(list)
  // From "nothing focused", k starts at the bottom, j at the top.
  const next =
    cur === -1
      ? delta === 1
        ? 0
        : list.length - 1
      : Math.min(list.length - 1, Math.max(0, cur + delta))
  focusItem(list, next)
}

export function listEdge(region: HTMLElement, edge: 'first' | 'last'): void {
  const list = items(region)
  if (list.length === 0) return
  focusItem(list, edge === 'first' ? 0 : list.length - 1)
}

export function listActivate(region: HTMLElement): void {
  const list = items(region)
  const cur = cursorIndex(list)
  list[cur === -1 ? 0 : cur]?.click()
}

// --- Reader scrolling ---------------------------------------------------

const SCROLL_STEP = 64

export function readerScroll(region: HTMLElement, delta: 1 | -1): void {
  region.scrollBy({ top: SCROLL_STEP * delta })
}

export function readerEdge(region: HTMLElement, edge: 'top' | 'bottom'): void {
  region.scrollTo({ top: edge === 'top' ? 0 : region.scrollHeight })
}

// Jump to the previous/next rendered heading (the reader stamps `.reader-heading`
// ids). "Current" = the last heading whose top has crossed the reading band.
export function readerSection(region: HTMLElement, delta: 1 | -1): void {
  const heads = Array.from(
    region.querySelectorAll<HTMLElement>('.reader-heading'),
  )
  if (heads.length === 0) return
  const band = region.getBoundingClientRect().top + 96
  let cur = 0
  for (let i = 0; i < heads.length; i++) {
    if (heads[i].getBoundingClientRect().top <= band + 1) cur = i
    else break
  }
  const next = Math.min(heads.length - 1, Math.max(0, cur + delta))
  heads[next]?.scrollIntoView({ block: 'start' })
}
